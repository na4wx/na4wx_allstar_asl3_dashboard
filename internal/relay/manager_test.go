package relay

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBackend records ApplyTunnel/TeardownInterface calls without
// shelling out -- same role as internal/wifi/manager_test.go's own
// fakeBackend.
type fakeBackend struct {
	applyCalls    int
	teardownCalls int
	lastPrivKey   string
	lastGrant     Grant
	applyErr      error
	teardownErr   error
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) ApplyTunnel(_ context.Context, privateKey string, grant Grant) error {
	f.applyCalls++
	f.lastPrivKey = privateKey
	f.lastGrant = grant
	return f.applyErr
}
func (f *fakeBackend) TeardownInterface(context.Context) error {
	f.teardownCalls++
	return f.teardownErr
}

// fakeAsterisk writes a fake "asterisk" binary that always succeeds --
// enough for ApplyGrant/Disable's own reload call, which this package's
// tests don't otherwise care about the exact CLI form of (that's covered
// by internal/system's own AsteriskReloadIax2 tests).
func fakeAsterisk(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "asterisk")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake asterisk: %v", err)
	}
	return path
}

// writeMinimalIax2Conf creates a real-shaped iax2.conf in dir --
// asteriskconf.SetValues (like the real Asterisk config file it edits)
// only ever edits an existing file, never creates one from scratch,
// same as every real deployment where Asterisk itself ships iax2.conf.
func writeMinimalIax2Conf(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "iax2.conf"), []byte("[general]\n"), 0644); err != nil {
		t.Fatalf("write iax2.conf fixture: %v", err)
	}
}

func newTestManager(t *testing.T) (*Manager, *fakeBackend) {
	t.Helper()
	asteriskDir := t.TempDir()
	writeMinimalIax2Conf(t, asteriskDir)
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	if err := settings.Save(Settings{Enabled: true, PrivateKey: "priv", PublicKey: "pub"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	m := NewManager(settings, asteriskDir, fakeAsterisk(t))
	fb := &fakeBackend{}
	m.SetBackend(fb)
	return m, fb
}

func TestApplyGrantAppliesTunnelAndWritesBindaddr(t *testing.T) {
	m, fb := newTestManager(t)
	grant := Grant{CloudPublicKey: "cloud-pub", Endpoint: "203.0.113.10:51820", TunnelIP: "10.90.0.2", TunnelCIDR: "/24"}

	if err := m.ApplyGrant(context.Background(), grant); err != nil {
		t.Fatalf("ApplyGrant() error = %v", err)
	}
	if fb.applyCalls != 1 {
		t.Fatalf("ApplyTunnel calls = %d, want 1", fb.applyCalls)
	}
	if fb.lastPrivKey != "priv" {
		t.Fatalf("ApplyTunnel privateKey = %q, want %q", fb.lastPrivKey, "priv")
	}
	if fb.lastGrant != grant {
		t.Fatalf("ApplyTunnel grant = %+v, want %+v", fb.lastGrant, grant)
	}

	active, gotGrant := m.Status()
	if !active {
		t.Fatal("Status() active = false, want true after a successful ApplyGrant")
	}
	if gotGrant != grant {
		t.Fatalf("Status() grant = %+v, want %+v", gotGrant, grant)
	}

	iax2 := filepath.Join(m.asteriskDir, "iax2.conf")
	data, err := os.ReadFile(iax2)
	if err != nil {
		t.Fatalf("reading iax2.conf: %v", err)
	}
	if got := string(data); !strings.Contains(got, "bindaddr") || !strings.Contains(got, "10.90.0.2") {
		t.Fatalf("iax2.conf = %q, want it to set bindaddr to the tunnel IP", got)
	}
}

func TestApplyGrantFailsWithoutLocalKeypair(t *testing.T) {
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	m := NewManager(settings, t.TempDir(), fakeAsterisk(t))
	m.SetBackend(&fakeBackend{})

	if err := m.ApplyGrant(context.Background(), Grant{}); err == nil {
		t.Fatal("ApplyGrant() error = nil, want an error when no local keypair has been generated yet")
	}
}

func TestDisableIsNoOpWhenNeverApplied(t *testing.T) {
	m, fb := newTestManager(t)
	if err := m.Disable(context.Background()); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if fb.teardownCalls != 0 {
		t.Fatalf("TeardownInterface calls = %d, want 0 when nothing was ever applied", fb.teardownCalls)
	}
}

func TestDisableTearsDownAfterApplyGrant(t *testing.T) {
	m, fb := newTestManager(t)
	grant := Grant{CloudPublicKey: "cloud-pub", Endpoint: "203.0.113.10:51820", TunnelIP: "10.90.0.2", TunnelCIDR: "/24"}
	if err := m.ApplyGrant(context.Background(), grant); err != nil {
		t.Fatalf("ApplyGrant() error = %v", err)
	}

	if err := m.Disable(context.Background()); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if fb.teardownCalls != 1 {
		t.Fatalf("TeardownInterface calls = %d, want 1", fb.teardownCalls)
	}
	active, _ := m.Status()
	if active {
		t.Fatal("Status() active = true, want false after Disable")
	}
}

func TestPublicKeyForHelloReturnsFalseWhenDisabled(t *testing.T) {
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	m := NewManager(settings, t.TempDir(), fakeAsterisk(t))

	_, ok, err := m.PublicKeyForHello(context.Background())
	if err != nil {
		t.Fatalf("PublicKeyForHello() error = %v", err)
	}
	if ok {
		t.Fatal("PublicKeyForHello() ok = true, want false when relay is disabled locally")
	}
}

func TestPublicKeyForHelloReusesExistingKeypair(t *testing.T) {
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	if err := settings.Save(Settings{Enabled: true, PrivateKey: "priv", PublicKey: "pub"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	m := NewManager(settings, t.TempDir(), fakeAsterisk(t))

	key, ok, err := m.PublicKeyForHello(context.Background())
	if err != nil {
		t.Fatalf("PublicKeyForHello() error = %v", err)
	}
	if !ok {
		t.Fatal("PublicKeyForHello() ok = false, want true when relay is enabled")
	}
	if key != "pub" {
		t.Fatalf("PublicKeyForHello() key = %q, want the already-persisted %q (not regenerated)", key, "pub")
	}
}
