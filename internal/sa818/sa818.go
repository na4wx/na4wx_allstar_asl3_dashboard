// Package sa818 programs an SA818/DRA818 VHF/UHF radio module (as used
// on a SHARI USB node) directly over its serial AT-command connection,
// without shelling out to any external tool.
//
// ASL3 ships its own sa818/sa818-menu tools (confirmed on a real node,
// /usr/bin/sa818 and /usr/bin/sa818-menu -- NOT HamVoIP's "818-prog", a
// different, unrelated Python script baked into that distro's own disk
// image, which ASL3 doesn't have at all). This package was originally
// built to shell out to ASL3's own sa818 CLI tool the same way. That
// approach was abandoned after confirming on real hardware that
// automated, scripted invocations of that tool reliably fail to
// actually reprogram the module -- despite the tool itself reporting a
// genuine, device-acknowledged "OK" reply, and despite sa818-menu (a
// bash wrapper that, read in full, builds and sends the exact same
// AT+DMOSETGROUP command for an equivalent setting) reportedly working.
// The real cause of that discrepancy was never identified. Rather than
// build on unexplained behavior, this package speaks the module's own
// AT-command protocol directly -- see serial.go.
package sa818

import (
	"context"
	"fmt"
	"strings"
)

// Settings is what an operator can configure through this app. JSON
// tags exist for internal/cloudagent's relayed sa818.program action;
// they have no effect on this struct's existing Go-field-name access
// elsewhere.
type Settings struct {
	Wide bool `json:"wide"` // false = narrow (12.5kHz), true = wide (25kHz)

	TxFreqMHz string `json:"txFreqMHz"` // pre-formatted "xxx.xxxx"
	RxFreqMHz string `json:"rxFreqMHz"`

	TxCTCSS string `json:"txCTCSS"` // Hz value from CTCSSTones, or "" for no tone
	RxCTCSS string `json:"rxCTCSS"`

	Squelch int `json:"squelch"` // 0-8
	Volume  int `json:"volume"`  // 1-8

	PreDeEmphasis  bool `json:"preDeEmphasis"`
	HighPassFilter bool `json:"highPassFilter"`
	LowPassFilter  bool `json:"lowPassFilter"`
}

// enableDisableCode encodes a filter flag the way the module's own
// AT+SETFILTER command expects -- confirmed directly against the
// reference tool's source (its enabledisable() helper: "Enable" -> 0,
// "Disable" -> 1). The inverted-looking convention (0 means on) is the
// module's own, not a mistake here.
func enableDisableCode(b bool) string {
	if b {
		return "0"
	}
	return "1"
}

// Program connects to the module (over portName, or by probing the
// usual candidate ports if portName is "") and applies s -- radio
// (frequency/tone/squelch), volume, and filters, as three AT commands
// over one connection -- returning a human-readable transcript of what
// was sent and received, and a best-effort success verdict (every
// command must have gotten the module's own documented "OK" reply).
//
// This is write-only: the SA818/DRA818 AT command set has no documented
// way to query the module's currently-programmed frequency/tone/squelch
// back out, so there's nothing to read from the hardware itself.
func Program(ctx context.Context, portName string, s Settings) (output string, ok bool, err error) {
	txTone, err := ctcssCode(s.TxCTCSS)
	if err != nil {
		return "", false, fmt.Errorf("transmit CTCSS: %w", err)
	}
	rxTone, err := ctcssCode(s.RxCTCSS)
	if err != nil {
		return "", false, fmt.Errorf("receive CTCSS: %w", err)
	}

	r, err := connect(portName)
	if err != nil {
		return "", false, err
	}
	defer r.close()

	var out strings.Builder
	record := func(cmd, reply string, exErr error) bool {
		fmt.Fprintf(&out, "> %s\n", cmd)
		if exErr != nil {
			fmt.Fprintf(&out, "< (error: %v)\n", exErr)
			return false
		}
		fmt.Fprintf(&out, "< %s\n", reply)
		return true
	}

	bw := "0"
	if s.Wide {
		bw = "1"
	}
	groupCmd := fmt.Sprintf("%s=%s,%s,%s,%s,%d,%s", setGroupCmd, bw, s.TxFreqMHz, s.RxFreqMHz, txTone, s.Squelch, rxTone)
	reply, exErr := r.exchange(groupCmd, settleDelay)
	okRadio := record(groupCmd, reply, exErr) && reply == "+DMOSETGROUP:0"

	volumeCmd := fmt.Sprintf("%s=%d", setVolumeCmd, s.Volume)
	reply, exErr = r.exchange(volumeCmd, settleDelay)
	okVolume := record(volumeCmd, reply, exErr) && reply == "+DMOSETVOLUME:0"

	filterCmd := fmt.Sprintf("%s=%s,%s,%s", setFilterCmd, enableDisableCode(s.PreDeEmphasis), enableDisableCode(s.HighPassFilter), enableDisableCode(s.LowPassFilter))
	reply, exErr = r.exchange(filterCmd, settleDelay)
	okFilters := record(filterCmd, reply, exErr) && reply == "+DMOSETFILTER:0"

	return out.String(), okRadio && okVolume && okFilters, nil
}
