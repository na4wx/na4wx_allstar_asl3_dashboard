package relay

import (
	"context"
	"path/filepath"
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

// fakeIax2Configurer records ApplyBindport/RestoreBindport calls without
// touching iax.conf or shelling out to systemctl -- same role as
// fakeBackend above.
type fakeIax2Configurer struct {
	applyCalls   int
	restoreCalls int
	lastPort     int
	applyErr     error
	restoreErr   error
}

func (f *fakeIax2Configurer) ApplyBindport(_ context.Context, port int) error {
	f.applyCalls++
	f.lastPort = port
	return f.applyErr
}
func (f *fakeIax2Configurer) RestoreBindport(context.Context) error {
	f.restoreCalls++
	return f.restoreErr
}

func newTestManager(t *testing.T) (*Manager, *fakeBackend) {
	t.Helper()
	m, fb, _ := newTestManagerWithIax2(t)
	return m, fb
}

func newTestManagerWithIax2(t *testing.T) (*Manager, *fakeBackend, *fakeIax2Configurer) {
	t.Helper()
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	if err := settings.Save(Settings{Enabled: true, PrivateKey: "priv", PublicKey: "pub"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	m := NewManager(settings, "")
	fb := &fakeBackend{}
	m.SetBackend(fb)
	fi := &fakeIax2Configurer{}
	m.SetIax2Configurer(fi)
	return m, fb, fi
}

func TestApplyGrantAppliesTunnel(t *testing.T) {
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
}

func TestApplyGrantFailsWithoutLocalKeypair(t *testing.T) {
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	m := NewManager(settings, "")
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

func TestApplyGrantAppliesBindportWhenExternalPortSet(t *testing.T) {
	m, _, fi := newTestManagerWithIax2(t)
	grant := Grant{CloudPublicKey: "cloud-pub", Endpoint: "203.0.113.10:51820", TunnelIP: "10.90.0.2", TunnelCIDR: "/24", ExternalPort: 40000}

	if err := m.ApplyGrant(context.Background(), grant); err != nil {
		t.Fatalf("ApplyGrant() error = %v", err)
	}
	if fi.applyCalls != 1 {
		t.Fatalf("ApplyBindport calls = %d, want 1", fi.applyCalls)
	}
	if fi.lastPort != 40000 {
		t.Fatalf("ApplyBindport port = %d, want 40000", fi.lastPort)
	}
}

func TestApplyGrantSkipsBindportWhenExternalPortZero(t *testing.T) {
	m, _, fi := newTestManagerWithIax2(t)
	grant := Grant{CloudPublicKey: "cloud-pub", Endpoint: "203.0.113.10:51820", TunnelIP: "10.90.0.2", TunnelCIDR: "/24"}

	if err := m.ApplyGrant(context.Background(), grant); err != nil {
		t.Fatalf("ApplyGrant() error = %v", err)
	}
	if fi.applyCalls != 0 {
		t.Fatalf("ApplyBindport calls = %d, want 0 when the grant has no ExternalPort", fi.applyCalls)
	}
}

func TestDisableRestoresBindport(t *testing.T) {
	m, _, fi := newTestManagerWithIax2(t)
	grant := Grant{CloudPublicKey: "cloud-pub", Endpoint: "203.0.113.10:51820", TunnelIP: "10.90.0.2", TunnelCIDR: "/24", ExternalPort: 40000}
	if err := m.ApplyGrant(context.Background(), grant); err != nil {
		t.Fatalf("ApplyGrant() error = %v", err)
	}

	if err := m.Disable(context.Background()); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if fi.restoreCalls != 1 {
		t.Fatalf("RestoreBindport calls = %d, want 1", fi.restoreCalls)
	}
}

func TestPublicKeyForHelloReturnsFalseWhenDisabled(t *testing.T) {
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	m := NewManager(settings, "")

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
	m := NewManager(settings, "")

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
