package asteriskconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConf(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rpt.conf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetValuesUpdatesExistingKeyPreservingComment(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2

[1999](node-main)
rxchannel = SimpleUSB/1999      ; SimpleUSB
`)
	if err := SetValues(path, "1999", map[string]string{"rxchannel": "Radio/1999"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `[node-main](!)
duplex = 2

[1999](node-main)
rxchannel = Radio/1999      ; SimpleUSB
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetValuesAppendsMissingKeyBeforeNextSection(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2

[1999](node-main)
rxchannel = SimpleUSB/1999

[2000](node-main)
rxchannel = SimpleUSB/2000
`)
	if err := SetValues(path, "1999", map[string]string{"idrecording": "|iWB6NIL"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "idrecording = |iWB6NIL") {
		t.Fatalf("missing appended key:\n%s", s)
	}
	// Must land inside [1999]'s body, before [2000] starts.
	idx1999 := strings.Index(s, "[1999]")
	idxNew := strings.Index(s, "idrecording")
	idx2000 := strings.Index(s, "[2000]")
	if !(idx1999 < idxNew && idxNew < idx2000) {
		t.Errorf("appended key not inside [1999]'s body: %s", s)
	}
}

func TestSetValuesDoesNotTouchOtherSections(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2
rxchannel = Local/pseudo

[1999](node-main)
rxchannel = SimpleUSB/1999
`)
	if err := SetValues(path, "1999", map[string]string{"rxchannel": "Radio/1999"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "rxchannel = Local/pseudo") {
		t.Errorf("node-main's own rxchannel must be untouched:\n%s", got)
	}
}

func TestSetValuesUpdatesLastOccurrenceOnly(t *testing.T) {
	// Matches Resolved.Value's own "last occurrence wins" semantics --
	// editing an earlier duplicate would leave the effective value (and
	// thus the operator's actual on-air behavior) unchanged.
	path := writeTempConf(t, `[1999](node-main)
rxmixerset = 200
rxmixerset = 500
`)
	if err := SetValues(path, "1999", map[string]string{"rxmixerset": "700"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[1999](node-main)\nrxmixerset = 200\nrxmixerset = 700\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSetValuesUnknownSectionErrors(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2
`)
	if err := SetValues(path, "9999", map[string]string{"rxchannel": "x"}); err == nil {
		t.Error("expected an error for a section that doesn't exist")
	}
}

func TestSetValuesPreservesFileModeAndTrailingNewline(t *testing.T) {
	path := writeTempConf(t, "[1999](node-main)\nrxchannel = SimpleUSB/1999\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := SetValues(path, "1999", map[string]string{"rxchannel": "Radio/1999"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", info.Mode().Perm())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), "\n") {
		t.Errorf("expected trailing newline, got %q", got)
	}
}

func TestSetNthValueInSectionRewritesByPosition(t *testing.T) {
	path := writeTempConf(t, `[extensions](!)
exten => 1,1,Answer()
exten => 1,2,Playback(demo)
exten => 2,1,Answer()
`)
	ok, err := SetNthValueInSection(path, "extensions", 1, "2,Playback(other)")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `[extensions](!)
exten => 1,1,Answer()
exten => 2,Playback(other)
exten => 2,1,Answer()
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetNthValueInSectionOutOfRangeIsNotAnError(t *testing.T) {
	path := writeTempConf(t, `[1999](node-main)
rxchannel = SimpleUSB/1999
`)
	ok, err := SetNthValueInSection(path, "1999", 5, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("ok = true, want false for an out-of-range index")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "rxchannel = SimpleUSB/1999") {
		t.Errorf("file was modified despite out-of-range index: %s", got)
	}
}

func TestSetNthValueInSectionNeverTouchesOtherSections(t *testing.T) {
	path := writeTempConf(t, `[1999](node-main)
rxchannel = SimpleUSB/1999

[2000](node-main)
rxchannel = SimpleUSB/2000
`)
	if _, err := SetNthValueInSection(path, "1999", 0, "Radio/1999"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "rxchannel = SimpleUSB/2000") {
		t.Errorf("[2000] should be untouched:\n%s", got)
	}
}

func TestSetValuesRoundTripsThroughParseAndResolve(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2
rxchannel = Local/pseudo

[1999](node-main)
rxchannel = SimpleUSB/1999
`)
	if err := SetValues(path, "1999", map[string]string{
		"rxchannel": "Radio/1999",
		"duplex":    "1",
	}); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Resolve("1999")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Value("rxchannel"); got != "Radio/1999" {
		t.Errorf("rxchannel = %q", got)
	}
	if got, _ := r.Value("duplex"); got != "1" {
		t.Errorf("duplex = %q", got)
	}
}
