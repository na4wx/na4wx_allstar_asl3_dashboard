package sa818

import "fmt"

// CTCSSTones is the SA818/DRA818's own fixed 38-tone CTCSS table, in the
// order its Tx_CTCSS/Rx_CTCSS index (1-38) selects — CTCSSTones[0] is
// code 1 (67.0 Hz), CTCSSTones[37] is code 38 (250.3 Hz). "0000" (not in
// this table) means no tone at all, per the module's own manual.
//
// This is NOT the generic ~38-tone CTCSS list many other radios use —
// that one stops at 196.6 Hz. This module's own table instead extends
// past 200 Hz (up to 250.3), which only became clear by checking actual
// working firmware for this specific chip rather than a general-purpose
// CTCSS reference; an initial guess based on the more common list was
// wrong from position 26 onward. Confirmed against two independent
// open-source DRA818/SA818 driver projects, cross-checked against the
// module's own programming manual, whose one worked example
// ("Tx_subaudio=0012") lines up with position 12 here (100.0 Hz).
var CTCSSTones = []string{
	"67.0", "71.9", "74.4", "77.0", "79.7", "82.5", "85.4", "88.5", "91.5", "94.8",
	"97.4", "100.0", "103.5", "107.2", "110.9", "114.8", "118.8", "123.0", "127.3", "131.8",
	"136.5", "141.3", "146.2", "151.4", "156.7", "162.2", "167.9", "173.8", "179.9", "186.2",
	"192.8", "203.5", "210.7", "218.1", "225.7", "233.6", "241.8", "250.3",
}

// CTCSSCode returns the 4-digit module code for a tone in CTCSSTones
// (e.g. "100.0" -> "0012"), or "" if hz isn't one of the module's
// standard tones.
func CTCSSCode(hz string) string {
	for i, t := range CTCSSTones {
		if t == hz {
			return fmt.Sprintf("%04d", i+1)
		}
	}
	return ""
}

// CTCSSHz is CTCSSCode's inverse: the Hz label for a 4-digit code
// ("0012" -> "100.0"), or "" for "0000" or anything outside 0001-0038,
// so a previously-applied code can be redisplayed as Hz.
func CTCSSHz(code string) string {
	i := ctcssIndex(code)
	if i < 1 || i > len(CTCSSTones) {
		return ""
	}
	return CTCSSTones[i-1]
}

// ValidCTCSSCode reports whether code is a value the module actually
// accepts for Tx_CTCSS/Rx_CTCSS: "0000" (no tone) or "0001"-"0038" (an
// index into CTCSSTones). Used to reject anything else before it's ever
// written to the module.
func ValidCTCSSCode(code string) bool {
	if code == "0000" {
		return true
	}
	i := ctcssIndex(code)
	return i >= 1 && i <= len(CTCSSTones)
}

// ctcssIndex parses a 4-digit CTCSS code into its integer index, or
// returns 0 if code isn't exactly 4 ASCII digits — deliberately strict
// (not strconv.Atoi, which would also accept "+12" or "0x12") since this
// gates what gets written to a live transmitter.
func ctcssIndex(code string) int {
	if len(code) != 4 {
		return 0
	}
	n := 0
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
