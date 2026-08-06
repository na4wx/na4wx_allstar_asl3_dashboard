package sa818

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LastApplied records the most recent settings this app sent to the
// radio module, plus the result. It's the closest thing to a "read"
// this feature can offer — the SA818/DRA818 AT command set has no way
// to query the module itself, so this is only ever what was last
// written from here, not confirmed live state.
type LastApplied struct {
	Settings
	Tool      string    `json:"tool"`
	AppliedAt time.Time `json:"applied_at"`
	Success   bool      `json:"success"`
	Output    string    `json:"output"`
}

// LoadLast reads the last-applied record from path. A missing file is
// not an error — it just means nothing has been sent yet.
func LoadLast(path string) (*LastApplied, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var la LastApplied
	if err := json.Unmarshal(data, &la); err != nil {
		return nil, err
	}
	return &la, nil
}

// SaveLast writes la to path as JSON, creating parent directories as
// needed.
func SaveLast(path string, la *LastApplied) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(la, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
