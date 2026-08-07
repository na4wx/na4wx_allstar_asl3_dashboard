package asteriskconf

import (
	"os"
	"strings"
	"testing"
)

func TestEnsureModuleLoadedFlipsNoloadToLoad(t *testing.T) {
	path := writeTempConf(t, `[modules]
autoload = no

noload  = chan_simpleusb.so              ; SimpleUSB Radio Interface Channel Driver
load    = chan_usbradio.so               ; USB Console Channel Driver
`)
	if err := EnsureModuleLoaded(path, "modules", "chan_simpleusb.so"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "load    = chan_simpleusb.so") {
		t.Errorf("chan_simpleusb.so should now be loaded:\n%s", s)
	}
	if strings.Contains(s, "noload  = chan_simpleusb.so") {
		t.Errorf("old noload line should be gone:\n%s", s)
	}
	// The comment should survive the rewrite.
	if !strings.Contains(s, "SimpleUSB Radio Interface Channel Driver") {
		t.Errorf("trailing comment should be preserved:\n%s", s)
	}
}

func TestEnsureModuleLoadedNoopWhenAlreadyLoaded(t *testing.T) {
	const orig = "[modules]\nload    = chan_usbradio.so\n"
	path := writeTempConf(t, orig)
	if err := EnsureModuleLoaded(path, "modules", "chan_usbradio.so"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("file should be unchanged when already loaded:\ngot:\n%q\nwant:\n%q", got, orig)
	}
}

func TestEnsureModuleLoadedNoopWhenRequired(t *testing.T) {
	// "require" is stronger than "load" -- must never be downgraded.
	const orig = "[modules]\nrequire = res_usbradio.so\n"
	path := writeTempConf(t, orig)
	if err := EnsureModuleLoaded(path, "modules", "res_usbradio.so"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("require line must not be touched:\ngot:\n%q", got)
	}
}

func TestEnsureModuleLoadedAppendsWhenNotListed(t *testing.T) {
	path := writeTempConf(t, "[modules]\nautoload = no\n")
	if err := EnsureModuleLoaded(path, "modules", "chan_voter.so"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "load    = chan_voter.so") {
		t.Errorf("should append a new load line, got:\n%s", got)
	}
}

func TestEnsureModuleNotLoadedFlipsLoadToNoload(t *testing.T) {
	path := writeTempConf(t, "[modules]\nload    = chan_simpleusb.so              ; comment\n")
	if err := EnsureModuleNotLoaded(path, "modules", "chan_simpleusb.so"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "noload  = chan_simpleusb.so") {
		t.Errorf("should now be noload:\n%s", s)
	}
	if strings.Contains(s, "load    = chan_simpleusb.so") {
		t.Errorf("old load line should be gone:\n%s", s)
	}
}

func TestEnsureModuleNotLoadedNeverTouchesRequire(t *testing.T) {
	const orig = "[modules]\nrequire = app_rpt.so\n"
	path := writeTempConf(t, orig)
	if err := EnsureModuleNotLoaded(path, "modules", "app_rpt.so"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("require line must never be disabled automatically:\ngot:\n%q", got)
	}
}

func TestEnsureModuleNotLoadedNoopWhenNotListed(t *testing.T) {
	const orig = "[modules]\nautoload = no\n"
	path := writeTempConf(t, orig)
	if err := EnsureModuleNotLoaded(path, "modules", "chan_voter.so"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("nothing to do, file should be unchanged:\ngot:\n%q", got)
	}
}

func TestEnsureModuleLoadedOnRealFixtureSwitchesSimplexToUSBRadioAndBack(t *testing.T) {
	data, err := os.ReadFile("testdata/modules.conf")
	if err != nil {
		t.Fatal(err)
	}
	path := writeTempConf(t, string(data))

	// The real fixture starts with chan_usbradio.so loaded and
	// chan_simpleusb.so noload'ed (matching the node's state right
	// after the fix that resolved the real bug this exists for).
	if err := EnsureModuleLoaded(path, "modules", "chan_simpleusb.so"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureModuleNotLoaded(path, "modules", "chan_usbradio.so"); err != nil {
		t.Fatal(err)
	}

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sec, ok := f.Section("modules")
	if !ok {
		t.Fatal("no [modules] section")
	}
	got, _ := sec.Value("autoload") // sanity: rest of the file still parses fine
	if got != "no" {
		t.Errorf("autoload = %q, want \"no\" -- rest of the file should be untouched", got)
	}

	// Confirm via a fresh parse that resolving each module's real
	// effective key now reflects the swap. Section.Value looks at the
	// last occurrence of a given KEY, but since we're checking two
	// DIFFERENT keys (load vs noload) for the same value, check by
	// scanning pairs directly instead.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "load    = chan_simpleusb.so") {
		t.Errorf("chan_simpleusb.so should be loaded now:\n%s", s)
	}
	if !strings.Contains(s, "noload  = chan_usbradio.so") {
		t.Errorf("chan_usbradio.so should be noload now:\n%s", s)
	}
	// require = res_usbradio.so must survive completely untouched --
	// confirmed it's a distinct, unrelated module.
	if !strings.Contains(s, "require = res_usbradio.so") {
		t.Errorf("unrelated require line should be untouched:\n%s", s)
	}
}
