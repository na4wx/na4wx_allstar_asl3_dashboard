package sa818

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"go.bug.st/serial"
)

// AT command protocol constants, reverse-engineered directly from ASL3's
// own /usr/bin/sa818 (a Python tool -- its full source was read on a
// real node rather than guessed at). This package talks to the module
// directly over serial instead of shelling out to that tool: confirmed
// on real hardware that repeated automated invocations of the reference
// tool's own CLI reliably fail to actually reprogram the module, despite
// it reporting a genuine device-acknowledged "OK" reply. The root cause
// was never pinned down (sa818-menu, a bash wrapper around the exact
// same CLI tool, was reported to work -- but its own script, also read
// in full, builds and sends the identical AT+DMOSETGROUP command for an
// equivalent setting, so no code-level difference explains it). Driving
// the protocol ourselves removes that entire layer of uncertainty rather
// than trying to work around behavior that was never understood.
const (
	initCmd      = "AT+DMOCONNECT"
	setGroupCmd  = "AT+DMOSETGROUP"
	setFilterCmd = "AT+SETFILTER"
	setVolumeCmd = "AT+DMOSETVOLUME"

	eol = "\r\n"

	defaultBaud = 9600
	// readTimeout matches the reference tool's own READ_TIMEOUT exactly.
	readTimeout = 3 * time.Second
)

// settleDelay is how long the reference tool waits after writing a
// SETGROUP/SETVOLUME/SETFILTER command before reading its reply
// (time.sleep(1) in its source) -- matched here exactly since the real
// reason the reference CLI is unreliable was never confirmed, and timing
// is one plausible factor worth not deviating from. A var (not a const)
// purely so tests can zero it out instead of spending real wall-clock
// time on a delay that's only meaningful against real hardware.
var settleDelay = 1 * time.Second

// candidatePorts is tried in order when no explicit port is configured,
// matching the reference tool's own PORTS tuple exactly.
var candidatePorts = []string{"/dev/serial0", "/dev/ttyUSB0"}

// openPort is swapped out in tests so they never touch a real serial
// device.
var openPort = func(name string, mode *serial.Mode) (serial.Port, error) {
	return serial.Open(name, mode)
}

// radio is one open, handshaken connection to the SA818/DRA818 module.
type radio struct {
	port serial.Port
	r    *bufio.Reader
}

// connect opens portName (or, if empty, tries each of candidatePorts in
// turn) and performs the module's own AT+DMOCONNECT handshake --
// matching the reference tool's own connection logic exactly: a port is
// only considered "found" once it actually replies "+DMOCONNECT:0",
// since there's no other way to distinguish "the SA818 module is on
// this port" from "some other, unrelated serial device is".
func connect(portName string) (*radio, error) {
	ports := candidatePorts
	if portName != "" {
		ports = []string{portName}
	}
	mode := &serial.Mode{BaudRate: defaultBaud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}

	var lastErr error
	for _, p := range ports {
		port, err := openPort(p, mode)
		if err != nil {
			lastErr = fmt.Errorf("open %s: %w", p, err)
			continue
		}
		if err := port.SetReadTimeout(readTimeout); err != nil {
			port.Close()
			lastErr = fmt.Errorf("%s: set read timeout: %w", p, err)
			continue
		}
		r := &radio{port: port, r: bufio.NewReader(port)}
		reply, err := r.exchange(initCmd, 0)
		if err != nil || reply != "+DMOCONNECT:0" {
			port.Close()
			lastErr = fmt.Errorf("%s: unexpected reply to %s: %q (%v)", p, initCmd, reply, err)
			continue
		}
		return r, nil
	}
	return nil, fmt.Errorf("could not find the SA818/DRA818 module on any port: %w", lastErr)
}

func (r *radio) close() {
	r.port.Close()
}

// exchange writes cmd (framed with the module's own \r\n line ending),
// waits delay (0 for the initial handshake, settleDelay for every other
// command -- matching the reference tool exactly), and reads back one
// line of reply.
func (r *radio) exchange(cmd string, delay time.Duration) (string, error) {
	if _, err := r.port.Write([]byte(cmd + eol)); err != nil {
		return "", fmt.Errorf("write %q: %w", cmd, err)
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	line, err := r.r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read reply to %q: %w", cmd, err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
