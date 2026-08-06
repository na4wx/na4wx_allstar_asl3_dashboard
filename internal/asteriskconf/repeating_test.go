package asteriskconf

import (
	"os"
	"strings"
	"testing"
)

func TestSetRepeatingValueAddsNewEntry(t *testing.T) {
	path := writeTempConf(t, `[general]
register_interval = 180 ; every 3 minutes

[registrations]
register => 1234:abcdef@register.allstarlink.org
`)
	if err := SetRepeatingValue(path, "registrations", "register", "1999:", "1999:secretpass@register.allstarlink.org"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "register => 1234:abcdef@register.allstarlink.org") {
		t.Errorf("existing entry should survive untouched:\n%s", s)
	}
	if !strings.Contains(s, "register => 1999:secretpass@register.allstarlink.org") {
		t.Errorf("new entry not added:\n%s", s)
	}
	if !strings.Contains(s, "register_interval = 180 ; every 3 minutes") {
		t.Errorf("[general] section should be untouched:\n%s", s)
	}
}

func TestSetRepeatingValueUpdatesExistingEntryByPrefix(t *testing.T) {
	path := writeTempConf(t, `[registrations]
register => 1999:oldpass@register.allstarlink.org
register => 2000:otherpass@register.allstarlink.org
`)
	if err := SetRepeatingValue(path, "registrations", "register", "1999:", "1999:newpass@register.allstarlink.org"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[registrations]\nregister => 1999:newpass@register.allstarlink.org\nregister => 2000:otherpass@register.allstarlink.org\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRemoveRepeatingValueDeletesMatchingLine(t *testing.T) {
	path := writeTempConf(t, `[registrations]
register => 1999:pass1@register.allstarlink.org
register => 2000:pass2@register.allstarlink.org
`)
	if err := RemoveRepeatingValue(path, "registrations", "register", "1999:"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[registrations]\nregister => 2000:pass2@register.allstarlink.org\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRemoveRepeatingValueNoMatchIsNoop(t *testing.T) {
	const orig = "[registrations]\nregister => 1999:pass1@register.allstarlink.org\n"
	path := writeTempConf(t, orig)
	if err := RemoveRepeatingValue(path, "registrations", "register", "9999:"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("file should be unchanged:\ngot:\n%q\nwant:\n%q", got, orig)
	}
}

func TestSetRepeatingValueOnRealRegistrationsFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/rpt_http_registrations.conf")
	if err != nil {
		t.Fatal(err)
	}
	path := writeTempConf(t, string(data))
	if err := SetRepeatingValue(path, "registrations", "register", "1999:", "1999:mypass@register.allstarlink.org"); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sec, ok := f.Section("registrations")
	if !ok {
		t.Fatal("no [registrations] section")
	}
	values := sec.Values("register")
	if len(values) != 2 {
		t.Fatalf("expected 2 registrations, got %d: %v", len(values), values)
	}
}
