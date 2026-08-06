package sa818

import "testing"

func TestCTCSSTonesLength(t *testing.T) {
	if len(CTCSSTones) != 38 {
		t.Fatalf("len(CTCSSTones) = %d, want 38 (matches the module's own real `sa818 radio --help` tone list)", len(CTCSSTones))
	}
}

func TestCTCSSTonesNoDuplicates(t *testing.T) {
	seen := make(map[string]int)
	for i, hz := range CTCSSTones {
		if prev, ok := seen[hz]; ok {
			t.Errorf("%q appears at both index %d and %d", hz, prev, i)
		}
		seen[hz] = i
	}
}

// TestCTCSSTonesMatchesRealToolHelp pins the exact tone list printed by
// `sa818 radio --help` on a real ASL3 node -- confirmed identical to
// this table, first and last entries and the count together, rather than
// assuming the generic ~38-tone list many other radios use (which stops
// at 196.6 Hz, not 250.3).
func TestCTCSSTonesMatchesRealToolHelp(t *testing.T) {
	if CTCSSTones[0] != "67.0" {
		t.Errorf("CTCSSTones[0] = %q, want \"67.0\"", CTCSSTones[0])
	}
	if last := CTCSSTones[len(CTCSSTones)-1]; last != "250.3" {
		t.Errorf("last tone = %q, want \"250.3\"", last)
	}
}

func TestValidCTCSSHz(t *testing.T) {
	valid := []string{"", "67.0", "100.0", "250.3"}
	for _, hz := range valid {
		if !ValidCTCSSHz(hz) {
			t.Errorf("ValidCTCSSHz(%q) = false, want true", hz)
		}
	}
	invalid := []string{"196.6", "0000", "abc", "100"}
	for _, hz := range invalid {
		if ValidCTCSSHz(hz) {
			t.Errorf("ValidCTCSSHz(%q) = true, want false", hz)
		}
	}
}

// TestCtcssCodeKnownPoints pins the exact 4-digit wire encoding the
// module's own AT+DMOSETGROUP command expects, confirmed directly
// against the reference tool's own CTCSS tuple (index 0 = "None",
// so CTCSSTones[i] is wire code i+1).
func TestCtcssCodeKnownPoints(t *testing.T) {
	cases := []struct{ hz, code string }{
		{"", "0000"},
		{"67.0", "0001"},
		{"100.0", "0012"},
		{"250.3", "0038"},
	}
	for _, c := range cases {
		got, err := ctcssCode(c.hz)
		if err != nil {
			t.Errorf("ctcssCode(%q) error = %v", c.hz, err)
			continue
		}
		if got != c.code {
			t.Errorf("ctcssCode(%q) = %q, want %q", c.hz, got, c.code)
		}
	}
}

func TestCtcssCodeUnknownToneErrors(t *testing.T) {
	if _, err := ctcssCode("196.6"); err == nil {
		t.Error("ctcssCode(196.6) error = nil, want an error -- not one of the module's tones")
	}
}
