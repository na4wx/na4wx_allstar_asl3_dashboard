package cloudagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings is the operator's own opt-in configuration for connecting
// this node to the public cloud platform: the API key that identifies
// this device, and whether the connection is enabled at all. Unlike
// internal/wxtone or internal/soundschedule (which persist a list of
// entries), there is exactly one Settings value per installation, so
// SettingsStore holds a single JSON object rather than an array.
type Settings struct {
	// CloudURL is deliberately never persisted by SettingsStore or read
	// from the operator-facing form (see internal/server/cloudsettings.go)
	// -- Run always overwrites it with Agent.cloudURL (the fixed URL
	// baked in at build/deploy time via -cloud-url) right before
	// dialing, regardless of whatever a settings.json on disk might
	// contain. It stays a field here only because runOnce's own tests
	// exercise it directly against a fake local server, bypassing Run
	// entirely.
	CloudURL string `json:"-"`
	APIKey   string `json:"api_key"`
	Enabled  bool   `json:"enabled"`

	// AllowRemoteReboot/AllowRawConfigEdit gate the most destructive
	// relayed actions behind their own, separate opt-in — off by
	// default even once Cloud Sync itself is enabled. Pasting an API
	// key shouldn't silently expose "reboot this device" or "rewrite an
	// arbitrary config file" — same explicit, narrow opt-in philosophy
	// this app already uses for SkywarnPlus (only ever configured if
	// already installed), applied here to capability rather than
	// presence. See actions_system.go/actions_rawconfig.go for where
	// these are actually checked.
	AllowRemoteReboot  bool `json:"allow_remote_reboot"`
	AllowRawConfigEdit bool `json:"allow_raw_config_edit"`
}

// SettingsStore persists Settings as a single JSON file at path, the
// same shape as internal/wxtone.Store and internal/soundschedule.Store
// (a real mutex, since Run's background goroutine reads this
// concurrently with an HTTP-handler write from the local Cloud Sync
// settings page).
type SettingsStore struct {
	path string
	mu   sync.Mutex
}

// NewSettingsStore builds a SettingsStore backed by path.
func NewSettingsStore(path string) *SettingsStore {
	return &SettingsStore{path: path}
}

// Load reads the current settings. A missing file reads as a zeroed,
// disabled Settings rather than an error — this feature is off until an
// operator explicitly opts in, matching how internal/wxtone.Store.List
// treats a missing file as "nothing configured yet".
func (s *SettingsStore) Load() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		return Settings{}, err
	}
	return out, nil
}

// Save writes settings, creating the parent directory if needed.
func (s *SettingsStore) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}
