package cloudagent

import (
	"context"
	"encoding/json"
	"testing"

	"hamvoipconfiggui-asl3/internal/automation"
)

func TestActionScheduleSaveListDeleteConnection(t *testing.T) {
	store := tempStoreFromFixtures(t)
	a := newTestAgent(t, "", store, "asterisk")

	listParams, _ := json.Marshal(map[string]string{"number": "1999"})
	result, err := a.dispatch(context.Background(), "schedule.list", listParams)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	before := result.([]automation.Row)

	saveParams, _ := json.Marshal(map[string]any{
		"number": "1999",
		"action": "connect_stay",
		"target": "2000",
		"minute": "0",
		"hour":   "3",
		"dom":    "*",
		"month":  "*",
	})
	if _, err := a.dispatch(context.Background(), "schedule.saveConnection", saveParams); err != nil {
		t.Fatalf("save error = %v", err)
	}

	result, err = a.dispatch(context.Background(), "schedule.list", listParams)
	if err != nil {
		t.Fatalf("list after save error = %v", err)
	}
	after := result.([]automation.Row)
	if len(after) != len(before)+1 {
		t.Fatalf("len(after) = %d, want %d; rows = %+v", len(after), len(before)+1, after)
	}
	var newRow automation.Row
	found := false
	for _, r := range after {
		if r.TimeSpec == "0 3 * * *" {
			newRow = r
			found = true
		}
	}
	if !found {
		t.Fatalf("saved connection not found in %+v", after)
	}
	if !newRow.Recognized || newRow.Label != "Connect (stay connected) 2000" {
		t.Errorf("newRow = %+v, want recognized \"Connect (stay connected) 2000\"", newRow)
	}

	deleteParams, _ := json.Marshal(map[string]string{"number": "1999", "macroNum": newRow.MacroNum})
	if _, err := a.dispatch(context.Background(), "schedule.deleteConnection", deleteParams); err != nil {
		t.Fatalf("delete error = %v", err)
	}
	result, err = a.dispatch(context.Background(), "schedule.list", listParams)
	if err != nil {
		t.Fatalf("list after delete error = %v", err)
	}
	if got := len(result.([]automation.Row)); got != len(before) {
		t.Fatalf("len after delete = %d, want %d", got, len(before))
	}
}

func TestActionScheduleSaveConnectionRejectsBadAction(t *testing.T) {
	store := tempStoreFromFixtures(t)
	a := newTestAgent(t, "", store, "asterisk")

	params, _ := json.Marshal(map[string]any{"number": "1999", "action": "not_a_real_action", "minute": "*", "hour": "*", "dom": "*", "month": "*"})
	if _, err := a.dispatch(context.Background(), "schedule.saveConnection", params); err == nil {
		t.Fatal("expected an error for an invalid action key")
	}
}
