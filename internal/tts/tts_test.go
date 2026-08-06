package tts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("fake model data"), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestListVoices(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "en_US-lessac-medium.onnx")
	writeFile(t, dir, "en_US-lessac-medium.onnx.json")
	writeFile(t, dir, "en_GB-alba-medium.onnx")
	writeFile(t, dir, "README.md")

	voices, err := ListVoices(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != 2 {
		t.Fatalf("got %+v, want 2 voices (README.md and the .onnx.json config must not be counted)", voices)
	}
	if voices[0].Name != "en_GB-alba-medium" || voices[1].Name != "en_US-lessac-medium" {
		t.Fatalf("got %+v, want sorted [en_GB-alba-medium, en_US-lessac-medium]", voices)
	}
	if voices[1].ModelPath != filepath.Join(dir, "en_US-lessac-medium.onnx") {
		t.Errorf("ModelPath = %q", voices[1].ModelPath)
	}
}

func TestListVoicesMissingDirIsNotAnError(t *testing.T) {
	voices, err := ListVoices(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("ListVoices() error = %v, want nil for a missing directory", err)
	}
	if voices != nil {
		t.Errorf("got %v, want nil", voices)
	}
}

func TestFindVoice(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "en_US-lessac-medium.onnx")

	v, ok, err := FindVoice(dir, "en_US-lessac-medium")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || v.ModelPath != filepath.Join(dir, "en_US-lessac-medium.onnx") {
		t.Fatalf("FindVoice = %+v, %v", v, ok)
	}

	_, ok, err = FindVoice(dir, "../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("FindVoice must not resolve a name that isn't a real listed voice, even a path-traversal-shaped one")
	}
}

// fakePiper writes a shell script standing in for piper: it always exits
// with exitOK's code after writing message to stderr, mirroring the
// pattern already used for sox (internal/sounds/sounds_test.go) and the
// SA818 tool (internal/sa818/sa818_test.go) — trust the real tool's exit
// code and output, verified against a controllable stand-in.
func fakePiper(t *testing.T, exitOK bool, message string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-piper")
	exit := "0"
	if !exitOK {
		exit = "1"
	}
	// Synthesize always invokes piper as: tool --model <path> --output_file <tmp>
	// -- $4 is that fixed output path.
	script := "#!/bin/sh\necho '" + message + "' >&2\nif [ \"" + exit + "\" = \"0\" ]; then printf 'fake wav bytes' > \"$4\"; fi\nexit " + exit + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake piper: %v", err)
	}
	return path
}

func TestSynthesizeSuccess(t *testing.T) {
	tool := fakePiper(t, true, "piper: done")
	wav, output, err := Synthesize(context.Background(), tool, "/voices/en_US-lessac-medium.onnx", "hello world")
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if string(wav) != "fake wav bytes" {
		t.Errorf("wav = %q", wav)
	}
	if !strings.Contains(output, "done") {
		t.Errorf("output = %q, want it to include the tool's message", output)
	}
}

func TestSynthesizeFailure(t *testing.T) {
	tool := fakePiper(t, false, "piper: model not found")
	_, output, err := Synthesize(context.Background(), tool, "/voices/missing.onnx", "hello")
	if err == nil {
		t.Fatal("Synthesize() error = nil, want an error when piper exits non-zero")
	}
	if !strings.Contains(err.Error(), "fake-piper") {
		t.Errorf("error = %v, want it to name the tool", err)
	}
	if !strings.Contains(output, "model not found") {
		t.Errorf("output = %q, want it to include the tool's message", output)
	}
}

func TestCheckToolSuccess(t *testing.T) {
	tool := fakePiper(t, true, "piper help")
	output, err := CheckTool(context.Background(), tool)
	if err != nil {
		t.Fatalf("CheckTool() error = %v", err)
	}
	if !strings.Contains(output, "piper help") {
		t.Errorf("output = %q, want it to include the tool's message", output)
	}
}

func TestCheckToolFailure(t *testing.T) {
	tool := fakePiper(t, false, "GLIBCXX_3.4.26 not found")
	output, err := CheckTool(context.Background(), tool)
	if err == nil {
		t.Fatal("CheckTool() error = nil, want an error when tool exits non-zero")
	}
	if !strings.Contains(err.Error(), "fake-piper") {
		t.Errorf("error = %v, want it to name the tool", err)
	}
	if !strings.Contains(output, "GLIBCXX_3.4.26 not found") {
		t.Errorf("output = %q, want it to include the tool's message", output)
	}
}

func fakeESpeak(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-espeak-ng")
	script := `#!/bin/sh
case "$1" in
  --version)
    echo "eSpeak NG text-to-speech: 1.52.0"
    exit 0
    ;;
  --voices)
    cat <<'EOF'
Pty Language       Age/Gender VoiceName          File                 Other Languages
 5  en-us          --/M      English_(America)  en/en-us
 5  en-gb          --/M      English_(Britain)  en/en-gb
 5  cmn            --/M      Mandarin           zh/cmn               (zh-cmn 5)(zh 5)
EOF
    exit 0
    ;;
  --stdout)
    if [ "$3" = "missing" ] || [ "$3" = "Mandarin" ]; then
      echo "Error: The specified espeak-ng voice does not exist." >&2
      exit 1
    fi
    printf 'RIFFfakewavbytes'
    exit 0
    ;;
esac
echo "unexpected args" >&2
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake espeak: %v", err)
	}
	return path
}

func TestListESpeakVoices(t *testing.T) {
	tool := fakeESpeak(t)
	voices, output, err := ListESpeakVoices(context.Background(), tool)
	if err != nil {
		t.Fatalf("ListESpeakVoices() error = %v", err)
	}
	if !strings.Contains(output, "VoiceName") {
		t.Errorf("output = %q, want espeak --voices table", output)
	}
	if len(voices) != 3 {
		t.Fatalf("got %d voices, want 3", len(voices))
	}
	want := []Voice{
		{Name: "English_(America)", ModelPath: "en/en-us"},
		{Name: "English_(Britain)", ModelPath: "en/en-gb"},
		{Name: "Mandarin", ModelPath: "zh/cmn"},
	}
	for i, w := range want {
		if voices[i] != w {
			t.Errorf("voices[%d] = %+v, want %+v", i, voices[i], w)
		}
	}
}

func TestSynthesizeESpeakSuccess(t *testing.T) {
	tool := fakeESpeak(t)
	wav, output, err := SynthesizeESpeak(context.Background(), tool, "en-us", "hello world")
	if err != nil {
		t.Fatalf("SynthesizeESpeak() error = %v", err)
	}
	if len(wav) == 0 {
		t.Fatal("wav is empty")
	}
	if output != "" {
		t.Errorf("output = %q, want empty stderr on success", output)
	}
}

func TestSynthesizeESpeakFailure(t *testing.T) {
	tool := fakeESpeak(t)
	_, output, err := SynthesizeESpeak(context.Background(), tool, "missing", "hello")
	if err == nil {
		t.Fatal("SynthesizeESpeak() error = nil, want non-zero tool error")
	}
	if !strings.Contains(output, "does not exist") {
		t.Errorf("output = %q, want tool stderr", output)
	}
}

// TestSynthesizeESpeakRejectsDisplayName guards against the regression
// this app actually hit: passing a Voice.Name (e.g. "Mandarin", the
// --voices table's display column) as -v fails, because espeak-ng only
// recognizes the "File" column (e.g. "zh/cmn") there. Callers must use
// the Voice.ModelPath returned by ListESpeakVoices, not Name.
func TestSynthesizeESpeakRejectsDisplayName(t *testing.T) {
	tool := fakeESpeak(t)
	if _, _, err := SynthesizeESpeak(context.Background(), tool, "Mandarin", "hello"); err == nil {
		t.Fatal("SynthesizeESpeak() with a display name error = nil, want an error")
	}
	if _, _, err := SynthesizeESpeak(context.Background(), tool, "zh/cmn", "hello"); err != nil {
		t.Fatalf("SynthesizeESpeak() with the real ModelPath error = %v, want success", err)
	}
}
