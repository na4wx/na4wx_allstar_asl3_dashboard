package rptstatus

import (
	"testing"
	"time"

	"hamvoipconfiggui-asl3/internal/nodedb"
)

// fakeDirectory stands in for the downloaded node database.
type fakeDirectory map[string]nodedb.Entry

func (f fakeDirectory) Lookup(n string) (nodedb.Entry, bool) { e, ok := f[n]; return e, ok }

// realDirectory uses entries exactly as they appear in the live
// AllStarLink database, including node 52829's empty description.
var realDirectory = fakeDirectory{
	"52829": {Number: "52829", Callsign: "NA4WX", Description: "", Location: "Maryville, Tennessee"},
	"49616": {Number: "49616", Callsign: "WB4GBI", Description: "444.300+", Location: "Sevierville, TN"},
}

func TestBuildLinkTablesAddsCallsigns(t *testing.T) {
	snaps := []LinkSnapshot{{
		At:         time.Now(),
		Nodes:      []string{"49616", "99999"},
		ActivityOK: true,
		Headers:    []string{"NODE", "PEER", "CONNECT TIME"},
		Rows:       [][]string{{"49616", "somepeer", "00:05:12"}},
	}}
	connected, headers, activity := BuildLinkTables(realDirectory, snaps)

	if got := connected[0].Nodes[0]; got.Number != "49616" || got.Callsign != "WB4GBI" {
		t.Errorf("connected[0] = %+v, want 49616/WB4GBI", got)
	}
	if got := connected[0].Nodes[0].Detail; got != "444.300+ · Sevierville, TN" {
		t.Errorf("Detail = %q", got)
	}
	// An unknown node must still render, just without a callsign.
	if got := connected[0].Nodes[1]; got.Number != "99999" || got.Callsign != "" {
		t.Errorf("unknown node = %+v, want number with empty callsign", got)
	}

	wantHeaders := []string{"Node", "Callsign", "Peer", "Connect time"}
	if len(headers) != len(wantHeaders) {
		t.Fatalf("headers = %q, want %q", headers, wantHeaders)
	}
	for i := range wantHeaders {
		if headers[i] != wantHeaders[i] {
			t.Errorf("header %d = %q, want %q", i, headers[i], wantHeaders[i])
		}
	}

	wantFields := []string{"49616", "WB4GBI", "somepeer", "00:05:12"}
	if len(activity[0].Fields) != len(wantFields) {
		t.Fatalf("fields = %q, want %q", activity[0].Fields, wantFields)
	}
	for i := range wantFields {
		if activity[0].Fields[i] != wantFields[i] {
			t.Errorf("field %d = %q, want %q", i, activity[0].Fields[i], wantFields[i])
		}
	}
}

// TestBuildLinkTablesColumnCountsMatch is the alignment guard: the
// inserted Callsign column must appear in both the header row and every
// data row, or every value after it renders under the wrong heading.
func TestBuildLinkTablesColumnCountsMatch(t *testing.T) {
	snaps := []LinkSnapshot{{
		At:         time.Now(),
		ActivityOK: true,
		Headers:    []string{"NODE", "PEER", "RECONNECTS", "DIRECTION", "CONNECT TIME", "CONNECT STATE"},
		Rows: [][]string{
			{"49616", "p", "0", "OUT", "00:05:12", "ESTABLISHED"},
			{"52829", "q", "1", "IN", "00:00:07", "ESTABLISHED"},
		},
	}}
	_, headers, activity := BuildLinkTables(realDirectory, snaps)
	for i, rec := range activity {
		if len(rec.Fields) != len(headers) {
			t.Errorf("row %d has %d fields but there are %d headers (%q vs %q)",
				i, len(rec.Fields), len(headers), rec.Fields, headers)
		}
	}
}

// TestBuildLinkTablesNilDirectory covers the pre-download state: no
// database yet must not break the page, just omit callsigns.
func TestBuildLinkTablesNilDirectory(t *testing.T) {
	snaps := []LinkSnapshot{{At: time.Now(), Nodes: []string{"49616"}}}
	connected, _, _ := BuildLinkTables(nil, snaps)
	if got := connected[0].Nodes[0]; got.Number != "49616" || got.Callsign != "" {
		t.Errorf("got %+v, want bare number", got)
	}
}

// TestBuildLinkTablesEmptyDescription pins node 52829's real shape: an
// empty description must fall back to the callsign, and Detail must not
// begin with a stray separator.
func TestBuildLinkTablesEmptyDescription(t *testing.T) {
	snaps := []LinkSnapshot{{At: time.Now(), Nodes: []string{"52829"}}}
	connected, _, _ := BuildLinkTables(realDirectory, snaps)
	n := connected[0].Nodes[0]
	if n.Callsign != "NA4WX" {
		t.Errorf("Callsign = %q, want NA4WX", n.Callsign)
	}
	if n.Detail != "Maryville, Tennessee" {
		t.Errorf("Detail = %q, want just the location with no leading separator", n.Detail)
	}
}
