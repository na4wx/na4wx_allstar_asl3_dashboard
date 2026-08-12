package relay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings is the operator's own opt-in configuration for the
// NAT-traversal relay: whether it's enabled, and this device's own
// persisted WireGuard keypair (generated once, the first time relay is
// enabled — see PrivateKey's own doc comment for why it's never
// regenerated after that). Exactly one Settings value per installation,
// same shape as cloudagent.Settings.
type Settings struct {
	Enabled bool `json:"enabled"`

	// PrivateKey/PublicKey are generated once (see wgctl.go's
	// GenerateKeypair) the first time relay is enabled, then persisted
	// and reused across every restart and reconnect — not regenerated on
	// every toggle. Regenerating on every enable would mean each
	// reconnect looks like a brand new device to the cloud's peer table
	// (see relayProvision.ts's key-rotation handling on the cloud side),
	// which is unnecessary churn for something that only actually needs
	// to change if the key is ever suspected compromised.
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// SettingsStore persists Settings as a single JSON file, the same shape
// as cloudagent.SettingsStore (a real mutex, since Manager's background
// reconcile reads this concurrently with an HTTP-handler write from the
// System page).
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
// operator explicitly opts in.
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
