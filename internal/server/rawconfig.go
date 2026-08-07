// Raw config editor: generic section/key-value access to a handful of
// whole config files (see config.AllowedRawConfigFiles), for edits the
// domain-specific node/system pages don't cover. The single most
// powerful, least guarded capability this app offers -- gated by normal
// login here (the local browser UI), and by its own separate
// AllowRawConfigEdit opt-in for the cloud relay's equivalent actions
// (deferred; see internal/cloudagent's dispatch.go).
package server

import (
	"net/http"
	"strconv"
	"strings"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
	"hamvoipconfiggui-asl3/internal/config"
)

// configPageData backs config.html for both the file-picker index and a
// selected file's editor, so the template can reference .Selected /
// .Sections unconditionally regardless of which handler rendered it.
type configPageData struct {
	pageData
	Files    []string
	Selected string
	Sections []configSection
}

type configSection struct {
	Name string
	Keys []asteriskconf.Pair
}

func (s *Server) handleConfigIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, "config.html", configPageData{
		pageData: pageData{LoggedIn: true},
		Files:    config.AllowedRawConfigFiles,
	})
}

func (s *Server) handleConfigFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !config.IsAllowedRawConfigFile(name) {
		http.NotFound(w, r)
		return
	}
	sections, err := s.cfg.RawSections(name)
	if err != nil {
		s.render(w, "config.html", configPageData{
			pageData: flash("error", err.Error()),
			Files:    config.AllowedRawConfigFiles,
		})
		return
	}

	out := make([]configSection, 0, len(sections))
	for _, sec := range sections {
		out = append(out, configSection{Name: sec.Name, Keys: sec.Pairs})
	}

	s.render(w, "config.html", configPageData{
		pageData: pageData{LoggedIn: true},
		Files:    config.AllowedRawConfigFiles,
		Selected: name,
		Sections: out,
	})
}

// handleConfigSave applies edits posted as repeated form fields named
// "kv:<section>:<n>", where n is the line's position among that
// section's own key/value pairs (i.e. its index in that section's
// Pairs, matching what handleConfigFile rendered). Indexing by position
// rather than by key lets duplicate keys within a section (e.g.
// modules.conf's repeated "load =" lines) be edited independently. Also
// handles "new_key:<section>"/"new_value:<section>" for adding one new
// key per section, and "new_section" for adding a section.
//
// Unlike a from-scratch config format with an in-memory round-trip
// serializer, each of these is its own surgical, re-read-then-rewrite
// edit against the physical file (see asteriskconf.SetNthValueInSection/
// SetValues/CreateSection) -- applied in sequence within this one
// request, not batched into a single write. Slightly more disk I/O for
// a multi-field save, but it's the same "never touch anything but the
// one line/section being changed" discipline this app already applies
// everywhere else in this config format.
func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !config.IsAllowedRawConfigFile(name) {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	sections, err := s.cfg.RawSections(name)
	if err != nil {
		s.render(w, "config.html", configPageData{pageData: flash("error", err.Error()), Files: config.AllowedRawConfigFiles})
		return
	}

	var failed []string
	for _, sec := range sections {
		for i, pair := range sec.Pairs {
			formKey := "kv:" + sec.Name + ":" + strconv.Itoa(i)
			newVal, ok := r.Form[formKey]
			if !ok || len(newVal) == 0 || newVal[0] == pair.Value {
				continue
			}
			if _, err := s.cfg.SetRawKey(name, sec.Name, i, newVal[0]); err != nil {
				failed = append(failed, sec.Name+"."+pair.Key+": "+err.Error())
			}
		}
		if newKey := strings.TrimSpace(r.FormValue("new_key:" + sec.Name)); newKey != "" {
			if err := s.cfg.AddRawKey(name, sec.Name, newKey, r.FormValue("new_value:"+sec.Name)); err != nil {
				failed = append(failed, sec.Name+" (add "+newKey+"): "+err.Error())
			}
		}
	}

	if newSection := strings.TrimSpace(r.FormValue("new_section")); newSection != "" {
		if err := s.cfg.AddRawSection(name, newSection); err != nil {
			failed = append(failed, "add section "+newSection+": "+err.Error())
		}
	}

	if len(failed) > 0 {
		s.renderConfigFileError(w, r, name, "Some edits couldn't be saved: "+strings.Join(failed, "; "))
		return
	}
	http.Redirect(w, r, "/config/"+name, http.StatusSeeOther)
}

func (s *Server) renderConfigFileError(w http.ResponseWriter, r *http.Request, name, msg string) {
	sections, err := s.cfg.RawSections(name)
	if err != nil {
		s.render(w, "config.html", configPageData{pageData: flash("error", msg+" (and: "+err.Error()+")"), Files: config.AllowedRawConfigFiles})
		return
	}
	out := make([]configSection, 0, len(sections))
	for _, sec := range sections {
		out = append(out, configSection{Name: sec.Name, Keys: sec.Pairs})
	}
	s.render(w, "config.html", configPageData{
		pageData: flash("error", msg),
		Files:    config.AllowedRawConfigFiles,
		Selected: name,
		Sections: out,
	})
}
