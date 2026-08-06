package sa818

// CTCSSTones is the SA818/DRA818's own fixed 38-tone CTCSS table.
// Confirmed to exactly match the tone list printed by ASL3's own real
// `sa818 radio --help` on a live node -- unlike 818-prog (which encodes
// each tone as a 4-digit index into this same table), ASL3's sa818 tool
// takes the Hz value itself directly as a string (e.g. "94.8"), so
// there's no code/index concept to carry here at all.
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
