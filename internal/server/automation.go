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
	view := data.View
	data.SchedulerSect = view.Scheduler

	scheduleEntries, err := s.cfg.ListScheduleEntries(view.Scheduler)
	if err != nil {
		return
	}
	macroEntries, err := s.cfg.ListFunctionMacros(view.Macro)
	if err != nil {
		return
	}
	macroByNum := make(map[string]string, len(macroEntries))
	for _, m := range macroEntries {
		macroByNum[m.Digits] = m.Command
	}
	functionsEntries, err := s.cfg.ListFunctionMacros(view.Functions)
	if err != nil {
		functionsEntries = nil
	}

	rows := make([]automation.Row, 0, len(scheduleEntries))
	for _, se := range scheduleEntries {
		row := automation.Row{MacroNum: se.MacroNum, TimeSpec: se.TimeSpec}
		if dtmf, ok := macroByNum[se.MacroNum]; ok {
			if label, recognized := automation.ParseMacro(dtmf, functionsEntries); recognized {
				row.Label = label
				row.Recognized = true
			} else {
				row.Label = dtmf
			}
		} else {
			row.Label = "(macro " + se.MacroNum + " not found)"
		}
		rows = append(rows, row)
	}
	data.AutomationConnections = rows
}

// handleNodeAutomationConnectionSave adds one connect/disconnect
// automation rule. Weekday checkboxes are a create-time convenience:
// app_rpt's schedule format allows only one day-of-week value (or "*")
// per entry, so selecting several fans out into that many independent
// schedule+macro pairs, each then listed/edited/deleted as its own row
// -- there is no native way to group them, so this doesn't pretend to.
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

	action, ok := automation.ActionByKey(r.FormValue("action"))
	if !ok {
		s.renderNodeEditErrorReq(w, r, num, "Pick a valid connect/disconnect action")
		return
	}
	target := strings.TrimSpace(r.FormValue("target"))
	if action.NeedsTarget && target == "" {
		s.renderNodeEditErrorReq(w, r, num, "Enter the node number for this action")
		return
	}

	minute := strings.TrimSpace(r.FormValue("minute"))
	hour := strings.TrimSpace(r.FormValue("hour"))
	dom := strings.TrimSpace(r.FormValue("dom"))
	month := strings.TrimSpace(r.FormValue("month"))
	for _, v := range []string{minute, hour, dom, month} {
		if !automation.TimeFieldRe.MatchString(v) {
			s.renderNodeEditErrorReq(w, r, num, "Minute/hour/day-of-month/month must each be a single number or * — app_rpt's scheduler doesn't support ranges or lists")
			return
		}
	}
	weekdays := r.Form["weekday"]
	for _, wd := range weekdays {
		if !automation.TimeFieldRe.MatchString(wd) {
			s.renderNodeEditErrorReq(w, r, num, "Invalid day-of-week value")
			return
		}
	}
	if len(weekdays) == 0 {
		weekdays = []string{"*"}
	}

	digit, err := automation.EnsureFunctionDigit(s.cfg, view.Functions, action.Command)
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	dtmf := automation.BuildDTMF(digit, target, action.NeedsTarget)

	schedulerSection := view.Scheduler
	if schedulerSection == "schedule" {
		// Still on the shared default -- give this node its own section
		// on first use, so its scheduled connections don't collide with
		// (or get deleted alongside) any other node's.
		schedulerSection = "schedule" + num
		if err := s.cfg.SetNodeScheduler(num, schedulerSection); err != nil {
			s.renderNodeEditErrorReq(w, r, num, err.Error())
			return
		}
	}

	for _, wd := range weekdays {
		macroEntries, err := s.cfg.ListFunctionMacros(view.Macro)
		if err != nil {
			s.renderNodeEditErrorReq(w, r, num, err.Error())
			return
		}
		macroNum := automation.AllocateMacroNumber(macroEntries)
		if err := s.cfg.SetFunctionMacro(view.Macro, macroNum, dtmf); err != nil {
			s.renderNodeEditErrorReq(w, r, num, err.Error())
			return
		}
		timeSpec := minute + " " + hour + " " + dom + " " + month + " " + wd
		if err := s.cfg.SetScheduleEntry(schedulerSection, macroNum, timeSpec); err != nil {
			s.renderNodeEditErrorReq(w, r, num, err.Error())
			return
		}
	}

	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeAutomationConnectionDelete removes one connect/disconnect
// automation rule's schedule entry and its own dedicated macro entry,
// but leaves the shared functions-table digit alone -- other rows may
// reuse it, and it's indistinguishable from one the operator wired up
// by hand.
func (s *Server) handleNodeAutomationConnectionDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	macroNum := r.PathValue("macronum")
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.cfg.DeleteScheduleEntry(view.Scheduler, macroNum); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	if err := s.cfg.DeleteFunctionMacro(view.Macro, macroNum); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}
