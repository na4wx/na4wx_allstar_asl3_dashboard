// Package server wires HTTP routes to the auth package and renders the
// embedded templates. This is the ASL3 port's scaffold: auth, template
// rendering, and session handling are copied from the HamVoIP app
// essentially unchanged (confirmed platform-agnostic), since none of it
// touches Asterisk config or the OS at all. Node/System/Config pages
// come in later phases once internal/config (ASL3's template-inheritance
// config I/O) exists -- see /Users/jwebb/.claude/plans/snazzy-honking-hennessy.md.
package server

import (
	"context"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"hamvoipconfiggui-asl3/internal/auth"
	"hamvoipconfiggui-asl3/internal/cloudagent"
	"hamvoipconfiggui-asl3/internal/config"
	"hamvoipconfiggui-asl3/internal/sa818"
	"hamvoipconfiggui-asl3/internal/sounds"
	"hamvoipconfiggui-asl3/internal/soundschedule"
	"hamvoipconfiggui-asl3/internal/tts"
	"hamvoipconfiggui-asl3/internal/wifi"
)

const sessionCookie = "hamvoip_gui_session"

type Server struct {
	auth *auth.Manager
	cfg  *config.Store
	tmpl map[string]*template.Template
	mux  *http.ServeMux

	asteriskBin    string
	sa818Port      string
	sa818StatePath string

	// sounds manages the shared (node-agnostic) custom/stock sound-file
	// directories -- see internal/sounds's package doc. ttsTool and
	// ttsVoicesDir configure the "Create from text" sound generator (see
	// internal/tts's package doc): ttsTool is normally the Piper binary,
	// with espeak-ng as a same-page fallback if Piper can't run on this
	// system; a voice named in ttsVoicesDir is only ever selected by
	// looking it up through tts.FindVoice, never built from raw form
	// input.
	sounds       *sounds.Store
	ttsTool      string
	ttsVoicesDir string

	// soundSchedule holds the operator's scheduled sound-playback
	// entries -- see internal/soundschedule's package doc.
	// StartSoundSchedulePoller fires them; the Scheduler tab manages
	// them.
	soundSchedule *soundschedule.Store

	// wifiManager owns wlan0's hotspot-fallback state machine -- see
	// internal/wifi's package doc. Always non-nil (constructed in New);
	// StartWiFiWatchdog swaps in the real detected backend and starts the
	// watchdog goroutine, so a Server that never calls it (e.g. in tests)
	// still renders the System page fine, just with WiFi management
	// reporting "unavailable".
	wifiManager *wifi.Manager

	// cloudAgent is this node's optional, off-by-default connection to
	// the public cloud platform -- see internal/cloudagent's package
	// doc. Always non-nil (constructed in New); whether it actually
	// dials out is controlled by cloudAgent.Settings()'s own Enabled
	// flag, checked by (*cloudagent.Agent).Run itself, not by anything
	// here. cloudURLDefault is shown read-only on the Cloud Sync
	// settings card -- see cloudagent.New's own doc comment for why the
	// cloud address itself is never operator-editable.
	cloudAgent      *cloudagent.Agent
	cloudURLDefault string
}

// StartCloudAgent begins this node's optional, off-by-default outbound
// connection to the public cloud platform. Safe to call even if Cloud
// Sync has never been configured -- Agent.Run just polls its own
// settings and stays idle until an operator enables it with an API key.
func (s *Server) StartCloudAgent(ctx context.Context) {
	go s.cloudAgent.Run(ctx)
}

// asteriskDir overrides where Asterisk's own config files
// (rpt.conf/usbradio.conf/simpleusb.conf) are read from; pass "" to use
// the real ASL3 default (/etc/asterisk). asteriskBin is the path (or
// bare name, if it's on PATH) to the asterisk binary, used for CLI
// status/reload calls (not restart -- see internal/system.AsteriskRestart,
// which goes through systemctl instead, since Asterisk is a confirmed
// native systemd unit on ASL3). sa818Port/sa818StatePath configure the
// SA818/DRA818 radio-module programmer card. wifiHotspotSSID/
// wifiHotspotPassword/wifiDashboardPort/wifiHotspotEnabled configure the
// wlan0 fallback hotspot -- see internal/wifi's package doc.
func New(authMgr *auth.Manager, templatesFS, staticFS fs.FS, asteriskDir, asteriskBin, sa818Port, sa818StatePath, soundsCustomDir, soundsStockDir, soxTool, ttsTool, ttsVoicesDir, soundSchedulePath, skywarnDir, cloudSettingsPath, cloudURLDefault, cloudAuditLogPath, wifiHotspotSSID, wifiHotspotPassword, wifiDashboardPort string, wifiHotspotEnabled bool) (*Server, error) {
	cfg := &config.Store{Dir: asteriskDir}
	soundsStore := sounds.New(soundsCustomDir, soundsStockDir, soxTool)
	soundScheduleStore := soundschedule.New(soundSchedulePath)

	s := &Server{
		auth:           authMgr,
		cfg:            cfg,
		mux:            http.NewServeMux(),
		asteriskBin:    asteriskBin,
		sa818Port:      sa818Port,
		sa818StatePath: sa818StatePath,
		sounds:         soundsStore,
		ttsTool:        ttsTool,
		ttsVoicesDir:   ttsVoicesDir,
		soundSchedule:  soundScheduleStore,
		wifiManager:    wifi.NewManager(wifiHotspotSSID, wifiHotspotPassword, wifiDashboardPort, wifiHotspotEnabled),
		cloudAgent: cloudagent.New(
			cloudSettingsPath, cloudURLDefault, cfg, asteriskBin,
			soundsStore, soundScheduleStore, skywarnDir,
			sa818Port, sa818StatePath, cloudAuditLogPath,
		),
		cloudURLDefault: cloudURLDefault,
	}

	tmpl, err := s.parseTemplates(templatesFS)
	if err != nil {
		return nil, err
	}
	s.tmpl = tmpl

	s.routes(staticFS)
	return s, nil
}

// StartWiFiWatchdog detects the real WiFi backend (NetworkManager, on
// ASL3 -- see wifi.DetectBackend) and starts the hotspot-fallback
// watchdog goroutine. Not called from New so tests can construct a
// Server without shelling out to systemctl/nmcli at all.
func (s *Server) StartWiFiWatchdog(ctx context.Context) {
	backend := wifi.DetectBackend(ctx)
	log.Printf("wifi: detected backend %q for the hotspot-fallback watchdog", backend.Name())
	s.wifiManager.SetBackend(backend)
	go s.wifiManager.Run(ctx)
}

// parseTemplates parses every page template up front, same set as the
// HamVoIP app -- the template *files* are already fully ported (see
// web/templates), even though most pages' Go handlers/data don't exist
// yet. Parsing succeeds regardless: it only needs the template syntax
// to be valid, not for any Go data to exist yet. Execution (render) is
// what would fail for a page whose handler isn't wired up yet, so only
// routes with a real handler are registered in routes() below.
func (s *Server) parseTemplates(templatesFS fs.FS) (map[string]*template.Template, error) {
	pages := []string{"setup.html", "login.html", "home.html", "stats.html", "nodes_index.html", "node_edit.html", "node_new.html", "node_form.html", "config.html", "system.html", "radio_form.html"}
	funcs := template.FuncMap{"restartNeeded": func() bool { return false }}
	out := map[string]*template.Template{}
	for _, page := range pages {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templatesFS, "layout.html", "radio_device_fields.html", "node_history.html", page)
		if err != nil {
			return nil, err
		}
		out[page] = t
	}
	return out, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes(staticFS fs.FS) {
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	s.mux.HandleFunc("GET /setup", s.handleSetupForm)
	s.mux.HandleFunc("POST /setup", s.handleSetupSubmit)
	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLoginSubmit)
	s.mux.HandleFunc("POST /logout", s.requireAuth(s.handleLogout))

	// Placeholder until Phase 2/3 bring real node data -- see this
	// package's own doc comment.
	s.mux.HandleFunc("GET /{$}", s.requireAuth(s.handlePlaceholderHome))

	s.mux.HandleFunc("GET /nodes", s.requireAuth(s.handleNodesIndex))
	s.mux.HandleFunc("GET /nodes/new", s.requireAuth(s.handleNodeNewForm))
	s.mux.HandleFunc("POST /nodes/new", s.requireAuth(s.handleNodeNewSubmit))
	s.mux.HandleFunc("GET /nodes/{node}", s.requireAuth(s.handleNodeEdit))
	s.mux.HandleFunc("POST /nodes/{node}", s.requireAuth(s.handleNodeUpdate))
	s.mux.HandleFunc("POST /nodes/{node}/radio-tuning", s.requireAuth(s.handleNodeRadioTuningUpdate))
	s.mux.HandleFunc("POST /nodes/{node}/registration", s.requireAuth(s.handleNodeRegistrationUpdate))
	s.mux.HandleFunc("POST /nodes/{node}/delete", s.requireAuth(s.handleNodeDelete))
	s.mux.HandleFunc("POST /nodes/{node}/sa818/apply", s.requireAuth(s.handleNodeSA818Apply))
	s.mux.HandleFunc("POST /nodes/{node}/station-id", s.requireAuth(s.handleNodeStationIDUpdate))
	s.mux.HandleFunc("POST /nodes/{node}/courtesy-tones", s.requireAuth(s.handleNodeCourtesyToneUpdate))
	s.mux.HandleFunc("POST /nodes/{node}/telemetry", s.requireAuth(s.handleNodeTelemetryUpdate))
	s.mux.HandleFunc("POST /nodes/{node}/sounds/upload", s.requireAuth(s.handleNodeSoundUpload))
	s.mux.HandleFunc("POST /nodes/{node}/sounds/tts", s.requireAuth(s.handleNodeSoundTTS))
	s.mux.HandleFunc("POST /nodes/{node}/sounds/tts/preview", s.requireAuth(s.handleNodeSoundTTSPreview))
	s.mux.HandleFunc("GET /nodes/{node}/sounds/{name}/audio", s.requireAuth(s.handleNodeSoundAudio))
	s.mux.HandleFunc("POST /nodes/{node}/sounds/{name}/delete", s.requireAuth(s.handleNodeSoundDelete))
	s.mux.HandleFunc("POST /nodes/{node}/schedule/sounds", s.requireAuth(s.handleNodeSoundScheduleSave))
	s.mux.HandleFunc("POST /nodes/{node}/schedule/sounds/{id}/delete", s.requireAuth(s.handleNodeSoundScheduleDelete))

	s.mux.HandleFunc("GET /system", s.requireAuth(s.handleSystemPage))
	s.mux.HandleFunc("POST /system/hostname", s.requireAuth(s.handleSystemHostname))
	s.mux.HandleFunc("POST /system/password", s.requireAuth(s.handleSystemPassword))
	s.mux.HandleFunc("POST /system/restart-asterisk", s.requireAuth(s.handleSystemRestartAsterisk))
	s.mux.HandleFunc("POST /system/reboot", s.requireAuth(s.handleSystemReboot))
	s.mux.HandleFunc("POST /system/wifi/scan", s.requireAuth(s.handleSystemWiFiScan))
	s.mux.HandleFunc("POST /system/wifi/connect", s.requireAuth(s.handleSystemWiFiConnect))
	s.mux.HandleFunc("POST /system/cloud", s.requireAuth(s.handleSystemCloudSave))
}

// pageData is the common template context. Handlers embed it and add
// page-specific fields.
type pageData struct {
	LoggedIn  bool
	FlashKind string
	Flash     string
}

func flash(kind, msg string) pageData {
	return pageData{LoggedIn: true, FlashKind: kind, Flash: msg}
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.tmpl[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

// currentUsername returns the logged-in user's name, or "" if called
// outside a requireAuth-wrapped handler (where a valid session is
// already guaranteed).
func (s *Server) currentUsername(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	username, _ := s.auth.ValidateSession(c.Value)
	return username
}

// requireAuth wraps a handler so it 302s to /login (or /setup, if no
// account has been created yet) without a valid session cookie.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Configured() {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if _, ok := s.auth.ValidateSession(c.Value); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	if s.auth.Configured() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, "setup.html", pageData{})
}

func (s *Server) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if s.auth.Configured() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if password != confirm {
		s.render(w, "setup.html", pageData{Flash: "Passwords do not match", FlashKind: "error"})
		return
	}
	if err := s.auth.SetCredentials(username, password); err != nil {
		s.render(w, "setup.html", pageData{Flash: err.Error(), FlashKind: "error"})
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Configured() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.render(w, "login.html", pageData{})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if !s.auth.Verify(username, password) {
		s.render(w, "login.html", pageData{Flash: "Invalid username or password", FlashKind: "error"})
		return
	}
	token, err := s.auth.CreateSession(username)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   int((12 * time.Hour).Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.auth.DestroySession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handlePlaceholderHome stands in for the real home page until Phase 2/3
// bring real node data from ASL3's own config. Deliberately not
// rendering home.html yet -- that template expects live node/history
// data this scaffold doesn't have.
func (s *Server) handlePlaceholderHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!doctype html><html><head><title>ASL3 Dashboard</title></head><body style="font-family:sans-serif;padding:2rem"><h1>ASL3 Dashboard</h1><p>Scaffold running. System page and Cloud Sync land in later phases.</p><p><a href="/nodes">Nodes</a></p><form method="post" action="/logout"><button type="submit">Log out</button></form></body></html>`))
}

// nodeRow is nodes_index.html's expected shape for one table row --
// matches the field paths that template already references (.Node.Number,
// .Node.RXChannel, .Callsign). Callsign isn't populated yet -- ASL3's
// rpt.conf has no dedicated per-node callsign field the way this template
// assumes; deferred until a real source for it turns up.
type nodeRow struct {
	Node struct {
		Number    string
		RXChannel string
	}
	Callsign string
}

func (s *Server) handleNodesIndex(w http.ResponseWriter, r *http.Request) {
	nums, err := s.cfg.ListNodes()
	if err != nil {
		log.Printf("list nodes: %v", err)
		data := flash("error", "Could not read node configuration: "+err.Error())
		s.render(w, "nodes_index.html", struct {
			pageData
			Nodes []nodeRow
		}{pageData: data})
		return
	}

	rows := make([]nodeRow, 0, len(nums))
	for _, num := range nums {
		view, err := s.cfg.LoadNode(num)
		if err != nil {
			log.Printf("load node %s: %v", num, err)
			continue
		}
		var row nodeRow
		row.Node.Number = view.Node
		row.Node.RXChannel = view.RxChannel
		rows = append(rows, row)
	}

	s.render(w, "nodes_index.html", struct {
		pageData
		Nodes []nodeRow
	}{pageData: pageData{LoggedIn: true}, Nodes: rows})
}

type nodeEditData struct {
	pageData
	View         *config.NodeView
	Registration config.Registration

	// SA818/DRA818 radio-module programming, scoped to this node's own
	// page since it's this node's physical radio hardware -- moved here
	// from a standalone System-page card, which just left an operator
	// setting CTCSS in two disconnected places (the module's own
	// hardware CTCSS here, and Asterisk's own ctcssfrom on this same
	// page) with no indication they were related, let alone that the
	// module-level one already made the Asterisk-level one redundant
	// (the module simply won't pass audio through at all unless its own
	// CTCSS requirement is met, so there's nothing left for Asterisk to
	// independently verify) -- see this file's own handleNodeUpdate,
	// which no longer has a ctcssfrom field at all for that reason.
	SA818Port    string
	SA818Last    *sa818.LastApplied
	CTCSSOptions []ctcssOption

	// Sounds tab: shared (node-agnostic) custom+stock sound library plus
	// whichever text-to-speech backend is available -- see
	// populateNodeSounds.
	SoundFiles      []sounds.File
	TTSVoices       []tts.Voice
	TTSEngine       string
	TTSNotice       string
	TTSError        string
	TTSDefaultVoice string

	// Sounds tab: courtesy-tone/telemetry editor -- see
	// populateNodeTelemetry.
	TelemetrySect string
	TelemetryRows []telemetryRow
	CTKeys        []string

	// Sounds tab: station ID editor (rpt.conf's own "idrecording") --
	// see populateNodeTelemetry. StationIDMode is "cw" or "sound";
	// StationIDText is the CW text (only meaningful in "cw" mode);
	// StationIDValue is the raw current value, for the sound picker to
	// match against.
	StationIDMode  string
	StationIDText  string
	StationIDValue string

	// Scheduler tab: scheduled sound-playback entries -- see
	// populateNodeSoundSchedule.
	SoundSchedules []soundschedule.Entry
}

func (s *Server) loadNodeEditData(num string, pd pageData) (nodeEditData, error) {
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		return nodeEditData{}, err
	}
	reg := config.Registration{Node: num}
	if regs, err := s.cfg.ListRegistrations(); err != nil {
		log.Printf("list registrations: %v", err)
	} else {
		for _, r := range regs {
			if r.Node == num {
				reg = r
				break
			}
		}
	}

	data := nodeEditData{pageData: pd, View: view, Registration: reg, SA818Port: s.sa818Port, CTCSSOptions: ctcssOptions()}
	if s.sa818StatePath != "" {
		if last, err := sa818.LoadLast(s.sa818StatePath); err == nil {
			data.SA818Last = last
		}
	}
	s.populateNodeSounds(&data)
	s.populateNodeTelemetry(&data)
	s.populateNodeSoundSchedule(&data)
	return data, nil
}

func (s *Server) handleNodeEdit(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	data, err := s.loadNodeEditData(num, pageData{LoggedIn: true})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_edit.html", data)
}

var validDuplex = map[string]bool{"0": true, "1": true, "2": true, "3": true, "4": true}
var validDriverDuplex = map[string]bool{"0": true, "1": true}

// validCarrierFromSimpleUSB is SimpleUSB's own real option set for
// carrierfrom, confirmed against simpleusb.conf's own comments on a real
// node. USBRadio's option set differs (it adds dsp and vox) -- confirmed
// against usbradio.conf's own comments; see validCarrierFromUSBRadio.
//
// There's no equivalent ctcssfrom field here: CTCSS is set once, on the
// SA818/DRA818 module itself (this node's own SA818 card, further down
// this page) -- the module simply won't pass audio through at all
// unless its own CTCSS requirement is met, so an independent
// Asterisk-side ctcssfrom check has nothing left to verify. An earlier
// version of this page exposed both, in two disconnected places, with
// nothing explaining they were related.
var validCarrierFromSimpleUSB = map[string]bool{"no": true, "usb": true, "usbinvert": true, "pp": true, "ppinvert": true}

var validCarrierFromUSBRadio = map[string]bool{"no": true, "usb": true, "usbinvert": true, "dsp": true, "vox": true, "pp": true, "ppinvert": true}

func (s *Server) handleNodeUpdate(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rxchannel := r.FormValue("rxchannel")
	validRx := map[string]bool{
		"Local/pseudo":     true,
		"SimpleUSB/" + num: true,
		"Radio/" + num:     true,
	}
	if !validRx[rxchannel] {
		s.renderNodeEditErrorReq(w, r, num, "Unrecognized radio interface selection")
		return
	}
	duplex := r.FormValue("duplex")
	if !validDuplex[duplex] {
		s.renderNodeEditErrorReq(w, r, num, "Duplex must be 0-4")
		return
	}

	if err := s.cfg.UpdateNodeSettings(num, map[string]string{
		"rxchannel": rxchannel,
		"duplex":    duplex,
	}); err != nil {
		log.Printf("update node %s: %v", num, err)
		s.renderNodeEditErrorReq(w, r, num, "Could not save node settings: "+err.Error())
		return
	}

	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeRadioTuningUpdate is the Radio tab's tuning form -- split out
// from handleNodeUpdate (which now only handles the Setup tab's
// rxchannel/duplex) so each tab on node_edit.html can be its own
// self-contained form, matching the original HamVoIP app's node_form.html
// tab structure. Loads the node fresh to find its current driver
// (SimpleUSB vs USBRadio) rather than trusting a rxchannel form field,
// since this form doesn't carry one.
func (s *Server) handleNodeRadioTuningUpdate(w http.ResponseWriter, r *http.Request) {
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
	if view.Radio == nil {
		s.renderNodeEditErrorReq(w, r, num, "This node has no radio interface to tune")
		return
	}
	isUSBRadio := view.Radio.Driver == "usbradio"

	updates := map[string]string{}
	for _, key := range []string{"rxmixerset", "txmixaset", "txmixbset"} {
		if v := strings.TrimSpace(r.FormValue(key)); v != "" {
			updates[key] = v
		}
	}

	validCarrierFrom := validCarrierFromSimpleUSB
	if isUSBRadio {
		validCarrierFrom = validCarrierFromUSBRadio
	}
	if v := r.FormValue("carrierfrom"); v != "" {
		if !validCarrierFrom[v] {
			s.renderNodeEditErrorReq(w, r, num, "Unrecognized carrier-detect source")
			return
		}
		updates["carrierfrom"] = v
	}

	if isUSBRadio {
		if v := r.FormValue("driverduplex"); v != "" {
			if !validDriverDuplex[v] {
				s.renderNodeEditErrorReq(w, r, num, "Audio driver duplex must be 0 or 1")
				return
			}
			updates["duplex"] = v
		}
		for _, key := range []string{"rxvoiceadj", "txctcssadj", "rxsquelchadj"} {
			if v := strings.TrimSpace(r.FormValue(key)); v != "" {
				updates[key] = v
			}
		}
	}

	if len(updates) > 0 {
		if err := s.cfg.UpdateRadioSettings(num, updates); err != nil {
			log.Printf("update radio settings %s: %v", num, err)
			s.renderNodeEditErrorReq(w, r, num, "Could not save radio tuning: "+err.Error())
			return
		}
	}

	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeRegistrationUpdate is the Allstar Network tab's registration
// form -- split out from handleNodeUpdate for the same reason as
// handleNodeRadioTuningUpdate above.
func (s *Server) handleNodeRegistrationUpdate(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	server := strings.TrimSpace(r.FormValue("reg_server"))
	password := r.FormValue("reg_password")
	// The server field is pre-filled with register.allstarlink.org by
	// default (see node_edit.html), so it can't double as an "I don't
	// want registration" signal the way an empty field normally would --
	// password is the field that actually decides whether a registration
	// exists: leave it blank to not register (or to remove an existing
	// one), fill it in to register (or update) using the server given.
	if password == "" {
		if err := s.cfg.RemoveRegistration(num); err != nil {
			log.Printf("remove registration %s: %v", num, err)
		}
	} else {
		if server == "" {
			s.renderNodeEditErrorReq(w, r, num, "A server is required to register")
			return
		}
		if err := s.cfg.SetRegistration(config.Registration{Node: num, Password: password, Server: server}); err != nil {
			log.Printf("set registration %s: %v", num, err)
			s.renderNodeEditErrorReq(w, r, num, "Could not save registration: "+err.Error())
			return
		}
	}

	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

func (s *Server) renderNodeEditErrorReq(w http.ResponseWriter, r *http.Request, num, msg string) {
	data, err := s.loadNodeEditData(num, flash("error", msg))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_edit.html", data)
}

type nodeNewData struct {
	pageData
	Number    string
	RxChannel string
	Duplex    string
}

func (s *Server) handleNodeNewForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "node_new.html", nodeNewData{pageData: pageData{LoggedIn: true}})
}

func (s *Server) handleNodeNewSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	num := strings.TrimSpace(r.FormValue("number"))
	rxchannelChoice := r.FormValue("rxchannel") // "Local/pseudo", "SimpleUSB", or "Radio"
	duplex := r.FormValue("duplex")

	renderErr := func(msg string) {
		data := nodeNewData{pageData: flash("error", msg), Number: num, RxChannel: rxchannelChoice, Duplex: duplex}
		s.render(w, "node_new.html", data)
	}

	if !config.ValidNodeNumber(num) {
		renderErr("Node number must be numeric, up to 6 digits")
		return
	}
	if !validDuplex[duplex] {
		renderErr("Duplex must be 0-4")
		return
	}

	var rxchannel string
	switch rxchannelChoice {
	case "Local/pseudo", "":
		rxchannel = "Local/pseudo"
	case "SimpleUSB", "Radio":
		rxchannel = rxchannelChoice + "/" + num
	default:
		renderErr("Unrecognized radio interface selection")
		return
	}

	if err := s.cfg.CreateNode(num, rxchannel, duplex); err != nil {
		log.Printf("create node %s: %v", num, err)
		renderErr("Could not create node: " + err.Error())
		return
	}

	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

func (s *Server) handleNodeDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.cfg.DeleteNode(num); err != nil {
		log.Printf("delete node %s: %v", num, err)
		data, loadErr := s.loadNodeEditData(num, flash("error", "Could not delete node: "+err.Error()))
		if loadErr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.render(w, "node_edit.html", data)
		return
	}
	http.Redirect(w, r, "/nodes", http.StatusSeeOther)
}
