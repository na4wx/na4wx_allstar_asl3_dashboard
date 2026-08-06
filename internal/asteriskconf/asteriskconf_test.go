package asteriskconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures in testdata/ are verbatim dumps from a real ASL3 node (Debian 13
// "trixie", Asterisk 22.9.0+asl3-3.9.3-1.deb13, node 1999) -- not
// synthesized from docs. Assertions below double-check specific values
// against that real node's actual on-screen output at the time it was
// captured.

func parseFixture(t *testing.T, name string) *File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	f, _, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return f
}

func TestRptConfNodeMainIsTemplate(t *testing.T) {
	f := parseFixture(t, "rpt.conf")
	sec, ok := f.Section("node-main")
	if !ok {
		t.Fatal("no [node-main] section")
	}
	if !sec.IsTemplate {
		t.Error("[node-main](!) should be marked as a template")
	}
	if len(sec.Inherits) != 0 {
		t.Errorf("[node-main] should have no parents, got %v", sec.Inherits)
	}
}

func TestRptConfNode1999InheritsNodeMain(t *testing.T) {
	f := parseFixture(t, "rpt.conf")
	sec, ok := f.Section("1999")
	if !ok {
		t.Fatal("no [1999] section")
	}
	if sec.IsTemplate {
		t.Error("[1999](node-main) is a real node, not a template")
	}
	if len(sec.Inherits) != 1 || sec.Inherits[0] != "node-main" {
		t.Errorf("want Inherits [node-main], got %v", sec.Inherits)
	}
}

func TestRptConfDuplexInheritedNotOverridden(t *testing.T) {
	// The real node's [1999] stanza only overrides rxchannel -- duplex=2 is
	// the node-main template's value and must resolve through unchanged.
	f := parseFixture(t, "rpt.conf")
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := r.Value("duplex")
	if !ok || got != "2" {
		t.Errorf("duplex = %q, %v; want \"2\"", got, ok)
	}
}

func TestRptConfRxchannelOverriddenByNode(t *testing.T) {
	// node-main's own rxchannel is "Local/pseudo" -- the real node's [1999]
	// stanza overrides it to SimpleUSB/1999, and that override must win.
	f := parseFixture(t, "rpt.conf")
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := r.Value("rxchannel")
	if !ok || got != "SimpleUSB/1999" {
		t.Errorf("rxchannel = %q, %v; want \"SimpleUSB/1999\"", got, ok)
	}
}

func TestRptConfInheritedScalarSurvives(t *testing.T) {
	f := parseFixture(t, "rpt.conf")
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"hangtime":      "2000",
		"context":       "radio",
		"linktolink":    "no",
		"telemetry":     "telemetry",
		"controlstates": "controlstates",
	} {
		got, ok := r.Value(key)
		if !ok || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, ok, want)
		}
	}
}

func TestRptConfIncludeDirectives(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "rpt.conf"))
	if err != nil {
		t.Fatal(err)
	}
	_, includes, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	want := []IncludeDirective{
		{Path: "custom/rpt.conf", Optional: true},
		{Path: "custom/rpt/*.conf", Optional: true},
	}
	if len(includes) != len(want) {
		t.Fatalf("got %d includes, want %d: %+v", len(includes), len(want), includes)
	}
	for i, w := range want {
		if includes[i] != w {
			t.Errorf("include %d = %+v, want %+v", i, includes[i], w)
		}
	}
}

func TestUsbradioConfDuplexIsDistinctFromRptConfDuplex(t *testing.T) {
	// usbradio.conf's own "duplex" (0=half/1=full, audio-driver level) is a
	// different setting from rpt.conf's "duplex" (0-4, repeater/telemetry
	// behavior) despite sharing a key name. The real node's [1999] stanza
	// in usbradio.conf doesn't override it, so it must resolve to
	// node-main's "0" here -- not rpt.conf's unrelated "2".
	f := parseFixture(t, "usbradio.conf")
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := r.Value("duplex")
	if !ok || got != "0" {
		t.Errorf("usbradio.conf duplex = %q, %v; want \"0\"", got, ok)
	}
}

func TestUsbradioConfTuneValuesAreNodeOwn(t *testing.T) {
	// Confirms ASL3 does NOT have HamVoIP's separate tune-file: these
	// values live directly in the node's own usbradio.conf stanza.
	f := parseFixture(t, "usbradio.conf")
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"rxmixerset": "500",
		"txmixaset":  "500",
		"txmixbset":  "500",
		"rxvoiceadj": "0.5",
	} {
		got, ok := r.Value(key)
		if !ok || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, ok, want)
		}
	}
}

func TestSimpleusbConfTuneValuesAreNodeOwn(t *testing.T) {
	f := parseFixture(t, "simpleusb.conf")
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := r.Value("rxmixerset")
	if !ok || got != "500" {
		t.Errorf("rxmixerset = %q, %v; want \"500\"", got, ok)
	}
	// carrierfrom is inherited from node-main, unmodified by the node.
	got, ok = r.Value("carrierfrom")
	if !ok || got != "usbinvert" {
		t.Errorf("carrierfrom = %q, %v; want \"usbinvert\"", got, ok)
	}
}

func TestRegistrationsConfUsesArrowOperator(t *testing.T) {
	f := parseFixture(t, "rpt_http_registrations.conf")
	sec, ok := f.Section("registrations")
	if !ok {
		t.Fatal("no [registrations] section")
	}
	values := sec.Values("register")
	if len(values) != 1 || values[0] != "1234:abcdef@register.allstarlink.org" {
		t.Errorf("register values = %v", values)
	}
	if sec.Pairs[0].Op != "=>" {
		t.Errorf("register op = %q, want \"=>\"", sec.Pairs[0].Op)
	}
}

func TestRegistrationsConfGeneralInterval(t *testing.T) {
	f := parseFixture(t, "rpt_http_registrations.conf")
	sec, ok := f.Section("general")
	if !ok {
		t.Fatal("no [general] section")
	}
	got, ok := sec.Value("register_interval")
	if !ok || got != "180" {
		t.Errorf("register_interval = %q, %v; want \"180\"", got, ok)
	}
}

func TestMultiParentInheritanceOrderMatchesASL3CustomizationMenu(t *testing.T) {
	// Mirrors the pattern documented in the real node's own
	// /etc/asterisk/custom/README.md for node customizations, e.g.
	// [events-63001](events-main,events-keyed-gpio4) -- later parents in
	// the list override earlier ones, and the section's own pairs win over
	// all of them.
	const conf = `
[a](!)
x = from-a
y = from-a

[b](!)
y = from-b
z = from-b

[c](a,b)
z = from-c
`
	f, _, err := Parse(strings.NewReader(conf))
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Resolve("c")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"x": "from-a", // only a defines it
		"y": "from-b", // b listed after a, so b wins
		"z": "from-c", // c's own pair wins over both parents
	} {
		got, ok := r.Value(key)
		if !ok || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, ok, want)
		}
	}
}

func TestResolveDetectsInheritanceCycle(t *testing.T) {
	const conf = `
[a](b)
[b](a)
`
	f, _, err := Parse(strings.NewReader(conf))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Resolve("a"); err == nil {
		t.Error("expected a cycle error, got nil")
	}
}

func TestStripCommentHandlesEscapedSemicolon(t *testing.T) {
	got := stripComment(`callerid = "Repeater\; Node" <0000000000> ; trailing comment`)
	want := `callerid = "Repeater; Node" <0000000000> `
	if got != want {
		t.Errorf("stripComment = %q, want %q", got, want)
	}
}

func TestLoadResolvesTryincludeGlobAndMergesSameNamedSections(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "rpt.conf")
	if err := os.WriteFile(main, []byte(`
[node-main](!)
x = 1

#tryinclude "custom/rpt/*.conf"

[1999](node-main)
y = 2
`), 0o644); err != nil {
		t.Fatal(err)
	}
	customDir := filepath.Join(dir, "custom", "rpt")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Re-opens [node-main] from a second file, as ASL3's own
	// node-customization menu does -- must merge into the same section,
	// not create a duplicate.
	if err := os.WriteFile(filepath.Join(customDir, "extra.conf"), []byte(`
[node-main](!)
z = 3
`), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Load(main)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"x": "1",
		"y": "2",
		"z": "3",
	} {
		got, ok := r.Value(key)
		if !ok || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, ok, want)
		}
	}
}

func TestLoadMissingTryincludeIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "rpt.conf")
	if err := os.WriteFile(main, []byte(`
[node-main](!)
x = 1
#tryinclude "custom/rpt.conf"
#tryinclude "custom/rpt/*.conf"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(main); err != nil {
		t.Errorf("Load with a missing #tryinclude target should not error: %v", err)
	}
}

func TestLoadMissingIncludeIsAnError(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "rpt.conf")
	if err := os.WriteFile(main, []byte(`
[node-main](!)
x = 1
#include "does-not-exist.conf"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(main); err == nil {
		t.Error("Load with a missing #include target should error")
	}
}
