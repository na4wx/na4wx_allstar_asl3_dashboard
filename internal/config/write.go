package config

import (
	"fmt"
	"path/filepath"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

// UpdateNodeSettings updates the given rpt.conf-level fields (e.g.
// rxchannel, duplex) within a node's own stanza, preserving everything
// else in the file. The node must already exist -- see internal/asteriskconf.SetValues.
func (s *Store) UpdateNodeSettings(node string, updates map[string]string) error {
	if err := asteriskconf.SetValues(filepath.Join(s.dir(), "rpt.conf"), node, updates); err != nil {
		return fmt.Errorf("config: update node %q in rpt.conf: %w", node, err)
	}
	if rxchannel, ok := updates["rxchannel"]; ok {
		if err := s.syncModulesForRxChannel(rxchannel); err != nil {
			return fmt.Errorf("config: update node %q: %w", node, err)
		}
	}
	return nil
}

// UpdateRadioSettings updates the given audio-interface fields (e.g.
// rxmixerset) within a node's own usbradio.conf or simpleusb.conf stanza
// -- whichever driver the node's current rxchannel names. Returns an
// error for a node with no radio interface (e.g. a hub node).
func (s *Store) UpdateRadioSettings(node string, updates map[string]string) error {
	view, err := s.LoadNode(node)
	if err != nil {
		return err
	}
	var filename string
	switch view.Interface {
	case "SimpleUSB":
		filename = "simpleusb.conf"
	case "USBRadio":
		filename = "usbradio.conf"
	default:
		return fmt.Errorf("config: node %q has no radio interface to update (rxchannel=%q)", node, view.RxChannel)
	}
	if err := asteriskconf.SetValues(filepath.Join(s.dir(), filename), node, updates); err != nil {
		return fmt.Errorf("config: update node %q in %s: %w", node, filename, err)
	}
	return nil
}
