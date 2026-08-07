package config

import "testing"

func TestOnChangeFiresOnSuccessfulWrite(t *testing.T) {
	store := tempStoreFromFixtures(t)
	calls := 0
	store.OnChange = func() { calls++ }

	if err := store.UpdateNodeSettings("1999", map[string]string{"duplex": "1"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("OnChange fired %d times, want 1", calls)
	}
}

func TestOnChangeDoesNotFireOnFailedWrite(t *testing.T) {
	store := tempStoreFromFixtures(t)
	calls := 0
	store.OnChange = func() { calls++ }

	// UpdateRadioSettings on a nonexistent node fails before ever
	// reaching a write primitive.
	if err := store.UpdateRadioSettings("no-such-node", map[string]string{"rxmixerset": "500"}); err == nil {
		t.Fatal("expected an error for a nonexistent node")
	}
	if calls != 0 {
		t.Errorf("OnChange fired %d times on a failed write, want 0", calls)
	}
}

func TestOnChangeFiresAcrossDifferentWriteKinds(t *testing.T) {
	store := tempStoreFromFixtures(t)
	calls := 0
	store.OnChange = func() { calls++ }

	if err := store.CreateNode("2001", "Local/pseudo", "2"); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("OnChange never fired for CreateNode (createSection/setValues)")
	}

	calls = 0
	if err := store.DeleteNode("2001"); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("OnChange never fired for DeleteNode (removeSection/removeValue)")
	}
}

func TestOnChangeNilIsSafe(t *testing.T) {
	store := tempStoreFromFixtures(t) // OnChange left nil
	if err := store.UpdateNodeSettings("1999", map[string]string{"duplex": "1"}); err != nil {
		t.Fatal(err)
	}
}
