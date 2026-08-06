// Package asteriskconf reads and writes ASL3's Asterisk-style config files
// (rpt.conf, usbradio.conf, simpleusb.conf, rpt_http_registrations.conf):
// ini-like files with template inheritance -- a base stanza written
// [name](!) and per-node stanzas written [1999](node-main) that inherit its
// settings, overriding only what differs.
//
// Verified against a real ASL3 node (Debian 13, Asterisk 22.9.0+asl3) rather
// than assumed from docs alone -- see internal/asteriskconf/testdata for the
// exact files that shaped this parser. Confirmed against AllStarLink's own
// templating docs (allstarlink.github.io/adv-topics/conftmpl/): "Settings
// changed in the node-specific stanza will override the same settings in
// the [node-main] template" -- and against the node's own
// /etc/asterisk/custom/README.md: a node's own stanza always stays in the
// main file, at the end, after the file's #tryinclude lines; the
// custom/*/*.conf files define additional, separately-named categories a
// node can multiply-inherit alongside node-main (e.g.
// [events-63001](events-main,events-keyed-gpio4)) -- they are not an
// alternate place to relocate a node's own primary stanza.
package asteriskconf

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Pair is one key/value line inside a section, in the order it appeared.
// Op distinguishes "=" from "=>" -- Asterisk uses "=>" by convention for
// keys meant to repeat (e.g. multiple "allow =>" or "register =>" lines),
// but both operators behave identically for parsing/merge purposes; Op is
// kept only so Write can round-trip the file's original style.
type Pair struct {
	Key   string
	Value string
	Op    string
}

// Section is one [name] or [name](parent,...) stanza.
type Section struct {
	Name       string
	IsTemplate bool
	Inherits   []string
	Pairs      []Pair
}

// Value returns the last occurrence of key directly in this section (no
// inheritance). Asterisk itself treats the last-written value as the
// effective one for single-value keys.
func (s *Section) Value(key string) (string, bool) {
	val, ok := "", false
	for _, p := range s.Pairs {
		if p.Key == key {
			val, ok = p.Value, true
		}
	}
	return val, ok
}

// Values returns every occurrence of key directly in this section, in
// file order -- for keys meant to repeat, like "allow".
func (s *Section) Values(key string) []string {
	var out []string
	for _, p := range s.Pairs {
		if p.Key == key {
			out = append(out, p.Value)
		}
	}
	return out
}

// File is a parsed config file (or set of files, once includes are
// resolved by Load): an ordered list of sections. Sections sharing the same
// name -- whether from one #tryinclude'd file re-opening a category, or a
// literal duplicate stanza -- are merged into a single logical Section by
// Load, in the order their pairs were encountered, matching Asterisk's own
// config loader.
type File struct {
	Sections []*Section
}

// Section looks up a section by name.
func (f *File) Section(name string) (*Section, bool) {
	for _, s := range f.Sections {
		if s.Name == name {
			return s, true
		}
	}
	return nil, false
}

// Resolved is the inheritance-flattened view of a section: every ancestor
// template's pairs, in inheritance order, followed by the section's own
// directly-set pairs. Because Value/Values look at the *last* matching
// pair, a section's own pairs -- appended last -- always win over an
// inherited template's, matching the confirmed AllStarLink override
// semantics.
type Resolved struct {
	Section *Section
	Pairs   []Pair
}

func (r *Resolved) Value(key string) (string, bool) {
	val, ok := "", false
	for _, p := range r.Pairs {
		if p.Key == key {
			val, ok = p.Value, true
		}
	}
	return val, ok
}

func (r *Resolved) Values(key string) []string {
	var out []string
	for _, p := range r.Pairs {
		if p.Key == key {
			out = append(out, p.Value)
		}
	}
	return out
}

// Resolve returns the inheritance-flattened view of the named section.
// Multiple parents (e.g. [child](parentA,parentB), used by ASL3's own
// node-customization menu to layer extra categories onto node-main) are
// applied left to right, each overriding the ones before it, with the
// section's own pairs applied last of all.
func (f *File) Resolve(name string) (*Resolved, error) {
	pairs, err := f.resolvePairs(name, nil)
	if err != nil {
		return nil, err
	}
	sec, ok := f.Section(name)
	if !ok {
		return nil, fmt.Errorf("asteriskconf: no such section %q", name)
	}
	return &Resolved{Section: sec, Pairs: pairs}, nil
}

func (f *File) resolvePairs(name string, chain []string) ([]Pair, error) {
	for _, seen := range chain {
		if seen == name {
			return nil, fmt.Errorf("asteriskconf: inheritance cycle: %s -> %s", strings.Join(chain, " -> "), name)
		}
	}
	chain = append(chain, name)

	sec, ok := f.Section(name)
	if !ok {
		return nil, fmt.Errorf("asteriskconf: no such section %q", name)
	}

	var pairs []Pair
	for _, parent := range sec.Inherits {
		parentPairs, err := f.resolvePairs(parent, chain)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, parentPairs...)
	}
	pairs = append(pairs, sec.Pairs...)
	return pairs, nil
}

// IncludeDirective is a "#include" or "#tryinclude" line. Path is the raw
// quoted argument, e.g. "custom/rpt/*.conf" -- may be a glob.
type IncludeDirective struct {
	Path     string
	Optional bool // true for #tryinclude, false for #include
}

// Parse parses raw config content into an ordered list of sections.
// #include/#tryinclude directives are returned separately, unresolved --
// use Load to read a file together with its includes from disk.
func Parse(r io.Reader) (*File, []IncludeDirective, error) {
	f := &File{}
	var includes []IncludeDirective
	var cur *Section

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := stripComment(scanner.Text())
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if dir, ok := parseInclude(line); ok {
			includes = append(includes, dir)
			continue
		}

		if strings.HasPrefix(line, "[") {
			sec, err := parseSectionHeader(line)
			if err != nil {
				return nil, nil, fmt.Errorf("asteriskconf: line %d: %w", lineNo, err)
			}
			f.Sections = append(f.Sections, sec)
			cur = sec
			continue
		}

		key, op, value, ok := parseKeyValue(line)
		if !ok {
			continue
		}
		if cur == nil {
			return nil, nil, fmt.Errorf("asteriskconf: line %d: %q outside any section", lineNo, line)
		}
		cur.Pairs = append(cur.Pairs, Pair{Key: key, Value: value, Op: op})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return f, includes, nil
}

// stripComment truncates line at the first unescaped ';'. "\;" is kept as a
// literal ';' with the backslash removed, matching Asterisk's own config
// parser.
func stripComment(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) && line[i+1] == ';' {
			b.WriteByte(';')
			i++
			continue
		}
		if c == ';' {
			break
		}
		b.WriteByte(c)
	}
	return b.String()
}

func parseInclude(line string) (IncludeDirective, bool) {
	optional := false
	rest := ""
	switch {
	case strings.HasPrefix(line, "#tryinclude"):
		optional = true
		rest = strings.TrimSpace(line[len("#tryinclude"):])
	case strings.HasPrefix(line, "#include"):
		rest = strings.TrimSpace(line[len("#include"):])
	default:
		return IncludeDirective{}, false
	}
	rest = strings.Trim(rest, `"`)
	if rest == "" {
		return IncludeDirective{}, false
	}
	return IncludeDirective{Path: rest, Optional: optional}, true
}

func parseSectionHeader(line string) (*Section, error) {
	if !strings.HasSuffix(line, ")") && !strings.HasSuffix(line, "]") {
		return nil, fmt.Errorf("malformed section header %q", line)
	}
	closeBracket := strings.Index(line, "]")
	if closeBracket < 0 {
		return nil, fmt.Errorf("malformed section header %q", line)
	}
	name := line[1:closeBracket]
	sec := &Section{Name: name}

	remainder := strings.TrimSpace(line[closeBracket+1:])
	if remainder == "" {
		return sec, nil
	}
	if !strings.HasPrefix(remainder, "(") || !strings.HasSuffix(remainder, ")") {
		return nil, fmt.Errorf("malformed inheritance list in %q", line)
	}
	inner := remainder[1 : len(remainder)-1]
	for _, tok := range strings.Split(inner, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if tok == "!" {
			sec.IsTemplate = true
			continue
		}
		sec.Inherits = append(sec.Inherits, tok)
	}
	return sec, nil
}

func parseKeyValue(line string) (key, op, value string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] != '=' {
			continue
		}
		if i+1 < len(line) && line[i+1] == '>' {
			return strings.TrimSpace(line[:i]), "=>", strings.TrimSpace(line[i+2:]), true
		}
		return strings.TrimSpace(line[:i]), "=", strings.TrimSpace(line[i+1:]), true
	}
	return "", "", "", false
}

// Load reads path and resolves its #include/#tryinclude directives
// (relative to path's own directory, matching Asterisk's own include
// resolution), merging sections of the same name across files -- in file
// order -- into one logical Section each, the same way Asterisk's config
// loader treats a category re-opened by an included file as a continuation
// of the same category rather than a separate one.
func Load(path string) (*File, error) {
	merged := &File{}
	byName := map[string]*Section{}
	if err := loadInto(path, merged, byName, map[string]bool{}); err != nil {
		return nil, err
	}
	return merged, nil
}

func loadInto(path string, merged *File, byName map[string]*Section, visited map[string]bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if visited[abs] {
		return fmt.Errorf("asteriskconf: include cycle at %s", path)
	}
	visited[abs] = true

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	parsed, includes, err := Parse(f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	for _, sec := range parsed.Sections {
		if existing, ok := byName[sec.Name]; ok {
			existing.Pairs = append(existing.Pairs, sec.Pairs...)
			for _, parent := range sec.Inherits {
				existing.Inherits = append(existing.Inherits, parent)
			}
			if sec.IsTemplate {
				existing.IsTemplate = true
			}
			continue
		}
		byName[sec.Name] = sec
		merged.Sections = append(merged.Sections, sec)
	}

	dir := filepath.Dir(path)
	for _, inc := range includes {
		pattern := inc.Path
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("%s: bad include pattern %q: %w", path, inc.Path, err)
		}
		if len(matches) == 0 {
			if inc.Optional {
				continue
			}
			return fmt.Errorf("%s: #include %q matched no files", path, inc.Path)
		}
		for _, m := range matches {
			if err := loadInto(m, merged, byName, visited); err != nil {
				return err
			}
		}
	}
	return nil
}
