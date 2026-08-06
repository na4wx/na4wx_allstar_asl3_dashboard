package config

import "testing"

func TestValidNodeNumber(t *testing.T) {
	for s, want := range map[string]bool{
		"1999":    true,
		"1":       true,
		"999999":  true,
		"9999999": false, // too many digits
		"":        false,
		"abc":     false,
		"19 99":   false,
		"-1999":   false,
		"1999.0":  false,
	} {
		if got := ValidNodeNumber(s); got != want {
			t.Errorf("ValidNodeNumber(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestCreateNodeHub(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("2000", "Local/pseudo", "2"); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("2000")
	if err != nil {
		t.Fatal(err)
	}
	if view.Interface != "Hub (no radio)" {
		t.Errorf("Interface = %q", view.Interface)
	}
	if view.Radio != nil {
		t.Error("hub node should have no Radio view")
	}
}

func TestCreateNodeSimpleUSBProvisionsBothDriverFiles(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("2000", "SimpleUSB/2000", "1"); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("2000")
	if err != nil {
		t.Fatal(err)
	}
	if view.Interface != "SimpleUSB" || view.Radio == nil {
		t.Fatalf("view = %+v", view)
	}
	if view.Radio.RxMixerSet != "500" {
		t.Errorf("RxMixerSet = %q, want default \"500\"", view.Radio.RxMixerSet)
	}

	// Switching to USBRadio afterward must work without error, proving
	// usbradio.conf's stanza was pre-provisioned too.
	if err := store.UpdateNodeSettings("2000", map[string]string{"rxchannel": "Radio/2000"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRadioSettings("2000", map[string]string{"rxmixerset": "600"}); err != nil {
		t.Fatalf("switching to a pre-provisioned USBRadio stanza should just work: %v", err)
	}
}

func TestCreateNodeAlreadyExistsErrors(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("1999", "Local/pseudo", "2"); err == nil {
		t.Error("expected an error creating a node number that already exists")
	}
}

func TestCreateNodeInvalidNumberErrors(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("not-a-number", "Local/pseudo", "2"); err == nil {
		t.Error("expected an error for an invalid node number")
	}
}

func TestCreateNodeAddsLoopbackEntry(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("2000", "Local/pseudo", "2"); err != nil {
		t.Fatal(err)
	}
	rpt, err := store.loadRpt()
	if err != nil {
		t.Fatal(err)
	}
	sec, ok := rpt.Section("nodes")
	if !ok {
		t.Fatal("no [nodes] section")
	}
	got, ok := sec.Value("2000")
	if !ok || got != "radio@127.0.0.1/2000,NONE" {
		t.Errorf("[nodes] entry = %q, %v", got, ok)
	}
}

func TestDeleteNodeRemovesEverything(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("2000", "SimpleUSB/2000", "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRegistration(Registration{Node: "2000", Password: "x", Server: "register.allstarlink.org"}); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteNode("2000"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.LoadNode("2000"); err == nil {
		t.Error("node should no longer exist after DeleteNode")
	}
	nodes, err := store.ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n == "2000" {
			t.Error("2000 should not be in ListNodes after deletion")
		}
	}
	regs, err := store.ListRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range regs {
		if r.Node == "2000" {
			t.Error("2000's registration should be gone after deletion")
		}
	}
	rpt, err := store.loadRpt()
	if err != nil {
		t.Fatal(err)
	}
	if sec, ok := rpt.Section("nodes"); ok {
		if _, ok := sec.Value("2000"); ok {
			t.Error("2000's [nodes] loopback entry should be gone after deletion")
		}
	}
}

func TestDeleteNodeLeavesOtherNodesIntact(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.CreateNode("2000", "SimpleUSB/2000", "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNode("2000"); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.RxChannel != "SimpleUSB/1999" {
		t.Errorf("node 1999 should be untouched, got RxChannel = %q", view.RxChannel)
	}
}
