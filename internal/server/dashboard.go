// Home and Stats pages: a live-updating status board for every
// configured node (who's connected, who's transmitting, connection
// history) plus a one-click link/unlink shortcut -- ASL3's own
// Supermon/AllMon-style view, built on internal/rptstatus (app_rpt CLI
// output parsing, already ported unchanged) and the SSE push mechanism
// in live.go.
//
// Two things the original HamVoIP app's equivalent page had are
// deliberately left out here: a "missing radio device" health check
// with a one-click repair, and a "sync dialplan entries" bulk action.
// Both exist there to work around HamVoIP-specific failure modes (a
// shared, separately-named radio-device entity that can go missing;
// extensions.conf dialplan entries that need backfilling) that don't
// exist on ASL3 -- tuning lives directly on the node, and modules.conf
// is kept in sync automatically by internal/config's own
// syncModulesForRxChannel whenever a node's radio interface changes.
package server

import (
	"net/http"
	"strings"

	"hamvoipconfiggui-asl3/internal/config"
	"hamvoipconfiggui-asl3/internal/rptstatus"
	"hamvoipconfiggui-asl3/internal/system"
)

// nodeQuickStatus is one node's at-a-glance info on Home: who else is
// connected, app_rpt's own link-activity output, and live on-air state.
// The activity output is shown unparsed rather than reformatted into a
// "currently transmitting: yes/no" claim -- rpt lstats's exact columns
// vary by app_rpt version, and this app has no hardware to verify a
// parsed interpretation against.
type nodeQuickStatus struct {
	Number       string
	Connected    string
	ConnectedErr string
	Activity     string
	ActivityErr  string

	// The two history tables shown for this node, newest first -- see
	// rptstatus.BuildLinkTables. ActivityHeaders is taken from app_rpt's
	// own output rather than named here, so a different app_rpt version's
	// columns still render correctly.
	ConnectedHistory []rptstatus.ConnectedRecord
	ActivityHeaders  []string
	ActivityHistory  []rptstatus.ActivityRecord

	// Live state from "rpt stats". Receiving means someone is keying
	// this node's receiver right now; see rptstatus.NodeReceiving for
	// what that does and doesn't cover. StatsRaw is shown instead of the
	// table when the output didn't parse.
	Stats     rptstatus.StatFields
	StatsOK   bool
	StatsRaw  string
	StatsErr  string
	Receiving bool

	// NowConnected is the current connected-node list with callsigns --
	// the same data as the newest history row, surfaced separately so
	// the live card doesn't make the reader parse a table to answer
	// "who is on right now".
	NowConnected []rptstatus.ConnectedNode
}

type homePageData struct {
	pageData
	Nodes  []*config.NodeView
	Status system.Status
	Quick  []nodeQuickStatus

	// OwnerSubscriptionActive mirrors cloudagent.Agent's own field of
	// the same name -- only populated for home.html (see renderHome),
	// which swaps its "manage from anywhere" promo card for a thank-you
	// once this node's cloud account has an active paid subscription.
	// Left at its false zero value for stats.html, which doesn't read
	// it.
	OwnerSubscriptionActive bool
}

// handleHome is the sole landing page: one card per configured node with
// live on-air/connected status and quick link/unlink. Detailed stats
// and connection history live on the separate Stats page.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.renderHome(w, r, pageData{LoggedIn: true})
}

func (s *Server) renderHome(w http.ResponseWriter, r *http.Request, pd pageData) {
	nodes, status, quick, err := s.gatherNodeStatuses(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "home.html", homePageData{
		pageData:                pd,
		Nodes:                   nodes,
		Status:                  status,
		Quick:                   quick,
		OwnerSubscriptionActive: s.cloudAgent.OwnerSubscriptionActive(),
	})
}

// handleStats is the detailed-status page: overall system status plus,
// per node, the full rpt-stats field table and connection history --
// everything Home doesn't show, to keep Home to just the live glance
// and the link/unlink action.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.renderStats(w, r, pageData{LoggedIn: true})
}

func (s *Server) renderStats(w http.ResponseWriter, r *http.Request, pd pageData) {
	nodes, status, quick, err := s.gatherNodeStatuses(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "stats.html", homePageData{
		pageData: pd,
		Nodes:    nodes,
		Status:   status,
		Quick:    quick,
	})
}

// gatherNodeStatuses reads everything Home and Stats both need: every
// configured node, overall system status, and each node's quick status
// (connected nodes, link activity, live stats, and connection history).
// Shared so the two pages can't drift on what a "current reading" means,
// and so this -- the most CLI-call-heavy read in the app -- is written
// once.
func (s *Server) gatherNodeStatuses(r *http.Request) ([]*config.NodeView, system.Status, []nodeQuickStatus, error) {
	numbers, err := s.cfg.ListNodes()
	if err != nil {
		return nil, system.Status{}, nil, err
	}
	var nodes []*config.NodeView
	for _, n := range numbers {
		view, err := s.cfg.LoadNode(n)
		if err != nil {
			continue // skip malformed entries rather than failing the whole page
		}
		nodes = append(nodes, view)
	}

	status := system.Snapshot(r.Context(), s.asteriskBin)

	var quick []nodeQuickStatus
	for _, view := range nodes {
		q := nodeQuickStatus{Number: view.Node}
		if out, err := system.AsteriskRX(r.Context(), s.asteriskBin, "rpt nodes "+view.Node); err != nil {
			q.ConnectedErr = err.Error()
		} else {
			q.Connected = out
		}
		if out, err := system.AsteriskRX(r.Context(), s.asteriskBin, "rpt lstats "+view.Node); err != nil {
			q.ActivityErr = err.Error()
		} else {
			q.Activity = out
		}
		// Fold this render's reading into the history too, so a change
		// that happened between polls still gets recorded rather than
		// waiting for the next tick.
		if q.ConnectedErr == "" {
			s.history.record(view.Node, q.Connected, q.Activity)
		}
		q.ConnectedHistory, q.ActivityHeaders, q.ActivityHistory = rptstatus.BuildLinkTables(s.nodes, s.history.forNode(view.Node))

		// Live state, for Home's "Right now" card and the Stats page's
		// detailed field table.
		if out, err := system.AsteriskRX(r.Context(), s.asteriskBin, "rpt stats "+view.Node); err != nil {
			q.StatsErr = err.Error()
		} else {
			q.StatsRaw = out
			q.Stats, q.StatsOK = rptstatus.ParseRptStats(out)
			q.Receiving = rptstatus.NodeReceiving(q.Stats)
		}
		for _, number := range rptstatus.ParseConnectedNodes(q.Connected) {
			q.NowConnected = append(q.NowConnected, rptstatus.DescribeNode(s.nodes, number))
		}
		// Mark which connected nodes are keying right now (RPT_ALINKS) --
		// the same live read the SSE stream uses, so the page-load
		// snapshot and the first pushed update agree.
		s.markKeyed(r.Context(), view.Node, q.NowConnected)

		quick = append(quick, q)
	}

	return nodes, status, quick, nil
}

// handleNodeLink sends a quick link ("*3<target>") or unlink
// ("*1<target>") touch-tone command from Home's quick actions -- the
// same underlying mechanism as the node page's touch-tone sender
// (asterisk -rx "rpt fun <node> <digits>"), scoped to just these two
// standard AllStarLink codes rather than an arbitrary typed sequence,
// since this is meant to be a one-click shortcut.
//
// It answers in JSON when the caller asks (the home page's fetch does),
// so the command can be sent without a full-page reload -- the live SSE
// stream then reflects the connection appearing or dropping. A plain
// form POST (no JS) still works and re-renders Home with a flash, so the
// action degrades gracefully.
func (s *Server) handleNodeLink(w http.ResponseWriter, r *http.Request) {
	number := r.PathValue("node")
	wantsJSON := strings.Contains(r.Header.Get("Accept"), "application/json")

	fail := func(msg string) {
		if wantsJSON {
			writeJSON(w, map[string]any{"ok": false, "message": msg})
			return
		}
		s.renderHome(w, r, flash("error", msg))
	}

	if err := r.ParseForm(); err != nil {
		fail("bad form")
		return
	}
	target := strings.TrimSpace(r.FormValue("target"))
	if target == "" {
		fail("Enter a node number to link or unlink")
		return
	}

	var digits string
	switch r.FormValue("action") {
	case "link":
		digits = "*3" + target
	case "unlink":
		digits = "*1" + target
	default:
		fail("Unknown action")
		return
	}

	out, err := system.AsteriskRX(r.Context(), s.asteriskBin, "rpt fun "+number+" "+digits)
	if err != nil {
		fail(err.Error())
		return
	}
	msg := "Sent " + digits + " to node " + number
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		msg += ": " + trimmed
	}
	if wantsJSON {
		writeJSON(w, map[string]any{"ok": true, "message": msg})
		return
	}
	s.renderHome(w, r, flash("ok", msg))
}
