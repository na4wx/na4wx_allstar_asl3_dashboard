package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

var nodeNumberRe = regexp.MustCompile(`^[0-9]{1,6}$`)

// ValidNodeNumber reports whether s looks like a real AllStarLink node
// number -- numeric only, up to 6 digits (rpt.conf's own comments note 6
// digits as the current max node-number length DNS lookups support).
func ValidNodeNumber(s string) bool {
	return nodeNumberRe.MatchString(s)
}

// CreateNode creates a brand-new node's stanza in rpt.conf (plus its
// [nodes] loopback entry, needed for the node to resolve its own number)
// and, if it has a radio interface, in both usbradio.conf and
// simpleusb.conf -- matching a real node's own layout, where both
// drivers' stanzas are provisioned up front so switching between them
// later via UpdateRadioSettings just works without needing to create
// anything at switch-time.
func (s *Store) CreateNode(node, rxchannel, duplex string) error {
	if !ValidNodeNumber(node) {
		return fmt.Errorf("config: %q is not a valid node number", node)
	}
	if _, err := s.LoadNode(node); err == nil {
		return fmt.Errorf("config: node %q already exists", node)
	}
	if rxchannel == "" {
		rxchannel = "Local/pseudo"
	}
	if duplex == "" {
		duplex = "2"
	}

	rptPath := filepath.Join(s.dir(), "rpt.conf")
	if err := asteriskconf.CreateSection(rptPath, node, []string{"node-main"}, []asteriskconf.Pair{
		{Key: "rxchannel", Value: rxchannel, Op: "="},
		{Key: "duplex", Value: duplex, Op: "="},
	}); err != nil {
		return fmt.Errorf("config: create node %q in rpt.conf: %w", node, err)
	}
	if err := asteriskconf.SetValues(rptPath, "nodes", map[string]string{
		node: fmt.Sprintf("radio@127.0.0.1/%s,NONE", node),
	}); err != nil {
		return fmt.Errorf("config: add node %q loopback entry: %w", node, err)
	}

	if strings.HasPrefix(rxchannel, "SimpleUSB/") || strings.HasPrefix(rxchannel, "Radio/") {
		simpleusbTune := []asteriskconf.Pair{
			{Key: "devstr", Value: "", Op: "="},
			{Key: "serial", Value: "", Op: "="},
			{Key: "rxmixerset", Value: "500", Op: "="},
			{Key: "txmixaset", Value: "500", Op: "="},
			{Key: "txmixbset", Value: "500", Op: "="},
		}
		if err := asteriskconf.CreateSection(filepath.Join(s.dir(), "simpleusb.conf"), node, []string{"node-main"}, simpleusbTune); err != nil {
			return fmt.Errorf("config: create node %q in simpleusb.conf: %w", node, err)
		}

		usbradioTune := append(append([]asteriskconf.Pair{}, simpleusbTune...),
			asteriskconf.Pair{Key: "rxvoiceadj", Value: "0.5", Op: "="},
			asteriskconf.Pair{Key: "txctcssadj", Value: "200", Op: "="},
			asteriskconf.Pair{Key: "rxsquelchadj", Value: "500", Op: "="},
		)
		if err := asteriskconf.CreateSection(filepath.Join(s.dir(), "usbradio.conf"), node, []string{"node-main"}, usbradioTune); err != nil {
			return fmt.Errorf("config: create node %q in usbradio.conf: %w", node, err)
		}
	}

	if err := s.syncModulesForRxChannel(rxchannel); err != nil {
		return fmt.Errorf("config: create node %q: %w", node, err)
	}
	return nil
}

// DeleteNode removes a node's stanza from rpt.conf (and its [nodes]
// loopback entry), usbradio.conf, simpleusb.conf, and any HTTP
// registration -- everything CreateNode might have written, whichever of
// it actually exists. No error for pieces that are already absent.
func (s *Store) DeleteNode(node string) error {
	rptPath := filepath.Join(s.dir(), "rpt.conf")
	if err := asteriskconf.RemoveSection(rptPath, node); err != nil {
		return fmt.Errorf("config: remove node %q from rpt.conf: %w", node, err)
	}
	if err := asteriskconf.RemoveValue(rptPath, "nodes", node); err != nil {
		return fmt.Errorf("config: remove node %q loopback entry: %w", node, err)
	}
	if err := asteriskconf.RemoveSection(filepath.Join(s.dir(), "usbradio.conf"), node); err != nil {
		return fmt.Errorf("config: remove node %q from usbradio.conf: %w", node, err)
	}
	if err := asteriskconf.RemoveSection(filepath.Join(s.dir(), "simpleusb.conf"), node); err != nil {
		return fmt.Errorf("config: remove node %q from simpleusb.conf: %w", node, err)
	}
	if err := s.RemoveRegistration(node); err != nil {
		return fmt.Errorf("config: remove node %q registration: %w", node, err)
	}
	return nil
}
