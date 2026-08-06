// Package sounds manages the audio files an app_rpt node can reference
// for station ID and telemetry playback (e.g. rpt.conf's "idrecording",
// or a telemetry entry like "patchup=rpt/callproceeding").
//
// Two directories are involved, and they're treated very differently:
//
//   - The "custom" directory (on a stock HamVoIP install,
//     /etc/asterisk/local/) is where an operator's own recordings live —
//     confirmed on real hardware to already hold a working
//     "node-id.gsm", the station ID file that install's rpt.conf
//     idrecording most likely points at. This app can list, upload to,
//     and reference files here.
//   - The "stock" directory (/var/lib/asterisk/sounds/rpt/) is
//     app_rpt's own prompt library — confirmed on real hardware to hold
//     ~90 files in a mix of formats. This app only ever reads this
//     directory to offer existing prompts (like "rpt/callproceeding")
//     as pick-list options; it never writes to it. That library isn't
//     this app's to own, and rewriting one of its files could break
//     stock behavior in ways this app has no way to verify.
package sounds

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// soundExtensions lists the file extensions this package recognizes as
// playable audio, based on what's actually present in a real HamVoIP
// sounds directory (a mix of .gsm/.ulaw/.pcm was confirmed there).
// Extensions not in this list are ignored when listing, so e.g. the
// custom directory's own README/example/script files never show up as
// if they were sounds.
var soundExtensions = map[string]bool{
	".gsm":  true,
	".ulaw": true,
	".ul":   true,
	".alaw": true,
	".al":   true,
	".wav":  true,
	".pcm":  true,
	".sln":  true,
	".g722": true,
}

// File is one playable sound this app knows about. JSON tags exist only
// for internal/cloudagent's relayed sounds.* actions.
type File struct {
	// Name is the bare reference name with no directory prefix or
	// extension, e.g. "node-id" or "callproceeding" — how the file is
	// identified in the UI.
	Name string `json:"name"`
	// Ref is exactly what app_rpt/rpt.conf wants written into a field
	// like idrecording or a telemetry value to play this file (always
	// without an extension — Asterisk appends whichever format it
	// actually wants at playback time). A stock file uses a bare
	// "rpt/callproceeding"-style reference because /var/lib/asterisk/
	// sounds/rpt is on Asterisk's own default sound search path — but a
	// custom file is not on that path at all (it lives in a
	// HamVoIP-specific directory, e.g. /etc/asterisk/local), and
	// AllStarLink's own docs are explicit that anything outside the
	// default tree needs a full absolute path or Asterisk will silently
	// fail to find it (confirmed the hard way: a bare custom-file
	// reference saved as idrecording, or handed to "rpt localplay",
	// simply produces no audio and no error — the file is never found in
	// the first place). So Ref is an absolute path
	// (/etc/asterisk/local/node-id) for a custom file, but stays a bare
	// rpt/-prefixed name for a stock one.
	Ref string `json:"ref"`
	// Custom is true for a file in the custom (uploadable/manageable)
	// directory, false for a stock library file (read-only reference).
	Custom bool `json:"custom"`
}

// Store manages the custom sound directory and lists the stock one.
type Store struct {
	customDir string
	stockDir  string
	soxTool   string
}

// New builds a Store. customDir and stockDir are the two directories
// described in the package doc comment. soxTool is the sox binary path,
// or a bare name ("sox") to resolve via PATH — matching how this app
// already shells out to other external tools it doesn't reimplement
// (818-prog, asterisk itself).
func New(customDir, stockDir, soxTool string) *Store {
	return &Store{customDir: customDir, stockDir: stockDir, soxTool: soxTool}
}

// listDir scans dir for recognized sound files, deduplicating by base
// name — a real sound is often stored in more than one format (e.g. both
// callproceeding.gsm and a .ulaw copy), and app_rpt's own reference to
// it never includes an extension, so from this app's perspective those
// are one sound, not two. refFor builds each entry's Ref from its bare
// name — a plain prefix isn't enough since a custom file's Ref needs a
// full path built from dir itself, not a fixed string (see File.Ref's
// doc comment).
func listDir(dir string, custom bool, refFor func(name string) string) ([]File, error) {
	// Non-nil even with zero results (missing dir, or nothing
	// recognized in it) -- ListAll's result reaches the cloud relay's
	// sounds.listAll action, sent straight to the browser as JSON,
	// which expects to always call .length/.map on it.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []File{}, nil
		}
		return nil, err
	}
	seen := make(map[string]bool)
	out := []File{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !soundExtensions[ext] {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, File{Name: name, Ref: refFor(name), Custom: custom})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ListCustom lists the operator's own uploaded/managed sounds. Ref is a
// full absolute path (see File.Ref's doc comment for why a bare name
// doesn't work here).
func (s *Store) ListCustom() ([]File, error) {
	return listDir(s.customDir, true, func(name string) string { return filepath.Join(s.customDir, name) })
}

// ListStock lists app_rpt's own prompt library, read-only. Ref is
// prefixed "rpt/" to match how those prompts are actually referenced
// elsewhere in rpt.conf (e.g. "patchup=rpt/callproceeding") — this
// assumes the stock directory is app_rpt's own "rpt" sound subdirectory,
// on Asterisk's own default sound search path, true on every HamVoIP
// install this app has been run against.
func (s *Store) ListStock() ([]File, error) {
	return listDir(s.stockDir, false, func(name string) string { return "rpt/" + name })
}

// ListAll returns custom sounds first (what an operator is most likely
// to want, and what they can also delete/replace) followed by the stock
// library, for a combined picker.
func (s *Store) ListAll() ([]File, error) {
	custom, err := s.ListCustom()
	if err != nil {
		return nil, err
	}
	stock, err := s.ListStock()
	if err != nil {
		return nil, err
	}
	return append(custom, stock...), nil
}

// validNameRe restricts an uploaded sound's name to characters safe to
// use directly as a filename — no path separators or traversal, nothing
// a shell or filesystem could reinterpret.
var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidName reports whether name is safe to use as a custom sound's
// filename.
func ValidName(name string) bool {
	return name != "" && len(name) <= 64 && validNameRe.MatchString(name)
}

// uploadTimeout bounds the sox conversion. Generous: sox transcoding a
// short voice recording is fast, but this also covers however long it
// takes the Pi to read a large upload off a slow SD card.
const uploadTimeout = 30 * time.Second

// Upload saves src (an uploaded audio file in whatever format the
// operator recorded it in — typically WAV) as name in the custom
// directory, transcoding it with sox to 8kHz mono mu-law (".ulaw"),
// which every sox build supports natively with no optional codec
// library, unlike GSM. That matches an existing convention already
// present in a real HamVoIP sounds directory (plenty of its own stock
// prompts are .ulaw), so this isn't introducing a new format, just
// picking the one this app can most reliably produce.
//
// sox's own exit code and combined output are the source of truth for
// whether the conversion actually worked, not an assumption — the same
// pattern this app already uses for the external 818-prog tool. output
// is returned even on success, so a caller can show it for
// troubleshooting.
func (s *Store) Upload(ctx context.Context, name string, src io.Reader) (output string, err error) {
	if !ValidName(name) {
		return "", fmt.Errorf("sound name must be letters, numbers, - or _ only")
	}
	if err := os.MkdirAll(s.customDir, 0o755); err != nil {
		return "", fmt.Errorf("create sounds directory: %w", err)
	}

	tmp, err := os.CreateTemp(s.customDir, ".upload-*")
	if err != nil {
		return "", fmt.Errorf("stage upload: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once sox has consumed it

	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return "", fmt.Errorf("save upload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("save upload: %w", err)
	}

	dest := filepath.Join(s.customDir, name+".ulaw")
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()

	// -t ul (raw mu-law) is specified explicitly on the output rather
	// than relying on sox to infer a format from the ".ulaw" extension —
	// unlike ".wav", that extension isn't one of sox's own recognized
	// format names, and this app would rather be explicit than depend on
	// guessing right.
	cmd := exec.CommandContext(ctx, s.soxTool, tmpPath, "-r", "8000", "-c", "1", "-t", "ul", dest)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	output = out.String()
	if runErr != nil {
		os.Remove(dest) // sox may have written a partial file before failing
		return output, fmt.Errorf("%s: %w", s.soxTool, runErr)
	}
	return output, nil
}

// DeleteCustom removes one of the operator's own sound files (all
// extensions sharing that base name, in case more than one format was
// ever produced for it) from the custom directory. Never touches the
// stock library — there is no delete path for it at all.
func (s *Store) DeleteCustom(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid sound name")
	}
	entries, err := os.ReadDir(s.customDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var lastErr error
	removed := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if base != name {
			continue
		}
		if err := os.Remove(filepath.Join(s.customDir, e.Name())); err != nil {
			lastErr = err
		} else {
			removed = true
		}
	}
	if lastErr != nil {
		return lastErr
	}
	if !removed {
		return fmt.Errorf("sound %q not found", name)
	}
	return nil
}

// soxInputArgs returns the sox arguments (placed before the input path)
// needed to correctly read a sound file of the given extension. Raw/
// headerless formats (everything except .wav) have no way to describe
// their own sample rate or encoding, so sox can't sniff them the way it
// can a WAV file — these values match Asterisk's own fixed convention
// for telephony audio (8kHz mono), the same assumption Upload's own
// output side already relies on. ok is false for a format this app
// doesn't know how to safely read for preview (currently just .g722,
// whose on-disk framing this app hasn't had a real sample to verify
// against — rather than guess and risk a garbled preview, previewing it
// is simply not offered).
func soxInputArgs(ext string) (args []string, ok bool) {
	switch ext {
	case ".wav":
		return nil, true // self-describing, no flags needed
	case ".gsm":
		return nil, true // GSM 06.10 is inherently 8kHz mono; sox infers this from -t gsm alone
	case ".ulaw", ".ul":
		return []string{"-r", "8000", "-c", "1", "-t", "ul"}, true
	case ".alaw", ".al":
		return []string{"-r", "8000", "-c", "1", "-t", "al"}, true
	case ".pcm", ".sln":
		return []string{"-r", "8000", "-c", "1", "-t", "sw"}, true // raw 16-bit signed PCM
	default:
		return nil, false
	}
}

// resolveInDir finds name's actual on-disk file (whichever recognized
// extension it's actually stored as) within dir.
func resolveInDir(dir, name string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("sound %q not found", name)
		}
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !soundExtensions[ext] {
			continue
		}
		if strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())) != name {
			continue
		}
		return filepath.Join(dir, e.Name()), nil
	}
	return "", fmt.Errorf("sound %q not found", name)
}

// ResolveCustomPath finds name's actual on-disk path (with whichever
// extension it's really stored as) in the custom directory — e.g. for
// wxtone's file-swap, which needs a real path to copy bytes from/to, not
// just the extension-less Ref app_rpt itself uses.
func (s *Store) ResolveCustomPath(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid sound name")
	}
	return resolveInDir(s.customDir, name)
}

// ResolveStockPath finds name's actual on-disk path in the read-only
// stock library — for reading only (e.g. as a wxtone copy source); there
// is no write path for the stock directory anywhere in this app.
// Doesn't apply ValidName's custom-upload character restriction: a match
// only ever comes from resolveInDir's own directory scan (never from
// building a path directly out of name), so an unusual stock filename
// can't cause a traversal regardless of its characters.
func (s *Store) ResolveStockPath(name string) (string, error) {
	return resolveInDir(s.stockDir, name)
}

// previewInfo resolves name to its actual file in the custom directory
// and the sox input args needed to read it. Never touches the stock
// library — a preview is only ever offered for the operator's own
// uploaded/generated sounds.
func (s *Store) previewInfo(name string) (path string, soxArgs []string, err error) {
	path, err = s.ResolveCustomPath(name)
	if err != nil {
		return "", nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	args, ok := soxInputArgs(ext)
	if !ok {
		return "", nil, fmt.Errorf("previewing %s files isn't supported", ext)
	}
	return path, args, nil
}

// Preview transcodes name (one of the operator's own custom sounds) to
// browser-playable WAV audio on the fly, so it can be heard in-browser
// without storing an extra converted copy alongside the original. Same
// "trust the tool's own exit code" discipline as Upload.
func (s *Store) Preview(ctx context.Context, name string) ([]byte, error) {
	path, args, err := s.previewInfo(name)
	if err != nil {
		return nil, err
	}
	args = append(args, path, "-t", "wav", "-")

	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.soxTool, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", s.soxTool, err, stderr.String())
	}
	return out.Bytes(), nil
}
