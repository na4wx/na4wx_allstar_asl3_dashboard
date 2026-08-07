// Package automation implements the rpt.conf-native connect/disconnect
// scheduler shared by internal/server's own "Automation" tab and
// internal/cloudagent's relayed schedule.* actions. Extracted from
// internal/server/automation.go (see that file's git history) so both
// callers share one implementation instead of the cloud agent
// reimplementing this algorithm — mirrors how internal/rptstatus was
// extracted earlier in this project for the same reason.
//
// One "connect/disconnect rule" is not a single config write: saving
// one touches the functions table (ensuring a digit maps to the right
// ilink command), the macro table (a dedicated DTMF-sequence entry per
// selected weekday), and the schedule table (one cron-like entry per
// macro) together. Every exported function here operates on a
// *config.Store the caller already loaded a node from — this package
// never loads a node itself, so both the local HTTP handlers and the
// cloudagent relay actions can resolve section names (Functions/Macro/
// Scheduler, falling back to bare defaults) their own way before calling in.
package automation

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"hamvoipconfiggui-asl3/internal/config"
)

// Action is one of the four connect/disconnect actions the "Automation"
// tab (or its cloud equivalent) can schedule — deliberately scoped to
// exactly the ilink commands ApplyStandardCommandSet already provisions
// (see config/standard_commands.go: digit "1"->ilink,1, "2"->ilink,2,
// "3"->ilink,3, "76"->ilink,6 — note digit does NOT equal the ilink
// number in every case), matching this app's own existing "Connect
// (stay connected)/(listen only)/Disconnect" quick actions rather than
// inventing new ilink modes.
type Action struct {
	Key         string // form value / stored marker
	Command     string // app_rpt command this action's functions-table digit must map to
	NeedsTarget bool   // whether a target node number is dialed after the digit
	Label       string // friendly name for the UI
}

// Actions is the fixed table of schedulable connect/disconnect actions.
var Actions = []Action{
	{Key: "connect_stay", Command: "ilink,3", NeedsTarget: true, Label: "Connect (stay connected)"},
	{Key: "connect_listen", Command: "ilink,2", NeedsTarget: true, Label: "Connect (listen only)"},
	{Key: "disconnect_one", Command: "ilink,1", NeedsTarget: true, Label: "Disconnect a specific node"},
	{Key: "disconnect_all", Command: "ilink,6", NeedsTarget: false, Label: "Disconnect all"},
}

// ActionByKey looks up an Action by its Key.
func ActionByKey(key string) (Action, bool) {
	for _, a := range Actions {
		if a.Key == key {
			return a, true
		}
	}
	return Action{}, false
}

// ActionByCommand looks up an Action by its Command.
func ActionByCommand(command string) (Action, bool) {
	for _, a := range Actions {
		if a.Command == command {
			return a, true
		}
	}
	return Action{}, false
}

// EnsureFunctionDigit finds the digit in section whose Command already
// equals command, creating one only if genuinely missing. In the common
// case (ApplyStandardCommandSet has already run) this is a read-only
// lookup — automation entries reuse whatever digit the node's own
// functions table already assigns a command to, rather than assuming a
// fixed digit.
func EnsureFunctionDigit(store *config.Store, section, command string) (string, error) {
	entries, err := store.ListFunctionMacros(section)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Command == command {
			return e.Digits, nil
		}
	}
	digit := AllocateDigit(entries)
	if err := store.SetFunctionMacro(section, digit, command); err != nil {
		return "", err
	}
	return digit, nil
}

// AllocateDigit picks an unused 3-digit function key starting at 900,
// so an automation-created entry can never collide with a digit an
// operator picked by hand (this app's Command list lets them type
// anything).
func AllocateDigit(entries []config.FunctionMacro) string {
	used := make(map[string]bool, len(entries))
	for _, e := range entries {
		used[e.Digits] = true
	}
	for n := 900; ; n++ {
		d := strconv.Itoa(n)
		if !used[d] {
			return d
		}
	}
}

// AllocateMacroNumber picks the smallest unused macro number >= 1 —
// never 0, which app_rpt reserves as "startupmacro". Scans every entry
// in the node's macro section, hand-authored ones included: the
// scheduler necessarily shares that section/namespace with the
// existing "Saved macros" table, since app_rpt's scheduler references
// "the node's own macro stanza" directly and there's no way to redirect
// it elsewhere.
func AllocateMacroNumber(entries []config.FunctionMacro) string {
	used := make(map[int]bool, len(entries))
	for _, e := range entries {
		if n, err := strconv.Atoi(e.Digits); err == nil {
			used[n] = true
		}
	}
	for n := 1; ; n++ {
		if !used[n] {
			return strconv.Itoa(n)
		}
	}
}

// BuildDTMF builds the DTMF sequence a macro entry "dials" to invoke
// digit's command — a trailing target node number for actions that
// take one, nothing for disconnect-all.
func BuildDTMF(digit, target string, needsTarget bool) string {
	if needsTarget {
		return "*" + digit + target
	}
	return "*" + digit
}

// ParseMacro reverse-parses a macro's raw DTMF value (e.g. "*32000")
// into a friendly automation description, resolving purely through the
// node's real functions-table entries — never assuming a digit equals
// a particular ilink number. Candidate digits are matched longest-first,
// so a real multi-digit prefix (e.g. "76") is never mistaken for a
// shorter one ("7") followed by leftover target digits. Anything that
// doesn't resolve to one of Actions' known commands renders as
// unrecognized rather than erroring — it may be a macro the operator
// wrote by hand for something else entirely.
func ParseMacro(dtmf string, functionsEntries []config.FunctionMacro) (label string, recognized bool) {
	dtmf = strings.TrimSpace(dtmf)
	if !strings.HasPrefix(dtmf, "*") {
		return "", false
	}
	rest := dtmf[1:]

	candidates := make([]config.FunctionMacro, len(functionsEntries))
	copy(candidates, functionsEntries)
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i].Digits) > len(candidates[j].Digits)
	})

	for _, fe := range candidates {
		if fe.Digits == "" || !strings.HasPrefix(rest, fe.Digits) {
			continue
		}
		action, ok := ActionByCommand(fe.Command)
		if !ok {
			continue
		}
		target := rest[len(fe.Digits):]
		switch {
		case action.NeedsTarget && target != "":
			return action.Label + " " + target, true
		case !action.NeedsTarget && target == "":
			return action.Label, true
		}
		// Digit matched but target-presence doesn't fit this action's
		// shape (e.g. disconnect-all with leftover digits) — keep
		// looking rather than returning a misleading label.
	}
	return "", false
}

// Row is one entry in a "Scheduled connections" table — joining a
// schedule entry with its macro's DTMF value by macro number.
type Row struct {
	MacroNum   string
	Label      string // friendly description, or the raw DTMF if unrecognized
	Recognized bool
	TimeSpec   string
}

// TimeFieldRe matches app_rpt's own schedule-field syntax: a single
// non-negative integer, or "*" — never a range, list, or step value
// (see config.ScheduleEntry's doc comment). Rejecting anything else
// means a malformed submission fails loudly instead of silently never
// scheduling.
var TimeFieldRe = regexp.MustCompile(`^\*$|^[0-9]+$`)
