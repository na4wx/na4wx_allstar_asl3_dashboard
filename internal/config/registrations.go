package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

// Registration is one node's outbound HTTP registration -- ASL3's
// replacement for the old IAX2 "register =>" lines in iax.conf, confirmed
// against a real node: iax.conf's own [general] section carries a comment
// warning "IAX registration will be discontinued at some point -- Setup
// rpt_http_registrations.conf instead."
type Registration struct {
	Node     string
	Password string
	Server   string
}

func (s *Store) registrationsPath() string {
	return filepath.Join(s.dir(), "rpt_http_registrations.conf")
}

// ListRegistrations returns every configured registration. Returns (nil,
// nil) if rpt_http_registrations.conf doesn't exist or has no
// [registrations] section -- a fresh node with none configured yet is not
// an error.
func (s *Store) ListRegistrations() ([]Registration, error) {
	f, err := asteriskconf.Load(s.registrationsPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("config: load rpt_http_registrations.conf: %w", err)
	}
	sec, ok := f.Section("registrations")
	if !ok {
		return nil, nil
	}
	var regs []Registration
	for _, raw := range sec.Values("register") {
		reg, err := parseRegistration(raw)
		if err != nil {
			continue // skip a malformed line rather than fail the whole read
		}
		regs = append(regs, reg)
	}
	return regs, nil
}

// SetRegistration adds a node's registration, or replaces it wholesale if
// one already exists for that node number.
//
// Note: unlike UpdateNodeSettings/UpdateRadioSettings, this requires
// rpt_http_registrations.conf to already exist with a [registrations]
// section -- true of every real ASL3 install confirmed so far, but not
// yet handled for a hypothetical node missing the file entirely.
func (s *Store) SetRegistration(reg Registration) error {
	if reg.Node == "" {
		return fmt.Errorf("config: registration node number is required")
	}
	err := asteriskconf.SetRepeatingValue(s.registrationsPath(), "registrations", "register", reg.Node+":", formatRegistration(reg))
	if err != nil {
		return fmt.Errorf("config: set registration for node %q: %w", reg.Node, err)
	}
	return nil
}

// RemoveRegistration deletes a node's registration, if one exists. No
// error if it doesn't.
func (s *Store) RemoveRegistration(node string) error {
	err := asteriskconf.RemoveRepeatingValue(s.registrationsPath(), "registrations", "register", node+":")
	if err != nil {
		return fmt.Errorf("config: remove registration for node %q: %w", node, err)
	}
	return nil
}

func formatRegistration(r Registration) string {
	return fmt.Sprintf("%s:%s@%s", r.Node, r.Password, r.Server)
}

// parseRegistration parses "NODE:PASSWORD@SERVER". The password is
// whatever falls between the first ':' and the *last* '@' -- a hostname
// can't contain '@', but nothing stops a password from containing one, so
// splitting from the right for the server is the safer assumption.
func parseRegistration(raw string) (Registration, error) {
	colon := strings.Index(raw, ":")
	at := strings.LastIndex(raw, "@")
	if colon < 0 || at < 0 || at < colon {
		return Registration{}, fmt.Errorf("malformed registration %q", raw)
	}
	return Registration{
		Node:     raw[:colon],
		Password: raw[colon+1 : at],
		Server:   raw[at+1:],
	}, nil
}
