package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func modulesConfContent(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "modules.conf"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSwitchingNodeInterfaceSyncsModulesConf reproduces the exact real
// bug found on a live node: the fixture's modules.conf starts with
// chan_usbradio.so loaded and chan_simpleusb.so noload'ed (its real
// post-incident state), while node 1999 in the fixture is still on
// SimpleUSB -- exactly the mismatch that made Asterisk log "Channel
// tech 'SimpleUSB' is not currently loaded, not adding node" and the
// node never come up. Switching it to SimpleUSB via UpdateNodeSettings
// must now also flip modules.conf so the two stay consistent.
func TestSwitchingNodeInterfaceSyncsModulesConf(t *testing.T) {
	store := tempStoreFromFixtures(t)

	before := modulesConfContent(t, store.Dir)
	if !strings.Contains(before, "noload  = chan_simpleusb.so") || !strings.Contains(before, "load    = chan_usbradio.so") {
		t.Fatalf("fixture assumption changed, adjust this test:\n%s", before)
	}

	if err := store.UpdateNodeSettings("1999", map[string]string{"rxchannel": "SimpleUSB/1999"}); err != nil {
		t.Fatal(err)
	}

	after := modulesConfContent(t, store.Dir)
	if !strings.Contains(after, "load    = chan_simpleusb.so") {
		t.Errorf("chan_simpleusb.so should now be loaded:\n%s", after)
	}
	// No other node in the fixture uses USBRadio, so it should be
	// disabled now that nothing needs it.
	if !strings.Contains(after, "noload  = chan_usbradio.so") {
		t.Errorf("chan_usbradio.so should be disabled since no node uses it anymore:\n%s", after)
	}
}

// TestSwitchingNodeInterfaceLeavesOtherDriverAloneWhenStillNeeded
// confirms syncModulesForRxChannel's own safety rule: it must never
// disable a driver another configured node still depends on.
func TestSwitchingNodeInterfaceLeavesOtherDriverAloneWhenStillNeeded(t *testing.T) {
	store := tempStoreFromFixtures(t)

	// A second node still on USBRadio.
	if err := store.CreateNode("2000", "Radio/2000", "2"); err != nil {
		t.Fatal(err)
	}
	// Now switch 1999 to SimpleUSB -- chan_usbradio.so must stay loaded
	// for node 2000's sake.
	if err := store.UpdateNodeSettings("1999", map[string]string{"rxchannel": "SimpleUSB/1999"}); err != nil {
		t.Fatal(err)
	}

	after := modulesConfContent(t, store.Dir)
	if !strings.Contains(after, "load    = chan_simpleusb.so") {
		t.Errorf("chan_simpleusb.so should be loaded for node 1999:\n%s", after)
	}
	if !strings.Contains(after, "load    = chan_usbradio.so") {
		t.Errorf("chan_usbradio.so must stay loaded -- node 2000 still needs it:\n%s", after)
	}
}

func TestCreateNodeSyncsModulesConf(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("2000", "SimpleUSB/2000", "2"); err != nil {
		t.Fatal(err)
	}
	after := modulesConfContent(t, store.Dir)
	if !strings.Contains(after, "load    = chan_simpleusb.so") {
		t.Errorf("chan_simpleusb.so should be loaded after creating a SimpleUSB node:\n%s", after)
	}
}

func TestCreateHubNodeDoesNotTouchModulesConf(t *testing.T) {
	store := tempStoreFromFixtures(t)
	before := modulesConfContent(t, store.Dir)
	if err := store.CreateNode("2000", "Local/pseudo", "2"); err != nil {
		t.Fatal(err)
	}
	after := modulesConfContent(t, store.Dir)
	if before != after {
		t.Errorf("a hub node needs no radio driver -- modules.conf should be untouched\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
