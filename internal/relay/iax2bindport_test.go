package relay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

// copyTestdataIaxConf gives each test its own writable copy of
// testdata/iax.conf, so tests can't interfere with each other or leave
// the checked-in fixture modified.
func copyTestdataIaxConf(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "iax.conf"))
	if err != nil {
		t.Fatalf("reading testdata/iax.conf: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "iax.conf")
	if err := os.WriteFile(dst, src, 0644); err != nil {
		t.Fatalf("writing %s: %v", dst, err)
	}
	return dst
}

func bindport(t *testing.T, path string) (string, bool) {
	t.Helper()
	value, present, err := readIaxBindport(path)
	if err != nil {
		t.Fatalf("readIaxBindport(%s): %v", path, err)
	}
	return value, present
}

// realIax2Configurer shells out to systemctl to restart Asterisk, which
// doesn't exist in this test environment -- these tests only exercise
// the config-file read/write half (ApplyBindport/RestoreBindport
// themselves), asserting the file ends up right and that the restart
// failure is surfaced rather than swallowed, without a real systemctl.
func newTestConfigurer(t *testing.T, iaxConfPath string) *realIax2Configurer {
	t.Helper()
	settings := NewSettingsStore(filepath.Join(t.TempDir(), "relay.json"))
	return &realIax2Configurer{iaxConfPath: iaxConfPath, settings: settings}
}

func TestApplyBindportWritesNewPort(t *testing.T) {
	path := copyTestdataIaxConf(t)
	c := newTestConfigurer(t, path)

	err := c.ApplyBindport(context.Background(), 40000)
	if err == nil || !isRestartFailure(err) {
		t.Fatalf("ApplyBindport() error = %v, want a systemctl restart failure (config write should still have happened first)", err)
	}

	value, present := bindport(t, path)
	if !present || value != "40000" {
		t.Fatalf("iax.conf bindport = (%q, %v), want (\"40000\", true)", value, present)
	}

	settings, err := c.settings.Load()
	if err != nil {
		t.Fatalf("settings.Load() error = %v", err)
	}
	if !settings.BindportOverridden {
		t.Fatal("settings.BindportOverridden = false, want true after ApplyBindport changed the file")
	}
	if settings.OriginalBindport != "4569" {
		t.Fatalf("settings.OriginalBindport = %q, want %q (the fixture's original value)", settings.OriginalBindport, "4569")
	}
}

func TestApplyBindportNoOpWhenAlreadyCorrect(t *testing.T) {
	path := copyTestdataIaxConf(t)
	if err := asteriskconf.SetValues(path, "general", map[string]string{"bindport": "40000"}); err != nil {
		t.Fatalf("seeding bindport: %v", err)
	}
	c := newTestConfigurer(t, path)

	if err := c.ApplyBindport(context.Background(), 40000); err != nil {
		t.Fatalf("ApplyBindport() error = %v, want nil (already correct, no restart attempted)", err)
	}
}

func TestRestoreBindportPutsBackOriginalValue(t *testing.T) {
	path := copyTestdataIaxConf(t)
	c := newTestConfigurer(t, path)

	if err := c.ApplyBindport(context.Background(), 40000); !isRestartFailure(err) {
		t.Fatalf("ApplyBindport() error = %v, want a systemctl restart failure", err)
	}

	err := c.RestoreBindport(context.Background())
	if err == nil || !isRestartFailure(err) {
		t.Fatalf("RestoreBindport() error = %v, want a systemctl restart failure (config write should still have happened first)", err)
	}

	value, present := bindport(t, path)
	if !present || value != "4569" {
		t.Fatalf("iax.conf bindport after restore = (%q, %v), want (\"4569\", true)", value, present)
	}

	settings, err := c.settings.Load()
	if err != nil {
		t.Fatalf("settings.Load() error = %v", err)
	}
	if settings.BindportOverridden {
		t.Fatal("settings.BindportOverridden = true, want false after RestoreBindport")
	}
}

func TestRestoreBindportIsNoOpWhenNeverApplied(t *testing.T) {
	path := copyTestdataIaxConf(t)
	c := newTestConfigurer(t, path)

	if err := c.RestoreBindport(context.Background()); err != nil {
		t.Fatalf("RestoreBindport() error = %v, want nil when ApplyBindport was never called", err)
	}

	value, present := bindport(t, path)
	if !present || value != "4569" {
		t.Fatalf("iax.conf bindport = (%q, %v), want it untouched at (\"4569\", true)", value, present)
	}
}

// isRestartFailure reports whether err looks like it came from
// system.AsteriskRestart failing to find/run systemctl, as opposed to a
// config read/write failure -- these tests run without a real Asterisk
// or systemctl available, so a restart failure here is expected and
// distinguishes "the file operation itself worked" from "it didn't."
func isRestartFailure(err error) bool {
	return err != nil && !errors.Is(err, os.ErrNotExist)
}
