package cloudagent

import (
	"context"
	"encoding/json"
	"testing"

	"hamvoipconfiggui-asl3/internal/config"
)

func TestActionConfigFunctionMacrosListSaveDelete(t *testing.T) {
	store := tempStoreFromFixtures(t)
	a := newTestAgent(t, "", store, "asterisk")

	listParams, _ := json.Marshal(map[string]string{"number": "1999", "kind": "macro"})
	result, err := a.dispatch(context.Background(), "config.listFunctionMacros", listParams)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	before, ok := result.([]config.FunctionMacro)
	if !ok {
		t.Fatalf("result type = %T, want []config.FunctionMacro", result)
	}

	saveParams, _ := json.Marshal(map[string]string{"number": "1999", "kind": "macro", "digits": "42", "command": "ilink,1"})
	if _, err := a.dispatch(context.Background(), "config.saveFunctionMacro", saveParams); err != nil {
		t.Fatalf("save error = %v", err)
	}

	result, err = a.dispatch(context.Background(), "config.listFunctionMacros", listParams)
	if err != nil {
		t.Fatalf("list after save error = %v", err)
	}
	after := result.([]config.FunctionMacro)
	if len(after) != len(before)+1 {
		t.Fatalf("len(after) = %d, want %d", len(after), len(before)+1)
	}
	found := false
	for _, m := range after {
		if m.Digits == "42" && m.Command == "ilink,1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("saved macro not found in %+v", after)
	}

	deleteParams, _ := json.Marshal(map[string]string{"number": "1999", "kind": "macro", "digits": "42"})
	if _, err := a.dispatch(context.Background(), "config.deleteFunctionMacro", deleteParams); err != nil {
		t.Fatalf("delete error = %v", err)
	}
	result, err = a.dispatch(context.Background(), "config.listFunctionMacros", listParams)
	if err != nil {
		t.Fatalf("list after delete error = %v", err)
	}
	if got := len(result.([]config.FunctionMacro)); got != len(before) {
		t.Fatalf("len after delete = %d, want %d", got, len(before))
	}
}

func TestActionConfigListFunctionMacrosRejectsBadKind(t *testing.T) {
	store := tempStoreFromFixtures(t)
	a := newTestAgent(t, "", store, "asterisk")

	params, _ := json.Marshal(map[string]string{"number": "1999", "kind": "bogus"})
	if _, err := a.dispatch(context.Background(), "config.listFunctionMacros", params); err == nil {
		t.Fatal("expected an error for an unrecognized kind")
	}
}
