package sa818

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOffsetMHzSimplexIsZero(t *testing.T) {
	got, err := offsetMHz("446.1000", "446.1000")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.0000" {
		t.Errorf("offsetMHz(same freq) = %q, want \"0.0000\"", got)
	}
}

func TestOffsetMHzPositiveShift(t *testing.T) {
	got, err := offsetMHz("146.700", "146.100")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.6000" {
		t.Errorf("offsetMHz = %q, want \"0.6000\"", got)
	}
}

func TestOffsetMHzNegativeShift(t *testing.T) {
	got, err := offsetMHz("146.100", "146.700")
	if err != nil {
		t.Fatal(err)
	}
	if got != "-0.6000" {
		t.Errorf("offsetMHz = %q, want \"-0.6000\"", got)
	}
}

func TestCtcssArgBothEmpty(t *testing.T) {
	if got := ctcssArg("", ""); got != "" {
		t.Errorf("ctcssArg(\"\",\"\") = %q, want \"\" (omit the flag, matching sa818's own \"None\" default)", got)
	}
}

func TestCtcssArgBothSet(t *testing.T) {
	if got := ctcssArg("94.8", "127.3"); got != "94.8,127.3" {
		t.Errorf("ctcssArg = %q, want \"94.8,127.3\"", got)
	}
}

func TestCtcssArgOneSided(t *testing.T) {
	if got := ctcssArg("94.8", ""); got != "94.8,None" {
		t.Errorf("ctcssArg = %q, want \"94.8,None\"", got)
	}
	if got := ctcssArg("", "94.8"); got != "None,94.8" {
		t.Errorf("ctcssArg = %q, want \"None,94.8\"", got)
	}
}

// fakeSa818 writes a shell script standing in for ASL3's real sa818
// tool: it records each invocation's argv (one line per call, appended
// across calls so a test can inspect all three subcommand invocations)
// and exits with exitCode, echoing tail to stdout.
func fakeSa818(t *testing.T, tail string, exitCode int) (tool, argvLog string) {
	t.Helper()
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "fake-sa818")
	logPath := filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\necho \"$@\" >> " + logPath + "\necho '" + tail + "'\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(toolPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}
	return toolPath, logPath
}

func TestProgramInvokesThreeSubcommandsWithExpectedFlags(t *testing.T) {
	tool, argvLog := fakeSa818(t, "OK", 0)
	s := Settings{
		Wide:           false,
		TxFreqMHz:      "146.700",
		RxFreqMHz:      "146.100",
		TxCTCSS:        "94.8",
		RxCTCSS:        "94.8",
		Squelch:        4,
		Volume:         4,
		PreDeEmphasis:  true,
		HighPassFilter: false,
		LowPassFilter:  true,
	}
	_, ok, err := Program(context.Background(), tool, s)
	if err != nil {
		t.Fatalf("Program() error = %v", err)
	}
	if !ok {
		t.Fatal("Program() ok = false, want true")
	}

	data, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d invocations, want 3 (radio, volume, filters): %v", len(lines), lines)
	}

	radio := lines[0]
	for _, want := range []string{"radio", "--frequency=146.100", "--offset=0.6000", "--bw=0", "--squelch=4", "--ctcss=94.8,94.8"} {
		if !strings.Contains(radio, want) {
			t.Errorf("radio invocation %q missing %q", radio, want)
		}
	}

	volume := lines[1]
	if !strings.Contains(volume, "volume") || !strings.Contains(volume, "--level=4") {
		t.Errorf("volume invocation = %q, want it to contain \"volume\" and \"--level=4\"", volume)
	}

	filters := lines[2]
	for _, want := range []string{"filters", "--emphasis=Enable", "--highpass=Disable", "--lowpass=Enable"} {
		if !strings.Contains(filters, want) {
			t.Errorf("filters invocation %q missing %q", filters, want)
		}
	}
}

func TestProgramNoToneOmitsCtcssFlag(t *testing.T) {
	tool, argvLog := fakeSa818(t, "OK", 0)
	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", Squelch: 4, Volume: 4}
	if _, ok, err := Program(context.Background(), tool, s); err != nil || !ok {
		t.Fatalf("Program() = ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "--ctcss") {
		t.Errorf("expected no --ctcss flag when neither side wants a tone, got: %s", data)
	}
}

func TestProgramStopsAfterFirstFailure(t *testing.T) {
	tool, argvLog := fakeSa818(t, "boom", 1)
	s := Settings{TxFreqMHz: "446.1000", RxFreqMHz: "446.1000", Squelch: 4, Volume: 4}
	_, ok, err := Program(context.Background(), tool, s)
	if err == nil {
		t.Fatal("Program() error = nil, want non-nil after the radio subcommand fails")
	}
	if ok {
		t.Fatal("Program() ok = true, want false")
	}
	data, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d invocations, want exactly 1 (radio failed, volume/filters should be skipped): %v", len(lines), lines)
	}
}

func TestProgramMissingTool(t *testing.T) {
	_, ok, err := Program(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), Settings{TxFreqMHz: "446.1", RxFreqMHz: "446.1"})
	if err == nil {
		t.Fatal("Program() error = nil, want an error for a missing binary")
	}
	if ok {
		t.Fatal("Program() ok = true, want false on error")
	}
}

func TestProgramInvalidFrequencyErrors(t *testing.T) {
	_, ok, err := Program(context.Background(), "irrelevant", Settings{TxFreqMHz: "not-a-number", RxFreqMHz: "446.1"})
	if err == nil {
		t.Fatal("Program() error = nil, want an error for an unparseable frequency")
	}
	if ok {
		t.Fatal("Program() ok = true, want false")
	}
}
