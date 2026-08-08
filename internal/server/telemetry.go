// Courtesy tones & telemetry editing, on the same tab as the sound
// library (Sounds & Tones) since the two are tightly related: a
// courtesy-tone/telemetry entry's value is either a tone-generator
// string or a sound file reference, and picking the latter needs the
// sound library right there. Ported from the original HamVoIP app's
// internal/server/telemetry.go, with one deliberate behavior change:
// each entry's tone-vs-sound editing mode is now an explicit toggle
// submitted with the form (see node_edit.html's mode_<key> radio pair),
// not silently re-inferred from whatever the value happened to parse as
// on the previous save -- the original's inference-only approach made it
// impossible to switch an entry from a tone to a sound file (or back)
// without hand-editing raw config.
package server

import (
	"net/http"
	"strconv"
	"strings"

	"hamvoipconfiggui-asl3/internal/config"
)

// telemetryRow is one entry in the "Sounds & Tones" editor. Mode is
// decided from the entry's actual current value, not its key name --
// app_rpt accepts either a tone-generator string or a sound file
// reference in any telemetry field, so e.g. "patchup" isn't hardcoded as
// a sound field, it's just that its real value never happens to parse as
// a tone. This decides the editor's *initial* state only -- the operator
// can flip it via the mode_<key> toggle regardless of how the value
// currently parses.
type telemetryRow struct {
	Key   string
	Value string
	// Mode is "tone" (single-segment -- friendly Hz/duration/amplitude
	// fields), "tone-raw" (a real tone this app doesn't offer per-field
	// editing for, because it has more than one segment), or "sound" (not
	// tone syntax at all -- edited as a sound-file reference).
	Mode string
	Tone config.ToneSpec // populated only when Mode == "tone"

	// Label and Description explain what this entry is *for*, since a
	// bare key like "ct1" means nothing to someone who didn't write
	// app_rpt. See telemetryKeyLabel/telemetryKeyDescription.
	Label       string
	Description string
}

// telemetryKeyLabel gives a plain-English name for a telemetry key,
// sourced from AllStarLink's own rpt.conf documentation
// (allstarlink.github.io/config/rpt_conf) rather than guessed. Courtesy
// tones (ct1-ct8) don't get a fixed label here -- what each one is *for*,
// if anything, depends on this node's own unlinkedct/remotect/
// linkunkeyct fields, which telemetryKeyDescription reads directly
// instead of assuming a universal meaning that could be wrong for a node
// that assigns them differently.
func telemetryKeyLabel(key string) string {
	switch key {
	case "cmdmode":
		return "Command-mode beep"
	case "functcomplete":
		return "Command-complete tone"
	case "remcomplete":
		return "Remote command-complete tone"
	case "patchup":
		return "Autopatch connected"
	case "patchdown":
		return "Autopatch ended"
	case "remotetx":
		return "Remote base transmitting"
	case "remotemon":
		return "Remote base monitoring"
	case "pfxtone":
		return "Prefix tone"
	default:
		if isCourtesyToneKey(key) {
			return "Courtesy tone"
		}
		return ""
	}
}

// telemetryKeyDescription explains what a telemetry entry is for. For
// the fixed-meaning keys this is a static, sourced description; for a
// courtesy tone (ct1-ct8) it's built from the node's own UnlinkedCT/
// RemoteCT/LinkUnkeyCT fields, since app_rpt doesn't give ctN a fixed
// meaning by number -- the node's own settings decide which one plays in
// which situation (confirmed both in AllStarLink's rpt.conf docs and in
// a real node's own inline comments: unlinkedct=ct2, remotect=ct3,
// linkunkeyct=ct8 on that node -- a different node could assign the
// exact same three roles to entirely different numbers).
func telemetryKeyDescription(key string, view *config.NodeView) string {
	switch key {
	case "cmdmode":
		return "Plays when you start entering a touch-tone command, confirming the node is listening for it."
	case "functcomplete":
		return "Plays right after a touch-tone command finishes successfully."
	case "patchup":
		return "Plays when an autopatch (phone) call connects."
	case "patchdown":
		return "Plays when an autopatch (phone) call ends."
	case "remotetx":
		return "Only used if this node controls a remote base radio: plays when that remote radio starts transmitting."
	case "remotemon":
		return "Only used if this node controls a remote base radio: plays while monitoring that remote radio."
	}
	if !isCourtesyToneKey(key) {
		return ""
	}
	var roles []string
	if view != nil {
		if key == view.UnlinkedCT && key != "" {
			roles = append(roles, "this node isn't connected to any other node")
		}
		if key == view.RemoteCT && key != "" {
			roles = append(roles, "a remote base radio is connected locally")
		}
		if key == view.LinkUnkeyCT && key != "" {
			roles = append(roles, "a connected node unkeys")
		}
	}
	if len(roles) == 0 {
		return "One of this node's courtesy tones. It isn't currently assigned to unlinked/remote-base/link-unkey above, so check what uses it before changing it."
	}
	return "Plays when " + strings.Join(roles, ", and also when ") + "."
}

// isCourtesyToneKey reports whether key is one of app_rpt's fixed
// courtesy-tone slots (ct1 through ct8, per its own documentation --
// this isn't an arbitrary "ct"-prefixed match, it's exactly that set;
// notably this excludes ct9, which a real node's telemetry-main also
// defines but rpt.conf's own docs don't document as one of the
// operator-assignable eight).
func isCourtesyToneKey(key string) bool {
	switch key {
	case "ct1", "ct2", "ct3", "ct4", "ct5", "ct6", "ct7", "ct8":
		return true
	default:
		return false
	}
}

func buildTelemetryRows(entries []config.TelemetryEntry, view *config.NodeView) []telemetryRow {
	rows := make([]telemetryRow, 0, len(entries))
	for _, e := range entries {
		row := telemetryRow{
			Key:         e.Key,
			Value:       e.Value,
			Label:       telemetryKeyLabel(e.Key),
			Description: telemetryKeyDescription(e.Key, view),
		}
		if spec, ok := config.ParseSingleTone(e.Value); ok {
			row.Mode = "tone"
			row.Tone = spec
		} else if config.IsToneValue(e.Value) {
			row.Mode = "tone-raw"
		} else {
			row.Mode = "sound"
		}
		rows = append(rows, row)
	}
	return rows
}

// courtesyToneKeys returns the courtesy-tone keys (ct1-ct8) actually
// present in entries, in file order -- the valid choices for this node's
// unlinkedct/remotect/linkunkeyct assignment fields. Built from what's
// really there rather than always offering all 8, since a node's
// telemetry section might only define the ones it actually uses.
func courtesyToneKeys(entries []config.TelemetryEntry) []string {
	var keys []string
	for _, e := range entries {
		if isCourtesyToneKey(e.Key) {
			keys = append(keys, e.Key)
		}
	}
	return keys
}

// populateNodeTelemetry fills nodeEditData's courtesy-tone/telemetry
// fields. Best-effort, like the rest of this page's supplementary data --
// a read failure just leaves the section looking empty rather than
// failing the whole page.
func (s *Server) populateNodeTelemetry(data *nodeEditData) {
	view := data.View
	if view == nil || view.Node == "" {
		return
	}
	section := view.Telemetry
	if section == "" {
		section = "telemetry"
	}
	data.TelemetrySect = section
	if entries, err := s.cfg.ListTelemetryEntries(section); err == nil {
		data.TelemetryRows = buildTelemetryRows(entries, view)
		data.CTKeys = courtesyToneKeys(entries)
	}

	if text, ok := config.ParseCWIDText(view.IDRecording); ok {
		data.StationIDMode = "cw"
		data.StationIDText = text
	} else {
		data.StationIDMode = "sound"
	}
	data.StationIDValue = view.IDRecording
	data.StationIDTime = view.IDTime

	morseSection := view.Morse
	if morseSection == "" {
		morseSection = "morse"
	}
	if freq, err := s.cfg.GetMorseIDFrequency(morseSection); err == nil {
		data.StationIDFrequency = freq
	}
}

// handleNodeCourtesyToneUpdate saves which courtesy tone (ct1-ct8) plays
// in each of the three situations app_rpt distinguishes.
func (s *Server) handleNodeCourtesyToneUpdate(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	unlinkedCT := strings.TrimSpace(r.FormValue("unlinkedct"))
	remoteCT := strings.TrimSpace(r.FormValue("remotect"))
	linkUnkeyCT := strings.TrimSpace(r.FormValue("linkunkeyct"))
	if err := s.cfg.SetCourtesyToneAssignments(num, unlinkedCT, remoteCT, linkUnkeyCT); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

func atoiField(v string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return n, err == nil
}

// handleNodeTelemetryUpdate saves every row of the "Sounds & Tones"
// courtesy-tone/telemetry editor in one submission -- there's nothing to
// add or delete here, since telemetry keys are fixed by whatever's
// already in the section. Each row's mode_<key> radio (tone vs sound)
// decides which of that row's fields to read, honoring the operator's
// explicit choice rather than re-inferring it from the old value -- see
// this file's own package doc for why that matters.
func (s *Server) handleNodeTelemetryUpdate(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	section := view.Telemetry
	if section == "" {
		section = "telemetry"
	}
	entries, err := s.cfg.ListTelemetryEntries(section)
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}

	updates := map[string]string{}
	for _, e := range entries {
		key := e.Key
		mode := r.FormValue("mode_" + key)
		var value string
		switch mode {
		case "tone":
			f1, ok1 := atoiField(r.FormValue("tone_" + key + "_freq1"))
			f2, ok2 := atoiField(r.FormValue("tone_" + key + "_freq2"))
			dur, ok3 := atoiField(r.FormValue("tone_" + key + "_duration"))
			amp, ok4 := atoiField(r.FormValue("tone_" + key + "_amplitude"))
			if ok1 && ok2 && ok3 && ok4 {
				value = config.ToneSpec{Freq1: f1, Freq2: f2, DurationMS: dur, Amplitude: amp}.String()
			} else {
				// Advanced/multi-segment tone this app doesn't offer
				// per-field editing for -- fall back to its raw text box.
				value = strings.TrimSpace(r.FormValue("raw_" + key))
			}
		case "sound":
			value = strings.TrimSpace(r.FormValue("raw_" + key))
		default:
			continue // row wasn't submitted (shouldn't happen) -- leave as-is
		}
		if value == "" || value == e.Value {
			continue
		}
		updates[key] = value
	}

	if len(updates) > 0 {
		if err := s.cfg.SetTelemetryEntries(section, updates); err != nil {
			s.renderNodeEditErrorReq(w, r, num, err.Error())
			return
		}
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeStationIDUpdate saves rpt.conf's "idrecording" -- either a
// CW/Morse station ID (app_rpt's own "|i<text>" syntax, see
// config.FormatCWID) or a sound file reference, decided by the id_mode
// toggle submitted with the form (same explicit-toggle pattern as the
// courtesy-tone/telemetry editor above, not inferred from the old
// value) -- plus two related settings on the same card: "idtime" (how
// often this node re-identifies, node-level, applies to either mode)
// and, for CW mode only, the CW tone's own "idfrequency" in the node's
// [morse] section (see config.SetMorseIDFrequency's own doc comment for
// why that's a section-level write, same sharing model as telemetry).
// Both are optional -- left blank, the template's own inherited value
// keeps applying, same convention as the Radio tab's squelch tail
// fields.
func (s *Server) handleNodeStationIDUpdate(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	var value string
	isCW := r.FormValue("id_mode") == "cw"
	switch r.FormValue("id_mode") {
	case "cw":
		text := strings.TrimSpace(r.FormValue("id_text"))
		if text == "" {
			s.renderNodeEditErrorReq(w, r, num, "Enter the callsign/text to send as CW")
			return
		}
		value = config.FormatCWID(text)
	case "sound":
		value = strings.TrimSpace(r.FormValue("id_sound"))
		if value == "" {
			s.renderNodeEditErrorReq(w, r, num, "Choose a sound file for the station ID")
			return
		}
	default:
		s.renderNodeEditErrorReq(w, r, num, "Choose CW ID or Sound file")
		return
	}

	nodeUpdates := map[string]string{"idrecording": value}
	if v := strings.TrimSpace(r.FormValue("id_time")); v != "" {
		if ms, err := strconv.Atoi(v); err != nil || ms < 0 {
			s.renderNodeEditErrorReq(w, r, num, "ID interval must be a non-negative number of milliseconds")
			return
		}
		nodeUpdates["idtime"] = v
	}
	if err := s.cfg.UpdateNodeSettings(num, nodeUpdates); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}

	if isCW {
		if v := strings.TrimSpace(r.FormValue("id_frequency")); v != "" {
			if hz, err := strconv.Atoi(v); err != nil || hz <= 0 {
				s.renderNodeEditErrorReq(w, r, num, "CW ID tone frequency must be a positive number of Hz")
				return
			}
			morseSection := view.Morse
			if morseSection == "" {
				morseSection = "morse"
			}
			if err := s.cfg.SetMorseIDFrequency(morseSection, v); err != nil {
				s.renderNodeEditErrorReq(w, r, num, err.Error())
				return
			}
		}
	}

	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}
