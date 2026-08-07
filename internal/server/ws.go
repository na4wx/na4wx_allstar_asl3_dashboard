// The /ws endpoint: one WebSocket connection per browser tab that
// replaces both full-page navigation and the old per-node SSE live
// stream, so the app never has to do a real browser navigation for
// anything a logged-in operator does.
//
// It works by replaying a link click or form submission as a synthetic
// *http.Request through the existing, completely unmodified handler
// stack (s.mux), the same way a real browser request would arrive --
// every handler in this app already follows one of two shapes (render
// the page in place with a flash on error, or http.Redirect to a fresh
// GET on success), so capturing that response and, if it's a redirect,
// following it with one more synthetic GET reproduces exactly what a
// real navigation would have shown. The rendered HTML is sent back to
// the browser, which swaps it into <main> instead of navigating -- see
// web/static/js/app.js's own doc comments for the client half.
//
// Every form keeps its real action and every link keeps its real href,
// so if JS or this connection is ever unavailable the app still works
// exactly as it does with this endpoint entirely absent: a plain
// POST/redirect and a plain GET. This endpoint is a transport
// optimization on top of that, never a replacement for it.
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// wsReplayTimeout bounds one full nav/submit replay, including the
// single redirect hop it may follow -- generous enough to cover the
// slowest handler this app has (WiFi scan/connect and TTS synthesis
// each bound their own work to 30s -- see internal/wifi/nmcli.go and
// internal/tts/tts.go's own timeouts) with margin for a second,
// normally-fast render pass.
const wsReplayTimeout = 60 * time.Second

// wsWriteTimeout bounds one write onto an already-open connection --
// short, since these are small JSON payloads, not a fresh handshake.
const wsWriteTimeout = 10 * time.Second

// wsPingInterval keeps the connection alive through proxies/load
// balancers that treat an idle socket as dead -- same value and reason
// as the old SSE endpoint's own keepalive comment (see git history of
// this file).
const wsPingInterval = 25 * time.Second

// wsMessage is the one shape carried over /ws in both directions --
// mirrors internal/cloudagent/protocol.go's envelope's own
// one-struct-many-optional-fields convention, for the same reason
// (trivial encode/decode on both ends of a small, hand-written
// protocol).
type wsMessage struct {
	Type string `json:"type"`

	// nav / submit (browser -> server): replay a link click or form
	// submission through the existing HTTP handlers instead of letting
	// the browser navigate. Method is "GET" for nav, "GET" or "POST" for
	// submit (this app has no PUT/DELETE/PATCH routes). Body is the
	// application/x-www-form-urlencoded form body for a POST submit.
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
	Body   string `json:"body,omitempty"`

	// page (server -> browser): the result of a nav/submit replay -- the
	// full rendered HTML of whichever page the browser should now be
	// showing, following one redirect hop server-side exactly like the
	// handler's own success path would via a real browser navigation.
	HTML string `json:"html,omitempty"`

	// subscribeLive / unsubscribeLive (browser -> server): start/stop
	// forwarding one node's live status (see liveHub in live.go) onto
	// this connection. live / history (server -> browser): the
	// forwarded events themselves -- same payload shape the old SSE
	// stream sent, just multiplexed onto this shared connection instead
	// of a dedicated per-node stream.
	Node string          `json:"node,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// wsConn tracks one browser tab's live-status subscriptions, so they
// can all be torn down together when the connection ends. cookie is the
// browser's own real session cookie header, captured once at upgrade
// time and replayed on every synthetic request so every
// requireAuth-wrapped handler downstream authenticates exactly as it
// would for a real request.
type wsConn struct {
	server *Server
	conn   *websocket.Conn
	cookie string

	mu   sync.Mutex
	live map[string]func() // node -> unsubscribe
}

// handleWS upgrades the connection and services it until the browser
// disconnects or a read fails. Registered behind requireAuth (same as
// every other page), so by the time this runs the session cookie has
// already been validated once; replayed requests carry that same cookie
// so nested requireAuth checks on individual routes pass too.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	wc := &wsConn{server: s, conn: conn, cookie: r.Header.Get("Cookie"), live: map[string]func(){}}
	defer wc.stopAllLive()

	ctx := r.Context()
	go wc.pingLoop(ctx)

	for {
		var msg wsMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}
		switch msg.Type {
		case "nav":
			wc.replay(ctx, http.MethodGet, msg.URL, "")
		case "submit":
			method := msg.Method
			if method == "" {
				method = http.MethodPost
			}
			wc.replay(ctx, method, msg.URL, msg.Body)
		case "subscribeLive":
			if msg.Node != "" {
				wc.subscribeLive(msg.Node)
			}
		case "unsubscribeLive":
			if msg.Node != "" {
				wc.unsubscribeLive(msg.Node)
			}
		}
	}
}

// pingLoop sends a WebSocket ping on wsPingInterval until ctx is done or
// a ping fails (a dead connection the read loop hasn't noticed yet --
// returning here lets that failure surface without duplicating
// connection-teardown logic; the read loop's next wsjson.Read will error
// out once the underlying conn is actually closed).
func (wc *wsConn) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := wc.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// replay runs one nav/submit request and writes its result back as a
// "page" message. Errors from the replay itself (a handler panicking
// would be a bug, not something to relay) aren't expected; a write
// failure here just means the connection is already going away, which
// the read loop's own next iteration will discover and clean up.
//
// URL is only included when it's actually a real, independently
// GET-able page: every nav is (it's always a GET already), and a submit
// is too if the handler redirected -- but a handler that renders
// in-place without redirecting (e.g. the DTMF sender, which re-renders
// node_edit.html directly at its own POST-only /dtmf path -- see
// commands.go's handleNodeSendDTMF) never had a navigable URL of its
// own to begin with. Sending that POST path back as "the page's URL"
// would leave the browser's address bar pointing at a route with no GET
// handler, breaking reload/bookmark/back. Omitting URL here tells the
// client to swap the content in place without touching the address bar
// at all, which is correct either way: the browser is still legitimately
// showing whatever page it was already on.
func (wc *wsConn) replay(ctx context.Context, method, url, body string) {
	replayCtx, cancel := context.WithTimeout(ctx, wsReplayTimeout)
	html, finalURL, redirected := wc.server.replayRequest(replayCtx, method, url, body, wc.cookie)
	cancel()

	msg := wsMessage{Type: "page", HTML: html}
	if method == http.MethodGet || redirected {
		msg.URL = finalURL
	}
	writeCtx, writeCancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer writeCancel()
	_ = wsjson.Write(writeCtx, wc.conn, msg)
}

// replayOne builds one synthetic request for method+url(+body), carrying
// cookie, and dispatches it through the existing handler stack -- no
// different from a real request arriving over HTTP except that its
// response is captured in memory instead of written to a real
// connection.
func (s *Server) replayOne(ctx context.Context, method, url, body, cookie string) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, url, bodyReader).WithContext(ctx)
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

// replayRequest is replayOne plus following at most one redirect hop,
// matching every handler's own render-in-place-on-error/redirect-on-
// success convention (see this file's own doc comment). Returns the
// final rendered HTML, the URL it corresponds to, and whether a redirect
// was actually followed -- callers use that last part to decide whether
// finalURL is a real, independently GET-able page (see replay's own doc
// comment for why that distinction matters).
func (s *Server) replayRequest(ctx context.Context, method, url, body, cookie string) (html, finalURL string, redirected bool) {
	rec := s.replayOne(ctx, method, url, body, cookie)
	finalURL = url
	if loc := rec.Header().Get("Location"); rec.Code >= 300 && rec.Code < 400 && loc != "" {
		rec = s.replayOne(ctx, http.MethodGet, loc, "", cookie)
		finalURL = loc
		redirected = true
	}
	return rec.Body.String(), finalURL, redirected
}

// subscribeLive starts forwarding node's live status (see liveHub) onto
// this connection. A no-op if already subscribed, so a client resending
// the same subscribe (e.g. after re-scanning the DOM post-swap) can't
// leak a second forwarder.
func (wc *wsConn) subscribeLive(node string) {
	wc.mu.Lock()
	if _, ok := wc.live[node]; ok {
		wc.mu.Unlock()
		return
	}
	ch, unsubscribe := wc.server.live.subscribe(node)
	wc.live[node] = unsubscribe
	wc.mu.Unlock()

	// Immediate snapshot so a freshly-subscribed card isn't blank until
	// the shared poller's next tick happens to broadcast a change (it
	// only broadcasts on a real change relative to its own dedup state,
	// which a brand new subscriber has never seen) -- same reasoning the
	// old SSE endpoint's own doc comment had for sending one before
	// entering its event loop. Run in its own goroutine so a slow
	// snapshot (a few `asterisk -rx` calls) can't stall the read loop
	// from handling the browser's next message.
	go wc.sendSnapshot(node)
	go wc.forwardLive(node, ch)
}

func (wc *wsConn) sendSnapshot(node string) {
	snapshotCtx, cancel := context.WithTimeout(context.Background(), liveFetchTimeout)
	live, historyHTML := wc.server.snapshotNode(snapshotCtx, node)
	cancel()

	writeCtx, wcancel := context.WithTimeout(context.Background(), wsWriteTimeout)
	defer wcancel()
	if b, err := json.Marshal(live); err == nil {
		_ = wsjson.Write(writeCtx, wc.conn, wsMessage{Type: "live", Node: node, Data: b})
	}
	if b, err := json.Marshal(historyHTML); err == nil {
		_ = wsjson.Write(writeCtx, wc.conn, wsMessage{Type: "history", Node: node, Data: b})
	}
}

func (wc *wsConn) unsubscribeLive(node string) {
	wc.mu.Lock()
	unsubscribe, ok := wc.live[node]
	if ok {
		delete(wc.live, node)
	}
	wc.mu.Unlock()
	if ok {
		unsubscribe()
	}
}

// stopAllLive tears down every active live subscription -- called once
// when the connection ends, so a browser tab closing (or reloading)
// never leaves a poller running for a subscriber that's gone.
func (wc *wsConn) stopAllLive() {
	wc.mu.Lock()
	live := wc.live
	wc.live = nil
	wc.mu.Unlock()
	for _, unsubscribe := range live {
		unsubscribe()
	}
}

// forwardLive relays one node's liveHub events onto this connection
// until ch is closed (unsubscribeLive, or stopAllLive on teardown).
// Safe to run concurrently with the read loop's own replies -- coder/
// websocket's Conn.Write (which wsjson.Write wraps) is documented safe
// for concurrent callers, the same assumption internal/cloudagent's own
// client.go already relies on (its heartbeatLoop and per-call replies
// write from separate goroutines too).
func (wc *wsConn) forwardLive(node string, ch <-chan liveMsg) {
	for msg := range ch {
		writeCtx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		err := wsjson.Write(writeCtx, wc.conn, wsMessage{Type: msg.event, Node: node, Data: msg.data})
		cancel()
		if err != nil {
			return
		}
	}
}
