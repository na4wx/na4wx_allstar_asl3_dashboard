package config

import "testing"

func TestSetAndListScheduleEntries(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetScheduleEntry("schedule", "1", "0 8 * * *"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListScheduleEntries("schedule")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.MacroNum == "1" && e.TimeSpec == "0 8 * * *" {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestSetScheduleEntryCreatesSectionIfMissing(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetScheduleEntry("schedule1999", "1", "0 8 * * *"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListScheduleEntries("schedule1999")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].MacroNum != "1" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestDeleteScheduleEntry(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetScheduleEntry("schedule", "1", "0 8 * * *"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteScheduleEntry("schedule", "1"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListScheduleEntries("schedule")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.MacroNum == "1" {
			t.Error("entry 1 still present after delete")
		}
	}
}

func TestSetNodeScheduler(t *testing.T) {
	store := tempStoreFromFixtures(t)
	if err := store.SetNodeScheduler("1999", "schedule1999"); err != nil {
		t.Fatal(err)
	}
	view, err := store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.Scheduler != "schedule1999" {
		t.Errorf("Scheduler = %q, want schedule1999", view.Scheduler)
	}
}
