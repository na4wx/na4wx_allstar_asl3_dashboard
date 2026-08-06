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
