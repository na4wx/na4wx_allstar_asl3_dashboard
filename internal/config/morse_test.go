package config

import "testing"

// TestGetMorseIDFrequencyResolvesFromRealFixture confirms
// GetMorseIDFrequency resolves "idfrequency" from morse-main through the
// node's own empty "[morse](morse-main)" override section, matching the
// real node's own confirmed structure (see testdata/rpt.conf).
func TestGetMorseIDFrequencyResolvesFromRealFixture(t *testing.T) {
	freq, err := testStore().GetMorseIDFrequency("morse")
	if err != nil {
		t.Fatal(err)
	}
	if freq != "1065" {
		t.Errorf("freq = %q, want 1065", freq)
	}
}

func TestSetMorseIDFrequencyOverridesTemplateDefault(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetMorseIDFrequency("morse", "800"); err != nil {
		t.Fatal(err)
	}
	freq, err := store.GetMorseIDFrequency("morse")
	if err != nil {
		t.Fatal(err)
	}
	if freq != "800" {
		t.Errorf("freq = %q, want 800", freq)
	}

	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.Morse != "morse" {
		t.Errorf("view.Morse = %q, want morse", view.Morse)
	}
}
