package cloudagent

import (
	"os"
	"path/filepath"
	"testing"

	"hamvoipconfiggui-asl3/internal/config"
	"hamvoipconfiggui-asl3/internal/sounds"
	"hamvoipconfiggui-asl3/internal/soundschedule"
	"hamvoipconfiggui-asl3/internal/wxtone"
)

// tempStoreFromFixtures copies the real node fixtures shared with
// internal/asteriskconf/internal/config's own tests (node 1999,
// SimpleUSB, Debian 13, Asterisk 22.9.0+asl3) into a fresh temp dir --
// unlike a minimal hand-written stanza, ListNodes/LoadNode require the
// real [node-main](!) template-inheritance structure to recognize a
// section as a node at all, so a trimmed-down fixture isn't enough here.
func tempStoreFromFixtures(t *testing.T) *config.Store {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"rpt.conf", "usbradio.conf", "simpleusb.conf", "rpt_http_registrations.conf", "modules.conf"} {
		data, err := os.ReadFile(filepath.Join("..", "asteriskconf", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &config.Store{Dir: dir}
}

// newTestAgent builds an Agent for tests that don't care about the
// sounds/soundschedule/wxtone/skywarnplus/sa818 dependencies -- each
// gets a working, empty store backed by its own temp dir/file, since a
// nil *Store would panic the moment a test path touched it. Tests that
// do care about one of these build their own Agent directly via New()
// instead.
func newTestAgent(t *testing.T, settingsPath string, store *config.Store, asteriskBin string) *Agent {
	t.Helper()
	return New(
		settingsPath,
		"wss://cloud.example.com/agent", // fixed test cloudURL -- see New's doc comment
		store,
		asteriskBin,
		sounds.New(t.TempDir(), t.TempDir(), "sox"),
		soundschedule.New(filepath.Join(t.TempDir(), "sound-schedule.json")),
		wxtone.New(filepath.Join(t.TempDir(), "wx-tones.json")),
		"", // skywarnDir -- not installed in these tests
		"", // sa818Port -- auto-detect, not exercised in these tests
		filepath.Join(t.TempDir(), "sa818-last.json"),
		"", // auditLogPath -- audit logging disabled in these tests
	)
}
