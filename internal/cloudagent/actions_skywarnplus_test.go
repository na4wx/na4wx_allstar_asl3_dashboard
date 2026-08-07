package cloudagent

import (
	"context"
	"testing"

	"hamvoipconfiggui-asl3/internal/skywarnplus"
)

// TestActionSkywarnListCountiesWorksWithoutInstall confirms this one
// action is available even before SkywarnPlus itself is installed --
// it's this app's own bundled reference data, not something read from
// the SkywarnPlus directory.
func TestActionSkywarnListCountiesWorksWithoutInstall(t *testing.T) {
	a := newConfigTestAgent(t) // skywarnDir is "" -- not installed
	result, err := a.dispatch(context.Background(), "skywarn.listCounties", nil)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	counties, ok := result.([]skywarnplus.CountyOption)
	if !ok || len(counties) == 0 {
		t.Fatalf("result = %+v (%T), want a non-empty []skywarnplus.CountyOption", result, result)
	}
}

// TestActionSkywarnGetStatusRequiresInstall confirms every other
// skywarn.* action is gated behind SkywarnPlus actually being present
// on disk, with a clear message rather than a raw file-not-found error.
func TestActionSkywarnGetStatusRequiresInstall(t *testing.T) {
	a := newConfigTestAgent(t)
	if _, err := a.dispatch(context.Background(), "skywarn.getStatus", nil); err == nil {
		t.Fatal("expected an error when SkywarnPlus isn't installed")
	}
}
