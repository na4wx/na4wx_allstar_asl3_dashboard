package rptstatus

import (
	"strconv"
	"strings"
	"time"

	"hamvoipconfiggui-asl3/internal/nodedb"
)

// LinkSnapshot is one recorded moment in a node's connection history,
// holding both commands' output parsed into table form plus the raw
// text as a fallback for output this app doesn't recognize.
type LinkSnapshot struct {
	At time.Time

	// Connected nodes, from "rpt nodes".
	Nodes        []string
	ConnectedRaw string

	// Link activity, from "rpt lstats". ActivityOK is false when the
	// output had no recognizable header/separator pair, in which case
	// only ActivityRaw is meaningful.
	Headers     []string
	Rows        [][]string
	ActivityRaw string
	ActivityOK  bool
}

// ConnectedNode is one node in a record, paired with whatever the node
// directory knows about it. Callsign is empty for a node that isn't in
// the directory (or when no directory has been downloaded), in which
// case the UI shows the bare number.
type ConnectedNode struct {
	Number   string `json:"number"`
	Callsign string `json:"callsign"`
	Detail   string `json:"detail"`

	// Keyed is set only on the live "connected right now" list, when
	// app_rpt's RPT_ALINKS reports this adjacent node as transmitting
	// (see KeyedNodes). It is never set on historical records, which
	// have no live keyed state to report.
	Keyed bool `json:"keyed"`
}

// ConnectedRecord is one row of the home page's "Connected nodes"
// history table.
type ConnectedRecord struct {
	When  string
	Ago   string
	Nodes []ConnectedNode
}

// ActivityRecord is one row of the "Link activity" history table. Each
// sampled moment contributes one row per connected peer, so the table
// reads chronologically; Empty marks a moment when nothing was linked,
// which is itself the meaningful event when a link drops.
type ActivityRecord struct {
	When   string
	Ago    string
	Fields []string
	Empty  bool
}

// Directory is the lookup BuildLinkTables uses to turn node numbers into
// callsigns. Satisfied by *nodedb.Store; an interface so the table
// building can be tested without one.
type Directory interface {
	Lookup(number string) (nodedb.Entry, bool)
}

// whenAgo renders a record time as an absolute clock time plus how long
// ago it was, e.g. "15:04:05" / "4m ago".
func whenAgo(at time.Time) (string, string) {
	d := time.Since(at).Round(time.Second)
	var ago string
	switch {
	case d < 5*time.Second:
		ago = "just now"
	case d < time.Minute:
		ago = strconv.Itoa(int(d.Seconds())) + "s ago"
	case d < time.Hour:
		ago = strconv.Itoa(int(d.Minutes())) + "m ago"
	default:
		ago = strconv.Itoa(int(d.Hours())) + "h ago"
	}
	return at.Format("Jan 2, 15:04:05"), ago
}

// DescribeNode pairs a node number with its directory entry. Detail is
// the description and location joined for a tooltip, skipping either if
// blank — plenty of real entries have an empty description (node 52829's
// own is empty in the live database).
func DescribeNode(dir Directory, number string) ConnectedNode {
	n := ConnectedNode{Number: number}
	if dir == nil {
		return n
	}
	e, ok := dir.Lookup(number)
	if !ok {
		return n
	}
	n.Callsign = e.Label()
	var parts []string
	if e.Description != "" {
		parts = append(parts, e.Description)
	}
	if e.Location != "" {
		parts = append(parts, e.Location)
	}
	n.Detail = strings.Join(parts, " · ")
	return n
}

// LstatsRow is one connected peer's row from "rpt lstats", with a
// directory-looked-up Callsign inserted right after the first column
// (every real app_rpt version puts the peer's node number there --
// confirmed in this package's own tests against real output) -- the
// same per-row shape BuildLinkTables' own Activity table already uses,
// factored out so the live "Connected right now" card (see internal/
// server/live.go) can't drift from it. Keyed is only meaningful for a
// live snapshot (see KeyedNodes); a historical row has no live keyed
// state to report, so BuildLinkTables never sets it.
type LstatsRow struct {
	Fields []string `json:"fields"`
	Keyed  bool     `json:"keyed"`
}

// BuildLstatsRows turns one "rpt lstats" reading (headers, rows -- see
// ParseLstats) into display-ready headers and rows: headers get
// DisplayHeader's sentence-casing plus the inserted "Callsign" column,
// each row gets its own Callsign lookup and, if keyed is non-nil, its
// live talking state (keyed[row[0]], the peer's node number). keyed may
// be nil (a historical reading has none), in which case every row's
// Keyed is simply false.
func BuildLstatsRows(dir Directory, headers []string, rows [][]string, keyed map[string]bool) (displayHeaders []string, out []LstatsRow) {
	for i, h := range headers {
		displayHeaders = append(displayHeaders, DisplayHeader(h))
		if i == 0 {
			displayHeaders = append(displayHeaders, "Callsign")
		}
	}
	for _, row := range rows {
		fields := make([]string, 0, len(row)+1)
		for i, v := range row {
			fields = append(fields, v)
			if i == 0 {
				fields = append(fields, DescribeNode(dir, v).Callsign)
			}
		}
		var isKeyed bool
		if len(row) > 0 {
			isKeyed = keyed[row[0]]
		}
		out = append(out, LstatsRow{Fields: fields, Keyed: isKeyed})
	}
	return displayHeaders, out
}

// BuildLinkTables flattens a node's records into the two tables the home
// page renders. Headers come from the most recent record that parsed
// cleanly, so the table keeps working even if a later sample was taken
// while Asterisk was mid-restart and returned something unrecognizable.
//
// The activity table gains a "Callsign" column immediately after
// app_rpt's own first column, which its header row identifies as the
// node number. If that lookup misses, the cell is simply blank — no
// attempt is made to guess a callsign from any other column, since what
// the remaining columns contain varies by app_rpt version.
func BuildLinkTables(dir Directory, snaps []LinkSnapshot) (connected []ConnectedRecord, headers []string, activity []ActivityRecord) {
	for _, snap := range snaps {
		when, ago := whenAgo(snap.At)

		rec := ConnectedRecord{When: when, Ago: ago}
		for _, number := range snap.Nodes {
			rec.Nodes = append(rec.Nodes, DescribeNode(dir, number))
		}
		connected = append(connected, rec)

		if headers == nil && snap.ActivityOK {
			for i, h := range snap.Headers {
				headers = append(headers, DisplayHeader(h))
				if i == 0 {
					headers = append(headers, "Callsign")
				}
			}
		}
		if !snap.ActivityOK || len(snap.Rows) == 0 {
			activity = append(activity, ActivityRecord{When: when, Ago: ago, Empty: true})
			continue
		}
		for _, row := range snap.Rows {
			fields := make([]string, 0, len(row)+1)
			for i, v := range row {
				fields = append(fields, v)
				if i == 0 {
					fields = append(fields, DescribeNode(dir, v).Callsign)
				}
			}
			activity = append(activity, ActivityRecord{When: when, Ago: ago, Fields: fields})
		}
	}
	return connected, headers, activity
}
