package server

import (
	"net/http"
)

// populateSystemRelay fills systemPageData's Relay fields from
// s.relayManager -- see internal/relay's package doc. Lives alongside
// Cloud Sync on the System page since the relay is meaningless without
// it (there is no other way for the cloud to hand back a grant).
func (s *Server) populateSystemRelay(data *systemPageData) {
	settings, err := s.relayManager.Settings().Load()
	if err != nil {
		return
	}
	backend := s.relayManager.Backend()
	data.RelayAvailable = backend.Name() != "unavailable"
	data.RelayEnabled = settings.Enabled
	active, grant := s.relayManager.Status()
	data.RelayActive = active
	if active {
		data.RelayTunnelIP = grant.TunnelIP
		data.RelayExternalHost = grant.ExternalHost
		data.RelayExternalPort = grant.ExternalPort
	}
}

// handleSystemRelaySave flips the relay's enabled flag. Enabling just
// saves the setting -- the actual grant only shows up once the next
// cloudagent hello round trip completes (Reload forces that
// immediately, same as Cloud Sync's own save handler). Disabling tears
// down any live tunnel/policy-routing state right away, a purely local
// action independent of what the cloud side still thinks (see
// relay.Manager.Disable's own doc comment).
func (s *Server) handleSystemRelaySave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	enabled := r.FormValue("relay_enabled") == "true"

	settings, err := s.relayManager.Settings().Load()
	if err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}
	settings.Enabled = enabled
	if err := s.relayManager.Settings().Save(settings); err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}

	if enabled {
		s.cloudAgent.Reload()
		s.renderSystemPage(w, r, flash("ok", "Relay enabled — it will come up once this node's next Cloud Sync connection completes."))
		return
	}

	if err := s.relayManager.Disable(r.Context()); err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}
	s.renderSystemPage(w, r, flash("ok", "Relay disabled."))
}
