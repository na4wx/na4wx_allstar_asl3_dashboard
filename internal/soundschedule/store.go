// Package soundschedule holds the operator's scheduled sound-playback
// entries — deliberately NOT part of internal/config, since these aren't
// an Asterisk-native mechanism (unlike rpt.conf's own scheduler, used for
// scheduled connect/disconnect): app_rpt has no built-in way to schedule
// arbitrary sound-file playback, so this app runs its own small ticker
// (see internal/server's StartSoundSchedulePoller) that reads this store
// and calls out to `asterisk -rx "rpt localplay/playback ..."` directly
// at the right times. Persisted as its own small JSON file, in the same
// spirit as internal/sa818's last-applied-settings record, but with real
// locking: unlike that file (written rarely, only from a human-driven
// HTTP handler), this one is read on a timer by a background goroutine
// while HTTP handlers write to it concurrently.
package soundschedule

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Reach values for Entry.Reach — see internal/system's RptLocalPlay vs
// RptPlayback doc comments for what each actually does.
const (
	ReachLocal   = "local"
	ReachNetwork = "network"
)

// Entry is one scheduled sound-playback rule. Minute/Hour/DayOfMonth/
// Month each hold "*" or a plain non-negative integer, matching the same
// field shape app_rpt's own native scheduler uses (see
// config.ScheduleEntry) for a consistent mental model across both halves
// of the "Automation" tab — but unlike that native mechanism, DaysOfWeek
// here is a real list, not limited to one value, since this is this app's
// own format and isn't constrained by app_rpt's schedule-stanza syntax.
// An empty DaysOfWeek means every day.
type Entry struct {
	ID    string `json:"id"`
	Node  string `json:"node"`
	File  string `json:"file"`
	Reach string `json:"reach"` // ReachLocal or ReachNetwork

	Minute     string `json:"minute"`
	Hour       string `json:"hour"`
	DayOfMonth string `json:"day_of_month"`
	Month      string `json:"month"`
	DaysOfWeek []int  `json:"days_of_week,omitempty"` // 0 = Sunday, matching time.Weekday
}

// matchField reports whether field ("*" or a plain integer) matches
// value.
func matchField(field string, value int) bool {
	if field == "" || field == "*" {
		return true
	}
	n, err := strconv.Atoi(field)
	return err == nil && n == value
}

// Matches reports whether t falls within e's schedule, to the minute.
func (e Entry) Matches(t time.Time) bool {
	if !matchField(e.Minute, t.Minute()) {
		return false
	}
	if !matchField(e.Hour, t.Hour()) {
		return false
	}
	if !matchField(e.DayOfMonth, t.Day()) {
		return false
	}
	if !matchField(e.Month, int(t.Month())) {
		return false
	}
	if len(e.DaysOfWeek) == 0 {
		return true
	}
	weekday := int(t.Weekday())
	for _, d := range e.DaysOfWeek {
		if d == weekday {
			return true
		}
	}
	return false
}

// Store persists Entry records as a single JSON file at path. A real
// mutex (not the lock-free pattern internal/sa818 uses) guards every
// load-modify-save cycle, since a background ticker reads this
// concurrently with HTTP-handler writes.
type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store {
	return &Store{path: path}
}

// load reads every entry from disk. A missing file is not an error — it
// just means nothing has been scheduled yet. Caller must hold s.mu.
func (s *Store) load() ([]Entry, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// save writes entries back to disk, creating the parent directory if
// needed. Caller must hold s.mu.
func (s *Store) save(entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// List returns every scheduled sound entry, across all nodes.
func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// ListForNode returns the scheduled sound entries for one node.
func (s *Store) ListForNode(node string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return nil, err
	}
	// Non-nil even with zero matches -- see config.Store.ListNodes's
	// identical comment; the cloud relay's soundSchedule.list action
	// sends this straight to the browser as JSON.
	out := []Entry{}
	for _, e := range entries {
		if e.Node == node {
			out = append(out, e)
		}
	}
	return out, nil
}

// Save adds e as a new entry (blank ID — a fresh one is generated) or
// updates an existing entry with a matching ID.
func (s *Store) Save(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	if e.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		e.ID = id
		entries = append(entries, e)
		return s.save(entries)
	}
	for i := range entries {
		if entries[i].ID == e.ID {
			entries[i] = e
			return s.save(entries)
		}
	}
	entries = append(entries, e)
	return s.save(entries)
}

// Delete removes one entry by ID. Deleting an ID that doesn't exist is
// not an error.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	out := entries[:0]
	for _, e := range entries {
		if e.ID != id {
			out = append(out, e)
		}
	}
	return s.save(out)
}

// DeleteByNode removes every entry for node — used to clean up when a
// node itself is deleted, since these entries live outside rpt.conf and
// nothing else knows to remove them.
func (s *Store) DeleteByNode(node string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Node != node {
			out = append(out, e)
		}
	}
	return s.save(out)
}

// newID generates a short random hex identifier for a new entry.
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
