// Package tts generates sound files from typed text using Piper — a
// free, fully offline neural text-to-speech engine, matching this app's
// whole approach of shelling out to real local tools (sox, 818-prog,
// asterisk itself) rather than depending on a cloud service or API key.
//
// Specifically this shells out to the standalone "piper" binary from
// rhasspy/piper's last release (https://github.com/rhasspy/piper,
// archived — development moved to OHF-Voice/piper1-gpl, but that
// project ships only as a pip package with a different, incompatible
// CLI and no 32-bit ARM build at all, a worse fit for a HamVoIP install
// that may be a Pi Zero/1/2). install.sh provisions this exact binary
// (see its own Piper section for the download details); this package
// just invokes it — "piper --model <path> --output_file <path>" with
// text on stdin, confirmed against that binary's own documented usage.
//
// Piper needs a downloaded voice model (a ".onnx" file, plus a
// same-named ".onnx.json" config it reads automatically) for each
// voice; this package only lists whichever models already exist in the
// configured voices directory and runs the piper binary against one —
// it never downloads voices itself (install.sh downloads one default
// voice; more are available at https://huggingface.co/rhasspy/piper-voices).
package tts

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Voice is one downloaded Piper voice model, or (for the espeak-ng
// fallback) one entry from `espeak-ng --voices`.
type Voice struct {
	// Name is how the voice is identified in the UI and submitted back in
	// a form: for Piper, the bare model name with no directory or
	// extension (e.g. "en_US-lessac-medium"); for espeak-ng, the
	// human-readable "VoiceName" column (e.g. "Mandarin").
	Name string
	// ModelPath is what's actually passed to the TTS tool to select this
	// voice: for Piper, the full path to the voice's ".onnx" file (passed
	// to --model); for espeak-ng, the "File" column from --voices (e.g.
	// "sit/cmn"), passed to -v — espeak-ng's -v flag takes that
	// identifier, not the display VoiceName, confirmed against a real
	// `espeak-ng --voices` (passing the VoiceName column, e.g. "Mandarin",
	// fails with "Error: The specified espeak-ng voice does not exist").
	// Never taken directly from user input — always resolved by looking
	// up a submitted Name against ListVoices/ListESpeakVoices, so a
	// request can't point the tool at an arbitrary voice string.
	ModelPath string
}

// ListVoices returns every Piper voice model found in dir, sorted by
// name. A missing directory (no voices downloaded yet) is not an error —
// it just means the "Create from text" UI has nothing to offer until the
// operator downloads one (e.g. `python3 -m piper.download_voices
// en_US-lessac-medium`).
func ListVoices(dir string) ([]Voice, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Voice
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".onnx") {
			continue
		}
		out = append(out, Voice{
			Name:      strings.TrimSuffix(e.Name(), ".onnx"),
			ModelPath: filepath.Join(dir, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// FindVoice looks up name (as returned by ListVoices) in dir, returning
// ok=false if there's no such voice — the only safe way a submitted voice
// name should ever be turned into a model path.
func FindVoice(dir, name string) (Voice, bool, error) {
	voices, err := ListVoices(dir)
	if err != nil {
		return Voice{}, false, err
	}
	for _, v := range voices {
		if v.Name == name {
			return v, true, nil
		}
	}
	return Voice{}, false, nil
}

// synthesizeTimeout bounds one piper run. Generous: even a long paragraph
// synthesizes in well under this on a Pi, and this also covers a slow SD
// card write.
const synthesizeTimeout = 30 * time.Second

// checkTimeout bounds a quick Piper smoke-test invocation.
const checkTimeout = 5 * time.Second

// CheckTool runs `tool <args...>` to confirm a TTS backend is actually
// runnable on this system (not just present on disk), returning the
// command's combined output either way for troubleshooting.
func CheckTool(ctx context.Context, tool string, args ...string) (output string, err error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	if len(args) == 0 {
		args = []string{"--help"}
	}
	cmd := exec.CommandContext(ctx, tool, args...)
	b, runErr := cmd.CombinedOutput()
	output = string(b)
	if runErr != nil {
		return output, fmt.Errorf("%s: %w", tool, runErr)
	}
	return output, nil
}

// ListESpeakVoices lists installed espeak-ng voice names sorted by name.
// It shells out to `espeak-ng --voices` and parses the documented
// tabular output.
func ListESpeakVoices(ctx context.Context, tool string) (voices []Voice, output string, err error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool, "--voices")
	b, runErr := cmd.CombinedOutput()
	output = string(b)
	if runErr != nil {
		return nil, output, fmt.Errorf("%s: %w", tool, runErr)
	}

	seen := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Pty") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		name, id := fields[3], fields[4]
		if name == "" || id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		voices = append(voices, Voice{Name: name, ModelPath: id})
	}
	sort.Slice(voices, func(i, j int) bool { return voices[i].Name < voices[j].Name })
	if len(voices) == 0 {
		return nil, output, fmt.Errorf("%s: no voices found", tool)
	}
	return voices, output, nil
}

// SynthesizeESpeak renders text using espeak-ng and returns WAV audio
// bytes from stdout. voiceID must be a Voice.ModelPath from
// ListESpeakVoices (the --voices table's "File" column), not a
// Voice.Name — passing the display name fails with "the specified
// espeak-ng voice does not exist".
func SynthesizeESpeak(ctx context.Context, tool, voiceID, text string) (wav []byte, output string, err error) {
	ctx, cancel := context.WithTimeout(ctx, synthesizeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool, "--stdout", "-v", voiceID, text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	wav, runErr := cmd.Output()
	output = stderr.String()
	if runErr != nil {
		return nil, output, fmt.Errorf("%s: %w", tool, runErr)
	}
	if len(wav) == 0 {
		return nil, output, fmt.Errorf("%s: no audio output", tool)
	}
	return wav, output, nil
}

// Synthesize renders text using the voice model at modelPath, via `<tool>
// --model <modelPath> --output_file <tmpfile>` with text piped to stdin
// (Piper's own documented CLI form), returning the generated WAV audio.
// piper's own exit code and combined output are the source of truth for
// whether synthesis actually worked, not an assumption — the same
// pattern this app already uses for sox and 818-prog. output is returned
// even on success, so a caller can show it for troubleshooting.
func Synthesize(ctx context.Context, tool, modelPath, text string) (wav []byte, output string, err error) {
	tmp, err := os.CreateTemp("", "piper-*.wav")
	if err != nil {
		return nil, "", fmt.Errorf("stage output file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	ctx, cancel := context.WithTimeout(ctx, synthesizeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool, "--model", modelPath, "--output_file", tmpPath)
	cmd.Stdin = strings.NewReader(text)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	output = out.String()
	if runErr != nil {
		return nil, output, fmt.Errorf("%s: %w", tool, runErr)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, output, fmt.Errorf("read synthesized audio: %w", err)
	}
	return data, output, nil
}
