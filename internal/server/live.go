package server

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"hamvoipconfiggui-asl3/internal/rptstatus"
	"hamvoipconfiggui-asl3/internal/system"
)

// livePollInterval is how often a node's live state is re-read while at
// least one browser tab is watching it (see ws.go's subscribeLive).
// Short, because the point of this stream is to catch someone keying up
// -- a PTT can last only a second or two. It costs a few `asterisk -rx`
// calls per tick, but only while a browser actually has the page open
// (the poller starts on the first subscriber and stops when the last
// one leaves).
const livePollInterval = 2 * time.Second

// liveFetchTimeout bounds one round of CLI reads.
const liveFetchTimeout = 8 * time.Second

// liveNodeState is the moment-to-moment state pushed to the "Right now"
// card: whether the local receiver is keyed, the raw "Signal on input"
// value (shown in the stats table, kept in sync with the pill), and the
// currently connected peers as an app_rpt "rpt lstats" table (see
// rptstatus.BuildLstatsRows) -- the same shape as the historical Link
// activity table below it, just live-updated instead of sampled, so the
// two read as one consistent AllMon-style view rather than a simpler
// live summary sitting above a richer history.
type liveNodeState struct {
	Receiving        bool                  `json:"receiving"`
	SignalOnInput    string                `json:"signalOnInput"`
	ConnectedHeaders []string              `json:"connectedHeaders"`
	ConnectedRows    []rptstatus.LstatsRow `json:"connectedRows"`
}

// snapshotNode reads everything the live stream pushes in one pass: the
// "Right now" state, and the two connection-history tables rendered to
// an HTML fragment. It's the single source for both, shared by the
// background poller and ws.go's immediate on-subscribe snapshot so they
// can't drift.
//
// It also records the reading into the rolling history (record is
// deduped on the connected set, so this is what makes the history table
// update live while someone is watching -- the same buffer the slower
// background poller fills while nobody is).
func (s *Server) snapshotNode(ctx context.Context, number string) (liveNodeState, string) {
	var live liveNodeState
	if out, err := system.AsteriskRX(ctx, s.asteriskBin, "rpt stats "+number); err == nil {
		fields, _ := rptstatus.ParseRptStats(out)
		live.Receiving = rptstatus.NodeReceiving(fields)
		live.SignalOnInput = fields.Value("Signal on input")
	}

	nodesOut, _ := system.AsteriskRX(ctx, s.asteriskBin, "rpt nodes "+number)
	activityOut, _ := system.AsteriskRX(ctx, s.asteriskBin, "rpt lstats "+number)
	s.history.record(number, nodesOut, activityOut)

	if headers, rows, ok := rptstatus.ParseLstats(activityOut); ok {
		keyed := s.keyedNodeSet(ctx, number, len(rows) > 0)
		live.ConnectedHeaders, live.ConnectedRows = rptstatus.BuildLstatsRows(s.nodes, headers, rows, keyed)
	}

	q := nodeQuickStatus{Number: number}
	q.ConnectedHistory, q.ActivityHeaders, q.ActivityHistory = rptstatus.BuildLinkTables(s.nodes, s.history.forNode(number))
	return live, s.renderHistoryFragment(q)
}

// renderHistoryFragment renders the node_history partial to a string, so
// the client can swap it into the history card without this app
// duplicating the table markup in JavaScript. Any render error yields an
// empty string, which the client treats as "no update" rather than
// blanking the card.
func (s *Server) renderHistoryFragment(q nodeQuickStatus) string {
	t := s.tmpl["home.html"]
	if t == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "node_history", q); err != nil {
		return ""
	}
	return buf.String()
}

// keyedNodeSet reads app_rpt's RPT_ALINKS to find which of number's
// currently connected peers are transmitting right now (see
// rptstatus.KeyedNodes). Skipped entirely when nothing is connected, so
// an idle node makes no extra CLI call -- the returned nil map is a safe
// "nothing keyed" value to index into either way.
func (s *Server) keyedNodeSet(ctx context.Context, number string, haveConnections bool) map[string]bool {
	if !haveConnections {
		return nil
	}
	out, err := system.AsteriskRX(ctx, s.asteriskBin, "rpt show variables "+number)
	if err != nil {
		return nil
	}
	return rptstatus.KeyedNodes(out)
}

// liveMsg is one named live-status event ("live" or "history") and its
// already-serialized data, forwarded onto a browser tab's /ws connection
// by ws.go's forwardLive.
type liveMsg struct {
	event string
	data  []byte
}

// liveHub fans out per-node live state to any number of connected
// browser tabs (see ws.go's subscribeLive/forwardLive). One background
// poller runs per node that has at least one subscriber; it broadcasts
// an event only when that part of the state actually changes, so an
// idle node produces no traffic.
type liveHub struct {
	server *Server

	mu       sync.Mutex
	channels map[string]map[chan liveMsg]struct{}
	stops    map[string]chan struct{}
}

func newLiveHub(s *Server) *liveHub {
	return &liveHub{
		server:   s,
		channels: make(map[string]map[chan liveMsg]struct{}),
		stops:    make(map[string]chan struct{}),
	}
}

// subscribe registers a listener for node and returns its channel plus
// an unsubscribe func. The first subscriber for a node starts its
// poller. The channel is buffered and lossy on the sending side (see
// broadcast), so a slow reader can't stall the poller or other clients.
func (h *liveHub) subscribe(node string) (<-chan liveMsg, func()) {
	ch := make(chan liveMsg, 8)
	h.mu.Lock()
	subs := h.channels[node]
	if subs == nil {
		subs = make(map[chan liveMsg]struct{})
		h.channels[node] = subs
		stop := make(chan struct{})
		h.stops[node] = stop
		go h.poll(node, stop)
	}
	subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() { h.unsubscribe(node, ch) }
}

func (h *liveHub) unsubscribe(node string, ch chan liveMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.channels[node]
	if subs == nil {
		return
	}
	if _, ok := subs[ch]; ok {
		delete(subs, ch)
		close(ch)
	}
	if len(subs) == 0 {
		delete(h.channels, node)
		if stop := h.stops[node]; stop != nil {
			close(stop)
			delete(h.stops, node)
		}
	}
}

// broadcast delivers msg to every subscriber of node, dropping it for any
// whose buffer is full rather than blocking -- a stalled client falls
// behind and catches up on the next change, never holding up the rest.
func (h *liveHub) broadcast(node string, msg liveMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.channels[node] {
		select {
		case ch <- msg:
		default:
		}
	}
}

// poll re-reads node's state on an interval and broadcasts the "live" and
// "history" events independently, each only when its own serialized form
// changes, until stop is closed (the last subscriber left).
//
// The two are deduped separately on purpose: live state changes on every
// keyup, but the history tables change only when the connected set does,
// so the heavier history fragment isn't re-sent (or re-rendered by the
// browser) just because someone keyed up.
func (h *liveHub) poll(node string, stop chan struct{}) {
	ticker := time.NewTicker(livePollInterval)
	defer ticker.Stop()

	var lastLive, lastHistory string
	tick := func() {
		ctx, cancel := context.WithTimeout(context.Background(), liveFetchTimeout)
		live, historyHTML := h.server.snapshotNode(ctx, node)
		cancel()

		if b, err := json.Marshal(live); err == nil && string(b) != lastLive {
			lastLive = string(b)
			h.broadcast(node, liveMsg{event: "live", data: b})
		}
		if b, err := json.Marshal(historyHTML); err == nil && string(b) != lastHistory {
			lastHistory = string(b)
			h.broadcast(node, liveMsg{event: "history", data: b})
		}
	}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			tick()
		}
	}
}
