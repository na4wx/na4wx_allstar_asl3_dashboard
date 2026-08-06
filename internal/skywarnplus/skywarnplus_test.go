package skywarnplus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsInstalled(t *testing.T) {
	dir := t.TempDir()
	if IsInstalled(dir) {
		t.Fatal("IsInstalled should be false for an empty directory")
	}
	if err := os.WriteFile(filepath.Join(dir, "SkywarnPlus.py"), []byte("#!/usr/bin/python3\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled(dir) {
		t.Fatal("IsInstalled should be true once SkywarnPlus.py exists")
	}
}

// fakePython3 stands in for the real `python3` binary, matching the
// established fakeSox/fakePiper/fakeEspeak pattern used throughout this
// repo's tests: PATH is temporarily pointed at a directory containing a
// script literally named "python3", so runPython's hardcoded
// exec.CommandContext(ctx, "python3", ...) call resolves to it via PATH
// resolution rather than needing runPython itself to take a
// configurable tool path (it deliberately doesn't -- see the package
// doc). script is written to stdout, "" to fail with the given stderr.
func fakePython3(t *testing.T, exitOK bool, stdout, stderrMsg string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "python3")
	exit := "0"
	if !exitOK {
		exit = "1"
	}
	script := "#!/bin/sh\n"
	if stderrMsg != "" {
		script += "echo '" + stderrMsg + "' >&2\n"
	}
	if exitOK {
		script += "printf '" + strings.ReplaceAll(stdout, "'", "'\\''") + "'\n"
	}
	script += "exit " + exit + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake python3: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGetStatusSuccess(t *testing.T) {
	fakePython3(t, true, `{"enable":true,"sayalert":false,"sayallclear":true,"tailmessage":false,"alertscript":true,"courtesytone_enable":true,"idchange_enable":false,"active_alert_count":3,"countycodes":["ARC125"],"nodes":["2000"],"pushover":{"enable":true,"userkey":"uk","apitoken":"at","debug":true},"skydescribe":{"apikey":"ak","language":"en-gb","speed":5,"voice":"Mary","maxwords":200}}`, "")
	status, err := GetStatus(context.Background(), "/unused/dir")
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	want := Status{
		Enable: true, SayAlert: false, SayAllClear: true, Tailmessage: false, AlertScript: true,
		CourtesyToneSwapEnabled: true, IDSwapEnabled: false, ActiveAlertCount: 3,
		CountyCodes: []string{"ARC125"}, Nodes: []string{"2000"},
		Pushover:    PushoverStatus{Enable: true, UserKey: "uk", APIToken: "at", Debug: true},
		SkyDescribe: SkyDescribeStatus{APIKey: "ak", Language: "en-gb", Speed: 5, Voice: "Mary", MaxWords: 200},
	}
	if status.Enable != want.Enable || status.SayAlert != want.SayAlert || status.SayAllClear != want.SayAllClear || status.Tailmessage != want.Tailmessage || status.AlertScript != want.AlertScript {
		t.Errorf("scalar fields = %+v, want %+v", status, want)
	}
	if status.CourtesyToneSwapEnabled != want.CourtesyToneSwapEnabled || status.IDSwapEnabled != want.IDSwapEnabled || status.ActiveAlertCount != want.ActiveAlertCount {
		t.Errorf("new fields = %+v, want %+v", status, want)
	}
	if len(status.CountyCodes) != 1 || status.CountyCodes[0] != "ARC125" {
		t.Errorf("CountyCodes = %v", status.CountyCodes)
	}
	if len(status.Nodes) != 1 || status.Nodes[0] != "2000" {
		t.Errorf("Nodes = %v", status.Nodes)
	}
	if status.Pushover != want.Pushover {
		t.Errorf("Pushover = %+v, want %+v", status.Pushover, want.Pushover)
	}
	if status.SkyDescribe != want.SkyDescribe {
		t.Errorf("SkyDescribe = %+v, want %+v", status.SkyDescribe, want.SkyDescribe)
	}
}

// TestGetStatusNoCountiesOrNodesIsNonNil confirms a station with
// neither county codes nor registered nodes configured -- where
// sky_configure.py's own JSON omits (or sends null for) those fields --
// gets back non-nil empty slices, not nil. A nil slice marshals to
// JSON null again, and the cloud relay's skywarn.getStatus action sends
// this straight to the browser as JSON.
func TestGetStatusNoCountiesOrNodesIsNonNil(t *testing.T) {
	fakePython3(t, true, `{"enable":true,"countycodes":null,"pushover":{},"skydescribe":{}}`, "")
	status, err := GetStatus(context.Background(), "/unused/dir")
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status.CountyCodes == nil {
		t.Error("CountyCodes = nil, want a non-nil empty slice")
	}
	if len(status.CountyCodes) != 0 {
		t.Errorf("CountyCodes = %v, want empty", status.CountyCodes)
	}
	if status.Nodes == nil {
		t.Error("Nodes = nil, want a non-nil empty slice")
	}
	if len(status.Nodes) != 0 {
		t.Errorf("Nodes = %v, want empty", status.Nodes)
	}
}

func TestGetStatusToolFailure(t *testing.T) {
	fakePython3(t, false, "", "config.yaml not found")
	_, err := GetStatus(context.Background(), "/unused/dir")
	if err == nil {
		t.Fatal("GetStatus() error = nil, want an error when the tool exits non-zero")
	}
	if !strings.Contains(err.Error(), "config.yaml not found") {
		t.Errorf("error = %v, want it to include the tool's stderr", err)
	}
}

func TestGetStatusMalformedJSON(t *testing.T) {
	fakePython3(t, true, "not json", "")
	_, err := GetStatus(context.Background(), "/unused/dir")
	if err == nil {
		t.Fatal("GetStatus() error = nil, want an error for malformed JSON output")
	}
}

func TestSetCountiesJoinsWithComma(t *testing.T) {
	fakePython3(t, true, "OK", "")
	out, err := SetCounties(context.Background(), "/unused/dir", []string{"ARC125", "ARC119"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "OK" {
		t.Errorf("output = %q", out)
	}
}

func TestAddNode(t *testing.T) {
	fakePython3(t, true, "OK", "")
	if _, err := AddNode(context.Background(), "/unused/dir", "2000"); err != nil {
		t.Fatal(err)
	}
}

func TestSetToggleRejectsUnknownKey(t *testing.T) {
	if _, err := SetToggle(context.Background(), "/unused/dir", "changect", true); err == nil {
		t.Fatal("SetToggle() error = nil, want rejection of a key not in VALID_KEYS' boolean set")
	}
}

func TestSetToggleValidKey(t *testing.T) {
	fakePython3(t, true, "OK", "")
	if _, err := SetToggle(context.Background(), "/unused/dir", "sayalert", true); err != nil {
		t.Fatal(err)
	}
}

func TestSetPushover(t *testing.T) {
	fakePython3(t, true, "OK", "")
	out, err := SetPushover(context.Background(), "/unused/dir", true, "uk", "at", false)
	if err != nil {
		t.Fatal(err)
	}
	if out != "OK" {
		t.Errorf("output = %q", out)
	}
}

func TestSetSkyDescribe(t *testing.T) {
	fakePython3(t, true, "OK", "")
	out, err := SetSkyDescribe(context.Background(), "/unused/dir", "ak", "en-us", 1, "John", 150)
	if err != nil {
		t.Fatal(err)
	}
	if out != "OK" {
		t.Errorf("output = %q", out)
	}
}
