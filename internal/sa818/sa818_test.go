package sa818

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnswersOrderMatchesPromptSequence(t *testing.T) {
	s := Settings{
		Wide:           false,
		TxFreqMHz:      "446.1000",
		RxFreqMHz:      "446.1000",
		TxCTCSS:        "0000",
		RxCTCSS:        "0000",
		Squelch:        4,
		Volume:         5,
		PreDeEmphasis:  true,
		HighPassFilter: true,
		LowPassFilter:  false,
	}
	got := s.answers()
	want := "0\n446.1000\n446.1000\n0000\n0000\n4\n5\ny\ny\nn\ny\n"
	if got != want {
		t.Fatalf("answers() = %q, want %q", got, want)
	}
}

func TestAnswersWideSendsOne(t *testing.T) {
	s := Settings{Wide: true, TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", TxCTCSS: "0000", RxCTCSS: "0000"}
	got := s.answers()
	if !strings.HasPrefix(got, "1\n") {
		t.Fatalf("answers() = %q, want it to start with \"1\\n\" for Wide", got)
	}
}

// fakeTool writes a shell script standing in for 818-prog that just
// dumps whatever it read from stdin plus a fixed tail, and always exits
// 0 -- mirroring the real tool, which (confirmed live) exits 0 even when
// the module itself rejects the command.
func fakeTool(t *testing.T, tail string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-818-prog")
	script := "#!/bin/sh\ncat >/dev/null\necho '" + tail + "'\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}
	return path
}

func TestProgramDetectsModuleErrorDespiteZeroExit(t *testing.T) {
	tool := fakeTool(t, "Error, invalid information (+DMOSETGROUP:1). Check input format..")
	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", TxCTCSS: "0000", RxCTCSS: "0000", Squelch: 4, Volume: 5}
	output, ok, err := Program(context.Background(), tool, s)
	if err != nil {
		t.Fatalf("Program() error = %v, want nil (tool itself exits 0)", err)
	}
	if ok {
		t.Fatalf("Program() ok = true, want false when output contains \"Error\"")
	}
	if !strings.Contains(output, "DMOSETGROUP") {
		t.Fatalf("Program() output = %q, want it to include the fake tool's message", output)
	}
}

func TestProgramSuccess(t *testing.T) {
	tool := fakeTool(t, "OK")
	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", TxCTCSS: "0000", RxCTCSS: "0000", Squelch: 4, Volume: 5}
	_, ok, err := Program(context.Background(), tool, s)
	if err != nil {
		t.Fatalf("Program() error = %v", err)
	}
	if !ok {
		t.Fatalf("Program() ok = false, want true when output has no \"Error\"")
	}
}

func TestProgramMissingTool(t *testing.T) {
	_, ok, err := Program(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), Settings{})
	if err == nil {
		t.Fatalf("Program() error = nil, want an error for a missing binary")
	}
	if ok {
		t.Fatalf("Program() ok = true, want false on error")
	}
}
