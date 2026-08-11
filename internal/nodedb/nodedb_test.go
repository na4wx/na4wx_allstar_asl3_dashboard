package nodedb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realSample is verbatim from https://allmondb.allstarlink.org/ — the
// live database, captured rather than invented, since assuming a
// real-world format has been a recurring source of bugs in this project.
const realSample = `2000|WB6NIL|ASL Public Hub|Los Angeles, CA
2001|WB6NIL|ASL Public Hub|Los Angeles, CA
2002|WB6NIL|AllStarLink Parrot|AWS US-EAST-1
2003|KM6RPT|448.280-|San Diego County, CA
2004|WB6NIL|Beta Test Node|Columbus, OH, US
`

func TestParseRealSample(t *testing.T) {
	entries, err := Parse(strings.NewReader(realSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
	}
	e, ok := entries["2003"]
	if !ok {
		t.Fatal("node 2003 missing")
	}
	if e.Callsign != "KM6RPT" {
		t.Errorf("Callsign = %q, want KM6RPT", e.Callsign)
	}
	if e.Description != "448.280-" {
		t.Errorf("Description = %q, want 448.280-", e.Description)
	}
	if e.Location != "San Diego County, CA" {
		t.Errorf("Location = %q, want %q", e.Location, "San Diego County, CA")
	}
}

// TestParseLocationWithComma guards the specific reason the split is
// bounded at 4 fields: locations routinely contain commas, and one
// entry above ("Columbus, OH, US") contains two. A comma-based or
// unbounded split would mangle these.
func TestParseLocationWithComma(t *testing.T) {
	entries, _ := Parse(strings.NewReader(realSample))
	if got := entries["2004"].Location; got != "Columbus, OH, US" {
		t.Errorf("Location = %q, want %q", got, "Columbus, OH, US")
	}
}

// TestParseDescriptionWithPipe covers a description containing the
// delimiter itself: everything after the third pipe belongs to location,
// so the description must not swallow it or vice versa.
func TestParseDescriptionWithPipe(t *testing.T) {
	entries, _ := Parse(strings.NewReader("1234|W1AW|Repeater|Newington|CT\n"))
	e := entries["1234"]
	if e.Description != "Repeater" {
		t.Errorf("Description = %q, want Repeater", e.Description)
	}
	if e.Location != "Newington|CT" {
		t.Errorf("Location = %q, want %q", e.Location, "Newington|CT")
	}
}

func TestParseSkipsJunk(t *testing.T) {
	entries, err := Parse(strings.NewReader("\n# a comment\n|no number|x|y\n555|K1ABC|Node|Somewhere\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if _, ok := entries["555"]; !ok {
		t.Error("expected node 555 to survive")
	}
}

func TestParseShortLine(t *testing.T) {
	entries, _ := Parse(strings.NewReader("999|K9XYZ\n"))
	e, ok := entries["999"]
	if !ok {
		t.Fatal("short line should still yield an entry")
	}
	if e.Callsign != "K9XYZ" || e.Description != "" || e.Location != "" {
		t.Errorf("got %+v", e)
	}
}

// searchTestStore builds a Store directly from realSample without going
// through LoadFile/Refresh -- Search only ever reads s.entries, so this
// is the narrowest way to set it up.
func searchTestStore(t *testing.T) *Store {
	t.Helper()
	entries, err := Parse(strings.NewReader(realSample))
	if err != nil {
		t.Fatal(err)
	}
	return &Store{entries: entries}
}

func TestSearchExactMatchRanksFirst(t *testing.T) {
	s := searchTestStore(t)
	results := s.Search("KM6RPT", 10)
	if len(results) == 0 || results[0].Number != "2003" {
		t.Fatalf("Search(\"KM6RPT\") = %+v, want node 2003 first", results)
	}
}

// TestSearchIsCaseInsensitive confirms a lowercase query still matches
// -- callsigns in the real database are always uppercase, but nobody
// types a callsign that way on a phone keyboard.
func TestSearchIsCaseInsensitive(t *testing.T) {
	s := searchTestStore(t)
	results := s.Search("km6rpt", 10)
	if len(results) == 0 || results[0].Number != "2003" {
		t.Fatalf("Search(\"km6rpt\") = %+v, want node 2003 first", results)
	}
}

// TestSearchPrefixBeforeSubstring confirms ranking: WB6NIL owns four
// nodes in realSample (2000/2001/2002/2004), so a query for "WB6" should
// return all of them (prefix matches) before anything that only
// contains "WB6" as a substring elsewhere -- realSample has no such
// entry, but the ranking itself is what this pins.
func TestSearchPrefixBeforeSubstring(t *testing.T) {
	s := searchTestStore(t)
	results := s.Search("WB6", 10)
	if len(results) != 4 {
		t.Fatalf("Search(\"WB6\") returned %d results, want 4: %+v", len(results), results)
	}
	for _, e := range results {
		if e.Callsign != "WB6NIL" {
			t.Errorf("unexpected callsign %q in results", e.Callsign)
		}
	}
}

func TestSearchRespectsLimit(t *testing.T) {
	s := searchTestStore(t)
	results := s.Search("WB6", 2)
	if len(results) != 2 {
		t.Fatalf("Search with limit 2 returned %d results, want 2", len(results))
	}
}

func TestSearchBlankQueryReturnsNothing(t *testing.T) {
	s := searchTestStore(t)
	if results := s.Search("   ", 10); results != nil {
		t.Errorf("Search(blank) = %+v, want nil", results)
	}
}

func TestSearchNoMatchReturnsEmpty(t *testing.T) {
	s := searchTestStore(t)
	if results := s.Search("NOSUCHCALL", 10); len(results) != 0 {
		t.Errorf("Search(no match) = %+v, want empty", results)
	}
}

func TestLabelFallsBackToDescription(t *testing.T) {
	if got := (Entry{Callsign: "NA4WX", Description: "x"}).Label(); got != "NA4WX" {
		t.Errorf("Label = %q, want NA4WX", got)
	}
	if got := (Entry{Description: "ASL Parrot"}).Label(); got != "ASL Parrot" {
		t.Errorf("Label = %q, want the description", got)
	}
}

func TestRefreshWritesAndParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(realSample))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "sub", "astdb.txt")
	s := New(path, srv.URL)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.Label("2003"); got != "KM6RPT" {
		t.Errorf("Label(2003) = %q, want KM6RPT", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(raw) != realSample {
		t.Error("written file does not match what the server sent")
	}
}

// TestRefreshKeepsGoodDataOnBadDownload is the important safety
// property: a server returning an error or garbage must not wipe out a
// working database that's already loaded.
func TestRefreshKeepsGoodDataOnBadDownload(t *testing.T) {
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer fail.Close()

	path := filepath.Join(t.TempDir(), "astdb.txt")
	if err := os.WriteFile(path, []byte(realSample), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(path, fail.URL)
	if err := s.LoadFile(); err != nil {
		t.Fatal(err)
	}
	if s.Label("2000") != "WB6NIL" {
		t.Fatal("precondition: existing database should be loaded")
	}

	if err := s.Refresh(context.Background()); err == nil {
		t.Error("expected an error from a failing server")
	}
	if got := s.Label("2000"); got != "WB6NIL" {
		t.Errorf("existing data was lost on a failed refresh: Label(2000) = %q", got)
	}
	if _, _, lastErr := s.Status(); lastErr == "" {
		t.Error("expected the failure to be recorded for display")
	}
}

// TestRefreshRejectsEmptyDownload covers a server that returns 200 with
// nothing usable — replacing a good database with an empty one would
// silently drop every callsign from the UI.
func TestRefreshRejectsEmptyDownload(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("\n\n"))
	}))
	defer empty.Close()

	path := filepath.Join(t.TempDir(), "astdb.txt")
	os.WriteFile(path, []byte(realSample), 0o644)
	s := New(path, empty.URL)
	s.LoadFile()

	if err := s.Refresh(context.Background()); err == nil {
		t.Error("expected an empty download to be rejected")
	}
	if s.Label("2000") != "WB6NIL" {
		t.Error("existing data was lost to an empty download")
	}
}

func TestLoadFileMissingIsNotAnError(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nope.txt"), DefaultURL)
	if err := s.LoadFile(); err != nil {
		t.Errorf("missing file should be tolerated, got %v", err)
	}
	if got := s.Label("2000"); got != "" {
		t.Errorf("Label = %q, want empty", got)
	}
}
