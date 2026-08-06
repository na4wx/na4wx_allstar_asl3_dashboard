package skywarnplus

import (
	_ "embed"
	"regexp"
	"sort"
	"strings"
)

// countyCodesMD is SkywarnPlus's own CountyCodes.md, copied verbatim
// from its v0.8.1 release — a state-by-state Markdown table mapping
// county name to NWS county code. Bundled so the "Weather Alerts" card's
// county picker can be a searchable list without a network round trip;
// re-copy this file from a newer SkywarnPlus release if NWS ever adds
// new codes.
//
//go:embed countycodes.md
var countyCodesMD string

// CountyOption is one selectable county in the picker.
type CountyOption struct {
	// Label is what's shown in the picker, e.g. "Autauga, AL (ALC001)".
	Label string `json:"label"`
	// Code is the NWS county code submitted back to SetCounties, e.g.
	// "ALC001".
	Code string `json:"code"`
}

// stateHeaderRe matches CountyCodes.md's "## XX" state section headers —
// exactly two uppercase letters, which is what distinguishes a real
// state/territory heading from the file's own leading "## WARNING"
// section (a caution note, not a state).
var stateHeaderRe = regexp.MustCompile(`^## ([A-Z]{2})$`)

// tableRowRe matches one Markdown table row: "| County | Code |".
var tableRowRe = regexp.MustCompile(`^\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|$`)

// countyOptions is parsed once at package init from the embedded file —
// it never changes at runtime, so there's no reason to reparse per
// request.
var countyOptions = parseCountyCodes(countyCodesMD)

// ListCounties returns every county SkywarnPlus knows a code for, sorted
// by label.
func ListCounties() []CountyOption {
	return countyOptions
}

// parseCountyCodes turns CountyCodes.md's Markdown tables into a flat,
// sorted list. Header/separator rows ("| County | Code |",
// "|--------|------|") are skipped by checking for the literal "County"
// header cell and a code cell of all dashes, rather than assuming line
// position, so the parser doesn't depend on exact formatting details
// that could shift between file versions.
func parseCountyCodes(md string) []CountyOption {
	// Non-nil even with zero matches -- this is always non-empty in
	// practice (bundled reference data), but ListCounties()'s result
	// reaches the cloud relay's skywarn.listCounties action as JSON, so
	// keep the same guarantee as every other list-returning action.
	out := []CountyOption{}
	state := ""
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := stateHeaderRe.FindStringSubmatch(line); m != nil {
			state = m[1]
			continue
		}
		m := tableRowRe.FindStringSubmatch(line)
		if m == nil || state == "" {
			continue
		}
		county, code := m[1], m[2]
		if county == "County" || code == "Code" {
			continue // header row
		}
		if strings.HasPrefix(code, "-") {
			continue // separator row ("|--------|------|")
		}
		out = append(out, CountyOption{
			Label: county + ", " + state + " (" + code + ")",
			Code:  code,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
