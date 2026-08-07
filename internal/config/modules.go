package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

// syncModulesForRxChannel ensures the Asterisk channel driver module a
// given rxchannel value needs (SimpleUSB/* -> chan_simpleusb.so,
// Radio/* -> chan_usbradio.so) is actually loaded in modules.conf --
// confirmed the hard way, on a real node, that setting rxchannel through
// rpt.conf alone isn't enough: modules.conf ships with only one of the
// two drivers loaded, and switching a node's interface without also
// updating it left the newly-selected driver unloaded. Asterisk then
// logs "Channel tech ... is not currently loaded, not adding node" and
// the node simply never comes up -- nothing else surfaces that to an
// operator, so hours of RX/TX debugging were spent against a node that
// was never actually running the driver being tested.
//
// Loading the needed driver is treated as a hard requirement -- if it
// fails, the caller's own node save fails too, since a node saved
// without it would silently never load. Disabling the now-unused OTHER
// driver (if no other configured node still needs it) is best-effort
// only: that part is an optimization, not something that should block
// the save the operator actually asked for.
func (s *Store) syncModulesForRxChannel(rxchannel string) error {
	var neededModule string
	switch {
	case strings.HasPrefix(rxchannel, "SimpleUSB/"):
		neededModule = "chan_simpleusb.so"
	case strings.HasPrefix(rxchannel, "Radio/"):
		neededModule = "chan_usbradio.so"
	default:
		return nil // hub node (Local/pseudo) -- no driver module needed
	}

	modulesPath := filepath.Join(s.dir(), "modules.conf")
	if err := asteriskconf.EnsureModuleLoaded(modulesPath, "modules", neededModule); err != nil {
		return fmt.Errorf("enable %s in modules.conf: %w", neededModule, err)
	}

	otherModule, otherInterface := "chan_usbradio.so", "USBRadio"
	if neededModule == "chan_usbradio.so" {
		otherModule, otherInterface = "chan_simpleusb.so", "SimpleUSB"
	}
	nums, err := s.ListNodes()
	if err != nil {
		return nil
	}
	for _, num := range nums {
		if view, err := s.LoadNode(num); err == nil && view.Interface == otherInterface {
			return nil // some other node still needs the other driver -- leave it alone
		}
	}
	_ = asteriskconf.EnsureModuleNotLoaded(modulesPath, "modules", otherModule)
	return nil
}
