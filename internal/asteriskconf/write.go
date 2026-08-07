package asteriskconf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// SetValues updates an existing section's own key/value pairs in place on
// disk, preserving everything else in the file byte for byte -- comments,
// formatting, other sections, #include directives. It operates on a single
// file directly (not through Load's include resolution): callers already
// know which physical file a node's stanza lives in -- confirmed against a
// real node that it's always the main file itself (rpt.conf/
// usbradio.conf/simpleusb.conf), never a #tryinclude'd one; see this
// package's own doc comment and the node's own custom/README.md.
//
// For each key in updates: the LAST existing occurrence of that key within
// the section is rewritten (matching Resolved.Value's own "last wins"
// semantics -- editing any earlier occurrence would leave the file's
// effective value unchanged and silently discard the edit). A key with no
// existing occurrence is appended as a new "key = value" line just before
// the section's closing boundary (the next section header, or EOF).
//
// SetValues never creates the section itself -- it returns an error if
// sectionName doesn't already exist in the file. It never touches any
// other section, so it can't accidentally edit a template a node inherits
// from.
func SetValues(path, sectionName string, updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}

	return editFile(path, sectionName, func(lines []string, start, end int) []string {
		remaining := make(map[string]string, len(updates))
		for k, v := range updates {
			remaining[k] = v
		}

		// Last occurrence wins: scan forward, keep overwriting so the
		// final match found is the one actually rewritten.
		lastMatch := map[string]int{}
		for i := start; i < end; i++ {
			key, _, _, ok := parseKeyValue(stripComment(lines[i]))
			if !ok {
				continue
			}
			if _, wanted := updates[key]; wanted {
				lastMatch[key] = i
			}
		}
		for key, idx := range lastMatch {
			lines[idx] = rewriteValueLine(lines[idx], updates[key])
			delete(remaining, key)
		}

		if len(remaining) == 0 {
			return lines
		}
		var toAppend []string
		for key, val := range remaining {
			toAppend = append(toAppend, fmt.Sprintf("%s = %s", key, val))
		}
		return spliceLines(lines, end, toAppend)
	})
}

// SetNthValueInSection rewrites the value of the Nth key/value line
// (0-indexed, in file order) within sectionName's own body -- for the
// raw config editor, which addresses a line by its position rather than
// its key name (matching the order Section.Pairs/Resolved.Pairs already
// enumerate a section in), since a section can legitimately repeat the
// same key name (e.g. multiple "allow =>" lines) with no other way to
// address one specific occurrence. Returns ok=false, nil error if n is
// out of range for this section; never appends a new line, never
// touches any other section.
func SetNthValueInSection(path, sectionName string, n int, newValue string) (ok bool, err error) {
	err = editFile(path, sectionName, func(lines []string, start, end int) []string {
		count := 0
		for i := start; i < end; i++ {
			if _, _, _, kvOK := parseKeyValue(stripComment(lines[i])); !kvOK {
				continue
			}
			if count == n {
				lines[i] = rewriteValueLine(lines[i], newValue)
				ok = true
				break
			}
			count++
		}
		return lines
	})
	return ok, err
}

// SectionExists reports whether path's own top-level section list (no
// #include/#tryinclude resolution, matching every other write.go
// primitive's "operate on the physical file directly" discipline)
// contains a section named name. Used by callers that need to create a
// section on first use (e.g. a node's own per-node scheduler section)
// before writing into it, since SetValues itself deliberately refuses
// to create a section that doesn't exist yet.
func SectionExists(path, name string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("asteriskconf: %w", err)
	}
	defer f.Close()
	parsed, _, err := Parse(f)
	if err != nil {
		return false, fmt.Errorf("asteriskconf: %w", err)
	}
	_, ok := parsed.Section(name)
	return ok, nil
}

// SetRepeatingValue finds, within the named section, the "key => value" (or
// "key = value") line whose value starts with valuePrefix, and replaces
// its value with newValue -- preserving the line's original operator and
// any trailing comment. If no line matches, appends "key => newValue" as a
// new line at the end of the section. For keys that legitimately repeat,
// distinguished by a prefix of their value rather than by key name --
// e.g. rpt_http_registrations.conf's "register => NODE:PASS@HOST" lines,
// one per node, selected by "NODE:".
func SetRepeatingValue(path, sectionName, key, valuePrefix, newValue string) error {
	return editFile(path, sectionName, func(lines []string, start, end int) []string {
		if idx := findRepeatingValueLine(lines, start, end, key, valuePrefix); idx >= 0 {
			lines[idx] = rewriteValueLine(lines[idx], newValue)
			return lines
		}
		return spliceLines(lines, end, []string{fmt.Sprintf("%s => %s", key, newValue)})
	})
}

// RemoveRepeatingValue deletes, within the named section, the line whose
// value (after the operator) starts with valuePrefix. No error, and no
// change to the file, if no line matches.
func RemoveRepeatingValue(path, sectionName, key, valuePrefix string) error {
	return editFile(path, sectionName, func(lines []string, start, end int) []string {
		idx := findRepeatingValueLine(lines, start, end, key, valuePrefix)
		if idx < 0 {
			return lines
		}
		out := append([]string{}, lines[:idx]...)
		out = append(out, lines[idx+1:]...)
		return out
	})
}

// CreateSection appends a brand-new section to the end of the file, with
// the given inheritance list and key/value pairs. ASL3's own convention
// (confirmed via a real node's own /etc/asterisk/custom/README.md: "All
// of the [####] node stanzas must be at the end of the file") is that
// per-node stanzas live at the very end, after any #tryinclude lines --
// so this always appends at EOF rather than guessing at a "right" spot.
// Returns an error if the section already exists.
func CreateSection(path, name string, inherits []string, pairs []Pair) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("asteriskconf: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("asteriskconf: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	hadTrailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if hadTrailingNewline {
		lines = lines[:len(lines)-1]
	}

	if _, _, err := sectionBodyRange(lines, name); err == nil {
		return fmt.Errorf("asteriskconf: %s: section %q already exists", path, name)
	}

	header := "[" + name + "]"
	if len(inherits) > 0 {
		header += "(" + strings.Join(inherits, ",") + ")"
	}
	block := []string{"", header}
	for _, p := range pairs {
		op := p.Op
		if op == "" {
			op = "="
		}
		block = append(block, fmt.Sprintf("%s %s %s", p.Key, op, p.Value))
	}
	lines = append(lines, block...)

	out := strings.Join(lines, "\n") + "\n"
	return writeFilePreservingOwnership(path, []byte(out), info)
}

// RemoveSection deletes a section -- its header line through its body --
// entirely. A no-op, not an error, if the section doesn't exist.
func RemoveSection(path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("asteriskconf: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("asteriskconf: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	hadTrailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if hadTrailingNewline {
		lines = lines[:len(lines)-1]
	}

	headerIdx, bodyEnd, found := sectionFullRange(lines, name)
	if !found {
		return nil
	}
	lines = append(lines[:headerIdx:headerIdx], lines[bodyEnd:]...)

	out := strings.Join(lines, "\n")
	if hadTrailingNewline || len(lines) > 0 {
		out += "\n"
	}
	return writeFilePreservingOwnership(path, []byte(out), info)
}

// RemoveValue deletes every occurrence of key within the named section. A
// no-op, not an error, if the section or key doesn't exist.
func RemoveValue(path, sectionName, key string) error {
	return editFile(path, sectionName, func(lines []string, start, end int) []string {
		var out []string
		out = append(out, lines[:start]...)
		for i := start; i < end; i++ {
			k, _, _, ok := parseKeyValue(stripComment(lines[i]))
			if ok && k == key {
				continue
			}
			out = append(out, lines[i])
		}
		out = append(out, lines[end:]...)
		return out
	})
}

// sectionFullRange returns the named section's header line index and the
// end (exclusive) of its body -- the same boundary sectionBodyRange
// finds, but including the header line itself, for callers that need to
// remove a section entirely.
func sectionFullRange(lines []string, name string) (headerIdx, bodyEnd int, found bool) {
	for i, line := range lines {
		stripped := strings.TrimSpace(stripComment(line))
		if !strings.HasPrefix(stripped, "[") {
			continue
		}
		sec, err := parseSectionHeader(stripped)
		if err != nil {
			continue
		}
		if found {
			return headerIdx, i, true
		}
		if sec.Name == name {
			found = true
			headerIdx = i
		}
	}
	if !found {
		return 0, 0, false
	}
	return headerIdx, len(lines), true
}

func findRepeatingValueLine(lines []string, start, end int, key, valuePrefix string) int {
	for i := start; i < end; i++ {
		k, _, v, ok := parseKeyValue(stripComment(lines[i]))
		if !ok || k != key {
			continue
		}
		if strings.HasPrefix(v, valuePrefix) {
			return i
		}
	}
	return -1
}

func spliceLines(lines []string, at int, insert []string) []string {
	tail := append([]string{}, lines[at:]...)
	return append(lines[:at:at], append(insert, tail...)...)
}

// editFile reads path, locates sectionName's body line range, hands the
// full line slice plus that range to edit, and writes the (possibly
// modified) result back atomically, preserving the original file's
// ownership and permissions.
func editFile(path, sectionName string, edit func(lines []string, start, end int) []string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("asteriskconf: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("asteriskconf: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	// strings.Split on a file ending in "\n" leaves a trailing "" element;
	// track that separately so it's re-added rather than treated as a
	// blank line inside whatever the last section happens to be.
	hadTrailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if hadTrailingNewline {
		lines = lines[:len(lines)-1]
	}

	start, end, err := sectionBodyRange(lines, sectionName)
	if err != nil {
		return fmt.Errorf("asteriskconf: %s: %w", path, err)
	}

	lines = edit(lines, start, end)

	out := strings.Join(lines, "\n")
	if hadTrailingNewline || len(lines) > 0 {
		out += "\n"
	}
	return writeFilePreservingOwnership(path, []byte(out), info)
}

// sectionBodyRange returns the [start, end) line-index range of the named
// section's body -- the lines strictly between its own header and the
// next section header (or EOF).
func sectionBodyRange(lines []string, name string) (start, end int, err error) {
	found := false
	for i, line := range lines {
		stripped := strings.TrimSpace(stripComment(line))
		if !strings.HasPrefix(stripped, "[") {
			continue
		}
		sec, err := parseSectionHeader(stripped)
		if err != nil {
			continue // not a real section header, e.g. "[foo" mid comment
		}
		if found {
			return start, i, nil
		}
		if sec.Name == name {
			found = true
			start = i + 1
		}
	}
	if !found {
		return 0, 0, fmt.Errorf("no such section %q", name)
	}
	return start, len(lines), nil
}

// rewriteValueLine replaces a "key = value" or "key => value" line's value
// with newValue, preserving everything else about the line's original
// formatting: the key spelling, whitespace around the operator, the
// operator itself, whitespace between the value and any trailing inline
// comment (so comment-alignment columns like those throughout rpt.conf
// survive an edit), and the comment itself.
func rewriteValueLine(line, newValue string) string {
	code, comment := splitComment(line)
	opEnd := -1
	opLen := 0
	for i := 0; i < len(code); i++ {
		if code[i] != '=' {
			continue
		}
		if i+1 < len(code) && code[i+1] == '>' {
			opEnd, opLen = i, 2
		} else {
			opEnd, opLen = i, 1
		}
		break
	}
	if opEnd < 0 {
		// No operator found (shouldn't happen -- caller only rewrites
		// lines parseKeyValue already matched) -- leave the line alone
		// rather than corrupt it.
		return line
	}
	prefix := code[:opEnd+opLen]
	rest := code[opEnd+opLen:]
	trimmed := strings.TrimSpace(rest)
	// strings.Index(rest, "") == 0 for an empty original value (e.g.
	// "devstr="), which correctly puts all of rest's whitespace (if any)
	// after newValue rather than splitting it.
	valueStart := strings.Index(rest, trimmed)
	leadingWS := rest[:valueStart]
	trailingWS := rest[valueStart+len(trimmed):]
	return prefix + leadingWS + newValue + trailingWS + comment
}

// splitComment is stripComment, but returns the comment portion (starting
// at its leading ';') instead of discarding it.
func splitComment(line string) (code, comment string) {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) && line[i+1] == ';' {
			i++
			continue
		}
		if c == ';' {
			return line[:i], line[i:]
		}
	}
	return line, ""
}

// writeFilePreservingOwnership atomically replaces path's contents,
// keeping the original file's owner, group, and permission bits. ASL3
// runs Asterisk itself as a non-root "asterisk" user (confirmed via the
// real node's own systemd unit: "asterisk -U asterisk"); this dashboard
// runs as root, so a naive create-temp-then-rename would silently hand
// root ownership to a config file Asterisk's own process may need to
// re-read -- preserving the original owner avoids that.
func writeFilePreservingOwnership(path string, data []byte, original os.FileInfo) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, original.Mode()); err != nil {
		return err
	}
	if stat, ok := original.Sys().(*syscall.Stat_t); ok {
		if err := os.Chown(tmpPath, int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}
	}
	return os.Rename(tmpPath, path)
}
