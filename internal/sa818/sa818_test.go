package sa818

import (
	"context"
	"strings"
	"testing"
)

func TestProgramSendsExpectedATCommands(t *testing.T) {
	port := &scriptedPort{responses: []string{
		"+DMOCONNECT:0\r\n",
		"+DMOSETGROUP:0\r\n",
		"+DMOSETVOLUME:0\r\n",
		"+DMOSETFILTER:0\r\n",
	}}
	installFakePort(t, port)

	s := Settings{
		Wide:           false,
		TxFreqMHz:      "446.1750",
		RxFreqMHz:      "446.1750",
		TxCTCSS:        "100.0",
		RxCTCSS:        "100.0",
		Squelch:        4,
		Volume:         5,
		PreDeEmphasis:  true,
		HighPassFilter: false,
		LowPassFilter:  true,
	}
	output, ok, err := Program(context.Background(), "", s)
	if err != nil {
		t.Fatalf("Program() error = %v", err)
	}
	if !ok {
		t.Fatalf("Program() ok = false, output:\n%s", output)
	}

	if len(port.writes) != 4 {
		t.Fatalf("got %d writes, want 4 (connect, radio, volume, filters): %v", len(port.writes), port.writes)
	}
	if port.writes[0] != "AT+DMOCONNECT\r\n" {
		t.Errorf("writes[0] = %q, want the handshake", port.writes[0])
	}
	// 100.0 Hz is index 12 in CTCSSTones (1-based) -> code "0012".
	wantRadio := "AT+DMOSETGROUP=0,446.1750,446.1750,0012,4,0012\r\n"
	if port.writes[1] != wantRadio {
		t.Errorf("writes[1] = %q, want %q", port.writes[1], wantRadio)
	}
	if port.writes[2] != "AT+DMOSETVOLUME=5\r\n" {
		t.Errorf("writes[2] = %q, want AT+DMOSETVOLUME=5", port.writes[2])
	}
	// PreDeEmphasis=true -> "0" (Enable), HighPass=false -> "1" (Disable), LowPass=true -> "0".
	wantFilters := "AT+SETFILTER=0,1,0\r\n"
	if port.writes[3] != wantFilters {
		t.Errorf("writes[3] = %q, want %q", port.writes[3], wantFilters)
	}
}

func TestProgramNoToneSendsZeroCode(t *testing.T) {
	port := &scriptedPort{responses: []string{
		"+DMOCONNECT:0\r\n",
		"+DMOSETGROUP:0\r\n",
		"+DMOSETVOLUME:0\r\n",
		"+DMOSETFILTER:0\r\n",
	}}
	installFakePort(t, port)

	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", Squelch: 4, Volume: 4}
	if _, ok, err := Program(context.Background(), "", s); err != nil || !ok {
		t.Fatalf("Program() ok=%v err=%v", ok, err)
	}
	if !strings.Contains(port.writes[1], ",0000,4,0000") {
		t.Errorf("radio command = %q, want tx/rx tone \"0000\" for no tone", port.writes[1])
	}
}

func TestProgramReportsModuleRejection(t *testing.T) {
	// The module can ACK the handshake but reject a specific command --
	// confirmed as a real, distinct code path in the reference tool's
	// own source (it compares the reply string, not just "did we get a
	// reply at all").
	port := &scriptedPort{responses: []string{
		"+DMOCONNECT:0\r\n",
		"+DMOSETGROUP:1\r\n", // non-zero = rejected
		"+DMOSETVOLUME:0\r\n",
		"+DMOSETFILTER:0\r\n",
	}}
	installFakePort(t, port)

	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", Squelch: 4, Volume: 4}
	output, ok, err := Program(context.Background(), "", s)
	if err != nil {
		t.Fatalf("Program() error = %v, want nil (a rejection is reported via ok/output, not err)", err)
	}
	if ok {
		t.Error("Program() ok = true, want false when the module rejects the radio command")
	}
	if !strings.Contains(output, "+DMOSETGROUP:1") {
		t.Errorf("output should include the module's own rejection reply:\n%s", output)
	}
}

func TestProgramNoPortRespondsFails(t *testing.T) {
	port := &scriptedPort{} // no responses queued at all -- every port "found" fails the handshake
	installFakePort(t, port)

	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", Squelch: 4, Volume: 4}
	_, ok, err := Program(context.Background(), "", s)
	if err == nil {
		t.Fatal("Program() error = nil, want an error when no port responds to the handshake")
	}
	if ok {
		t.Error("Program() ok = true, want false")
	}
}

func TestProgramExplicitPortSkipsProbing(t *testing.T) {
	port := &scriptedPort{responses: []string{
		"+DMOCONNECT:0\r\n",
		"+DMOSETGROUP:0\r\n",
		"+DMOSETVOLUME:0\r\n",
		"+DMOSETFILTER:0\r\n",
	}}
	installFakePort(t, port)

	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", Squelch: 4, Volume: 4}
	if _, ok, err := Program(context.Background(), "/dev/ttyUSB3", s); err != nil || !ok {
		t.Fatalf("Program() ok=%v err=%v", ok, err)
	}
}

func TestProgramInvalidCTCSSErrors(t *testing.T) {
	port := &scriptedPort{}
	installFakePort(t, port)

	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", TxCTCSS: "196.6", Squelch: 4, Volume: 4}
	_, ok, err := Program(context.Background(), "", s)
	if err == nil {
		t.Fatal("Program() error = nil, want an error for a non-standard CTCSS tone")
	}
	if ok {
		t.Error("Program() ok = true, want false")
	}
	if len(port.writes) != 0 {
		t.Errorf("should never touch the port when settings fail to validate, got %d writes", len(port.writes))
	}
}

func TestEnableDisableCodeMatchesModuleConvention(t *testing.T) {
	// Confirmed against the reference tool's own enabledisable(): the
	// module's own convention is inverted from what you'd guess -- 0
	// means "enable".
	if enableDisableCode(true) != "0" {
		t.Errorf("enableDisableCode(true) = %q, want \"0\" (enable)", enableDisableCode(true))
	}
	if enableDisableCode(false) != "1" {
		t.Errorf("enableDisableCode(false) = %q, want \"1\" (disable)", enableDisableCode(false))
	}
}
