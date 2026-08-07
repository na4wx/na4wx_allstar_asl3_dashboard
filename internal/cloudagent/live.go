package cloudagent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"hamvoipconfiggui-asl3/internal/rptstatus"
	"hamvoipconfiggui-asl3/internal/system"
)

// liveWatchPollInterval/liveWatchFetchTimeout: short enough to catch a
// momentary keyup, expensive enough that it must only run while someone
// is actually watching.
const (
	liveWatchPollInterval = 2 * time.Second
	liveWatchFetchTimeout = 8 * time.Second
	nodeLiveEvent         = "nodeLive"
)

// liveNodeState is the moment-to-moment state pushed for a watched node.
type liveNodeState struct {
	Receiving     bool                      `json:"receiving"`
	SignalOnInput string                    `json:"signalOnInput"`
	Connected     []rptstatus.ConnectedNode `json:"connected"`
}

// validNodeNumber reports whether number is safe to interpolate into an
// Asterisk CLI command string built by plain string concatenation
// (every AsteriskRX caller in this package does exactly that, with no
// shell involved but also no escaping) -- true only if it's an
// existing, digits-only rpt.conf node section. A crafted value (stray
// whitespace, control characters, anything non-numeric) can never match
// a real section, since sections are only ever created via
// config.Store.CreateNode's own digits-only validation, so this doubles
// as the "does this node even exist" check every other node-scoped
// action already gets for free by routing through LoadNode -- these
// call sites (here, dtmf, stats) build raw CLI strings directly instead,
// so they need the same check made explicit.
func (a *Agent) validNodeNumber(number string) bool {
	_, err := a.store.LoadNode(number)
	return err == nil
}

// snapshotLiveNode reads a node's live state in one pass.
func (a *Agent) snapshotLiveNode(ctx context.Context, number string) liveNodeState {
	// Connected starts as a non-nil empty slice, not the zero value --
	// a nil slice encodes to JSON as null, and the cloud client renders
	// this straight off the wire expecting an array it can always call
	// .length/.map on. A node with nothing currently connected is an
	// extremely common, everyday state, not an edge case, so this must
	// never come across as null.
	live := liveNodeState{Connected: []rptstatus.ConnectedNode{}}
	if !a.validNodeNumber(number) {
		return live
	}
	if out, err := system.AsteriskRX(ctx, a.asteriskBin, "rpt stats "+number); err == nil {
		fields, _ := rptstatus.ParseRptStats(out)
		live.Receiving = rptstatus.NodeReceiving(fields)
		live.SignalOnInput = fields.Value("Signal on input")
	}

	nodesOut, _ := system.AsteriskRX(ctx, a.asteriskBin, "rpt nodes "+number)
	for _, num := range rptstatus.ParseConnectedNodes(nodesOut) {
		live.Connected = append(live.Connected, rptstatus.DescribeNode(nil, num))
	}
	if len(live.Connected) > 0 {
		if out, err := system.AsteriskRX(ctx, a.asteriskBin, "rpt show variables "+number); err == nil {
			keyed := rptstatus.KeyedNodes(out)
			for i := range live.Connected {
				if keyed[live.Connected[i].Number] {
					live.Connected[i].Keyed = true
				}
			}
		}
	}
	return live
}

// liveWatches tracks which nodes currently have an active poller for
// the current connection. watch/unwatch are idempotent -- watching an
// already-watched node, or unwatching one that isn't watched, is a
// no-op, so this stays correct even if the cloud side's own dedup ever
// has a bug.
type liveWatches struct {
	mu    sync.Mutex
	stops map[string]chan struct{}
}

func newLiveWatches() *liveWatches {
	return &liveWatches{stops: make(map[string]chan struct{})}
}

// watch starts polling node if it isn't already being watched on this
// connection.
func (lw *liveWatches) watch(ctx context.Context, a *Agent, conn *websocket.Conn, node string) {
	if node == "" || !a.validNodeNumber(node) {
		return
	}
	lw.mu.Lock()
	if _, already := lw.stops[node]; already {
		lw.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	lw.stops[node] = stop
	lw.mu.Unlock()

	go lw.poll(ctx, a, conn, node, stop)
}

// unwatch stops polling node, if it was being watched.
func (lw *liveWatches) unwatch(node string) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if stop, ok := lw.stops[node]; ok {
		close(stop)
		delete(lw.stops, node)
	}
}

// stopAll tears down every active watch -- called once the connection
// itself ends, so a reconnect always starts with a clean slate rather
// than leaking pollers from a session that's already gone. The cloud
// side re-sends "watch" for whatever's still actually open in a
// browser once the new connection's hello succeeds.
func (lw *liveWatches) stopAll() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	for node, stop := range lw.stops {
		close(stop)
		delete(lw.stops, node)
	}
}

func (lw *liveWatches) poll(ctx context.Context, a *Agent, conn *websocket.Conn, node string, stop chan struct{}) {
	ticker := time.NewTicker(liveWatchPollInterval)
	defer ticker.Stop()

	send := func() {
		fetchCtx, cancel := context.WithTimeout(ctx, liveWatchFetchTimeout)
		live := a.snapshotLiveNode(fetchCtx, node)
		cancel()
		data, err := json.Marshal(live)
		if err != nil {
			return
		}
		writeCtx, cancel2 := context.WithTimeout(ctx, helloTimeout)
		defer cancel2()
		_ = wsjson.Write(writeCtx, conn, envelope{Type: typeEvent, Event: nodeLiveEvent, Node: node, Data: data})
	}

	send()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}
