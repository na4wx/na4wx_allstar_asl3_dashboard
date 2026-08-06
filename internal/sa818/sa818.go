// Package sa818 drives ASL3's own built-in `sa818` command-line tool to
// program an SA818/DRA818 VHF/UHF radio module (as used on a SHARI USB
// node) over its serial connection, without reimplementing the module's
// AT-command protocol directly.
//
// This is NOT HamVoIP's "818-prog" (a Python script baked into that
// distro's own disk image, absent on ASL3 entirely) -- confirmed on a
// real ASL3 node that it ships its own, different `sa818` tool at
// /usr/bin/sa818 (see AllStarLink's own docs,
// allstarlink.github.io/adv-topics/sa818modules/). The two are
// structurally different, not just differently named: sa818 takes
// command-line flags across three independent subcommands (radio,
// volume, filters) rather than answering one interactive prompt
// sequence; CTCSS is given as the tone's raw Hz value (e.g. "94.8"),
// not 818-prog's 4-digit module code; squelch ranges 0-8 (818-prog: 1-9)
// and volume ranges 1-8 (818-prog: 0-8), confirmed via
// `sa818 radio --help`/`sa818 volume --help` on a live node; and
// frequency is given as a single receive frequency plus a MHz shift
// ("--offset"), rather than independent transmit/receive values.
package sa818

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Settings mirrors ASL3's own sa818 tool's flags. JSON tags exist for
// internal/cloudagent's relayed sa818.program action; they have no
// effect on this struct's existing Go-field-name access elsewhere.
type Settings struct {
	Wide bool `json:"wide"` // sa818's own "--bw": false=0 (narrow, 12.5kHz), true=1 (wide, 25kHz)

	// TxFreqMHz/RxFreqMHz are independent transmit/receive frequencies,
	// matching this app's own form -- sa818 itself only takes one
	// frequency (confirmed: the receive frequency, via "--frequency")
	// plus a MHz "--offset" describing the shift to transmit; Program
	// computes that offset from these two fields.
	TxFreqMHz string `json:"txFreqMHz"`
	RxFreqMHz string `json:"rxFreqMHz"`

	// TxCTCSS/RxCTCSS are the tone's raw Hz value (e.g. "94.8", from
	// CTCSSTones) or "" for no tone -- sa818's own "--ctcss" flag takes
	// these directly, unlike 818-prog's 4-digit code.
	TxCTCSS string `json:"txCTCSS"`
	RxCTCSS string `json:"rxCTCSS"`

	Squelch int `json:"squelch"` // sa818's own range: 0-8
	Volume  int `json:"volume"`  // sa818's own range: 1-8

	PreDeEmphasis  bool `json:"preDeEmphasis"`
	HighPassFilter bool `json:"highPassFilter"`
	LowPassFilter  bool `json:"lowPassFilter"`
}

func enableDisable(b bool) string {
	if b {
		return "Enable"
	}
	return "Disable"
}

// ctcssArg builds sa818's own "--ctcss tx,rx" value, or "" to omit the
// flag entirely when neither side wants a tone ("None" is the tool's own
// documented default, so there's no need to pass it explicitly in that
// case). When only one side wants a tone, the other is spelled out as
// the literal "None" -- confirmed via `sa818 radio --help`'s own
// example ("--ctcss 94.8,127.3") as the documented way to give
// independent transmit/receive values.
func ctcssArg(tx, rx string) string {
	if tx == "" && rx == "" {
		return ""
	}
	t, r := tx, rx
	if t == "" {
		t = "None"
	}
	if r == "" {
		r = "None"
	}
	return t + "," + r
}

// offsetMHz computes sa818's own "--offset" (transmit minus receive, in
// MHz) from this app's independently-specified Tx/Rx frequencies.
func offsetMHz(txFreqMHz, rxFreqMHz string) (string, error) {
	tx, err := strconv.ParseFloat(txFreqMHz, 64)
	if err != nil {
		return "", fmt.Errorf("tx frequency: %w", err)
	}
	rx, err := strconv.ParseFloat(rxFreqMHz, 64)
	if err != nil {
		return "", fmt.Errorf("rx frequency: %w", err)
	}
	return strconv.FormatFloat(tx-rx, 'f', 4, 64), nil
}

// Program runs ASL3's own sa818 tool across its three independent
// subcommands (radio, volume, filters) to apply s, stopping at the first
// one that fails to even run (e.g. the binary isn't on PATH) rather than
// piling on repeat failures, and returns their combined stdout+stderr
// alongside a best-effort success verdict.
//
// This is write-only: the SA818/DRA818 AT command set has no documented
// way to query the module's currently-programmed frequency/tone/squelch
// back out, so there's nothing to read from the hardware itself.
//
// Unlike 818-prog (confirmed, on real hardware, to exit 0 even when the
// module itself rejects a setting -- success there has to be judged from
// its output text, not its exit code), sa818's own exit code is trusted
// here to reflect success/failure. That contract -- specifically,
// whether sa818 also exits 0 on a module-level rejection the way
// 818-prog does -- has NOT yet been confirmed against a real rejection
// on real hardware.
func Program(ctx context.Context, tool string, s Settings) (output string, ok bool, err error) {
	offset, offsetErr := offsetMHz(s.TxFreqMHz, s.RxFreqMHz)
	if offsetErr != nil {
		return "", false, offsetErr
	}
	bw := "0"
	if s.Wide {
		bw = "1"
	}
	radioArgs := []string{"radio", "--frequency=" + s.RxFreqMHz, "--offset=" + offset, "--bw=" + bw, "--squelch=" + strconv.Itoa(s.Squelch)}
	if ctcss := ctcssArg(s.TxCTCSS, s.RxCTCSS); ctcss != "" {
		radioArgs = append(radioArgs, "--ctcss="+ctcss)
	}
	volumeArgs := []string{"volume", "--level=" + strconv.Itoa(s.Volume)}
	filterArgs := []string{"filters", "--emphasis=" + enableDisable(s.PreDeEmphasis), "--highpass=" + enableDisable(s.HighPassFilter), "--lowpass=" + enableDisable(s.LowPassFilter)}

	var out bytes.Buffer
	run := func(args []string) bool {
		if err != nil {
			return false // a prior subcommand already failed to run -- don't pile on
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		c := exec.CommandContext(cctx, tool, args...)
		c.Stdout = &out
		c.Stderr = &out
		if runErr := c.Run(); runErr != nil {
			err = fmt.Errorf("%s %s: %w", tool, strings.Join(args, " "), runErr)
			return false
		}
		return true
	}

	okRadio := run(radioArgs)
	okVolume := run(volumeArgs)
	okFilters := run(filterArgs)

	return out.String(), okRadio && okVolume && okFilters, err
}
