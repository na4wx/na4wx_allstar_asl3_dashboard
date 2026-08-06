// Package server wires HTTP routes to the auth package and renders the
// embedded templates. This is the ASL3 port's scaffold: auth, template
// rendering, and session handling are copied from the HamVoIP app
// essentially unchanged (confirmed platform-agnostic), since none of it
// touches Asterisk config or the OS at all. Node/System/Config pages
// come in later phases once internal/config (ASL3's template-inheritance
// config I/O) exists -- see /Users/jwebb/.claude/plans/snazzy-honking-hennessy.md.
package server

import (
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"time"

	"hamvoipconfiggui-asl3/internal/auth"
	"hamvoipconfiggui-asl3/internal/config"
)

const sessionCookie = "hamvoip_gui_session"

type Server struct {
	auth *auth.Manager
	cfg  *config.Store
	tmpl map[string]*template.Template
	mux  *http.ServeMux
}

// asteriskDir overrides where Asterisk's own config files
// (rpt.conf/usbradio.conf/simpleusb.conf) are read from; pass "" to use
// the real ASL3 default (/etc/asterisk).
func New(authMgr *auth.Manager, templatesFS, staticFS fs.FS, asteriskDir string) (*Server, error) {
	s := &Server{auth: authMgr, cfg: &config.Store{Dir: asteriskDir}, mux: http.NewServeMux()}

	tmpl, err := s.parseTemplates(templatesFS)
	if err != nil {
		return nil, err
	}
	s.tmpl = tmpl

	s.routes(staticFS)
	return s, nil
}

// parseTemplates parses every page template up front, same set as the
// HamVoIP app -- the template *files* are already fully ported (see
// web/templates), even though most pages' Go handlers/data don't exist
// yet. Parsing succeeds regardless: it only needs the template syntax
// to be valid, not for any Go data to exist yet. Execution (render) is
// what would fail for a page whose handler isn't wired up yet, so only
// routes with a real handler are registered in routes() below.
func (s *Server) parseTemplates(templatesFS fs.FS) (map[string]*template.Template, error) {
	pages := []string{"setup.html", "login.html", "home.html", "stats.html", "nodes_index.html", "node_view.html", "node_new.html", "node_form.html", "config.html", "system.html", "radio_form.html"}
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

	// Phase 2: read-only, for verifying internal/config's resolved values
	// against a real node before any write path is built (per the
	// approved plan). Node creation/editing is Phase 3.
	s.mux.HandleFunc("GET /nodes", s.requireAuth(s.handleNodesIndex))
	s.mux.HandleFunc("GET /nodes/{node}", s.requireAuth(s.handleNodeView))
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
	w.Write([]byte(`<!doctype html><html><head><title>ASL3 Dashboard</title></head><body style="font-family:sans-serif;padding:2rem"><h1>ASL3 Dashboard</h1><p>Scaffold running. Node management, System page, and Cloud Sync land in later phases.</p><p><a href="/nodes">View configured nodes</a> (read-only)</p><form method="post" action="/logout"><button type="submit">Log out</button></form></body></html>`))
}

// nodeRow is nodes_index.html's expected shape for one table row --
// matches the field paths that template already references (.Node.Number,
// .Node.RXChannel, .Callsign) so the ported template needs no changes to
// render this Phase 2 read-only view; Task #3 fills in Callsign and wires
// the edit link.
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
			Nodes    []nodeRow
			ReadOnly bool
		}{pageData: data, ReadOnly: true})
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
		Nodes    []nodeRow
		ReadOnly bool
	}{pageData: pageData{LoggedIn: true}, Nodes: rows, ReadOnly: true})
}

func (s *Server) handleNodeView(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_view.html", struct {
		pageData
		View *config.NodeView
	}{pageData: pageData{LoggedIn: true}, View: view})
}
