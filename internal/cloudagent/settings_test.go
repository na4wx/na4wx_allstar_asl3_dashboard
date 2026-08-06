package cloudagent

import (
	"path/filepath"
	"testing"
)

func TestSettingsLoadMissingFileIsZeroNotError(t *testing.T) {
	s := NewSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load on missing file should not error, got: %v", err)
	}
	if got != (Settings{}) {
		t.Fatalf("Load = %+v, want zero value", got)
	}
}

func TestSettingsSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	s := NewSettingsStore(path)
	want := Settings{APIKey: "hvc_live_abc123", Enabled: true}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestSettingsSavePersistsAcrossNewStoreInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s1 := NewSettingsStore(path)
	want := Settings{APIKey: "key", Enabled: true}
	if err := s1.Save(want); err != nil {
		t.Fatal(err)
	}
	s2 := NewSettingsStore(path)
	got, err := s2.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestSettingsCloudURLIsNeverPersisted confirms CloudURL round-trips as
// empty even when set on the value passed to Save -- see the field's
// own doc comment for why (Run always overrides it with Agent.cloudURL
// before dialing; it must never be trusted from disk).
func TestSettingsCloudURLIsNeverPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := NewSettingsStore(path)
	if err := s.Save(Settings{CloudURL: "wss://attacker.example.com/agent", APIKey: "key", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.CloudURL != "" {
		t.Errorf("CloudURL = %q after round-trip, want empty (never persisted)", got.CloudURL)
	}
}
