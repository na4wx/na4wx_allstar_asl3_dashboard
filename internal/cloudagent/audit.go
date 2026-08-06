package cloudagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// auditEntry is one relayed action attempt, appended as one JSON line
// (JSON Lines format — easy to tail/grep on the device itself, no need
// to load the whole file to append one more record).
type auditEntry struct {
	Time   time.Time `json:"time"`
	Action string    `json:"action"`
	OK     bool      `json:"ok"`
	Error  string    `json:"error,omitempty"`
}

// auditWriter appends one entry per dispatched action to a local file
// — an independent record of what was actually asked of this device,
// readable even if the cloud side's own database were compromised or
// unavailable. mu serializes writes since handleCall dispatches
// concurrently (one goroutine per relayed call).
type auditWriter struct {
	path string
	mu   sync.Mutex
}

func newAuditWriter(path string) *auditWriter {
	return &auditWriter{path: path}
}

// log appends entry, creating the parent directory if needed.
// Best-effort: a logging failure must never block or fail the action
// itself, so every error here is swallowed, matching this app's own
// established "best-effort, log and continue" convention for
// supplementary bookkeeping elsewhere.
func (w *auditWriter) log(entry auditEntry) {
	if w.path == "" {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0700); err != nil {
		return
	}
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}
