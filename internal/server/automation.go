// Scheduler tab, connections half: app_rpt's own native rpt.conf
// scheduler for connect/disconnect actions -- unlike scheduled sound
// playback (soundschedule.go), this needs nothing running here to keep
// working once saved; Asterisk itself dials the scheduled macro when
// the time matches.
package server

import (
	"net/http"
	"strings"

	"hamvoipconfiggui-asl3/internal/automation"
)

// populateNodeAutomation fills nodeEditData's "Scheduled connections"
// rows. Best-effort, like the rest of this page's supplementary data --
// a read failure just leaves the section looking empty rather than
// failing the whole page.
func (s *Server) populateNodeAutomation(data *nodeEditData) {
	if data.View == nil || data.View.Node == "" {
		return
	}
	data.SchedulerSect = data.View.Scheduler
	rows, err := automation.BuildRows(s.cfg, data.View)
	if err != nil {
		return
	}
	data.AutomationConnections = rows
}

// handleNodeAutomationConnectionSave adds one connect/disconnect
// automation rule -- see automation.SaveConnection's own doc comment
// for the actual save algorithm, shared with the cloud agent's
// schedule.saveConnection action.
func (s *Server) handleNodeAutomationConnectionSave(w http.ResponseWriter, r *http.Request) {
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

	p := automation.SaveConnectionParams{
		ActionKey:  r.FormValue("action"),
		Target:     strings.TrimSpace(r.FormValue("target")),
		Minute:     strings.TrimSpace(r.FormValue("minute")),
		Hour:       strings.TrimSpace(r.FormValue("hour")),
		DayOfMonth: strings.TrimSpace(r.FormValue("dom")),
		Month:      strings.TrimSpace(r.FormValue("month")),
		Weekdays:   r.Form["weekday"],
	}
	if err := automation.SaveConnection(s.cfg, view, p); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}

	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeAutomationConnectionDelete removes one connect/disconnect
// automation rule -- see automation.DeleteConnection's own doc comment.
func (s *Server) handleNodeAutomationConnectionDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	macroNum := r.PathValue("macronum")
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := automation.DeleteConnection(s.cfg, view, macroNum); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}
