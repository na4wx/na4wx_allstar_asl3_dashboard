package skywarnplus

import (
	"strings"
	"testing"
)

// TestListCountiesRealFile guards against a mis-copied or reformatted
// countycodes.md silently losing rows (e.g. if a future SkywarnPlus
// release changes the table's exact spacing) — checked against real,
// known-good county codes, not just a count.
func TestListCountiesRealFile(t *testing.T) {
	opts := ListCounties()
	// The real US county/equivalent count is ~3142; also excludes the
	// two header rows per state ("| County | Code |" and the
	// "|---|---|" separator) and the leading "## WARNING" section.
	if len(opts) < 3000 || len(opts) > 3300 {
		t.Fatalf("got %d counties, want roughly 3142 (real US county count)", len(opts))
	}

	byCode := make(map[string]CountyOption, len(opts))
	for _, o := range opts {
		byCode[o.Code] = o
	}
	for _, want := range []struct{ code, county, state string }{
		{"ALC001", "Autauga", "AL"},
		{"ARC125", "Saline", "AR"},
		{"WYC045", "Weston", "WY"},
	} {
		got, ok := byCode[want.code]
		if !ok {
			t.Fatalf("county code %s not found", want.code)
		}
		if got.Label != want.county+", "+want.state+" ("+want.code+")" {
			t.Errorf("%s label = %q, want county %q state %q", want.code, got.Label, want.county, want.state)
		}
	}

	// Header/separator/warning-section rows must never leak in as if
	// they were real counties.
	for _, o := range opts {
		if o.Code == "Code" || o.Code == "" || strings.HasPrefix(o.Code, "-") {
			t.Errorf("header/separator row leaked into results: %+v", o)
		}
	}
}

func TestParseCountyCodesSkipsWarningSection(t *testing.T) {
	sample := `# County Codes

## WARNING
This file is integral to the functioning of SkywarnPlus.
Do not modify.

## AL

| County | Code |
|--------|------|
| Autauga     | ALC001   |
| Baldwin     | ALC003   |
`
	opts := parseCountyCodes(sample)
	if len(opts) != 2 {
		t.Fatalf("got %d options, want 2: %+v", len(opts), opts)
	}
	if opts[0].Code != "ALC001" && opts[1].Code != "ALC001" {
		t.Errorf("expected ALC001 among results, got %+v", opts)
	}
}

func TestParseCountyCodesSorted(t *testing.T) {
	sample := `## AK
| County | Code |
|--------|------|
| Zeta County  | AKC900   |
| Alpha County | AKC100   |
`
	opts := parseCountyCodes(sample)
	if len(opts) != 2 {
		t.Fatalf("got %d options, want 2", len(opts))
	}
	if opts[0].Label > opts[1].Label {
		t.Errorf("results not sorted: %+v", opts)
	}
}
