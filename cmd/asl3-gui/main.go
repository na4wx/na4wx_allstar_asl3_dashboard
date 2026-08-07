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
	sa818Port := flag.String("sa818-port", "", "serial device path for the SA818/DRA818 radio module, e.g. /dev/ttyUSB0 (default: auto-detect /dev/serial0 then /dev/ttyUSB0, matching ASL3's own sa818 tool)")
	sa818StatePath := flag.String("sa818-state-file", "/etc/asl3-gui/sa818-last.json", "path to store the last settings sent to the SA818/DRA818 module")
	soundsCustomDir := flag.String("sounds-custom-dir", "/etc/asterisk/local", "directory for the operator's own uploadable sound files (station ID, custom courtesy tones)")
	soundsStockDir := flag.String("sounds-stock-dir", "/var/lib/asterisk/sounds/rpt", "app_rpt's own built-in prompt library, offered as read-only pick-list options (e.g. \"rpt/callproceeding\") -- never written to")
	soxTool := flag.String("sox-tool", "sox", "path to the sox audio tool, or bare name if it's on PATH (used to transcode an uploaded sound file to the 8kHz mono format app_rpt expects)")
	ttsTool := flag.String("tts-tool", "piper", "path to the Piper text-to-speech binary, or bare name if it's on PATH (used by the \"Create from text\" sound generator); falls back to espeak-ng if Piper can't run on this system")
	ttsVoicesDir := flag.String("tts-voices-dir", "/etc/asl3-gui/piper-voices", "directory holding downloaded Piper voice models (.onnx files); empty until at least one is downloaded, e.g. via `python3 -m piper.download_voices en_US-lessac-medium`, more at https://huggingface.co/rhasspy/piper-voices")
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

	srv, err := server.New(authMgr, templatesFS, staticFS, *asteriskDir, *asteriskBin, *sa818Port, *sa818StatePath, *soundsCustomDir, *soundsStockDir, *soxTool, *ttsTool, *ttsVoicesDir, *wifiHotspotSSID, *wifiHotspotPassword, wifiDashboardPort, *wifiHotspotEnabled)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	srv.StartWiFiWatchdog(context.Background())

	log.Printf("asl3-gui listening on %s", *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
