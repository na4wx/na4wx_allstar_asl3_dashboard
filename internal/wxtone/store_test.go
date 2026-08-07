package wxtone

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "wx-tones.json")
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

func TestSaveAddsNewEntryWithGeneratedIDAndDefaultMode(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", CTKey: "ct1", NormalType: TypeSound, NormalSound: "ct1-normal", WXType: TypeSound, WXSound: "ct1-storm"}); err != nil {
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
	if entries[0].Mode != ModeNormal {
		t.Fatalf("Mode = %q, want default %q", entries[0].Mode, ModeNormal)
	}
	if entries[0].CTKey != "ct1" || entries[0].NormalSound != "ct1-normal" || entries[0].WXSound != "ct1-storm" {
		t.Fatalf("entries[0] = %+v", entries[0])
	}
}

func TestSaveUpdatesExistingByID(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", CTKey: "ct1", NormalType: TypeSound, NormalSound: "a", WXType: TypeSound, WXSound: "b"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.List()
	id := entries[0].ID

	if err := s.Save(Entry{ID: id, Node: "2000", CTKey: "ct1", NormalType: TypeSound, NormalSound: "a2", WXType: TypeSound, WXSound: "b2"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("update should not add a new entry, got %v", entries)
	}
	if entries[0].NormalSound != "a2" || entries[0].WXSound != "b2" {
		t.Fatalf("entries[0] = %+v, want updated fields", entries[0])
	}
}

func TestSaveToneTypeEntry(t *testing.T) {
	s := New(newTestStorePath(t))
	e := Entry{
		Node: "2000", CTKey: "ct2",
		NormalType: TypeTone, NormalTone: "|t(660,0,150,2048)",
		WXType: TypeTone, WXTone: "|t(650,0,100,2048)(770,0,100,2048)",
	}
	if err := s.Save(e); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("List = %v, want 1 entry", entries)
	}
	got := entries[0]
	if got.NormalType != TypeTone || got.NormalTone != "|t(660,0,150,2048)" {
		t.Errorf("Normal side = %+v", got)
	}
	if got.WXType != TypeTone || got.WXTone != "|t(650,0,100,2048)(770,0,100,2048)" {
		t.Errorf("WX side = %+v", got)
	}
}

func TestSaveMixedTypeEntry(t *testing.T) {
	s := New(newTestStorePath(t))
	e := Entry{
		Node: "2000", CTKey: "ct3",
		NormalType: TypeSound, NormalSound: "ct3-normal",
		WXType: TypeTone, WXTone: "|t(650,0,100,2048)",
	}
	if err := s.Save(e); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	got := entries[0]
	if got.NormalType != TypeSound || got.NormalSound != "ct3-normal" {
		t.Errorf("Normal side = %+v", got)
	}
	if got.WXType != TypeTone || got.WXTone != "|t(650,0,100,2048)" {
		t.Errorf("WX side = %+v", got)
	}
}

func TestListForNode(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", CTKey: "ct1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Node: "3000", CTKey: "ct2"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListForNode("2000")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].CTKey != "ct1" {
		t.Fatalf("ListForNode(2000) = %v", entries)
	}
}

// TestListForNodeNoMatchesIsNonNil confirms a node with zero WX tone
// mappings gets back a non-nil empty slice, not nil -- a nil slice
// marshals to JSON null, and the cloud relay's wxTone.list action
// sends this straight to the browser as JSON.
func TestListForNodeNoMatchesIsNonNil(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "3000", CTKey: "ct2"}); err != nil {
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

func TestSetMode(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", CTKey: "ct1"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.List()
	id := entries[0].ID

	if err := s.SetMode(id, ModeWX); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Mode != ModeWX {
		t.Fatalf("Mode = %q, want %q", entries[0].Mode, ModeWX)
	}
}

func TestSetModeUnknownIDIsNotError(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.SetMode("does-not-exist", ModeWX); err != nil {
		t.Fatal(err)
	}
}

func TestDelete(t *testing.T) {
	s := New(newTestStorePath(t))
	if err := s.Save(Entry{Node: "2000", CTKey: "ct1"}); err != nil {
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
	if err := s.Save(Entry{Node: "2000", CTKey: "ct1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Node: "2000", CTKey: "ct2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Node: "3000", CTKey: "ct1"}); err != nil {
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
	if err := s1.Save(Entry{Node: "2000", CTKey: "ct1"}); err != nil {
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
