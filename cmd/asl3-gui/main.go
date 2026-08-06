// Command asl3-gui serves a browser-based configuration UI for an
// AllStarLink 3 (ASL3) node. Scaffold stage: auth + template rendering
// only, see internal/server's own doc comment and
// /Users/jwebb/.claude/plans/snazzy-honking-hennessy.md for the phased
// plan this is built against.
package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"hamvoipconfiggui-asl3/internal/auth"
	"hamvoipconfiggui-asl3/internal/server"
	"hamvoipconfiggui-asl3/web"
)

func main() {
	addr := flag.String("addr", ":8089", "listen address")
	authFile := flag.String("auth-file", "/etc/asl3-gui/auth.json", "path to store admin credentials")
	flag.Parse()

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

	srv, err := server.New(authMgr, templatesFS, staticFS)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	log.Printf("asl3-gui listening on %s", *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
