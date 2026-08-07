// Raw config editor support: generic section/key-value access to a
// handful of whole config files, for edits the domain-specific methods
// elsewhere in this package don't cover. The single most powerful, least
// guarded capability this app offers -- see internal/cloudagent's own
// gating of its equivalent rawconfig.* actions behind
// Settings.AllowRawConfigEdit -- so the allowed-file list stays narrow
// and explicit rather than "any file in the Asterisk config directory".
//
// Deliberately reads/writes exactly one physical file, never following
// #include/#tryinclude the way LoadNode's Resolve-based reads do: an
// operator editing rpt.conf here should see and change exactly what's
// in rpt.conf itself, not a merged view that could silently write a
// value into the wrong physical file.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

// AllowedRawConfigFiles whitelists which files the raw config editor may
// touch -- both the local html UI and internal/cloudagent's relayed
// rawconfig.* actions share this one list, since a file name from
// either surface ultimately becomes a filesystem path and the two
// HTTP-facing layers must never disagree about what's editable.
var AllowedRawConfigFiles = []string{
	"rpt.conf",
	"usbradio.conf",
	"simpleusb.conf",
	"rpt_http_registrations.conf",
	"modules.conf",
}

// IsAllowedRawConfigFile reports whether name is one of
// AllowedRawConfigFiles.
func IsAllowedRawConfigFile(name string) bool {
	for _, f := range AllowedRawConfigFiles {
		if f == name {
			return true
		}
	}
	return false
}

// RawSections parses name (one physical file, no #include/#tryinclude
// resolution) and returns its sections in file order, each with its own
// directly-written key/value pairs -- template sections ([x](!)) and
// per-node stanzas alike, exactly as the file itself is written. A
// missing file is not an error; it just has no sections yet, the same
// convention as this package's other List* methods.
func (s *Store) RawSections(name string) ([]*asteriskconf.Section, error) {
	if !IsAllowedRawConfigFile(name) {
		return nil, fmt.Errorf("config: %q is not one of this app's editable config files", name)
	}
	f, err := os.Open(filepath.Join(s.dir(), name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("config: open %s: %w", name, err)
	}
	defer f.Close()

	parsed, _, err := asteriskconf.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", name, err)
	}
	return parsed.Sections, nil
}

// SetRawKey rewrites the value of the Nth key/value line (0-indexed, in
// file order) within section's own body -- addressing by position
// rather than key name, since a section can legitimately repeat the
// same key (e.g. multiple "exten =>" lines). ok is false if index is
// out of range for that section; never appends a new line.
func (s *Store) SetRawKey(name, section string, index int, value string) (ok bool, err error) {
	if !IsAllowedRawConfigFile(name) {
		return false, fmt.Errorf("config: %q is not one of this app's editable config files", name)
	}
	ok, err = s.setNthValueInSection(filepath.Join(s.dir(), name), section, index, value)
	if err != nil {
		return false, fmt.Errorf("config: set key in %s %q: %w", name, section, err)
	}
	return ok, nil
}

// AddRawKey appends a new "key = value" line to section's body (or
// updates it in place if that exact key already exists -- see
// asteriskconf.SetValues's own doc comment). section must already
// exist; use AddRawSection first for a brand-new one.
func (s *Store) AddRawKey(name, section, key, value string) error {
	if !IsAllowedRawConfigFile(name) {
		return fmt.Errorf("config: %q is not one of this app's editable config files", name)
	}
	if err := s.setValues(filepath.Join(s.dir(), name), section, map[string]string{key: value}); err != nil {
		return fmt.Errorf("config: add key to %s %q: %w", name, section, err)
	}
	return nil
}

// AddRawSection appends a brand-new, empty section to name. Returns an
// error if a section by that name already exists.
func (s *Store) AddRawSection(name, section string) error {
	if !IsAllowedRawConfigFile(name) {
		return fmt.Errorf("config: %q is not one of this app's editable config files", name)
	}
	if err := s.createSection(filepath.Join(s.dir(), name), section, nil, nil); err != nil {
		return fmt.Errorf("config: add section %q to %s: %w", section, name, err)
	}
	return nil
}
