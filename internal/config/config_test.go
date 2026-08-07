package config

import (
	"os"
	"path/filepath"
	"testing"
)

// testdata/*.conf are copies of the same real-node fixtures used by
// internal/asteriskconf's own tests (node 1999, SimpleUSB, Debian 13,
// Asterisk 22.9.0+asl3).

func testStore() *Store {
	return &Store{Dir: "testdata"}
}

func TestListNodesFindsRealNode(t *testing.T) {
	nodes, err := testStore().ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0] != "1999" {
		t.Errorf("ListNodes = %v, want [1999]", nodes)
	}
}

func TestListNodesExcludesTemplates(t *testing.T) {
	nodes, err := testStore().ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n == "node-main" || n == "functions-main" || n == "telemetry-main" {
			t.Errorf("ListNodes should not include template section %q", n)
		}
	}
}

func TestLoadNodeResolvesSimpleUSBInterface(t *testing.T) {
	view, err := testStore().LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.Interface != "SimpleUSB" {
		t.Errorf("Interface = %q, want %q", view.Interface, "SimpleUSB")
	}
	if view.RxChannel != "SimpleUSB/1999" {
		t.Errorf("RxChannel = %q", view.RxChannel)
	}
	// rpt.conf's own duplex (repeater/telemetry), inherited from node-main
	// since the real node's [1999] stanza doesn't override it.
	if view.Duplex != "2" {
		t.Errorf("Duplex = %q, want \"2\"", view.Duplex)
	}
}

func TestLoadNodePopulatesRadioFromSimpleusbConf(t *testing.T) {
	view, err := testStore().LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.Radio == nil {
		t.Fatal("Radio is nil, want populated (node uses SimpleUSB)")
	}
	if view.Radio.Driver != "simpleusb" {
		t.Errorf("Radio.Driver = %q, want \"simpleusb\"", view.Radio.Driver)
	}
	if view.Radio.RxMixerSet != "500" {
		t.Errorf("Radio.RxMixerSet = %q, want \"500\"", view.Radio.RxMixerSet)
	}
	// usbradio-only fields must stay empty for a simpleusb node.
	if view.Radio.DriverDuplex != "" {
		t.Errorf("Radio.DriverDuplex = %q, want empty (simpleusb has no driver-level duplex)", view.Radio.DriverDuplex)
	}
	// Confirmed against the real node's own simpleusb.conf: carrierfrom
	// and ctcssfrom both default to usbinvert there.
	if view.Radio.CarrierFrom != "usbinvert" {
		t.Errorf("Radio.CarrierFrom = %q, want \"usbinvert\"", view.Radio.CarrierFrom)
	}
	if view.Radio.CtcssFrom != "usbinvert" {
		t.Errorf("Radio.CtcssFrom = %q, want \"usbinvert\"", view.Radio.CtcssFrom)
	}
}

func TestLoadNodePopulatesCarrierFromForUsbradio(t *testing.T) {
	// Confirmed against the real node's own usbradio.conf: carrierfrom
	// and ctcssfrom both default to dsp there -- this is the setting
	// that (along with the corresponding channel driver actually being
	// loaded, see modules.go) determines whether RX audio detection
	// uses a hardware COR line or software/DSP-based signal analysis.
	store := tempStoreFromFixtures(t)
	if err := store.UpdateNodeSettings("1999", map[string]string{"rxchannel": "Radio/1999"}); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.Radio == nil || view.Radio.Driver != "usbradio" {
		t.Fatalf("view.Radio = %+v", view.Radio)
	}
	if view.Radio.CarrierFrom != "dsp" {
		t.Errorf("Radio.CarrierFrom = %q, want \"dsp\"", view.Radio.CarrierFrom)
	}
	if view.Radio.CtcssFrom != "dsp" {
		t.Errorf("Radio.CtcssFrom = %q, want \"dsp\"", view.Radio.CtcssFrom)
	}
}

func TestLoadNodeUnknownNodeErrors(t *testing.T) {
	if _, err := testStore().LoadNode("9999"); err == nil {
		t.Error("LoadNode(\"9999\") should error for a node that doesn't exist")
	}
}

func TestLoadNodeHubHasNoRadio(t *testing.T) {
	// Synthetic: a hub node (rxchannel = Local/pseudo, never overridden)
	// should resolve to Interface "Hub (no radio)" with no Radio view --
	// distinct from a real radio-backed node.
	dir := t.TempDir()
	writeFile(t, dir, "rpt.conf", `
[node-main](!)
rxchannel = Local/pseudo
duplex = 2

[1998](node-main)
`)
	store := &Store{Dir: dir}
	view, err := store.LoadNode("1998")
	if err != nil {
		t.Fatal(err)
	}
	if view.Interface != "Hub (no radio)" {
		t.Errorf("Interface = %q, want \"Hub (no radio)\"", view.Interface)
	}
	if view.Radio != nil {
		t.Errorf("Radio = %+v, want nil for a hub node", view.Radio)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
