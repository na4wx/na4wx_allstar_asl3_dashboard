package asteriskconf

import "fmt"

// EnsureModuleLoaded ensures modFile (e.g. "chan_usbradio.so") is loaded
// by the given section (modules.conf's own real structure, confirmed on
// a live node: a single [modules] section, autoload=no, then a flat list
// of "load = X.so" / "noload = X.so" / "require = X.so" lines, one per
// module, each key padded to align every "=" at the same column).
//
// If modFile is currently listed under "noload", that line is rewritten
// to "load" in place. If it isn't listed under any of the three keys at
// all, a new "load = modFile" line is appended. A module already listed
// under "load" or "require" is left untouched -- both already load it,
// and "require" (used for modules ASL3 considers critical to Asterisk's
// own startup, e.g. app_rpt.so) is the stronger of the two, never
// something to silently weaken.
//
// This exists because switching a node's radio interface (SimpleUSB <->
// USBRadio) only ever changes rpt.conf/usbradio.conf/simpleusb.conf --
// confirmed the hard way, on a real node, that modules.conf not being
// kept in sync leaves the newly-selected channel driver unloaded, so
// Asterisk logs "Channel tech ... is not currently loaded, not adding
// node" and the node simply never comes up, with no obvious error
// surfaced anywhere an operator would see it.
func EnsureModuleLoaded(path, sectionName, modFile string) error {
	return editFile(path, sectionName, func(lines []string, start, end int) []string {
		for i := start; i < end; i++ {
			key, _, value, ok := parseKeyValue(stripComment(lines[i]))
			if !ok || value != modFile {
				continue
			}
			if key == "noload" {
				lines[i] = rewriteKeyValueLine(lines[i], "load", modFile)
			}
			return lines
		}
		return spliceLines(lines, end, []string{formatModuleLine("load", modFile)})
	})
}

// EnsureModuleNotLoaded is EnsureModuleLoaded's inverse: a "load =
// modFile" line is rewritten to "noload". A module marked "require" is
// left alone -- that means ASL3 itself considers it critical to
// Asterisk's own startup, not something this should ever silently
// disable. A module not listed at all is left alone too (nothing to
// do -- modules.conf's own default is not to load anything not
// explicitly listed, since autoload=no).
func EnsureModuleNotLoaded(path, sectionName, modFile string) error {
	return editFile(path, sectionName, func(lines []string, start, end int) []string {
		for i := start; i < end; i++ {
			key, _, value, ok := parseKeyValue(stripComment(lines[i]))
			if !ok || value != modFile {
				continue
			}
			if key == "load" {
				lines[i] = rewriteKeyValueLine(lines[i], "noload", modFile)
			}
			return lines
		}
		return lines
	})
}

// rewriteKeyValueLine replaces a line's key and value (e.g. turning
// "noload  = chan_usbradio.so  ; comment" into "load    = chan_usbradio.so  ; comment"),
// preserving its trailing comment (if any) but not its original
// key/value column alignment, which a key-length change (e.g.
// "noload" (6 chars) -> "load" (4 chars)) would throw off regardless.
func rewriteKeyValueLine(line, newKey, value string) string {
	_, comment := splitComment(line)
	rebuilt := formatModuleLine(newKey, value)
	if comment == "" {
		return rebuilt
	}
	return rebuilt + " " + comment
}

// formatModuleLine matches modules.conf's own real column convention,
// confirmed on a live node: every key ("load"/"noload"/"require") is
// left-padded to a shared 8-character width so every line's "=" lines up
// in the same column.
func formatModuleLine(key, value string) string {
	return fmt.Sprintf("%-8s= %s", key, value)
}
