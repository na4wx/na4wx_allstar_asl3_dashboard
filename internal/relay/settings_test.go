package relay

import (
	"path/filepath"
	"testing"
)

func TestSettingsStoreLoadMissingFileReadsAsDisabled(t *testing.T) {
	s := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != (Settings{}) {
		t.Fatalf("Load() = %+v, want zero value for a missing file", got)
	}
}

func TestSettingsStoreSaveLoadRoundTrip(t *testing.T) {
	s := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	want := Settings{Enabled: true, PrivateKey: "priv-key", PublicKey: "pub-key"}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestSettingsStoreSaveCreatesParentDir(t *testing.T) {
	s := NewSettingsStore(filepath.Join(t.TempDir(), "nested", "dir", "relay.json"))
	if err := s.Save(Settings{Enabled: true}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.Enabled {
		t.Fatal("Load().Enabled = false, want true after Save")
	}
}
