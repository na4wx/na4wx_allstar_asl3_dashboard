// SkywarnPlus tab, WX courtesy tone half: the operator's own
// alert-driven courtesy-tone mappings -- a safer, fully-visible
// alternative to SkywarnPlus's own courtesy-tone/ID swap. See
// internal/wxtone's package doc for the two different ways a swap is
// actually applied (sound-to-sound is a pure file-bytes copy; anything
// involving a tone rewrites rpt.conf's ctX= value and reloads app_rpt).
package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"hamvoipconfiggui-asl3/internal/config"
	"hamvoipconfiggui-asl3/internal/skywarnplus"
	"hamvoipconfiggui-asl3/internal/system"
	"hamvoipconfiggui-asl3/internal/wxtone"
)

// wxTonePollInterval checks roughly twice per SkywarnPlus cron cycle
// (that cron runs every 60s -- see install.sh's SkywarnPlus section), so
// a courtesy tone reacts to an alert starting or clearing within about a
// minute, without polling SkywarnPlus's own state file needlessly often.
const wxTonePollInterval = 30 * time.Second

// StartWXTonePoller checks on wxTonePollInterval whether SkywarnPlus
// currently has any active alert and swaps each configured wxtone.Entry's
// courtesy tone accordingly. Runs until ctx is cancelled. Call once, from
// main.
func (s *Server) StartWXTonePoller(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(wxTonePollInterval)
		defer ticker.Stop()
		s.checkWXTones(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkWXTones(ctx)
			}
		}
	}()
}

// checkWXTones reads SkywarnPlus's current active-alert count once (not
// per entry -- it's one global count, not per-node), then brings every
// configured entry's courtesy tone in line with the desired mode. Every
// step is best-effort: a problem with one entry is logged and skipped,
// never fatal to the ticker or to any other entry.
func (s *Server) checkWXTones(ctx context.Context) {
	if !skywarnplus.IsInstalled(s.skywarnDir) {
		return
	}
	entries, err := s.wxTones.List()
	if err != nil || len(entries) == 0 {
		return
	}
	desired, err := s.desiredWXMode(ctx)
	if err != nil {
		log.Printf("wxtone: couldn't read SkywarnPlus status: %v", err)
		return
	}
	for _, e := range entries {
		if e.Mode == desired {
			continue
		}
		if err := s.applyWXTone(ctx, e, desired); err != nil {
			log.Printf("wxtone: node %s %s: %v", e.Node, e.CTKey, err)
			continue
		}
		if err := s.wxTones.SetMode(e.ID, desired); err != nil {
			log.Printf("wxtone: node %s %s: recording new mode: %v", e.Node, e.CTKey, err)
		}
	}
}

// desiredWXMode reads SkywarnPlus's current status and reports which of
// ModeNormal/ModeWX every configured entry should currently be in --
// shared by the poller and by a newly-saved entry, so a mapping applies
// right away rather than waiting for the next actual alert transition.
func (s *Server) desiredWXMode(ctx context.Context) (string, error) {
	status, err := skywarnplus.GetStatus(ctx, s.skywarnDir)
	if err != nil {
		return "", err
	}
	if status.ActiveAlertCount > 0 {
		return wxtone.ModeWX, nil
	}
	return wxtone.ModeNormal, nil
}

// resolveCTDestPath finds the real on-disk file e.CTKey's current
// rpt.conf value points at, confirming it's actually one of this app's
// own custom sound files (via an exact Ref match against
// sounds.ListCustom(), not path-string inspection) -- never the stock
// library (read-only) and never an arbitrary path some other config tool
// might have put there. Checked both at save time (the handler below)
// and apply time (here), since the operator could have changed ctX's
// value on the Sounds & Tones tab after configuring a mapping.
func (s *Server) resolveCTDestPath(view *config.NodeView, ctKey string) (string, error) {
	section := view.Telemetry
	if section == "" {
		section = "telemetry"
	}
	telemetryEntries, err := s.cfg.ListTelemetryEntries(section)
	if err != nil {
		return "", fmt.Errorf("read telemetry section: %w", err)
	}
	var value string
	found := false
	for _, te := range telemetryEntries {
		if te.Key == ctKey {
			value, found = te.Value, true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("%s is no longer present in this node's telemetry section", ctKey)
	}
	if config.IsToneValue(value) {
		return "", fmt.Errorf("%s is a tone-generator value, not a sound file — pick a custom sound file for it on the Sounds & Tones tab first", ctKey)
	}
	customFiles, err := s.sounds.ListCustom()
	if err != nil {
		return "", fmt.Errorf("list custom sounds: %w", err)
	}
	for _, f := range customFiles {
		if f.Ref == value {
			return s.sounds.ResolveCustomPath(f.Name)
		}
	}
	return "", fmt.Errorf("%s isn't set to one of this app's own custom sound files (it may point at the read-only stock library, or a file managed by Raw Config)", ctKey)
}

// resolveSoundSourcePath finds name's real on-disk file, trying the
// custom directory first (the common case) and falling back to the
// stock library, matching how every other sound picker in this app
// offers custom+stock together.
func (s *Server) resolveSoundSourcePath(name string) (string, error) {
	if path, err := s.sounds.ResolveCustomPath(name); err == nil {
		return path, nil
	}
	return s.sounds.ResolveStockPath(name)
}

// resolveSoundRef finds name's rpt.conf-ready reference (sounds.File.Ref
// -- a bare "rpt/..." name for a stock file, an absolute path for a
// custom one), trying custom then stock like resolveSoundSourcePath.
// Used only on the tone-involved apply path below, where a sound
// state's value has to be written into rpt.conf itself rather than
// copied onto an existing destination file's bytes.
func (s *Server) resolveSoundRef(name string) (string, error) {
	files, err := s.sounds.ListAll()
	if err != nil {
		return "", fmt.Errorf("list sounds: %w", err)
	}
	for _, f := range files {
		if f.Name == name {
			return f.Ref, nil
		}
	}
	return "", fmt.Errorf("sound %q not found", name)
}

// applyWXTone brings e.CTKey to whichever of its Normal/WX states
// desired selects. Two entirely different mechanisms (see internal/
// wxtone's package doc for why):
//   - both states are TypeSound: copy the sound file's bytes onto
//     e.CTKey's existing destination file -- never touching rpt.conf
//     itself, so this takes effect immediately with no Asterisk
//     involvement.
//   - either state is TypeTone: rewrite rpt.conf's ctX= value itself,
//     then a live app_rpt reload (system.AsteriskReloadRpt).
func (s *Server) applyWXTone(ctx context.Context, e wxtone.Entry, desired string) error {
	view, err := s.cfg.LoadNode(e.Node)
	if err != nil {
		return fmt.Errorf("load node: %w", err)
	}

	if e.NormalType == wxtone.TypeSound && e.WXType == wxtone.TypeSound {
		destPath, err := s.resolveCTDestPath(view, e.CTKey)
		if err != nil {
			return err
		}
		sourceName := e.NormalSound
		if desired == wxtone.ModeWX {
			sourceName = e.WXSound
		}
		srcPath, err := s.resolveSoundSourcePath(sourceName)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", sourceName, err)
		}
		if srcPath == destPath {
			return nil // already identical on disk, nothing to copy
		}
		return copyFileContents(srcPath, destPath)
	}

	stateType, sound, tone := e.NormalType, e.NormalSound, e.NormalTone
	if desired == wxtone.ModeWX {
		stateType, sound, tone = e.WXType, e.WXSound, e.WXTone
	}
	value := tone
	if stateType == wxtone.TypeSound {
		ref, err := s.resolveSoundRef(sound)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", sound, err)
		}
		value = ref
	}
	section := view.Telemetry
	if section == "" {
		section = "telemetry"
	}
	if err := s.cfg.SetTelemetryEntries(section, map[string]string{e.CTKey: value}); err != nil {
		return fmt.Errorf("set %s: %w", e.CTKey, err)
	}
	if err := system.AsteriskReloadRpt(ctx, s.asteriskBin); err != nil {
		return fmt.Errorf("reload app_rpt: %w", err)
	}
	return nil
}

// copyFileContents overwrites dest's content with src's -- dest keeps
// its own filename/extension, only its bytes change. Replacing it in
// place (same inode) rather than a temp-file-plus-rename matches this
// app's other direct-write paths, since app_rpt could have this exact
// file open at the moment of a swap.
func copyFileContents(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// populateNodeWXTones fills nodeEditData's "WX courtesy tone" rows --
// best-effort like the rest of this page's supplementary data, and only
// meaningful once SkywarnPlus is installed (see populateNodeSkywarn).
func (s *Server) populateNodeWXTones(data *nodeEditData) {
	if data.View == nil || data.View.Node == "" || !data.SkywarnInstalled {
		return
	}
	entries, err := s.wxTones.ListForNode(data.View.Node)
	if err != nil {
		return
	}
	data.WXTones = entries
}

// parseWXToneState reads one of the Normal/WX state's submitted
// fields -- form field names prefixed with prefix -- as either a tone
// (four numbers, built into app_rpt's own "|t(f1,f2,dur,amp)" syntax via
// config.ToneSpec.String()) or a sound file name, per the prefix+"_type"
// radio's value.
func parseWXToneState(r *http.Request, prefix string) (string, string, string, error) {
	switch typ := r.FormValue(prefix + "_type"); typ {
	case wxtone.TypeSound:
		sound := r.FormValue(prefix + "_sound")
		if sound == "" {
			return "", "", "", fmt.Errorf("pick a sound file")
		}
		return wxtone.TypeSound, sound, "", nil
	case wxtone.TypeTone:
		f1, err1 := strconv.Atoi(r.FormValue(prefix + "_freq1"))
		f2, err2 := strconv.Atoi(r.FormValue(prefix + "_freq2"))
		dur, err3 := strconv.Atoi(r.FormValue(prefix + "_duration"))
		amp, err4 := strconv.Atoi(r.FormValue(prefix + "_amplitude"))
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return "", "", "", fmt.Errorf("tone fields must all be numbers")
		}
		spec := config.ToneSpec{Freq1: f1, Freq2: f2, DurationMS: dur, Amplitude: amp}
		return wxtone.TypeTone, "", spec.String(), nil
	default:
		return "", "", "", fmt.Errorf("pick a type: tone or sound file")
	}
}

// findWXToneEntry looks up the entry handleNodeWXToneSave just saved for
// node/ctKey, so it can be applied immediately (see desiredWXMode) --
// Store.Save doesn't hand back a freshly-generated ID, so this re-reads
// the node's entries and matches on CTKey (unique per node in this UI's
// own add flow).
func (s *Server) findWXToneEntry(node, ctKey string) (wxtone.Entry, bool) {
	entries, err := s.wxTones.ListForNode(node)
	if err != nil {
		return wxtone.Entry{}, false
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].CTKey == ctKey {
			return entries[i], true
		}
	}
	return wxtone.Entry{}, false
}

// handleNodeWXToneSave adds one alert-driven courtesy-tone mapping. Each
// of Normal/WX is independently either a tone or a sound file (see
// parseWXToneState); only the pure-sound-file combination is checked
// against resolveCTDestPath, since that's the only combination that
// still relies on ctX already pointing at a fixed destination file (see
// applyWXTone) -- any other combination is free to pick any of this
// node's existing courtesy-tone keys, since its value is going to be
// overwritten either way.
func (s *Server) handleNodeWXToneSave(w http.ResponseWriter, r *http.Request) {
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
	ctKey := r.FormValue("wxtone_ctkey")
	if ctKey == "" {
		s.renderNodeEditErrorReq(w, r, num, "Pick a courtesy tone key")
		return
	}
	normalType, normalSound, normalTone, err := parseWXToneState(r, "wxtone_normal")
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Normal: "+err.Error())
		return
	}
	wxType, wxSound, wxTone, err := parseWXToneState(r, "wxtone_wx")
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "WX: "+err.Error())
		return
	}
	if normalType == wxtone.TypeSound {
		if _, err := s.resolveSoundSourcePath(normalSound); err != nil {
			s.renderNodeEditErrorReq(w, r, num, "Normal sound: "+err.Error())
			return
		}
	}
	if wxType == wxtone.TypeSound {
		if _, err := s.resolveSoundSourcePath(wxSound); err != nil {
			s.renderNodeEditErrorReq(w, r, num, "WX sound: "+err.Error())
			return
		}
	}
	if normalType == wxtone.TypeSound && wxType == wxtone.TypeSound {
		if _, err := s.resolveCTDestPath(view, ctKey); err != nil {
			s.renderNodeEditErrorReq(w, r, num, err.Error())
			return
		}
	}
	entry := wxtone.Entry{
		Node: num, CTKey: ctKey,
		NormalType: normalType, NormalSound: normalSound, NormalTone: normalTone,
		WXType: wxType, WXSound: wxSound, WXTone: wxTone,
	}
	if err := s.wxTones.Save(entry); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	if skywarnplus.IsInstalled(s.skywarnDir) {
		if desired, derr := s.desiredWXMode(r.Context()); derr == nil {
			if saved, ok := s.findWXToneEntry(num, ctKey); ok {
				if err := s.applyWXTone(r.Context(), saved, desired); err != nil {
					log.Printf("wxtone: node %s %s: applying immediately: %v", num, ctKey, err)
				} else if err := s.wxTones.SetMode(saved.ID, desired); err != nil {
					log.Printf("wxtone: node %s %s: recording new mode: %v", num, ctKey, err)
				}
			}
		}
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeWXToneDelete removes one alert-driven courtesy-tone mapping.
// Deliberately does not restore the courtesy tone's file to any
// particular state on removal -- whatever is currently applied simply
// stops being managed, same as this app leaves any other manually-set
// value alone once a feature stops touching it.
func (s *Server) handleNodeWXToneDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	id := r.PathValue("id")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.wxTones.Delete(id); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}
