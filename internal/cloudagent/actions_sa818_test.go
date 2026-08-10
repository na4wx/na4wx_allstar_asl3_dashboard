package cloudagent

import (
	"context"
	"testing"
)

// TestActionSA818LastWithNoStatePath confirms this is a documented
// nil,nil (not an error) when the feature is unconfigured -- callers
// should treat that as "no record yet", not a failure.
func TestActionSA818LastWithNoStatePath(t *testing.T) {
	a := New(
		"", "wss://cloud.example.com/agent", tempStoreFromFixtures(t), "asterisk",
		nil, nil, nil, "", "",
		"", // sa818StatePath -- unconfigured
		"",
	)
	result, err := a.dispatch(context.Background(), "sa818.last", nil)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
}

// TestActionSA818LastWithNoRecordYet confirms a configured but
// never-written state path also comes back nil, not an error.
func TestActionSA818LastWithNoRecordYet(t *testing.T) {
	a := newConfigTestAgent(t) // sa818StatePath points at a fresh temp dir with no file in it
	result, err := a.dispatch(context.Background(), "sa818.last", nil)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
}
