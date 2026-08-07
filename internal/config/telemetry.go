package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

// ToneSpec is one single-segment app_rpt tone-generator instruction --
// the value format used throughout rpt.conf's [telemetry] section for
// courtesy tones etc., e.g. "|t(660,880,150,2048)" (confirmed against a
// real node's own telemetry-main template). Freq2 is 0 for a
// single-frequency tone, or a second frequency for a dual-tone sound.
// DurationMS and Amplitude are exactly what app_rpt itself calls them in
// its own tone-generator syntax.
type ToneSpec struct {
	Freq1, Freq2, DurationMS, Amplitude int
}

// String renders back to app_rpt's own "|t(f1,f2,dur,amp)" syntax.
func (t ToneSpec) String() string {
	return fmt.Sprintf("|t(%d,%d,%d,%d)", t.Freq1, t.Freq2, t.DurationMS, t.Amplitude)
}

// singleToneRe matches exactly one tone-generator segment and nothing
// else -- anchored at both ends, so a multi-segment value (several
// "(...)" groups back to back, e.g. a 3-part courtesy tone) does not
// match and is left for ParseSingleTone to reject.
var singleToneRe = regexp.MustCompile(`^\|t\((-?\d+),(-?\d+),(\d+),(\d+)\)$`)

// anyToneSegmentRe matches the general tone-generator shape, one or more
// "(...)" segments, used only to tell "a tone this app doesn't offer a
// friendly per-field editor for yet" apart from "not a tone at all,
// probably a sound file reference" -- see IsToneValue.
var anyToneSegmentRe = regexp.MustCompile(`^\|t(\((-?\d+),(-?\d+),(\d+),(\d+)\))+$`)

// ParseSingleTone parses value as exactly one "|t(f1,f2,dur,amp)"
// segment. ok is false for anything else: no segments, more than one
// segment (a real, multi-part courtesy tone like
// "|t(350,0,100,2048)(500,0,100,2048)(660,0,100,2048)"), or not tone
// syntax at all (e.g. a sound file reference like "rpt/callproceeding").
// A multi-segment tone is still a valid, working value -- this just
// means the caller should fall back to editing it as raw text rather
// than silently truncating it to one segment.
func ParseSingleTone(value string) (ToneSpec, bool) {
	m := singleToneRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return ToneSpec{}, false
	}
	f1, _ := strconv.Atoi(m[1])
	f2, _ := strconv.Atoi(m[2])
	dur, _ := strconv.Atoi(m[3])
	amp, _ := strconv.Atoi(m[4])
	return ToneSpec{Freq1: f1, Freq2: f2, DurationMS: dur, Amplitude: amp}, true
}

// IsToneValue reports whether value is any app_rpt tone-generator string
// (one or more "|t(...)" segments) at all, regardless of whether
// ParseSingleTone can break it into friendly fields.
func IsToneValue(value string) bool {
	return anyToneSegmentRe.MatchString(strings.TrimSpace(value))
}

// TelemetryEntry is one key/value pair from a node's [telemetry] section
// -- a courtesy tone (e.g. "ct1"), a named event tone ("cmdmode",
// "remotetx"), or a sound-file playback reference ("patchup",
// "patchdown"). Which of those a given key is isn't fixed by the key
// name -- app_rpt accepts either a tone-generator string or a sound file
// name in any of these fields -- so this only carries the raw,
// template-inheritance-resolved value; callers decide how to render it
// by trying ParseSingleTone/IsToneValue on Value, not by switching on
// Key.
type TelemetryEntry struct {
	Key   string
	Value string
}

// ListTelemetryEntries returns section's resolved (template-inheritance
// flattened) key/value pairs, in first-defined order -- so a per-node
// override of e.g. ct2 changes its value in place rather than moving it
// to the end of the list, matching how an operator expects to see it.
func (s *Store) ListTelemetryEntries(section string) ([]TelemetryEntry, error) {
	rpt, err := s.loadRpt()
	if err != nil {
		return nil, err
	}
	r, err := rpt.Resolve(section)
	if err != nil {
		return nil, fmt.Errorf("config: telemetry section %q not found in rpt.conf: %w", section, err)
	}

	var order []string
	values := map[string]string{}
	for _, p := range r.Pairs {
		if _, seen := values[p.Key]; !seen {
			order = append(order, p.Key)
		}
		values[p.Key] = p.Value
	}
	out := make([]TelemetryEntry, 0, len(order))
	for _, k := range order {
		out = append(out, TelemetryEntry{Key: k, Value: values[k]})
	}
	return out, nil
}

// SetTelemetryEntries writes several key/value pairs into section (e.g.
// "telemetry") in one edit, overriding whatever telemetry-main's own
// template default was for each -- the same per-node/per-section
// override mechanism as UpdateNodeSettings, just targeting the
// telemetry section instead of the node's own stanza. The section must
// already exist (every node's node-main template creates an empty
// "[telemetry](telemetry-main)" override section by default, confirmed
// on a real node).
func (s *Store) SetTelemetryEntries(section string, updates map[string]string) error {
	if err := asteriskconf.SetValues(filepath.Join(s.dir(), "rpt.conf"), section, updates); err != nil {
		return fmt.Errorf("config: update telemetry section %q: %w", section, err)
	}
	return nil
}

// SetCourtesyToneAssignments sets node's own unlinkedct/remotect/
// linkunkeyct fields -- node-main-level settings (like rxchannel/
// duplex), not part of the telemetry section itself. Every node's
// node-main template already carries its own non-blank default for all
// three (confirmed on a real node: unlinkedct=ct2, remotect=ct3,
// linkunkeyct=ct8, set directly on [node-main](!) itself) -- so clearing
// a field to "use app_rpt's own default" can't be done by deleting a
// line from the node's own stanza (there isn't one to delete; the value
// is only ever inherited), it has to explicitly write a blank override
// that beats the inherited non-blank one.
func (s *Store) SetCourtesyToneAssignments(node, unlinkedCT, remoteCT, linkUnkeyCT string) error {
	path := filepath.Join(s.dir(), "rpt.conf")
	updates := map[string]string{
		"unlinkedct":  unlinkedCT,
		"remotect":    remoteCT,
		"linkunkeyct": linkUnkeyCT,
	}
	if err := asteriskconf.SetValues(path, node, updates); err != nil {
		return fmt.Errorf("config: set courtesy tone assignments for node %q: %w", node, err)
	}
	return nil
}
