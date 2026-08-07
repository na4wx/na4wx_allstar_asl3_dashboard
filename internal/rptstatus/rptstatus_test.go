package rptstatus

import (
	"strings"
	"testing"
)

// realNodesNone / realLstatsEmpty are verbatim output from a real
// HamVoIP node (Asterisk 1.4.23-pre.hamvoip-V1.7.1, app_rpt-0.327) with
// nothing connected — captured rather than invented, since guessing at
// this app's real-world formats has been a recurring source of bugs.
const realNodesNone = `
************************* CONNECTED NODES *************************

<NONE>
`

const realLstatsEmpty = `NODE           PEER                RECONNECTS  DIRECTION  CONNECT TIME        CONNECT STATE
----           ----                ----------  ---------  ------------        -------------
`

// realLstatsConnected mirrors the same header/separator geometry with
// data rows filled in, to exercise the value-extraction path the empty
// capture can't reach.
const realLstatsConnected = `NODE           PEER                RECONNECTS  DIRECTION  CONNECT TIME        CONNECT STATE
----           ----                ----------  ---------  ------------        -------------
49616          NA4WX               0           OUT        00:05:12            ESTABLISHED
2000           W1AW                3           IN         01:20:44            ESTABLISHED
`

func TestParseConnectedNodesNone(t *testing.T) {
	got := ParseConnectedNodes(realNodesNone)
	if len(got) != 0 {
		t.Errorf("expected no connected nodes, got %q", got)
	}
}

func TestParseConnectedNodesSome(t *testing.T) {
	out := `
************************* CONNECTED NODES *************************

49616  2000  27225
`
	got := ParseConnectedNodes(out)
	want := []string{"49616", "2000", "27225"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseLstatsEmpty(t *testing.T) {
	headers, rows, ok := ParseLstats(realLstatsEmpty)
	if !ok {
		t.Fatal("expected the header/separator pair to be recognized")
	}
	want := []string{"NODE", "PEER", "RECONNECTS", "DIRECTION", "CONNECT TIME", "CONNECT STATE"}
	if len(headers) != len(want) {
		t.Fatalf("got %d headers %q, want %d %q", len(headers), headers, len(want), want)
	}
	for i := range want {
		if headers[i] != want[i] {
			t.Errorf("header %d = %q, want %q", i, headers[i], want[i])
		}
	}
	if len(rows) != 0 {
		t.Errorf("expected no data rows, got %q", rows)
	}
}

// TestParseLstatsMultiWordHeaders guards the specific reason column
// positions are derived from the separator row instead of splitting on
// whitespace: "CONNECT TIME" and "CONNECT STATE" contain spaces, so a
// naive Fields() split yields 8 columns instead of 6 and silently
// misaligns every value under the wrong heading.
func TestParseLstatsMultiWordHeaders(t *testing.T) {
	headers, _, ok := ParseLstats(realLstatsEmpty)
	if !ok {
		t.Fatal("parse failed")
	}
	if len(headers) != 6 {
		t.Fatalf("got %d columns %q, want 6 — whitespace splitting would give 8", len(headers), headers)
	}
	if n := len(strings.Fields(realLstatsEmpty[:strings.Index(realLstatsEmpty, "\n")])); n != 8 {
		t.Fatalf("precondition: expected naive Fields() to give 8, got %d", n)
	}
}

func TestParseLstatsRows(t *testing.T) {
	headers, rows, ok := ParseLstats(realLstatsConnected)
	if !ok {
		t.Fatal("parse failed")
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %q", len(rows), rows)
	}
	if len(rows[0]) != len(headers) {
		t.Fatalf("row 0 has %d fields, headers have %d", len(rows[0]), len(headers))
	}
	want := []string{"49616", "NA4WX", "0", "OUT", "00:05:12", "ESTABLISHED"}
	for i := range want {
		if rows[0][i] != want[i] {
			t.Errorf("row0[%d] (%s) = %q, want %q", i, headers[i], rows[0][i], want[i])
		}
	}
	if rows[1][0] != "2000" || rows[1][5] != "ESTABLISHED" {
		t.Errorf("row1 = %q", rows[1])
	}
}

// TestParseLstatsUnrecognized asserts the fallback contract: output with
// no separator row must report ok=false so the caller shows raw text
// rather than an empty table that looks like "nothing is connected".
func TestParseLstatsUnrecognized(t *testing.T) {
	if _, _, ok := ParseLstats("some other app_rpt version output\nwith no rule row\n"); ok {
		t.Error("expected ok=false for output with no separator row")
	}
}

// TestDisplayHeader covers the presentational softening of app_rpt's
// all-caps headings, including the guard that leaves already-mixed-case
// headings from a hypothetical future version untouched.
func TestDisplayHeader(t *testing.T) {
	cases := []struct{ in, want string }{
		{"NODE", "Node"},
		{"CONNECT TIME", "Connect time"},
		{"CONNECT STATE", "Connect state"},
		{"RECONNECTS", "Reconnects"},
		{"Connect Time", "Connect Time"}, // already mixed case: leave alone
		{"", ""},
	}
	for _, c := range cases {
		if got := DisplayHeader(c.in); got != c.want {
			t.Errorf("DisplayHeader(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// realRptStats is verbatim output from the real node (Asterisk
// 1.4.23-pre.hamvoip-V1.7.1, app_rpt-0.327), captured rather than
// invented.
const realRptStats = `************************ NODE 52829 STATISTICS *************************

Selected system state............................: 0
Signal on input..................................: NO
System...........................................: ENABLED
Parrot Mode......................................: DISABLED
Scheduler........................................: ENABLED
Tail Time........................................: STANDARD
Time out timer...................................: ENABLED
Incoming connections.............................: ENABLED
Time out timer state.............................: RESET
Time outs since system initialization............: 0
Identifier state.................................: CLEAN
Kerchunks today..................................: 0
Kerchunks since system initialization............: 3
Keyups today.....................................: 0
Keyups since system initialization...............: 16
DTMF commands today..............................: 0
DTMF commands since system initialization........: 5
Last DTMF command executed.......................: 150001
TX time today....................................: 00:00:00.0
TX time since system initialization..............: 00:02:26.702
Uptime...........................................: 03:25:47
Nodes currently connected to us..................: <NONE>
Autopatch........................................: ENABLED
Autopatch state..................................: DOWN
Autopatch called number..........................: N/A
Reverse patch/IAXRPT connected...................: DOWN
User linking commands............................: ENABLED
User functions...................................: ENABLED
`

func TestParseRptStatsRealOutput(t *testing.T) {
	fields, ok := ParseRptStats(realRptStats)
	if !ok {
		t.Fatal("expected the stats block to parse")
	}
	if len(fields) != 28 {
		t.Errorf("got %d fields, want 28", len(fields))
	}
	// The banner must not become a field.
	for _, f := range fields {
		if f.Label == "" || f.Label[0] == '*' {
			t.Errorf("banner leaked into fields: %+v", f)
		}
	}
	for _, c := range []struct{ label, want string }{
		{"Signal on input", "NO"},
		{"System", "ENABLED"},
		{"Nodes currently connected to us", "<NONE>"},
		{"Last DTMF command executed", "150001"},
		{"Keyups since system initialization", "16"},
	} {
		if got := fields.Value(c.label); got != c.want {
			t.Errorf("Value(%q) = %q, want %q", c.label, got, c.want)
		}
	}
}

// TestParseRptStatsTimeValues is why the split is at the FIRST colon:
// these values contain colons of their own, and splitting at the last
// one (or on every colon) would truncate them.
func TestParseRptStatsTimeValues(t *testing.T) {
	fields, _ := ParseRptStats(realRptStats)
	for _, c := range []struct{ label, want string }{
		{"TX time today", "00:00:00.0"},
		{"TX time since system initialization", "00:02:26.702"},
		{"Uptime", "03:25:47"},
	} {
		if got := fields.Value(c.label); got != c.want {
			t.Errorf("Value(%q) = %q, want %q", c.label, got, c.want)
		}
	}
}

func TestParseRptStatsLabelExactMatch(t *testing.T) {
	fields, _ := ParseRptStats(realRptStats)
	if got := fields.Value("Signal"); got != "" {
		t.Errorf("partial label matched: %q", got)
	}
	if got := fields.Value("nope"); got != "" {
		t.Errorf("unknown label returned %q", got)
	}
}

func TestParseRptStatsUnrecognized(t *testing.T) {
	if _, ok := ParseRptStats("no colons here\njust text\n"); ok {
		t.Error("expected ok=false when nothing parses")
	}
}

// TestNodeReceiving covers the one field this section exists to
// surface: whether someone is keying the node's receiver right now.
func TestNodeReceiving(t *testing.T) {
	fields, _ := ParseRptStats(realRptStats)
	if NodeReceiving(fields) {
		t.Error("Signal on input is NO, so the node should not read as receiving")
	}

	keyed, _ := ParseRptStats("Signal on input....: YES\n")
	if !NodeReceiving(keyed) {
		t.Error("Signal on input YES should read as receiving")
	}
}

// TestKeyedNodesDocumentedExample uses the exact RPT_ALINKS example from
// AllStarLink's Event Management documentation: node 2001 is keyed (K),
// node 2000 is not (U).
func TestKeyedNodesDocumentedExample(t *testing.T) {
	out := "Node 52829 variables:\nRPT_ALINKS=2,2000TU,2001RK\nRPT_NUMALINKS=2\n"
	keyed := KeyedNodes(out)
	if !keyed["2001"] {
		t.Error("2001 has flag K and should read as keyed")
	}
	if keyed["2000"] {
		t.Error("2000 has flag U and should not read as keyed")
	}
	if len(keyed) != 1 {
		t.Errorf("keyed set = %v, want just {2001}", keyed)
	}
}

// TestKeyedNodesToleratesLayout confirms the value is found regardless of
// whether the command separates the name with =, :, or whitespace, since
// the exact "rpt show variables" layout on this build is unverified.
func TestKeyedNodesToleratesLayout(t *testing.T) {
	for _, out := range []string{
		"RPT_ALINKS=1,3000TK",
		"RPT_ALINKS: 1,3000TK",
		"  RPT_ALINKS   1,3000TK  ",
	} {
		if !KeyedNodes(out)["3000"] {
			t.Errorf("did not find keyed node in %q", out)
		}
	}
}

// TestKeyedNodesFailsClosed is the safety contract: no RPT_ALINKS, an
// empty list, or a value that doesn't fit the grammar must yield an empty
// set, never a wrong guess — so a build lacking this variable shows no
// talking markers rather than misleading ones.
func TestKeyedNodesFailsClosed(t *testing.T) {
	for _, out := range []string{
		"",
		"No such command 'rpt show variables'",
		"RPT_ALINKS=0",
		"RPT_ALINKS=garbage-without-grammar",
		"RPT_RXKEYED=1\nRPT_TXKEYED=0",
	} {
		if n := len(KeyedNodes(out)); n != 0 {
			t.Errorf("expected empty set for %q, got %d", out, n)
		}
	}
}
