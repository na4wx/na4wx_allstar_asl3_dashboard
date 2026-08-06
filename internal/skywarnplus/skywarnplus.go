// Package skywarnplus configures an already-installed copy of
// SkywarnPlus (https://github.com/Mason10198/SkywarnPlus) — a
// third-party, no-longer-maintained tool that announces National
// Weather Service alerts over an AllStar node. Installation itself
// happens in install.sh (an interactive, opt-in step run once on the
// Pi, not from this running app) — see that script's own SkywarnPlus
// section for exactly what it sets up and why. This package only ever
// configures a copy that's already there.
//
// Every write goes through one of SkywarnPlus's own scripts rather than
// editing its config.yaml directly: boolean feature toggles (enable,
// sayalert, sayallclear, tailmessage, courtesytone, idchange,
// alertscript) go through its own SkyControl.py, and everything that
// script doesn't cover — the county-code list, the node-number list, and
// the Pushover/SkyDescribe sections' string/int fields — goes through
// deploy/sky_configure.py — a small companion script this app ships
// (install.sh copies it in alongside SkywarnPlus) that uses the exact
// same ruamel.yaml dependency SkywarnPlus itself already requires, so
// config.yaml's own extensive inline comments survive edits the same way
// they already do through SkyControl.py. This package only ever talks
// JSON to that companion script — it never parses YAML itself, and this
// app has no YAML dependency of its own.
//
// This app deliberately doesn't manage AlertScript's own Mappings list
// (letting the operator configure arbitrary shell/DTMF commands that run
// automatically on an external trigger is a meaningfully different, more
// advanced feature than everything else here) or SkywarnPlus's own
// courtesy-tone/ID swap during alerts (that would mean SkywarnPlus and
// this app independently rewriting the same file) — both stay manual,
// edited via SkywarnPlus's own config.yaml directly. Status does read
// (never write) whether SkywarnPlus's own swap is enabled, purely so the
// UI can warn before an operator edits a file SkywarnPlus might change
// out from under them. internal/wxtone is this app's own safer
// alternative: it reads SkywarnPlus's already-fetched active-alert count
// (see Status.ActiveAlertCount) and performs the same kind of courtesy-tone
// swap itself, fully tracked and visible in this app rather than an
// uncoordinated second process.
package skywarnplus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// commandTimeout bounds one shelled-out call. Generous: even a cold
// Python interpreter start on a Pi is well under this.
const commandTimeout = 15 * time.Second

// IsInstalled reports whether SkywarnPlus appears to be installed at
// dir — checked by looking for its own main script, not this package's
// companion script, since a partially-set-up directory (e.g. an
// interrupted install.sh run) should read as "not installed" rather than
// as installed-but-broken.
func IsInstalled(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SkywarnPlus.py"))
	return err == nil
}

// Status is SkywarnPlus's current configuration, as reported by
// sky_configure.py status.
// JSON tags exist only for internal/cloudagent's relayed skywarnplus.*
// actions.
type Status struct {
	Enable      bool              `json:"enable"`
	SayAlert    bool              `json:"sayAlert"`
	SayAllClear bool              `json:"sayAllClear"`
	Tailmessage bool              `json:"tailmessage"`
	AlertScript bool              `json:"alertScript"`
	CountyCodes []string          `json:"countyCodes"`
	Nodes       []string          `json:"nodes"`
	Pushover    PushoverStatus    `json:"pushover"`
	SkyDescribe SkyDescribeStatus `json:"skyDescribe"`

	// CourtesyToneSwapEnabled/IDSwapEnabled report whether SkywarnPlus's
	// own courtesy-tone/ID swap (CourtesyTones.Enable / IDChange.Enable
	// in its config.yaml) is turned on — this app never configures that
	// swap itself (see this package's doc comment for why), but surfaces
	// it read-only so the UI can warn before an operator edits a file
	// SkywarnPlus might change out from under them.
	CourtesyToneSwapEnabled bool `json:"courtesyToneSwapEnabled"`
	IDSwapEnabled           bool `json:"idSwapEnabled"`

	// ActiveAlertCount is how many weather alerts SkywarnPlus currently
	// has active for the operator's configured county codes, read
	// straight from its own already-fetched runtime state (see
	// GetStatus's doc comment) — not a second alert source this app
	// fetches itself.
	ActiveAlertCount int `json:"activeAlertCount"`
}

// PushoverStatus is SkywarnPlus's Pushover push-notification settings.
// Unlike the four boolean toggles above, Enable/Debug here aren't
// reachable through SkyControl.py (its VALID_KEYS is a fixed set that
// doesn't include Pushover), so every field goes through
// sky_configure.py's own set-pushover.
type PushoverStatus struct {
	Enable   bool   `json:"enable"`
	UserKey  string `json:"userKey"`
	APIToken string `json:"apiToken"`
	Debug    bool   `json:"debug"`
}

// SkyDescribeStatus is SkywarnPlus's SkyDescribe (VoiceRSS-based
// detailed-alert-description) settings. SkyDescribe has no on/off flag
// of its own — see SetSkyDescribe's doc comment — so there's no Enable
// field here, only its connection/voice settings.
type SkyDescribeStatus struct {
	APIKey   string `json:"apiKey"`
	Language string `json:"language"`
	Speed    int    `json:"speed"`
	Voice    string `json:"voice"`
	MaxWords int    `json:"maxWords"`
}

// runPython runs `python3 <dir>/<script> <args...>`, matching this
// app's established exec.CommandContext + context.WithTimeout +
// stderr-capture + wrapped-error pattern (see internal/system) — the
// tool's own exit code and stderr are the source of truth for whether
// it worked, not an assumption.
func runPython(ctx context.Context, dir, script string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	fullArgs := append([]string{filepath.Join(dir, script)}, args...)
	cmd := exec.CommandContext(ctx, "python3", fullArgs...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("python3 %s: %w: %s", script, err, stderr.String())
	}
	return out.String(), nil
}

// GetStatus reads SkywarnPlus's current configuration.
func GetStatus(ctx context.Context, dir string) (Status, error) {
	out, err := runPython(ctx, dir, "sky_configure.py", "status")
	if err != nil {
		return Status{}, err
	}
	var raw struct {
		Enable             bool     `json:"enable"`
		SayAlert           bool     `json:"sayalert"`
		SayAllClear        bool     `json:"sayallclear"`
		Tailmessage        bool     `json:"tailmessage"`
		AlertScript        bool     `json:"alertscript"`
		CourtesyToneEnable bool     `json:"courtesytone_enable"`
		IDChangeEnable     bool     `json:"idchange_enable"`
		ActiveAlertCount   int      `json:"active_alert_count"`
		CountyCodes        []string `json:"countycodes"`
		Nodes              []string `json:"nodes"`
		Pushover           struct {
			Enable   bool   `json:"enable"`
			UserKey  string `json:"userkey"`
			APIToken string `json:"apitoken"`
			Debug    bool   `json:"debug"`
		} `json:"pushover"`
		SkyDescribe struct {
			APIKey   string `json:"apikey"`
			Language string `json:"language"`
			Speed    int    `json:"speed"`
			Voice    string `json:"voice"`
			MaxWords int    `json:"maxwords"`
		} `json:"skydescribe"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return Status{}, fmt.Errorf("parse sky_configure.py status output: %w", err)
	}
	// A station with none configured gets back JSON null (or an
	// omitted key) for these, which json.Unmarshal leaves as a nil Go
	// slice -- re-marshaled as null again by the cloud relay's
	// skywarn.getStatus action, which the browser expects to always be
	// able to call .length/.map on.
	if raw.CountyCodes == nil {
		raw.CountyCodes = []string{}
	}
	if raw.Nodes == nil {
		raw.Nodes = []string{}
	}
	return Status{
		Enable:                  raw.Enable,
		SayAlert:                raw.SayAlert,
		SayAllClear:             raw.SayAllClear,
		Tailmessage:             raw.Tailmessage,
		AlertScript:             raw.AlertScript,
		CourtesyToneSwapEnabled: raw.CourtesyToneEnable,
		IDSwapEnabled:           raw.IDChangeEnable,
		ActiveAlertCount:        raw.ActiveAlertCount,
		CountyCodes:             raw.CountyCodes,
		Nodes:                   raw.Nodes,
		Pushover: PushoverStatus{
			Enable:   raw.Pushover.Enable,
			UserKey:  raw.Pushover.UserKey,
			APIToken: raw.Pushover.APIToken,
			Debug:    raw.Pushover.Debug,
		},
		SkyDescribe: SkyDescribeStatus{
			APIKey:   raw.SkyDescribe.APIKey,
			Language: raw.SkyDescribe.Language,
			Speed:    raw.SkyDescribe.Speed,
			Voice:    raw.SkyDescribe.Voice,
			MaxWords: raw.SkyDescribe.MaxWords,
		},
	}, nil
}

// SetCounties replaces the full county-code list.
func SetCounties(ctx context.Context, dir string, codes []string) (string, error) {
	return runPython(ctx, dir, "sky_configure.py", "set-counties", strings.Join(codes, ","))
}

// AddNode registers node with SkywarnPlus (a no-op if it's already
// registered — sky_configure.py's add-node is idempotent).
func AddNode(ctx context.Context, dir, node string) (string, error) {
	return runPython(ctx, dir, "sky_configure.py", "add-node", node)
}

// boolArg renders a Go bool the way sky_configure.py's set-pushover
// parses it ("true"/"false").
func boolArg(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// SetPushover replaces SkywarnPlus's whole Pushover section — its
// fields (Enable, UserKey, APIToken, Debug) aren't reachable through
// SkyControl.py, whose VALID_KEYS is a fixed set of section.key boolean
// pairs that doesn't include Pushover, so this goes through
// sky_configure.py like the county/node lists do. Confirmed against the
// real SkywarnPlus.py that Pushover is otherwise fully self-contained:
// its own main loop reads config["Pushover"] directly every run, so
// setting these fields is the only wiring needed — no AlertScript
// mapping or DTMF command required, unlike SkyDescribe below.
func SetPushover(ctx context.Context, dir string, enable bool, userKey, apiToken string, debug bool) (string, error) {
	return runPython(ctx, dir, "sky_configure.py", "set-pushover", boolArg(enable), userKey, apiToken, boolArg(debug))
}

// SetSkyDescribe replaces SkywarnPlus's whole SkyDescribe section
// (APIKey, Language, Speed, Voice, MaxWords) — the VoiceRSS-based
// detailed-alert-description feature. Confirmed against the real
// SkyDescribe.py and SkywarnPlus.py that, unlike Pushover, SkyDescribe
// has no on/off flag and is never invoked by SkywarnPlus's own run
// loop: it's a standalone script meant to be triggered either by an
// AlertScript Mapping (out of scope — see this package's doc) or a DTMF
// command, which this app already supports generically via the Live &
// Commands tab's command-list editor. This function only configures
// SkyDescribe's own settings; wiring up how it gets triggered is a
// separate, already-possible step.
func SetSkyDescribe(ctx context.Context, dir string, apiKey, language string, speed int, voice string, maxWords int) (string, error) {
	return runPython(ctx, dir, "sky_configure.py", "set-skydescribe", apiKey, language, strconv.Itoa(speed), voice, strconv.Itoa(maxWords))
}

// validToggleKeys mirrors SkyControl.py's own VALID_KEYS exactly (minus
// changect/changeid, which take "wx"/"normal" rather than a boolean and
// aren't exposed by this app) — rejecting anything else here means a
// typo can't silently become a confusing error from the Python side.
var validToggleKeys = map[string]bool{
	"enable":       true,
	"sayalert":     true,
	"sayallclear":  true,
	"tailmessage":  true,
	"courtesytone": true,
	"idchange":     true,
	"alertscript":  true,
}

// SetToggle flips one of SkywarnPlus's own boolean features by shelling
// out to its own SkyControl.py directly, reusing its already-safe
// ruamel.yaml editing rather than reimplementing it.
func SetToggle(ctx context.Context, dir, key string, value bool) (string, error) {
	if !validToggleKeys[key] {
		return "", fmt.Errorf("skywarnplus: unrecognized toggle %q", key)
	}
	v := "false"
	if value {
		v = "true"
	}
	return runPython(ctx, dir, "SkyControl.py", key, v)
}
