// SkywarnPlus tab: configures an operator-installed copy of SkywarnPlus
// (github.com/Mason10198/SkywarnPlus, a third-party weather-alert tool)
// via internal/skywarnplus, already ported unchanged. This app never
// installs SkywarnPlus itself -- only configures a copy that's already
// there (install.sh doesn't yet provision it on ASL3; see that file's
// own TODO).
//
// The WX courtesy-tone swap section from the original HamVoIP app's
// equivalent tab isn't ported here -- it needs internal/wxtone, which
// hasn't been ported to ASL3.
package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"hamvoipconfiggui-asl3/internal/skywarnplus"
)

// skywarnToggleKeys lists the boolean features exposed on the Weather
// Alerts card, saved together from one form/button -- matching this
// app's own "select over checkbox for an explicit on/off setting"
// convention (an unchecked checkbox submits nothing at all) rather than
// SkyControl.py's own one-key-per-invocation shape. alertscript is just
// AlertScript's own Enable flag -- its Mappings list (arbitrary
// shell/DTMF commands run automatically per alert type) stays out of
// scope, edited via config.yaml directly if wanted.
var skywarnToggleKeys = []string{"enable", "sayalert", "sayallclear", "tailmessage", "alertscript"}

// populateNodeSkywarn fills nodeEditData's SkywarnPlus-tab fields from
// an operator-installed copy of SkywarnPlus, if there is one.
// Best-effort like the rest of this page's supplementary data: any read
// failure just leaves the section looking not-installed rather than
// failing the whole page. The county picker's option list is populated
// regardless of whether SkywarnPlus is installed, since it's just this
// app's own bundled reference data.
func (s *Server) populateNodeSkywarn(ctx context.Context, data *nodeEditData) {
	data.CountyCodeOptions = skywarnplus.ListCounties()
	if !skywarnplus.IsInstalled(s.skywarnDir) {
		return
	}
	data.SkywarnInstalled = true
	status, err := skywarnplus.GetStatus(ctx, s.skywarnDir)
	if err != nil {
		return
	}
	data.SkywarnStatus = status
	if data.View == nil {
		return
	}
	for _, n := range status.Nodes {
		if n == data.View.Node {
			data.SkywarnNodeRegistered = true
			break
		}
	}
}

// handleNodeSkywarnToggle saves every boolean feature in one submission
// (see skywarnToggleKeys), each via SkywarnPlus's own SkyControl.py -- a
// failure on one key doesn't stop the others from being attempted, and
// every failure is reported together.
func (s *Server) handleNodeSkywarnToggle(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var failed []string
	for _, key := range skywarnToggleKeys {
		value := r.FormValue(key) == "true"
		if _, err := skywarnplus.SetToggle(r.Context(), s.skywarnDir, key, value); err != nil {
			failed = append(failed, key+": "+err.Error())
		}
	}
	if len(failed) > 0 {
		s.renderNodeEditErrorReq(w, r, num, "Some settings couldn't be saved: "+strings.Join(failed, "; "))
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeSkywarnAddCounty adds one county code to SkywarnPlus's list.
// SetCounties always replaces the whole list (sky_configure.py has no
// single-item-append for counties, unlike AddNode for the node list), so
// this reads the current list first and appends to it -- a no-op if the
// code's already present, rather than a duplicate entry.
func (s *Server) handleNodeSkywarnAddCounty(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if code == "" {
		s.renderNodeEditErrorReq(w, r, num, "Pick a county to add")
		return
	}
	status, err := skywarnplus.GetStatus(r.Context(), s.skywarnDir)
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't read SkywarnPlus's current settings: "+err.Error())
		return
	}
	codes := status.CountyCodes
	already := false
	for _, c := range codes {
		if c == code {
			already = true
			break
		}
	}
	if !already {
		codes = append(codes, code)
	}
	if _, err := skywarnplus.SetCounties(r.Context(), s.skywarnDir, codes); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't add county: "+err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeSkywarnDeleteCounty removes one county code, the same
// read-modify-replace-whole-list way handleNodeSkywarnAddCounty adds
// one.
func (s *Server) handleNodeSkywarnDeleteCounty(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	code := r.PathValue("code")
	status, err := skywarnplus.GetStatus(r.Context(), s.skywarnDir)
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't read SkywarnPlus's current settings: "+err.Error())
		return
	}
	remaining := make([]string, 0, len(status.CountyCodes))
	for _, c := range status.CountyCodes {
		if c != code {
			remaining = append(remaining, c)
		}
	}
	if _, err := skywarnplus.SetCounties(r.Context(), s.skywarnDir, remaining); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't remove county: "+err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeSkywarnRegister adds this node's own number to SkywarnPlus's
// broadcast list -- idempotent (sky_configure.py's add-node is a no-op
// if already present).
func (s *Server) handleNodeSkywarnRegister(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := skywarnplus.AddNode(r.Context(), s.skywarnDir, num); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't register this node with SkywarnPlus: "+err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeSkywarnPushover saves SkywarnPlus's Pushover notification
// settings in one submission -- self-contained: once Enable/UserKey/
// APIToken are set, SkywarnPlus's own run loop sends notifications with
// no other wiring needed.
func (s *Server) handleNodeSkywarnPushover(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	enable := r.FormValue("pushover_enable") == "true"
	debug := r.FormValue("pushover_debug") == "true"
	userKey := r.FormValue("pushover_userkey")
	apiToken := r.FormValue("pushover_apitoken")
	if _, err := skywarnplus.SetPushover(r.Context(), s.skywarnDir, enable, userKey, apiToken, debug); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't save Pushover settings: "+err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeSkywarnSkyDescribe saves SkyDescribe's connection/voice
// settings in one submission. This only configures SkyDescribe itself --
// triggering it (a DTMF command, or an AlertScript mapping) is a
// separate step this handler doesn't cover.
func (s *Server) handleNodeSkywarnSkyDescribe(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	speed, err := strconv.Atoi(r.FormValue("skydescribe_speed"))
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Speed must be a whole number from -10 to 10")
		return
	}
	maxWords, err := strconv.Atoi(r.FormValue("skydescribe_maxwords"))
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Max words must be a whole number")
		return
	}
	apiKey := r.FormValue("skydescribe_apikey")
	language := r.FormValue("skydescribe_language")
	voice := r.FormValue("skydescribe_voice")
	if _, err := skywarnplus.SetSkyDescribe(r.Context(), s.skywarnDir, apiKey, language, speed, voice, maxWords); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't save SkyDescribe settings: "+err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}
