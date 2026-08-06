package sa818

import "testing"

func TestCTCSSTonesLength(t *testing.T) {
	if len(CTCSSTones) != 38 {
		t.Fatalf("len(CTCSSTones) = %d, want 38 (the module's own documented range is 1-38)", len(CTCSSTones))
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

// TestCTCSSCodeKnownPoints pins the three cross-checked data points that
// grounded this table: the manual's own worked example uses code 0012
// for what the standard ordering says is 100.0 Hz, and an independently
// confirmed reference point (118.8 Hz -> code 17) landed exactly where
// this table predicts.
func TestCTCSSCodeKnownPoints(t *testing.T) {
	cases := []struct{ hz, code string }{
		{"67.0", "0001"},
		{"100.0", "0012"}, // the manual's own AT+DMOSETGROUP example value
		{"118.8", "0017"}, // independently confirmed reference point
		{"250.3", "0038"}, // last tone in the module's own (non-generic) table
	}
	for _, c := range cases {
		if got := CTCSSCode(c.hz); got != c.code {
			t.Errorf("CTCSSCode(%q) = %q, want %q", c.hz, got, c.code)
		}
	}
}

func TestCTCSSCodeUnknownTone(t *testing.T) {
	// 196.6 Hz is the top of the *generic* CTCSS list many other radios
	// use, but not one of this module's own 38 tones -- guarding the
	// exact mistake an initial, unverified guess at this table made.
	if got := CTCSSCode("196.6"); got != "" {
		t.Errorf("CTCSSCode(196.6) = %q, want \"\" -- 196.6 Hz is not one of this module's tones", got)
	}
}

func TestCTCSSHzRoundTrip(t *testing.T) {
	for i, hz := range CTCSSTones {
		code := CTCSSCode(hz)
		if got := CTCSSHz(code); got != hz {
			t.Errorf("index %d: CTCSSHz(CTCSSCode(%q)=%q) = %q, want %q", i, hz, code, got, hz)
		}
	}
}

func TestCTCSSHzNoTone(t *testing.T) {
	if got := CTCSSHz("0000"); got != "" {
		t.Errorf("CTCSSHz(0000) = %q, want \"\"", got)
	}
}

func TestValidCTCSSCode(t *testing.T) {
	valid := []string{"0000", "0001", "0012", "0038"}
	for _, c := range valid {
		if !ValidCTCSSCode(c) {
			t.Errorf("ValidCTCSSCode(%q) = false, want true", c)
		}
	}
	invalid := []string{"", "0039", "0", "12", "abcd", "-001", "00012"}
	for _, c := range invalid {
		if ValidCTCSSCode(c) {
			t.Errorf("ValidCTCSSCode(%q) = true, want false", c)
		}
	}
}
