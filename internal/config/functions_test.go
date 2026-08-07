package config

import "testing"

func TestListFunctionMacrosResolvesFromRealFixture(t *testing.T) {
	entries, err := testStore().ListFunctionMacros("functions")
	if err != nil {
		t.Fatal(err)
	}
	byDigit := map[string]string{}
	for _, e := range entries {
		byDigit[e.Digits] = e.Command
	}
	if byDigit["1"] != "ilink,1" {
		t.Errorf(`digit "1" = %q, want "ilink,1"`, byDigit["1"])
	}
	if byDigit["70"] != "ilink,5" {
		t.Errorf(`digit "70" = %q, want "ilink,5"`, byDigit["70"])
	}
}

func TestSetFunctionMacroOverridesTemplateDefault(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetFunctionMacro("functions", "1", "ilink,3"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFunctionMacros("functions")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Digits == "1" && e.Command != "ilink,3" {
			t.Errorf("digit 1 = %q, want overridden ilink,3", e.Command)
		}
	}
}

func TestSetFunctionMacroCreatesSectionIfMissing(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetFunctionMacro("functions-1999", "1", "ilink,3"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFunctionMacros("functions-1999")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Digits != "1" || entries[0].Command != "ilink,3" {
		t.Errorf("entries = %+v", entries)
	}
}

// TestDeleteFunctionMacroRevertsToTemplateDefault confirms deleting an
// override on the shared "functions" section (itself inherited from
// functions-main) falls back to the template's own value rather than
// making the digit disappear entirely -- it's still defined there, just
// no longer overridden on this section.
func TestDeleteFunctionMacroRevertsToTemplateDefault(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetFunctionMacro("functions", "1", "ilink,3"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFunctionMacro("functions", "1"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFunctionMacros("functions")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Digits == "1" && e.Command != "ilink,1" {
			t.Errorf("digit 1 = %q, want reverted to template default ilink,1", e.Command)
		}
	}
}

// TestDeleteFunctionMacroOnPerNodeSectionRemovesEntirely confirms
// deleting a digit that was only ever set on a plain, non-inheriting
// section (no template to fall back to) actually removes it.
func TestDeleteFunctionMacroOnPerNodeSectionRemovesEntirely(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetFunctionMacro("functions-1999", "1", "ilink,3"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFunctionMacro("functions-1999", "1"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFunctionMacros("functions-1999")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Digits == "1" {
			t.Errorf("digit 1 still present after delete: %q", e.Command)
		}
	}
}
