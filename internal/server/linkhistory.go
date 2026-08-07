package server

import (
	"context"
	"strings"
	"sync"
	"time"

	"hamvoipconfiggui-asl3/internal/rptstatus"
	"hamvoipconfiggui-asl3/internal/system"
)

// linkHistoryLimit is how many past records are kept per node. Small on
// purpose: this is a "what just happened" glance for the home page, not
// a log -- Asterisk's own log file (shown on the System page) is where
// real history lives.
const linkHistoryLimit = 10

// linkHistoryInterval is how often each node's link state is sampled.
// Frequent enough to catch a short-lived connection, cheap enough to be
// irrelevant on a dedicated node Pi (one asterisk -rx per node per tick).
const linkHistoryInterval = 30 * time.Second

// linkSnapshotKey is the value used to decide whether a snapshot
// represents a real change from the previous one -- the set of connected
// nodes. Deliberately not the whole snapshot: "rpt lstats" includes
// running connection timers, so it differs on literally every poll and
// would otherwise push every real event out of the buffer within
// minutes.
func linkSnapshotKey(s rptstatus.LinkSnapshot) string { return strings.Join(s.Nodes, ",") }

// linkHistory keeps a small rolling record, per node, of how its
// connection state has changed over time. Safe for concurrent use: the
// background poller writes while page renders read.
type linkHistory struct {
	mu     sync.Mutex
	byNode map[string][]rptstatus.LinkSnapshot
}

func newLinkHistory() *linkHistory {
	return &linkHistory{byNode: make(map[string][]rptstatus.LinkSnapshot)}
}

// record parses a round of command output and appends it for node, but
// only when the set of connected nodes differs from the most recent
// record -- so the 10 retained records cover 10 real connect/disconnect
// events rather than the last five minutes of polling.
func (h *linkHistory) record(node, connectedOut, activityOut string) {
	snap := rptstatus.LinkSnapshot{
		At:           time.Now(),
		Nodes:        rptstatus.ParseConnectedNodes(connectedOut),
		ConnectedRaw: connectedOut,
		ActivityRaw:  activityOut,
	}
	snap.Headers, snap.Rows, snap.ActivityOK = rptstatus.ParseLstats(activityOut)

	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.byNode[node]
	if n := len(list); n > 0 && linkSnapshotKey(list[n-1]) == linkSnapshotKey(snap) {
		return
	}
	list = append(list, snap)
	if len(list) > linkHistoryLimit {
		list = list[len(list)-linkHistoryLimit:]
	}
	h.byNode[node] = list
}

// forNode returns node's records newest first, as a copy the caller can
// hold without holding the lock.
func (h *linkHistory) forNode(node string) []rptstatus.LinkSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.byNode[node]
	out := make([]rptstatus.LinkSnapshot, len(list))
	for i, snap := range list {
		out[len(list)-1-i] = snap
	}
	return out
}

// StartLinkHistoryPoller samples every configured node's link state on
// an interval, so the history reflects what actually happened rather
// than only what someone happened to have the page open for. Runs until
// ctx is cancelled. Call once, from main.
func (s *Server) StartLinkHistoryPoller(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(linkHistoryInterval)
		defer ticker.Stop()
		s.sampleLinkHistory(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sampleLinkHistory(ctx)
			}
		}
	}()
}

// sampleLinkHistory records one round of snapshots. Errors are ignored
// rather than logged per-tick: Asterisk being down is an expected,
// already-visible state (the home page's status pill says so), and
// logging it every 30 seconds would bury the log in noise.
func (s *Server) sampleLinkHistory(ctx context.Context) {
	numbers, err := s.cfg.ListNodes()
	if err != nil {
		return
	}
	for _, number := range numbers {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		connected, err := system.AsteriskRX(callCtx, s.asteriskBin, "rpt nodes "+number)
		if err != nil {
			cancel()
			continue
		}
		activity, _ := system.AsteriskRX(callCtx, s.asteriskBin, "rpt lstats "+number)
		cancel()
		s.history.record(number, connected, activity)
	}
}
