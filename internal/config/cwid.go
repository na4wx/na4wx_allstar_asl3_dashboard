package config

import "strings"

// cwIDPrefix is app_rpt's own syntax for a CW (Morse code) station ID,
// confirmed on a real node's own node-main template: "idrecording =
// |iNOTSET" (its shipped placeholder, meant to be replaced with a real
// callsign). Sent using the [morse] stanza's speed/frequency/amplitude,
// not this field.
const cwIDPrefix = "|i"

// IsCWIDValue reports whether value is app_rpt's "|i<text>" CW-ID
// syntax, as opposed to a plain sound file reference.
func IsCWIDValue(value string) bool {
	return strings.HasPrefix(value, cwIDPrefix)
}

// ParseCWIDText extracts the text after "|i" -- the string sent as
// Morse code. ok is false for a value that isn't CW-ID syntax at all
// (see IsCWIDValue).
func ParseCWIDText(value string) (text string, ok bool) {
	if !IsCWIDValue(value) {
		return "", false
	}
	return strings.TrimPrefix(value, cwIDPrefix), true
}

// FormatCWID builds an idrecording value from CW-ID text.
func FormatCWID(text string) string {
	return cwIDPrefix + text
}
