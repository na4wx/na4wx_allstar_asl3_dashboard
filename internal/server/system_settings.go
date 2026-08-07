// System page: hostname, Asterisk restart/reboot, WiFi (scan/connect,
// plus the automatic hotspot-fallback watchdog -- see internal/wifi's
// package doc), and Cloud Sync (see cloudsettings.go and
// internal/cloudagent's package doc).
//
// Deliberately NOT ported from the HamVoIP app's own system_settings.go:
// static-IP editing (HamVoIP writes dhcpcd.conf directly; ASL3 uses
// NetworkManager/netplan instead, and ASL3's own Cockpit already
// provides a web UI for that on port 9090 -- see the project plan), and
// the flat RadioDevices/EmptyRadioFiles model (doesn't map to ASL3's
// per-node tune stanzas in usbradio.conf/simpleusb.conf -- see
// internal/config).
//
// SA818/DRA818 radio-module programming lives on the node edit page
// instead (see sa818.go's handleNodeSA818Apply) -- it's this node's own
// physical radio hardware, not a system-wide setting, and having it here
// as a disconnected card left CTCSS configurable in two unrelated places
// with nothing explaining they were related.
package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"hamvoipconfiggui-asl3/internal/system"
	"hamvoipconfiggui-asl3/internal/wifi"
)

type systemPageData struct {
	pageData
	Hostname string

	WiFiAvailable     bool
	WiFiBackendName   string
	WiFiStatus        wifi.Status
	WiFiStatusError   string
	WiFiNetworks      []wifi.Network
	WiFiHotspotSSID   string
	WiFiKnownNetworks []string

	// Cloud Sync -- see populateSystemCloud.
	CloudURL                string
	CloudAPIKey             string
	CloudEnabled            bool
	CloudAllowRemoteReboot  bool
	CloudAllowRawConfigEdit bool
	CloudLastConnected      string
}

func (s *Server) handleSystemPage(w http.ResponseWriter, r *http.Request) {
	s.renderSystemPage(w, r, pageData{LoggedIn: true})
}

func (s *Server) renderSystemPage(w http.ResponseWriter, r *http.Request, pd pageData) {
	s.renderSystemPageWithNetworks(w, r, pd, nil)
}

// renderSystemPageWithNetworks is renderSystemPage's full body -- split
// out so handleSystemWiFiScan can inject freshly scanned results into
// one specific render without a new persistence layer for what's
// inherently ephemeral data (see systemPageData.WiFiNetworks's own doc
// comment). networks is nil for every other caller.
func (s *Server) renderSystemPageWithNetworks(w http.ResponseWriter, r *http.Request, pd pageData, networks []wifi.Network) {
	ctx := r.Context()

	hostname, _ := system.Hostname(ctx)

	data := systemPageData{
		pageData: pd,
		Hostname: hostname,
	}

	s.populateSystemWiFi(ctx, &data)
	if networks != nil {
		data.WiFiNetworks = networks
	}
	s.populateSystemCloud(&data)

	s.render(w, "system.html", data)
}

// populateSystemWiFi fills systemPageData's Wireless fields from
// s.wifiManager. If no supported backend was detected (WiFiAvailable
// false), it stops there -- WiFiStatus stays its zero value, and the
// template shows a plain "not available" line instead of the rest of
// the card.
func (s *Server) populateSystemWiFi(ctx context.Context, data *systemPageData) {
	data.WiFiHotspotSSID = s.wifiManager.HotspotSSID()
	backend := s.wifiManager.Backend()
	data.WiFiBackendName = backend.Name()
	data.WiFiAvailable = backend.Name() != "unavailable"
	if !data.WiFiAvailable {
		return
	}
	st, err := backend.Status(ctx)
	if err != nil {
		data.WiFiStatusError = err.Error()
		return
	}
	data.WiFiStatus = st

	// Only fetched while the hotspot is active, since that's the one case
	// Scan is refused and an operator has no other way to see network
	// names this node already knows about. Best-effort: a failure here
	// just means an empty list, not a broken page -- the manual
	// SSID/password form still works regardless.
	if st.Mode == wifi.ModeHotspot {
		if known, err := backend.ListKnownNetworks(ctx); err == nil {
			data.WiFiKnownNetworks = known
		}
	}
}

func (s *Server) handleSystemHostname(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("hostname"))
	var pd pageData
	if err := system.SetHostname(r.Context(), name); err != nil {
		pd = flash("error", err.Error())
	} else {
		pd = flash("ok", "Hostname updated. Reboot for the change to fully take effect.")
	}
	s.renderSystemPage(w, r, pd)
}

func (s *Server) handleSystemPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := s.currentUsername(r)
	current := r.FormValue("current_password")
	next := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	if !s.auth.Verify(username, current) {
		s.renderSystemPage(w, r, flash("error", "Current password is incorrect"))
		return
	}
	if next != confirm {
		s.renderSystemPage(w, r, flash("error", "New passwords do not match"))
		return
	}
	if err := s.auth.SetCredentials(username, next); err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}
	// SetCredentials invalidates every session, including this request's;
	// send the user back through login rather than rendering a page that
	// requireAuth would immediately bounce anyway.
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSystemRestartAsterisk(w http.ResponseWriter, r *http.Request) {
	if err := system.AsteriskRestart(r.Context()); err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}
	s.renderSystemPage(w, r, flash("ok", "Asterisk restarted"))
}

func (s *Server) handleSystemReboot(w http.ResponseWriter, r *http.Request) {
	if err := system.Reboot(r.Context()); err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}
	// Best-effort: the process is very likely to be killed by the
	// shutdown before this ever reaches the client.
	s.renderSystemPage(w, r, flash("ok", "Rebooting now — this page will stop responding shortly."))
}

// handleSystemWiFiScan scans for nearby WiFi networks and re-renders the
// System page with the results -- see systemPageData.WiFiNetworks's own
// doc comment for why these aren't persisted anywhere.
func (s *Server) handleSystemWiFiScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	backend := s.wifiManager.Backend()
	// Scanning needs wlan0 in station mode -- doing that while the
	// fallback hotspot is running on the very same physical radio
	// conflicts directly with it. Confirmed on a real node (HamVoIP
	// side): scanning while joined only via the hotspot knocked the
	// operator's own device straight off it. Connect doesn't have this
	// problem -- it's *expected* to drop the hotspot as it hands wlan0
	// over to the new network.
	if st, err := backend.Status(ctx); err == nil && st.Mode == wifi.ModeHotspot {
		s.renderSystemPage(w, r, flash("error", "Can't scan while broadcasting the fallback hotspot — this node's WiFi radio is busy running it, and scanning would drop your connection to this page. Enter the network name and password directly below and click Connect instead; the hotspot will drop automatically once the new connection is confirmed."))
		return
	}

	networks, err := backend.Scan(ctx)
	if err != nil {
		s.renderSystemPage(w, r, flash("error", "Scan failed: "+err.Error()))
		return
	}
	s.renderSystemPageWithNetworks(w, r, flash("ok", fmt.Sprintf("Found %d network(s)", len(networks))), networks)
}

// handleSystemWiFiConnect connects wlan0 to the submitted network.
// password is intentionally never trimmed (a leading/trailing space is
// technically legal in a WPA passphrase) and never appears in any flash
// message, error string, or log -- only the SSID does.
func (s *Server) handleSystemWiFiConnect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ssid := strings.TrimSpace(r.FormValue("ssid"))
	password := r.FormValue("password")

	if err := wifi.ValidateSSID(ssid); err != nil {
		s.renderSystemPage(w, r, flash("error", err.Error()))
		return
	}
	if password != "" {
		if err := wifi.ValidatePSK(password); err != nil {
			s.renderSystemPage(w, r, flash("error", err.Error()))
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	// Manager.Connect (not Backend().Connect() directly) so the fallback
	// hotspot -- if it's the thing currently running on wlan0 -- gets
	// torn down properly first.
	if err := s.wifiManager.Connect(ctx, ssid, password); err != nil {
		s.renderSystemPage(w, r, flash("error", "Couldn't connect: "+err.Error()))
		return
	}
	s.renderSystemPage(w, r, flash("ok", `Connected to "`+ssid+`".`))
}
