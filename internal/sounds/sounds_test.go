package sounds

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a small test helper for populating a fixture directory.
func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestListCustomMatchesRealDirectory mirrors the confirmed /etc/asterisk/local
// contents on real hardware: a mix of sound files (node-id.gsm) and
// unrelated management scripts/docs (README, .sh, .example, .txt) that
// must NOT show up as sounds.
func TestListCustomMatchesRealDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"node-id.gsm", "README", "alaska_connect.example",
		"balance_node_stats.sh", "privatenodes.txt", "weather.ini",
	} {
		writeFile(t, dir, f)
	}
	s := New(dir, t.TempDir(), "sox")
	files, err := s.ListCustom()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "node-id" {
		t.Fatalf("got %+v, want exactly [node-id]", files)
	}
	// Ref must be a full path, not a bare name -- /etc/asterisk/local (or
	// wherever the custom dir is) isn't on Asterisk's own default sound
	// search path, so a bare reference silently fails to play at all.
	wantRef := filepath.Join(dir, "node-id")
	if files[0].Ref != wantRef || !files[0].Custom {
		t.Errorf("got %+v, want Ref=%q Custom=true", files[0], wantRef)
	}
}

// TestListStockMatchesRealDirectory mirrors a slice of the confirmed
// real /var/lib/asterisk/sounds/rpt contents: mixed .gsm/.ulaw/.pcm
// extensions all recognized, and Ref prefixed "rpt/" to match how
// rpt.conf actually references these (e.g. "rpt/callproceeding").
func TestListStockMatchesRealDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"callproceeding.gsm", "callterminated.gsm", "hipwr.ulaw",
		"functioncomplete.pcm", "remote_tx.pcm",
	} {
		writeFile(t, dir, f)
	}
	s := New(t.TempDir(), dir, "sox")
	files, err := s.ListStock()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("got %d files, want 5: %+v", len(files), files)
	}
	for _, f := range files {
		if f.Custom {
			t.Errorf("%s: Custom = true, want false (stock)", f.Name)
		}
		if f.Ref != "rpt/"+f.Name {
			t.Errorf("%s: Ref = %q, want %q", f.Name, f.Ref, "rpt/"+f.Name)
		}
	}
}

// TestListDedupesMultipleFormats covers a sound stored in more than one
// format (a real, common case -- app_rpt lets the codec negotiation pick
// whichever's available at playback). It must appear once, not twice.
func TestListDedupesMultipleFormats(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "node-id.gsm")
	writeFile(t, dir, "node-id.ulaw")
	writeFile(t, dir, "node-id.wav")
	s := New(dir, t.TempDir(), "sox")
	files, err := s.ListCustom()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1 (deduped): %+v", len(files), files)
	}
}

func TestListCustomMissingDirIsNotAnError(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir(), "sox")
	files, err := s.ListCustom()
	if err != nil {
		t.Fatalf("ListCustom() error = %v, want nil for a missing directory", err)
	}
	// Non-nil (an empty slice, not nil) even for a missing directory --
	// a nil slice would marshal to JSON null, and the cloud relay's
	// sounds.listAll action sends this straight to the browser.
	if files == nil {
		t.Error("got nil, want a non-nil empty slice")
	}
	if len(files) != 0 {
		t.Errorf("got %v, want empty", files)
	}
}

func TestListAllOrdersCustomBeforeStock(t *testing.T) {
	customDir, stockDir := t.TempDir(), t.TempDir()
	writeFile(t, customDir, "myid.gsm")
	writeFile(t, stockDir, "callproceeding.gsm")
	s := New(customDir, stockDir, "sox")
	files, err := s.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !files[0].Custom || files[1].Custom {
		t.Fatalf("got %+v, want [custom, stock]", files)
	}
}

func TestValidName(t *testing.T) {
	valid := []string{"node-id", "my_sound", "ID2", "a"}
	for _, n := range valid {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) = false, want true", n)
		}
	}
	invalid := []string{"", "../etc/passwd", "a/b", "a b", "a.gsm", strings.Repeat("x", 65)}
	for _, n := range invalid {
		if ValidName(n) {
			t.Errorf("ValidName(%q) = true, want false", n)
		}
	}
}

// fakeSox writes a shell script standing in for sox: it reads stdin
// contents from the argument list, and always exits with tail's code
// after writing tail's text to stderr -- mirroring the pattern already
// used to test the SA818 tool integration (internal/sa818/sa818_test.go),
// since the same principle applies: trust the real tool's exit code and
// output, verified against a controllable stand-in, not a guess about
// what a real sox invocation would do.
func fakeSox(t *testing.T, exitOK bool, message string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-sox")
	exit := "0"
	if !exitOK {
		exit = "1"
	}
	// Upload always invokes sox as: tool tmpPath -r 8000 -c 1 -t ul dest
	// -- $8 is that fixed destination path. On success, touch it, since
	// that's what a real conversion producing an output file looks like.
	script := "#!/bin/sh\necho '" + message + "' >&2\nif [ \"" + exit + "\" = \"0\" ]; then touch \"$8\"; fi\nexit " + exit + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake sox: %v", err)
	}
	return path
}

func TestUploadSuccess(t *testing.T) {
	customDir := t.TempDir()
	s := New(customDir, t.TempDir(), fakeSox(t, true, "sox: done"))
	output, err := s.Upload(context.Background(), "my-id", strings.NewReader("fake wav bytes"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if !strings.Contains(output, "done") {
		t.Errorf("output = %q, want it to include the tool's message", output)
	}
	if _, err := os.Stat(filepath.Join(customDir, "my-id.ulaw")); err != nil {
		t.Errorf("expected my-id.ulaw to exist: %v", err)
	}
	// The staged temp upload must be cleaned up, not left behind.
	entries, _ := os.ReadDir(customDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") {
			t.Errorf("temp upload file %s was not cleaned up", e.Name())
		}
	}
}

func TestUploadSoxFailureLeavesNoPartialFile(t *testing.T) {
	customDir := t.TempDir()
	s := New(customDir, t.TempDir(), fakeSox(t, false, "sox: unknown file type"))
	_, err := s.Upload(context.Background(), "my-id", strings.NewReader("garbage"))
	if err == nil {
		t.Fatal("Upload() error = nil, want an error when sox exits non-zero")
	}
	if !strings.Contains(err.Error(), "fake-sox") {
		t.Errorf("error = %v, want it to name the tool", err)
	}
	if _, statErr := os.Stat(filepath.Join(customDir, "my-id.ulaw")); !os.IsNotExist(statErr) {
		t.Error("expected no output file after a failed conversion")
	}
}

func TestUploadRejectsInvalidName(t *testing.T) {
	s := New(t.TempDir(), t.TempDir(), fakeSox(t, true, "ok"))
	if _, err := s.Upload(context.Background(), "../etc/passwd", strings.NewReader("x")); err == nil {
		t.Fatal("Upload() error = nil, want rejection of a path-traversal name")
	}
}

func TestDeleteCustomRemovesAllFormats(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "myid.gsm")
	writeFile(t, dir, "myid.ulaw")
	s := New(dir, t.TempDir(), "sox")
	if err := s.DeleteCustom("myid"); err != nil {
		t.Fatal(err)
	}
	files, _ := s.ListCustom()
	if len(files) != 0 {
		t.Errorf("got %+v, want empty after delete", files)
	}
}

func TestDeleteCustomMissingIsError(t *testing.T) {
	s := New(t.TempDir(), t.TempDir(), "sox")
	if err := s.DeleteCustom("nope"); err == nil {
		t.Error("DeleteCustom() error = nil, want an error for a nonexistent sound")
	}
}

func TestSoxInputArgs(t *testing.T) {
	if _, ok := soxInputArgs(".wav"); !ok {
		t.Error(".wav should be supported")
	}
	args, ok := soxInputArgs(".ulaw")
	if !ok || len(args) == 0 {
		t.Errorf(".ulaw should be supported with raw-format args, got %v, %v", args, ok)
	}
	if _, ok := soxInputArgs(".g722"); ok {
		t.Error(".g722 should not be offered for preview (unverified on-disk framing)")
	}
}

// fakeSoxStreaming writes a shell script standing in for sox's Preview
// invocation, which streams to stdout rather than touching a fixed
// destination path (unlike Upload's fakeSox above) — always writes
// wavData to stdout and message to stderr, exiting with exitOK's code.
func fakeSoxStreaming(t *testing.T, exitOK bool, wavData, message string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-sox-stream")
	exit := "0"
	if !exitOK {
		exit = "1"
	}
	script := "#!/bin/sh\necho '" + message + "' >&2\nif [ \"" + exit + "\" = \"0\" ]; then printf '" + wavData + "'; fi\nexit " + exit + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake sox: %v", err)
	}
	return path
}

func TestPreviewSuccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "myid.ulaw")
	s := New(dir, t.TempDir(), fakeSoxStreaming(t, true, "fake wav bytes", "sox: ok"))
	wav, err := s.Preview(context.Background(), "myid")
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if string(wav) != "fake wav bytes" {
		t.Errorf("wav = %q", wav)
	}
}

func TestPreviewUnknownName(t *testing.T) {
	s := New(t.TempDir(), t.TempDir(), "sox")
	if _, err := s.Preview(context.Background(), "nope"); err == nil {
		t.Fatal("Preview() error = nil, want an error for a nonexistent sound")
	}
}

func TestPreviewInvalidName(t *testing.T) {
	s := New(t.TempDir(), t.TempDir(), "sox")
	if _, err := s.Preview(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("Preview() error = nil, want rejection of a path-traversal name")
	}
}

func TestPreviewUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "myid.g722")
	s := New(dir, t.TempDir(), "sox")
	if _, err := s.Preview(context.Background(), "myid"); err == nil {
		t.Fatal("Preview() error = nil, want rejection of an unsupported format")
	}
}

func TestPreviewSoxFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "myid.ulaw")
	s := New(dir, t.TempDir(), fakeSoxStreaming(t, false, "", "sox: decode error"))
	_, err := s.Preview(context.Background(), "myid")
	if err == nil {
		t.Fatal("Preview() error = nil, want an error when sox exits non-zero")
	}
	if !strings.Contains(err.Error(), "fake-sox-stream") {
		t.Errorf("error = %v, want it to name the tool", err)
	}
}
