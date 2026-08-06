package config

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStoreFromFixtures(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"rpt.conf", "usbradio.conf", "simpleusb.conf"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Store{Dir: dir}
}

func TestUpdateNodeSettingsChangesRxChannel(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.UpdateNodeSettings("1999", map[string]string{"rxchannel": "Radio/1999"}); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.RxChannel != "Radio/1999" {
		t.Errorf("RxChannel = %q, want \"Radio/1999\"", view.RxChannel)
	}
	if view.Interface != "USBRadio" {
		t.Errorf("Interface = %q, want \"USBRadio\" after switching driver", view.Interface)
	}
}

func TestUpdateRadioSettingsChangesSimpleusbTune(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.UpdateRadioSettings("1999", map[string]string{"rxmixerset": "700"}); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.Radio.RxMixerSet != "700" {
		t.Errorf("RxMixerSet = %q, want \"700\"", view.Radio.RxMixerSet)
	}
}

func TestUpdateRadioSettingsHubNodeErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rpt.conf", `
[node-main](!)
rxchannel = Local/pseudo
duplex = 2

[1998](node-main)
`)
	store := &Store{Dir: dir}
	if err := store.UpdateRadioSettings("1998", map[string]string{"rxmixerset": "700"}); err == nil {
		t.Error("expected an error updating radio settings on a hub node")
	}
}
