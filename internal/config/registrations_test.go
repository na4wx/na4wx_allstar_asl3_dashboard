package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRegistration(t *testing.T) {
	reg, err := parseRegistration("1234:abcdef@register.allstarlink.org")
	if err != nil {
		t.Fatal(err)
	}
	want := Registration{Node: "1234", Password: "abcdef", Server: "register.allstarlink.org"}
	if reg != want {
		t.Errorf("got %+v, want %+v", reg, want)
	}
}

func TestParseRegistrationPasswordContainingAt(t *testing.T) {
	// Splits from the right for the server, since a hostname can't
	// contain '@' but nothing stops a password from having one.
	reg, err := parseRegistration("1234:weird@pass@register.allstarlink.org")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Password != "weird@pass" || reg.Server != "register.allstarlink.org" {
		t.Errorf("got %+v", reg)
	}
}

func TestParseRegistrationMalformed(t *testing.T) {
	if _, err := parseRegistration("not-a-registration"); err == nil {
		t.Error("expected an error for a malformed registration")
	}
}

func registrationsStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "asteriskconf", "testdata", "rpt_http_registrations.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rpt_http_registrations.conf"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return &Store{Dir: dir}
}

func TestListRegistrationsReadsRealFixture(t *testing.T) {
	store := registrationsStore(t)
	regs, err := store.ListRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 || regs[0].Node != "1234" {
		t.Errorf("ListRegistrations = %+v", regs)
	}
}

func TestListRegistrationsMissingFileIsNotAnError(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	regs, err := store.ListRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if regs != nil {
		t.Errorf("expected nil for a node with no registrations file, got %v", regs)
	}
}

func TestSetRegistrationAddsNewNode(t *testing.T) {
	store := registrationsStore(t)
	if err := store.SetRegistration(Registration{Node: "1999", Password: "secret", Server: "register.allstarlink.org"}); err != nil {
		t.Fatal(err)
	}
	regs, err := store.ListRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 2 {
		t.Fatalf("expected 2 registrations, got %d: %+v", len(regs), regs)
	}
}

func TestSetRegistrationReplacesExisting(t *testing.T) {
	store := registrationsStore(t)
	if err := store.SetRegistration(Registration{Node: "1234", Password: "newpass", Server: "register.allstarlink.org"}); err != nil {
		t.Fatal(err)
	}
	regs, err := store.ListRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 || regs[0].Password != "newpass" {
		t.Errorf("got %+v, want a single updated registration", regs)
	}
}

func TestRemoveRegistrationDeletesNode(t *testing.T) {
	store := registrationsStore(t)
	if err := store.RemoveRegistration("1234"); err != nil {
		t.Fatal(err)
	}
	regs, err := store.ListRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 0 {
		t.Errorf("expected no registrations left, got %+v", regs)
	}
}
