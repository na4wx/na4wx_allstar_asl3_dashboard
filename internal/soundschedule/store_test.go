package soundschedule

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMatchesWildcards(t *testing.T) {
	e := Entry{Minute: "*", Hour: "*", DayOfMonth: "*", Month: "*"}
	if !e.Matches(time.Date(2026, 7, 21, 13, 45, 0, 0, time.UTC)) {
		t.Fatal("all-wildcard entry should match any time")
	}
}

func TestMatchesExactFields(t *testing.T) {
	e := Entry{Minute: "30", Hour: "20", DayOfMonth: "*", Month: "*"}
	if !e.Matches(time.Date(2026, 7, 21, 20, 30, 0, 0, time.UTC)) {
		t.Fatal("expected a match at 20:30")
	}
	if e.Matches(time.Date(2026, 7, 21, 20, 31, 0, 0, time.UTC)) {
		t.Fatal("expected no match at 20:31")
	}
}

func TestMatchesEmptyDaysOfWeekMeansEveryDay(t *testing.T) {
	e := Entry{Minute: "0", Hour: "6", DayOfMonth: "*", Month: "*"}
	// 2026-07-21 is a Tuesday.
	if !e.Matches(time.Date(2026, 7, 21, 6, 0, 0, 0, time.UTC)) {
		t.Fatal("empty DaysOfWeek should match every day")
	}
}

func TestMatchesDaysOfWeekList(t *testing.T) {
	// 2026-07-21 is a Tuesday (weekday 2); 2026-07-25 is a Saturday (weekday 6).
	e := Entry{Minute: "0", Hour: "6", DayOfMonth: "*", Month: "*", DaysOfWeek: []int{1, 3, 5}}
	if e.Matches(time.Date(2026, 7, 21, 6, 0, 0, 0, time.UTC)) {
		t.Fatal("Tuesday should not match Mon/Wed/Fri")
	}
	wed := time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC)
	if wed.Weekday() != time.Wednesday {
		t.Fatalf("test fixture date is %v, not Wednesday", wed.Weekday())
	}
	if !e.Matches(wed) {
		t.Fatal("Wednesday should match Mon/Wed/Fri")
	}
}

func newTestStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "sound-schedule.json")
}

func TestListOnMissingFileIsEmptyNotError(t *testing.T) {
	s := New(newTestStorePath(t))
	entries, err := s.List()
	if err != nil {
		t.Fatalf("List on missing file should not error, got: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %v", entries)
	}
}

func TestSaveAddsNewEntryWithGeneratedID(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", File: "myfile", Reach: ReachLocal, Minute: "*", Hour: "*", DayOfMonth: "*", Month: "*"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("List = %v, want 1 entry", entries)
	}
	if entries[0].ID == "" {
		t.Fatal("expected a generated ID")
	}
	if entries[0].Node != "2000" || entries[0].File != "myfile" {
		t.Fatalf("entries[0] = %+v", entries[0])
	}
}

func TestSaveUpdatesExistingByID(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", File: "myfile", Reach: ReachLocal}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.List()
	id := entries[0].ID

	if err := s.Save(Entry{ID: id, Node: "2000", File: "otherfile", Reach: ReachNetwork}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("update should not add a new entry, got %v", entries)
	}
	if entries[0].File != "otherfile" || entries[0].Reach != ReachNetwork {
		t.Fatalf("entries[0] = %+v, want updated fields", entries[0])
	}
}

func TestListForNode(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", File: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Node: "3000", File: "b"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListForNode("2000")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].File != "a" {
		t.Fatalf("ListForNode(2000) = %v", entries)
	}
}

// TestListForNodeNoMatchesIsNonNil confirms a node with zero scheduled
// entries gets back a non-nil empty slice, not nil -- a nil slice
// marshals to JSON null, and the cloud relay's soundSchedule.list
// action sends this straight to the browser as JSON.
func TestListForNodeNoMatchesIsNonNil(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "3000", File: "b"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListForNode("2000")
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil {
		t.Error("ListForNode() = nil, want a non-nil empty slice")
	}
	if len(entries) != 0 {
		t.Errorf("ListForNode() = %v, want empty", entries)
	}
}

func TestDelete(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", File: "a"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.List()
	id := entries[0].ID

	if err := s.Delete(id); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected entry to be deleted, got %v", entries)
	}
}

func TestDeleteByNode(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", File: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Node: "2000", File: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Node: "3000", File: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteByNode("2000"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Node != "3000" {
		t.Fatalf("DeleteByNode should leave only node 3000's entry, got %v", entries)
	}
}

func TestSavePersistsAcrossNewStoreInstance(t *testing.T) {
	path := newTestStorePath(t)
	s1 := New(path)
	if err := s1.Save(Entry{Node: "2000", File: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}

	s2 := New(path)
	entries, err := s2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Node != "2000" {
		t.Fatalf("expected persisted entry to be readable by a fresh Store, got %v", entries)
	}
}
