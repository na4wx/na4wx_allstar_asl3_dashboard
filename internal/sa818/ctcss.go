package sa818

import "fmt"

// CTCSSTones is the SA818/DRA818's own fixed 38-tone CTCSS table --
// confirmed directly against ASL3's own /usr/bin/sa818 tool's own CTCSS
// tuple (its source was read on a real node), index 1-38 (its own index
// 0 is the literal string "None").
var CTCSSTones = []string{
	"67.0", "71.9", "74.4", "77.0", "79.7", "82.5", "85.4", "88.5", "91.5", "94.8",
	"97.4", "100.0", "103.5", "107.2", "110.9", "114.8", "118.8", "123.0", "127.3", "131.8",
	"136.5", "141.3", "146.2", "151.4", "156.7", "162.2", "167.9", "173.8", "179.9", "186.2",
	"192.8", "203.5", "210.7", "218.1", "225.7", "233.6", "241.8", "250.3",
}

// ValidCTCSSHz reports whether hz is one of the module's standard tones,
// or "" (meaning no tone at all).
func ValidCTCSSHz(hz string) bool {
	if hz == "" {
		return true
	}
	for _, t := range CTCSSTones {
		if t == hz {
			return true
		}
	}
	return false
}

// ctcssCode returns the module's own 4-digit AT+DMOSETGROUP tone index
// for hz (e.g. "100.0" -> "0012"), or "0000" for "" (no tone) -- this is
// the module's real wire format for this field, confirmed directly
// against the reference tool's own source (its CTCSS tuple, indexed via
// CTCSS.index(...) then zero-padded to 4 digits).
func ctcssCode(hz string) (string, error) {
	if hz == "" {
		return "0000", nil
	}
	for i, t := range CTCSSTones {
		if t == hz {
			return fmt.Sprintf("%04d", i+1), nil
		}
	}
	return "", fmt.Errorf("%q is not one of the module's standard CTCSS tones", hz)
}
