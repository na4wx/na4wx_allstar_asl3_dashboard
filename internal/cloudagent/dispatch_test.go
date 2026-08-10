package cloudagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatchUnknownActionErrors(t *testing.T) {
	a := newTestAgent(t, filepath.Join(t.TempDir(), "settings.json"), tempStoreFromFixtures(t), "asterisk")
	_, err := a.dispatch(context.Background(), "not.a.real.action", nil)
	if err == nil {
		t.Fatal("expected an error for an unregistered action")
	}
}

// TestActionRegistryHasNoNilEntries guards against a typo'd map literal
// (e.g. a duplicate key silently shadowing another, or a nil method
// value) reaching production undetected.
func TestActionRegistryHasNoNilEntries(t *testing.T) {
	a := newTestAgent(t, filepath.Join(t.TempDir(), "settings.json"), tempStoreFromFixtures(t), "asterisk")
	actions := a.actions()
	if len(actions) == 0 {
		t.Fatal("action registry is empty")
	}
	for name, fn := range actions {
		if fn == nil {
			t.Errorf("action %q has a nil handler", name)
		}
	}
}

func TestDispatchAuditsEveryCall(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	a := New(
		filepath.Join(t.TempDir(), "settings.json"),
		"wss://cloud.example.com/agent",
		tempStoreFromFixtures(t),
		"asterisk",
		nil, nil, nil, "", "", "",
		auditPath,
	)
	if _, err := a.dispatch(context.Background(), "config.listNodes", nil); err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	if _, err := a.dispatch(context.Background(), "not.a.real.action", nil); err == nil {
		t.Fatal("expected an error for an unregistered action")
	}
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d audit lines, want 2: %q", len(lines), string(data))
	}
	if !strings.Contains(lines[0], `"action":"config.listNodes"`) || !strings.Contains(lines[0], `"ok":true`) {
		t.Errorf("line 1 = %q, missing expected fields", lines[0])
	}
	if !strings.Contains(lines[1], `"action":"not.a.real.action"`) || !strings.Contains(lines[1], `"ok":false`) {
		t.Errorf("line 2 = %q, missing expected fields", lines[1])
	}
}
