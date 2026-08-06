package sa818

import (
	"bytes"
	"errors"
	"time"

	"go.bug.st/serial"
)

// scriptedPort is a fake serial.Port for tests: every Write is recorded,
// and queues up the next canned response (if any) to become available
// from Read -- simulating "the device replies after receiving a
// command" without needing real concurrency, since this package's own
// protocol is strictly half-duplex request/response.
type scriptedPort struct {
	writes    []string
	responses []string
	readBuf   bytes.Buffer

	openErr  error // if set, openPort returns this instead of a port
	writeErr error
	readErr  error
}

func (p *scriptedPort) SetMode(*serial.Mode) error { return nil }

func (p *scriptedPort) Write(b []byte) (int, error) {
	if p.writeErr != nil {
		return 0, p.writeErr
	}
	p.writes = append(p.writes, string(b))
	if len(p.responses) > 0 {
		next := p.responses[0]
		p.responses = p.responses[1:]
		p.readBuf.WriteString(next)
	}
	return len(b), nil
}

func (p *scriptedPort) Read(b []byte) (int, error) {
	if p.readErr != nil {
		return 0, p.readErr
	}
	if p.readBuf.Len() == 0 {
		return 0, errors.New("scriptedPort: read timeout (no more canned responses)")
	}
	return p.readBuf.Read(b)
}

func (p *scriptedPort) Drain() error             { return nil }
func (p *scriptedPort) ResetInputBuffer() error  { return nil }
func (p *scriptedPort) ResetOutputBuffer() error { return nil }
func (p *scriptedPort) SetDTR(bool) error        { return nil }
func (p *scriptedPort) SetRTS(bool) error        { return nil }
func (p *scriptedPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (p *scriptedPort) SetReadTimeout(time.Duration) error { return nil }
func (p *scriptedPort) Close() error                       { return nil }
func (p *scriptedPort) Break(time.Duration) error          { return nil }

// installFakePort replaces openPort for the duration of one test,
// returning port for any candidate name -- restoring the real openPort
// (which would touch actual hardware) once the test finishes. Also
// zeroes settleDelay for the test's duration: the real delay only means
// anything against real hardware, and there's no reason to actually
// wait 3 real seconds (3 commands x 1s) per test.
func installFakePort(t interface{ Cleanup(func()) }, port *scriptedPort) {
	origPort := openPort
	origDelay := settleDelay
	settleDelay = 0
	openPort = func(name string, mode *serial.Mode) (serial.Port, error) {
		if port.openErr != nil {
			return nil, port.openErr
		}
		return port, nil
	}
	t.Cleanup(func() {
		openPort = origPort
		settleDelay = origDelay
	})
}
