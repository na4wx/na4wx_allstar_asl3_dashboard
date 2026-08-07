// Package config reads ASL3's Asterisk configuration (rpt.conf,
// usbradio.conf, simpleusb.conf) into a resolved, read-only view of each
// locally-configured node, on top of internal/asteriskconf's generic
// template-inheritance parser.
package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

const defaultAsteriskDir = "/etc/asterisk"

// Store reads config files from an ASL3 node's /etc/asterisk directory.
type Store struct {
	// Dir overrides the config directory, for tests. Defaults to
	// /etc/asterisk.
	Dir string
}

func NewStore() *Store { return &Store{} }

func (s *Store) dir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return defaultAsteriskDir
}

// RadioView is the resolved audio-interface (driver-level) settings for a
// node's SimpleUSB or USBRadio device -- distinct from rpt.conf's own
// node-level settings. Populated only when the node's rxchannel points at
// one of these drivers.
type RadioView struct {
	// Driver is "simpleusb" or "usbradio".
	Driver string

	// DriverDuplex is usbradio.conf's own "duplex" (0=half, 1=full) --
	// only meaningful for the usbradio driver; SimpleUSB has no equivalent
	// setting. This is NOT the same value as NodeView.Duplex (rpt.conf's
	// 0-4 repeater/telemetry duplex) despite the shared key name -- see
	// asteriskconf's TestUsbradioConfDuplexIsDistinctFromRptConfDuplex.
	DriverDuplex string

	// CarrierFrom/CtcssFrom control where carrier/CTCSS detection comes
	// from -- confirmed the hard way, on real hardware, that a mismatch
	// here (or the corresponding channel driver module simply not being
	// loaded, see internal/config's own modules.go) is what actually
	// determines whether RX works at all, not something cosmetic. Valid
	// values differ by Driver: simpleusb supports
	// no/usb/usbinvert/pp/ppinvert (see simpleusb.conf's own comments);
	// usbradio additionally supports dsp (CarrierFrom and CtcssFrom) and
	// vox (CarrierFrom only) (see usbradio.conf's own comments).
	CarrierFrom string
	CtcssFrom   string

	RxMixerSet string
	TxMixASet  string
	TxMixBSet  string

	// usbradio-only tune fields; empty for simpleusb.
	RxVoiceAdj   string
	TxCtcssAdj   string
	RxSquelchAdj string
}

// NodeView is the resolved, read-only configuration of one locally-hosted
// AllStar node, merged across rpt.conf, and usbradio.conf/simpleusb.conf
// when the node has a radio interface.
type NodeView struct {
	Node string

	// RxChannel and Duplex are rpt.conf's own values for this node,
	// resolved through its [node-main](!) template per ASL3's confirmed
	// override rule (a node's own stanza always wins).
	RxChannel string
	Duplex    string // 0-4: repeater/telemetry duplex, see rpt.conf's own comments

	// Interface is a human-readable label derived from RxChannel:
	// "SimpleUSB", "USBRadio", "Hub (no radio)", or "" if RxChannel names
	// a driver this package doesn't yet recognize (e.g. Voter, USRP).
	Interface string

	// Radio is non-nil only when Interface is "SimpleUSB" or "USBRadio".
	Radio *RadioView

	// Telemetry is the name of the rpt.conf section this node's courtesy
	// tones/status audio resolve from (usually the shared "telemetry"
	// section every node points at by default via node-main's own
	// "telemetry = telemetry", confirmed on a real node -- a node can
	// override this to a different section for its own independent set).
	Telemetry string

	// UnlinkedCT/RemoteCT/LinkUnkeyCT are node-main-level fields (same
	// override tier as RxChannel/Duplex above, NOT part of the Telemetry
	// section) naming which courtesy-tone key (ct1-ct8) plays in each
	// situation -- confirmed on a real node's own node-main template.
	// Empty means "use app_rpt's own built-in default for that
	// situation."
	UnlinkedCT  string
	RemoteCT    string
	LinkUnkeyCT string

	// IDRecording is rpt.conf's own "idrecording" -- a node-main-level
	// field, same tier as RxChannel/Duplex -- confirmed on a real node's
	// own node-main template as "idrecording = |iNOTSET" (its shipped
	// placeholder). Either a sound file reference (played for station
	// ID) or app_rpt's own "|i<text>" CW/morse-code syntax (sent using
	// the [morse] stanza's speed/frequency/amplitude) -- see
	// internal/config/telemetry.go's IsCWIDValue/ParseCWIDText, which
	// apply the exact same "|i" convention this field's own inline
	// comment documents.
	IDRecording string
}

// ListNodes returns the node numbers of every locally-configured node in
// rpt.conf, sorted. A "local node" here means: a non-template section that
// inherits, directly or transitively, from [node-main] -- rpt.conf has no
// separate list of local nodes to read, so this is the only reliable way
// to discover them (confirmed against a real node: its own [1999] stanza
// exists purely as [1999](node-main), nowhere else).
func (s *Store) ListNodes() ([]string, error) {
	rpt, err := s.loadRpt()
	if err != nil {
		return nil, err
	}
	var nodes []string
	for _, sec := range rpt.Sections {
		if sec.IsTemplate {
			continue
		}
		ok, err := inheritsFrom(rpt, sec.Name, "node-main", nil)
		if err != nil {
			return nil, fmt.Errorf("config: checking %q: %w", sec.Name, err)
		}
		if ok {
			nodes = append(nodes, sec.Name)
		}
	}
	sort.Strings(nodes)
	return nodes, nil
}

func inheritsFrom(f *asteriskconf.File, name, ancestor string, chain []string) (bool, error) {
	for _, seen := range chain {
		if seen == name {
			return false, fmt.Errorf("inheritance cycle: %s -> %s", strings.Join(chain, " -> "), name)
		}
	}
	chain = append(chain, name)

	sec, ok := f.Section(name)
	if !ok {
		return false, nil
	}
	for _, parent := range sec.Inherits {
		if parent == ancestor {
			return true, nil
		}
		ok, err := inheritsFrom(f, parent, ancestor, chain)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// LoadNode returns the resolved view of one node's configuration.
func (s *Store) LoadNode(node string) (*NodeView, error) {
	rpt, err := s.loadRpt()
	if err != nil {
		return nil, err
	}
	r, err := rpt.Resolve(node)
	if err != nil {
		return nil, fmt.Errorf("config: node %q not found in rpt.conf: %w", node, err)
	}

	view := &NodeView{Node: node}
	view.RxChannel, _ = r.Value("rxchannel")
	view.Duplex, _ = r.Value("duplex")
	view.Telemetry, _ = r.Value("telemetry")
	if view.Telemetry == "" {
		view.Telemetry = "telemetry"
	}
	view.UnlinkedCT, _ = r.Value("unlinkedct")
	view.RemoteCT, _ = r.Value("remotect")
	view.LinkUnkeyCT, _ = r.Value("linkunkeyct")
	view.IDRecording, _ = r.Value("idrecording")

	switch {
	case strings.HasPrefix(view.RxChannel, "SimpleUSB/"):
		view.Interface = "SimpleUSB"
		radio, err := s.loadRadio("simpleusb.conf", node, "simpleusb")
		if err != nil {
			return nil, err
		}
		view.Radio = radio
	case strings.HasPrefix(view.RxChannel, "Radio/"):
		view.Interface = "USBRadio"
		radio, err := s.loadRadio("usbradio.conf", node, "usbradio")
		if err != nil {
			return nil, err
		}
		view.Radio = radio
	case strings.HasPrefix(view.RxChannel, "Local/"):
		view.Interface = "Hub (no radio)"
	}

	return view, nil
}

func (s *Store) loadRpt() (*asteriskconf.File, error) {
	f, err := asteriskconf.Load(filepath.Join(s.dir(), "rpt.conf"))
	if err != nil {
		return nil, fmt.Errorf("config: load rpt.conf: %w", err)
	}
	return f, nil
}

func (s *Store) loadRadio(filename, node, driver string) (*RadioView, error) {
	f, err := asteriskconf.Load(filepath.Join(s.dir(), filename))
	if err != nil {
		return nil, fmt.Errorf("config: load %s: %w", filename, err)
	}
	r, err := f.Resolve(node)
	if err != nil {
		return nil, fmt.Errorf("config: node %q not found in %s: %w", node, filename, err)
	}

	radio := &RadioView{Driver: driver}
	radio.CarrierFrom, _ = r.Value("carrierfrom")
	radio.CtcssFrom, _ = r.Value("ctcssfrom")
	radio.RxMixerSet, _ = r.Value("rxmixerset")
	radio.TxMixASet, _ = r.Value("txmixaset")
	radio.TxMixBSet, _ = r.Value("txmixbset")
	if driver == "usbradio" {
		radio.DriverDuplex, _ = r.Value("duplex")
		radio.RxVoiceAdj, _ = r.Value("rxvoiceadj")
		radio.TxCtcssAdj, _ = r.Value("txctcssadj")
		radio.RxSquelchAdj, _ = r.Value("rxsquelchadj")
	}
	return radio, nil
}
