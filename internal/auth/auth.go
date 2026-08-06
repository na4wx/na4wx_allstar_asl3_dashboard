// Package auth provides simple username/password authentication with
// server-side sessions. The GUI edits Asterisk configuration and runs
// privileged commands, so every request except the initial setup and
// login pages must carry a valid session.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = 12 * time.Hour

type credentials struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

type session struct {
	username string
	expires  time.Time
}

// Manager owns the credential file and in-memory session table.
type Manager struct {
	path string

	mu    sync.RWMutex
	creds *credentials

	sessMu   sync.Mutex
	sessions map[string]session
}

// NewManager loads credentials from path if it exists. A missing file is
// not an error: Configured() will report false until SetCredentials is
// called (the server should route to a first-run setup page).
func NewManager(path string) (*Manager, error) {
	m := &Manager{path: path, sessions: map[string]session{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read %s: %w", path, err)
	}
	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("auth: parse %s: %w", path, err)
	}
	m.creds = &c
	return m, nil
}

// Configured reports whether initial credentials have been set up.
func (m *Manager) Configured() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.creds != nil
}

// SetCredentials hashes and persists a new username/password, replacing
// any existing credentials (and invalidating all sessions).
func (m *Manager) SetCredentials(username, password string) error {
	if username == "" {
		return errors.New("username is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}
	c := &credentials{Username: username, PasswordHash: string(hash)}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.path, data, 0600); err != nil {
		return fmt.Errorf("auth: write %s: %w", m.path, err)
	}

	m.mu.Lock()
	m.creds = c
	m.mu.Unlock()

	m.sessMu.Lock()
	m.sessions = map[string]session{}
	m.sessMu.Unlock()
	return nil
}

// Verify checks username/password against stored credentials.
func (m *Manager) Verify(username, password string) bool {
	m.mu.RLock()
	c := m.creds
	m.mu.RUnlock()
	if c == nil || username != c.Username {
		// Still run bcrypt to keep timing similar whether or not the
		// username matched.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinva"), []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(password)) == nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateSession issues a new session token for username.
func (m *Manager) CreateSession(username string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	m.sessMu.Lock()
	defer m.sessMu.Unlock()
	m.sessions[token] = session{username: username, expires: time.Now().Add(sessionTTL)}
	return token, nil
}

// ValidateSession returns the username for a live session token, sliding
// its expiry forward on use.
func (m *Manager) ValidateSession(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	m.sessMu.Lock()
	defer m.sessMu.Unlock()
	s, ok := m.sessions[token]
	if !ok || time.Now().After(s.expires) {
		delete(m.sessions, token)
		return "", false
	}
	s.expires = time.Now().Add(sessionTTL)
	m.sessions[token] = s
	return s.username, true
}

// DestroySession invalidates a session token (logout).
func (m *Manager) DestroySession(token string) {
	m.sessMu.Lock()
	defer m.sessMu.Unlock()
	delete(m.sessions, token)
}
