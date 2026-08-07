// Scheduler tab: scheduled sound-playback entries (internal/
// soundschedule). Unlike scheduled connect/disconnect (app_rpt's own
// native rpt.conf scheduler, which needs nothing running here to keep
// working), there is no native app_rpt mechanism to schedule arbitrary
// sound-file playback -- StartSoundSchedulePoller exists to fill that
// gap, and only fires while this process is running. Connect/disconnect
// scheduling itself isn't ported here: it needs the original HamVoIP
// app's internal/automation package (DTMF function-digit allocation,
// macro numbering), which hasn't been ported to ASL3.
package server

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hamvoipconfiggui-asl3/internal/soundschedule"
	"hamvoipconfiggui-asl3/internal/system"
)

// soundSchedulePollInterval is how often the sound-schedule ticker
// checks for a matching entry. Deliberately finer than 60s: a bare
// 60-second ticker started at an arbitrary process-start offset can
// drift across a minute boundary and skip a scheduled minute entirely.
const soundSchedulePollInterval = 15 * time.Second

// StartSoundSchedulePoller checks on soundSchedulePollInterval whether
// any scheduled sound entry matches the current wall-clock minute,
// calling out to `asterisk -rx "rpt localplay/playback ..."` when one
// does. lastFired is only ever touched from this one goroutine (ticks
// are handled sequentially, never concurrently with themselves), so it
// needs no locking of its own. Runs until ctx is cancelled. Call once,
// from main.
func (s *Server) StartSoundSchedulePoller(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(soundSchedulePollInterval)
		defer ticker.Stop()
		lastFired := make(map[string]string)
		s.checkSoundSchedule(ctx, lastFired)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkSoundSchedule(ctx, lastFired)
			}
		}
	}()
}

// checkSoundSchedule fires every entry that matches now and hasn't
// already fired for this exact minute, tracked via lastFired (entry ID
// -> the minute it last fired, truncated). A play failure is logged,
// not fatal -- the next scheduled minute (or a different entry) should
// still get a chance to run.
func (s *Server) checkSoundSchedule(ctx context.Context, lastFired map[string]string) {
	entries, err := s.soundSchedule.List()
	if err != nil {
		return
	}
	now := time.Now()
	minuteKey := now.Truncate(time.Minute).Format(time.RFC3339)
	for _, e := range entries {
		if !e.Matches(now) {
			continue
		}
		if lastFired[e.ID] == minuteKey {
			continue
		}
		lastFired[e.ID] = minuteKey

		var playErr error
		if e.Reach == soundschedule.ReachNetwork {
			playErr = system.RptPlayback(ctx, s.asteriskBin, e.Node, e.File)
		} else {
			playErr = system.RptLocalPlay(ctx, s.asteriskBin, e.Node, e.File)
		}
		if playErr != nil {
			log.Printf("sound schedule: node %s file %s: %v", e.Node, e.File, playErr)
		}
	}
}

// populateNodeSoundSchedule fills nodeEditData's Scheduler-tab fields.
// Best-effort, like the rest of this page's supplementary data.
func (s *Server) populateNodeSoundSchedule(data *nodeEditData) {
	if data.View == nil || data.View.Node == "" {
		return
	}
	entries, err := s.soundSchedule.ListForNode(data.View.Node)
	if err != nil {
		return
	}
	data.SoundSchedules = entries
}

// handleNodeSoundScheduleSave adds one scheduled sound-playback entry.
// Multiple selected weekdays stay a single entry -- DaysOfWeek is a real
// list here, unlike app_rpt's own native scheduler (which only allows
// one day-of-week value per stanza entry).
func (s *Server) handleNodeSoundScheduleSave(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	file := strings.TrimSpace(r.FormValue("sound_file"))
	if file == "" {
		s.renderNodeEditErrorReq(w, r, num, "Pick a sound file to schedule")
		return
	}
	reach := r.FormValue("reach")
	if reach != soundschedule.ReachNetwork {
		reach = soundschedule.ReachLocal
	}

	minute := strings.TrimSpace(r.FormValue("minute"))
	hour := strings.TrimSpace(r.FormValue("hour"))
	dom := strings.TrimSpace(r.FormValue("dom"))
	month := strings.TrimSpace(r.FormValue("month"))
	for _, v := range []string{minute, hour, dom, month} {
		if !soundschedule.TimeFieldRe.MatchString(v) {
			s.renderNodeEditErrorReq(w, r, num, "Minute/hour/day-of-month/month must each be a single number or *")
			return
		}
	}

	var weekdays []int
	for _, wd := range r.Form["weekday"] {
		n, err := strconv.Atoi(wd)
		if err != nil || n < 0 || n > 6 {
			s.renderNodeEditErrorReq(w, r, num, "Invalid day-of-week value")
			return
		}
		weekdays = append(weekdays, n)
	}

	entry := soundschedule.Entry{
		Node:       num,
		File:       file,
		Reach:      reach,
		Minute:     minute,
		Hour:       hour,
		DayOfMonth: dom,
		Month:      month,
		DaysOfWeek: weekdays,
	}
	if err := s.soundSchedule.Save(entry); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeSoundScheduleDelete removes one scheduled sound-playback
// entry.
func (s *Server) handleNodeSoundScheduleDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	id := r.PathValue("id")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.soundSchedule.Delete(id); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}
