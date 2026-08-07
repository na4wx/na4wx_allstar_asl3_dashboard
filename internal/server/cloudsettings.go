package server

import (
	"net/http"
	"strings"

	"hamvoipconfiggui-asl3/internal/cloudagent"
)

// populateSystemCloud fills systemPageData's Cloud Sync fields from the
// operator's own saved settings (see internal/cloudagent.Settings) --
// except CloudURL, which always shows this server's fixed
// cloudURLDefault (see cloudagent.New's doc comment), never anything
// read from disk: the cloud address is baked in at build/deploy time
// and displayed read-only, not operator-editable.
func (s *Server) populateSystemCloud(data *systemPageData) {
	settings, err := s.cloudAgent.Settings().Load()
	if err != nil {
		return
	}
	data.CloudURL = s.cloudURLDefault
	data.CloudAPIKey = settings.APIKey
	data.CloudEnabled = settings.Enabled
	data.CloudAllowRemoteReboot = settings.AllowRemoteReboot
	data.CloudAllowRawConfigEdit = settings.AllowRawConfigEdit
	if last := s.cloudAgent.LastConnected(); !last.IsZero() {
		data.CloudLastConnected = last.Format("Jan 2, 2006 3:04 PM")
	} else {
		data.CloudLastConnected = "never"
	}
}

// handleSystemCloudSave saves the Cloud Sync card in one submission --
// the API key and enabled flag together. There is no cloud_url form
// field to read: the cloud address is fixed (s.cloudURLDefault, set at
// build/deploy time via -cloud-url) and never something this handler
// accepts from the client -- even a hand-crafted POST with its own
// cloud_url is ignored, not just hidden from the rendered form. Saving
// wakes a currently-waiting Agent.Run loop immediately (see
// cloudagent.Agent.Reload) so turning the feature on, or fixing a bad
// API key, takes effect right away instead of waiting for the next
// backoff/poll tick.
func (s *Server) handleSystemCloudSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	settings := cloudagent.Settings{
		APIKey:             strings.TrimSpace(r.FormValue("cloud_api_key")),
		Enabled:            r.FormValue("cloud_enabled") == "true",
		AllowRemoteReboot:  r.FormValue("cloud_allow_remote_reboot") == "true",
		AllowRawConfigEdit: r.FormValue("cloud_allow_raw_config_edit") == "true",
	}
	if settings.Enabled && (s.cloudURLDefault == "" || settings.APIKey == "") {
		s.renderSystemPage(w, r, flash("error", "This build has no cloud URL configured, or no API key was entered — enter an API key before enabling Cloud Sync"))
		return
	}
	if err := s.cloudAgent.Settings().Save(settings); err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}
	s.cloudAgent.Reload()
	s.renderSystemPage(w, r, flash("ok", "Cloud Sync settings saved."))
}
