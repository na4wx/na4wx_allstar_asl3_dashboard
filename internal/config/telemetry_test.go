package config

import "testing"

func TestParseSingleTone(t *testing.T) {
	spec, ok := ParseSingleTone("|t(660,880,150,2048)")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if spec != (ToneSpec{Freq1: 660, Freq2: 880, DurationMS: 150, Amplitude: 2048}) {
		t.Errorf("spec = %+v", spec)
	}
	if spec.String() != "|t(660,880,150,2048)" {
		t.Errorf("String() = %q", spec.String())
	}
}

func TestParseSingleToneRejectsMultiSegment(t *testing.T) {
	if _, ok := ParseSingleTone("|t(350,0,100,2048)(500,0,100,2048)(660,0,100,2048)"); ok {
		t.Error("expected ok=false for a multi-segment tone")
	}
}

func TestParseSingleToneRejectsSoundReference(t *testing.T) {
	if _, ok := ParseSingleTone("rpt/callproceeding"); ok {
		t.Error("expected ok=false for a sound file reference")
	}
}

func TestIsToneValue(t *testing.T) {
	if !IsToneValue("|t(350,0,100,2048)(500,0,100,2048)(660,0,100,2048)") {
		t.Error("expected multi-segment tone to be recognized as a tone value")
	}
	if IsToneValue("rpt/callproceeding") {
		t.Error("expected sound reference to not be recognized as a tone value")
	}
}

// TestListTelemetryEntriesResolvesFromRealFixture confirms
// ListTelemetryEntries correctly resolves ct1-ct9/etc from
// telemetry-main through the node's own empty "[telemetry](telemetry-main)"
// override section, matching the real node's own confirmed structure.
func TestListTelemetryEntriesResolvesFromRealFixture(t *testing.T) {
	entries, err := testStore().ListTelemetryEntries("telemetry")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]string{}
	var order []string
	for _, e := range entries {
		byKey[e.Key] = e.Value
		order = append(order, e.Key)
	}
	if byKey["ct2"] != "|t(660,880,150,2048)" {
		t.Errorf("ct2 = %q", byKey["ct2"])
	}
	if byKey["patchup"] != "rpt/callproceeding" {
		t.Errorf("patchup = %q", byKey["patchup"])
	}
	if len(order) == 0 || order[0] != "ct1" {
		t.Errorf("expected ct1 first (file order), got order = %v", order)
	}
}

func TestSetTelemetryEntriesOverridesTemplateDefault(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetTelemetryEntries("telemetry", map[string]string{"ct2": "|t(100,200,50,1000)"}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListTelemetryEntries("telemetry")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Key == "ct2" {
			found = true
			if e.Value != "|t(100,200,50,1000)" {
				t.Errorf("ct2 = %q, want overridden value", e.Value)
			}
		}
		if e.Key == "ct1" && e.Value != "|t(350,0,100,2048)(500,0,100,2048)(660,0,100,2048)" {
			t.Errorf("ct1 should be untouched, got %q", e.Value)
		}
	}
	if !found {
		t.Error("ct2 not found after override")
	}
}

func TestSetCourtesyToneAssignmentsSetsAndClears(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetCourtesyToneAssignments("1999", "ct5", "", "ct7"); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.UnlinkedCT != "ct5" {
		t.Errorf("UnlinkedCT = %q, want ct5", view.UnlinkedCT)
	}
	if view.RemoteCT != "" {
		t.Errorf("RemoteCT = %q, want cleared (was ct3 in the fixture)", view.RemoteCT)
	}
	if view.LinkUnkeyCT != "ct7" {
		t.Errorf("LinkUnkeyCT = %q, want ct7", view.LinkUnkeyCT)
	}
}
