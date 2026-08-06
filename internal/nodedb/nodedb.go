// Package nodedb keeps a local copy of AllStarLink's published node
// directory, which maps a node number to the callsign, description and
// location its owner registered.
//
// This is a different file from rpt_extnodes, which app_rpt itself uses:
// that one maps node numbers to IP addresses so a connection can be
// routed, and carries no callsign at all. This one is purely
// descriptive — it makes "49616" read as "49616 · NA4WX" and nothing in
// the node's actual operation depends on it. If it's missing or stale,
// everything still works and the UI just shows bare numbers.
package nodedb

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultURL is AllStarLink's published database endpoint.
const DefaultURL = "https://allmondb.allstarlink.org/allmondb.php"

// DefaultPath is where ASL's own tooling (asl3-update-astdb) writes this
// file, so anything else on the system that reads a node database looks
// here too.
const DefaultPath = "/var/lib/asterisk/astdb.txt"

// refreshInterval is how often the database is re-downloaded. The
// directory changes slowly — new nodes get registered, callsigns rarely
// change — so daily is generous. ASL's own updater runs 4x/day.
const refreshInterval = 24 * time.Hour

// fetchTimeout bounds a single download. The file is a few megabytes,
// and this runs on a Pi that may be on a slow connection, so this is
// deliberately patient rather than snappy.
const fetchTimeout = 2 * time.Minute

// Entry is one node's published directory information.
type Entry struct {
	Number      string
	Callsign    string
	Description string
	Location    string
}

// Label renders the entry for display next to a node number, preferring
// the callsign and falling back to the description when a node was
// registered without one.
func (e Entry) Label() string {
	if e.Callsign != "" {
		return e.Callsign
	}
	return e.Description
}

// Parse reads the pipe-delimited node database:
//
//	2000|WB6NIL|ASL Public Hub|Los Angeles, CA
//
// Fields are number, callsign, description, location. Lines with fewer
// fields are kept with the rest left blank rather than dropped, since a
// node number alone is still worth having; lines with no number at all
// are skipped. A description containing a pipe would otherwise split
// into a bogus extra field, so the split is bounded at 4.
func Parse(r io.Reader) (map[string]Entry, error) {
	out := make(map[string]Entry)
	sc := bufio.NewScanner(r)
	// The longest lines are a few hundred bytes, but a corrupt download
	// shouldn't abort the scan with "token too long".
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "|", 4)
		number := strings.TrimSpace(f[0])
		if number == "" {
			continue
		}
		e := Entry{Number: number}
		if len(f) > 1 {
			e.Callsign = strings.TrimSpace(f[1])
		}
		if len(f) > 2 {
			e.Description = strings.TrimSpace(f[2])
		}
		if len(f) > 3 {
			e.Location = strings.TrimSpace(f[3])
		}
		out[number] = e
	}
	return out, sc.Err()
}

// Store holds the parsed database plus where it lives on disk and where
// it's fetched from. Safe for concurrent use: the refresh goroutine
// writes while page renders read.
type Store struct {
	path string
	url  string

	mu        sync.RWMutex
	entries   map[string]Entry
	fetchedAt time.Time
	lastErr   string
}

func New(path, url string) *Store {
	if path == "" {
		path = DefaultPath
	}
	if url == "" {
		url = DefaultURL
	}
	return &Store{path: path, url: url, entries: make(map[string]Entry)}
}

// Lookup returns the directory entry for a node number, if known.
func (s *Store) Lookup(number string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[number]
	return e, ok
}

// Label is the common case: the callsign to show beside a node number,
// or "" when the node isn't in the directory.
func (s *Store) Label(number string) string {
	if e, ok := s.Lookup(number); ok {
		return e.Label()
	}
	return ""
}

// Status reports what the UI needs to explain itself: how many nodes are
// known, when the copy was last refreshed, and the last failure if any.
func (s *Store) Status() (count int, fetchedAt time.Time, lastErr string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries), s.fetchedAt, s.lastErr
}

// LoadFile reads the on-disk copy. A missing file is not an error — it
// just means nothing has been downloaded yet, which is the normal state
// on a fresh install and on HamVoIP, which ships no updater of its own.
func (s *Store) LoadFile() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	entries, err := Parse(f)
	if err != nil {
		return err
	}
	info, statErr := f.Stat()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = entries
	if statErr == nil {
		s.fetchedAt = info.ModTime()
	}
	return nil
}

// Refresh downloads a fresh copy, writes it to disk, and swaps it in.
// The download is written to a temporary file and renamed into place, so
// an interrupted or truncated transfer can't leave a half-written
// database behind — the previous copy stays intact until a complete one
// has landed.
func (s *Store) Refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return s.recordErr(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return s.recordErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return s.recordErr(fmt.Errorf("node database server returned %s", resp.Status))
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return s.recordErr(err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".astdb-*")
	if err != nil {
		return s.recordErr(err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return s.recordErr(err)
	}
	if err := tmp.Close(); err != nil {
		return s.recordErr(err)
	}

	// Parse before publishing, so a syntactically broken download is
	// rejected rather than replacing a good database with an empty one.
	f, err := os.Open(tmpName)
	if err != nil {
		return s.recordErr(err)
	}
	entries, parseErr := Parse(f)
	f.Close()
	if parseErr != nil {
		return s.recordErr(parseErr)
	}
	if len(entries) == 0 {
		return s.recordErr(fmt.Errorf("downloaded node database contained no usable entries"))
	}

	if err := os.Chmod(tmpName, 0o644); err != nil {
		return s.recordErr(err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return s.recordErr(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = entries
	s.fetchedAt = time.Now()
	s.lastErr = ""
	return nil
}

func (s *Store) recordErr(err error) error {
	s.mu.Lock()
	s.lastErr = err.Error()
	s.mu.Unlock()
	return err
}

// stale reports whether the on-disk copy is old enough to re-download.
func (s *Store) stale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries) == 0 || time.Since(s.fetchedAt) >= refreshInterval
}

// StartDailyRefresh loads the on-disk copy, downloads immediately if
// there isn't a current one, and re-downloads daily thereafter. Runs
// until ctx is cancelled.
//
// A failed download is not fatal and is not retried aggressively: the
// node's actual operation doesn't depend on this file, and a box that
// can't reach the internet shouldn't spend the day retrying. The last
// error is surfaced in the UI instead.
func (s *Store) StartDailyRefresh(ctx context.Context, onErr func(error)) {
	report := func(err error) {
		if err != nil && onErr != nil {
			onErr(err)
		}
	}
	go func() {
		report(s.LoadFile())
		if s.stale() {
			report(s.Refresh(ctx))
		}
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report(s.Refresh(ctx))
			}
		}
	}()
}
