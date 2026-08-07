package config

import "testing"

func TestIsAllowedRawConfigFile(t *testing.T) {
	if !IsAllowedRawConfigFile("rpt.conf") {
		t.Error("rpt.conf should be allowed")
	}
	if IsAllowedRawConfigFile("../../etc/passwd") {
		t.Error("a path-traversal attempt should not be allowed")
	}
	if IsAllowedRawConfigFile("extensions.conf") {
		t.Error("extensions.conf is not ASL3-relevant and should not be allowed")
	}
}

func TestRawSectionsReadsRealFixture(t *testing.T) {
	sections, err := testStore().RawSections("rpt.conf")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sec := range sections {
		if sec.Name == "1999" {
			found = true
			hasRxChannel := false
			for _, p := range sec.Pairs {
				if p.Key == "rxchannel" {
					hasRxChannel = true
				}
			}
			if !hasRxChannel {
				t.Error("[1999]'s own pairs should include rxchannel")
			}
		}
	}
	if !found {
		t.Fatal("section [1999] not found in raw rpt.conf sections")
	}
}

func TestRawSectionsRejectsDisallowedFile(t *testing.T) {
	if _, err := testStore().RawSections("not-allowed.conf"); err == nil {
		t.Fatal("expected an error for a disallowed file name")
	}
}

func TestRawSectionsMissingFileIsNotAnError(t *testing.T) {
	store := tempStoreFromFixtures(t)
	sections, err := store.RawSections("simpleusb.conf")
	if err != nil {
		t.Fatal(err)
	}
	_ = sections // may or may not be empty depending on fixtures; just must not error
}

func TestSetRawKeyByPosition(t *testing.T) {
	store := tempStoreFromFixtures(t)
	sections, err := store.RawSections("rpt.conf")
	if err != nil {
		t.Fatal(err)
	}
	var idx int
	var found bool
	for _, sec := range sections {
		if sec.Name != "1999" {
			continue
		}
		for i, p := range sec.Pairs {
			if p.Key == "rxchannel" {
				idx = i
				found = true
			}
		}
	}
	if !found {
		t.Fatal("rxchannel not found in [1999]")
	}
	ok, err := store.SetRawKey("rpt.conf", "1999", idx, "Radio/1999")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.RxChannel != "Radio/1999" {
		t.Errorf("RxChannel = %q, want Radio/1999", view.RxChannel)
	}
}

func TestAddRawKeyThenAddRawSection(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.AddRawKey("rpt.conf", "1999", "custom_test_key", "hello"); err != nil {
		t.Fatal(err)
	}
	sections, err := store.RawSections("rpt.conf")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sec := range sections {
		if sec.Name != "1999" {
			continue
		}
		for _, p := range sec.Pairs {
			if p.Key == "custom_test_key" && p.Value == "hello" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("custom_test_key not found after AddRawKey")
	}

	if err := store.AddRawSection("rpt.conf", "brand-new-section"); err != nil {
		t.Fatal(err)
	}
	sections, err = store.RawSections("rpt.conf")
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, sec := range sections {
		if sec.Name == "brand-new-section" {
			found = true
		}
	}
	if !found {
		t.Fatal("brand-new-section not found after AddRawSection")
	}

	// Adding the same section again should fail rather than silently
	// duplicate it.
	if err := store.AddRawSection("rpt.conf", "brand-new-section"); err == nil {
		t.Fatal("expected an error adding a duplicate section")
	}
}
