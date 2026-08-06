// Command asl3-gui serves a browser-based configuration UI for an
// AllStarLink 3 (ASL3) node. See internal/server's own doc comment and
// /Users/jwebb/.claude/plans/snazzy-honking-hennessy.md for the phased
// plan this is built against.
package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"hamvoipconfiggui-asl3/internal/auth"
	"hamvoipconfiggui-asl3/internal/server"
	"hamvoipconfiggui-asl3/internal/wifi"
	"hamvoipconfiggui-asl3/web"
)

func main() {
	addr := flag.String("addr", ":8089", "listen address")
	authFile := flag.String("auth-file", "/etc/asl3-gui/auth.json", "path to store admin credentials")
	asteriskDir := flag.String("asterisk-dir", "", "override where Asterisk's own config files are read from (default: /etc/asterisk)")
	asteriskBin := flag.String("asterisk-bin", "asterisk", "path to the asterisk binary, or bare name if it's on PATH")
	sa818Tool := flag.String("sa818-tool", "sa818", "path to ASL3's own sa818 SA818/DRA818 radio module programmer, or bare name if it's on PATH (NOT HamVoIP's 818-prog, a different tool ASL3 doesn't ship)")
	sa818StatePath := flag.String("sa818-state-file", "/etc/asl3-gui/sa818-last.json", "path to store the last settings sent to the SA818/DRA818 module")
	wifiHotspotSSID := flag.String("wifi-hotspot-ssid", "ASL3 Dashboard", "SSID this node broadcasts as a fallback WiFi hotspot on wlan0 the moment it has no active network connection")
	wifiHotspotPassword := flag.String("wifi-hotspot-password", "", "password for the fallback WiFi hotspot above (WPA2, 8-63 characters); empty broadcasts it open")
	wifiHotspotEnabled := flag.Bool("wifi-hotspot-enabled", true, "automatically stand up the fallback WiFi hotspot on wlan0 when this node has no active network connection")
	flag.Parse()

	if err := wifi.ValidateSSID(*wifiHotspotSSID); err != nil {
		log.Fatalf("-wifi-hotspot-ssid: %v", err)
	}
	if *wifiHotspotPassword != "" {
		if err := wifi.ValidatePSK(*wifiHotspotPassword); err != nil {
			log.Fatalf("-wifi-hotspot-password: %v", err)
		}
	}
	_, wifiDashboardPort, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatalf("-addr: %v", err)
	}

	templatesFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		log.Fatalf("templates: %v", err)
	}
	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		log.Fatalf("static: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*authFile), 0700); err != nil {
		log.Fatalf("create auth dir: %v", err)
	}
	authMgr, err := auth.NewManager(*authFile)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	if !authMgr.Configured() {
		log.Printf("no admin account configured yet; visit http://<this-host>%s/setup to create one", *addr)
	}

	srv, err := server.New(authMgr, templatesFS, staticFS, *asteriskDir, *asteriskBin, *sa818Tool, *sa818StatePath, *wifiHotspotSSID, *wifiHotspotPassword, wifiDashboardPort, *wifiHotspotEnabled)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	srv.StartWiFiWatchdog(context.Background())

	log.Printf("asl3-gui listening on %s", *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
