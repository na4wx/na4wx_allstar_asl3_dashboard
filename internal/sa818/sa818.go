// Package sa818 drives the 818-prog command-line tool to program an
// SA818/DRA818 VHF/UHF radio module (as used on a SHARI USB node) over
// its serial connection, without reimplementing the module's AT-command
// protocol directly.
//
// That's a deliberate choice, not a shortcut: 818-prog is already
// confirmed (on real hardware, in this project's development) to reach
// the module over /dev/ttyUSB0, but several details of the protocol
// itself are not fully verified — notably, a real test run sent
// "AT+DMOSETGROUP=1,..." to the module after "0" was typed at the
// "Channel Spacing (0 or 1)" prompt, an unexplained discrepancy. Driving
// the existing tool through the same prompts a person would answer by
// hand avoids guessing at framing/encoding details that could otherwise
// misprogram a live transmitter.
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

// Settings mirrors 818-prog's interactive prompts in order. Field order
// in answers() below must match that prompt sequence exactly, since the
// answers are piped in over stdin regardless of what the tool has
// printed so far.
// JSON tags exist only for internal/cloudagent's relayed sa818.program
// action (see that package's actions_sa818.go); they have no effect on
// this struct's existing Go-field-name access elsewhere.
type Settings struct {
	Wide           bool   `json:"wide"`           // "Channel Spacing (0 or 1)" — true sends "1", false sends "0"
	TxFreqMHz      string `json:"txFreqMHz"`      // "Tx Frequency (xxx.xxxx)" — caller should pass it pre-formatted, e.g. "446.1000"
	RxFreqMHz      string `json:"rxFreqMHz"`      // "Rx Frequency (xxx.xxxx)"
	TxCTCSS        string `json:"txCTCSS"`        // "Tx ctcss Code Value (xxxx)" — "0000" means no tone
	RxCTCSS        string `json:"rxCTCSS"`        // "Rx ctcss Code Value (xxxx)"
	Squelch        int    `json:"squelch"`        // "Squelch Value (1-9)"
	Volume         int    `json:"volume"`         // "Volume (0-8)"
	PreDeEmphasis  bool   `json:"preDeEmphasis"`  // "Enable Pre/De-Emphasis (y/[n])"
	HighPassFilter bool   `json:"highPassFilter"` // "Enable High Pass Filter (y/[n])"
	LowPassFilter  bool   `json:"lowPassFilter"`  // "Enable Low Pass Filter (y/[n])"
}

func yn(b bool) string {
	if b {
		return "y"
	}
	return "n"
}

// answers builds the newline-separated responses 818-prog's prompts
// expect, ending with "y" to accept its verify screen.
func (s Settings) answers() string {
	spacing := "0"
	if s.Wide {
		spacing = "1"
	}
	lines := []string{
		spacing,
		s.TxFreqMHz,
		s.RxFreqMHz,
		s.TxCTCSS,
		s.RxCTCSS,
		strconv.Itoa(s.Squelch),
		strconv.Itoa(s.Volume),
		yn(s.PreDeEmphasis),
		yn(s.HighPassFilter),
		yn(s.LowPassFilter),
		"y",
	}
	return strings.Join(lines, "\n") + "\n"
}

// Program runs tool (normally "818-prog", resolved via PATH), feeding it
// the answer sequence its prompts expect over stdin, and returns its
// full combined stdout+stderr for display alongside a best-effort
// success verdict. 818-prog exits 0 even when the module itself rejects
// the command — confirmed live, where it printed "Error, invalid
// information (+DMOSETGROUP:1)" but still exited successfully — so
// success has to be judged from the tool's own output text, not its
// exit code.
//
// This is write-only: the SA818/DRA818 AT command set has no documented
// way to query the module's currently-programmed frequency/tone/squelch
// back out, so there's nothing to read from the hardware itself.
func Program(ctx context.Context, tool string, s Settings) (output string, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	c := exec.CommandContext(ctx, tool)
	c.Stdin = strings.NewReader(s.answers())
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	runErr := c.Run()
	output = out.String()
	if runErr != nil {
		return output, false, fmt.Errorf("%s: %w", tool, runErr)
	}
	if strings.Contains(output, "Error") {
		return output, false, nil
	}
	return output, true, nil
}
