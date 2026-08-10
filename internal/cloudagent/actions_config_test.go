package cloudagent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"hamvoipconfiggui-asl3/internal/config"
)

func newConfigTestAgent(t *testing.T) *Agent {
	t.Helper()
	return newTestAgent(t, filepath.Join(t.TempDir(), "settings.json"), tempStoreFromFixtures(t), "asterisk")
}

func TestActionConfigListNodes(t *testing.T) {
	a := newConfigTestAgent(t)
	result, err := a.dispatch(context.Background(), "config.listNodes", nil)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	nodes, ok := result.([]string)
	if !ok {
		t.Fatalf("result type = %T, want []string", result)
	}
	if len(nodes) != 1 || nodes[0] != "1999" {
		t.Fatalf("nodes = %v, want [1999]", nodes)
	}
}

func TestActionConfigLoadNode(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"number": "1999"})
	result, err := a.dispatch(context.Background(), "config.loadNode", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view, ok := result.(*config.NodeView)
	if !ok {
		t.Fatalf("result type = %T, want *config.NodeView", result)
	}
	if view.Interface != "SimpleUSB" {
		t.Errorf("Interface = %q, want SimpleUSB", view.Interface)
	}
}

func TestActionConfigLoadNodeUnknownErrors(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"number": "9999"})
	if _, err := a.dispatch(context.Background(), "config.loadNode", params); err == nil {
		t.Fatal("expected an error for an unknown node")
	}
}

func TestActionConfigCreateAndDeleteNode(t *testing.T) {
	a := newConfigTestAgent(t)
	createParams, _ := json.Marshal(map[string]string{"number": "54321", "rxChannel": "Local/pseudo", "duplex": "1"})
	result, err := a.dispatch(context.Background(), "config.createNode", createParams)
	if err != nil {
		t.Fatalf("create dispatch error = %v", err)
	}
	view, ok := result.(*config.NodeView)
	if !ok || view.Node != "54321" {
		t.Fatalf("create result = %+v (%T)", result, result)
	}

	nodesResult, err := a.dispatch(context.Background(), "config.listNodes", nil)
	if err != nil {
		t.Fatalf("listNodes dispatch error = %v", err)
	}
	nodes := nodesResult.([]string)
	found := false
	for _, n := range nodes {
		if n == "54321" {
			found = true
		}
	}
	if !found {
		t.Fatalf("54321 not found after create: %v", nodes)
	}

	deleteParams, _ := json.Marshal(map[string]string{"number": "54321"})
	if _, err := a.dispatch(context.Background(), "config.deleteNode", deleteParams); err != nil {
		t.Fatalf("delete dispatch error = %v", err)
	}
	nodesResult, err = a.dispatch(context.Background(), "config.listNodes", nil)
	if err != nil {
		t.Fatalf("listNodes dispatch error = %v", err)
	}
	for _, n := range nodesResult.([]string) {
		if n == "54321" {
			t.Fatal("54321 still present after delete")
		}
	}
}

func TestActionConfigUpdateNodeSettings(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]any{
		"number":  "1999",
		"updates": map[string]string{"duplex": "3"},
	})
	result, err := a.dispatch(context.Background(), "config.updateNodeSettings", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view := result.(*config.NodeView)
	if view.Duplex != "3" {
		t.Errorf("Duplex = %q, want 3", view.Duplex)
	}
}

func TestActionConfigSaveNodeCreatesNew(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{
		"number":    "54322",
		"rxChannel": "Local/pseudo",
		"duplex":    "1",
		"hangTime":  "3000",
		"idTime":    "300000",
	})
	result, err := a.dispatch(context.Background(), "config.saveNode", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view := result.(*config.NodeView)
	if view.RxChannel != "Local/pseudo" || view.Duplex != "1" {
		t.Fatalf("view = %+v, want RxChannel=Local/pseudo Duplex=1", view)
	}
	if view.HangTime != "3000" || view.IDTime != "300000" {
		t.Fatalf("view = %+v, want HangTime=3000 IDTime=300000", view)
	}
}

func TestActionConfigSaveNodeUpdatesExistingLeavingBlankFieldsAlone(t *testing.T) {
	a := newConfigTestAgent(t)
	before, err := a.store.LoadNode("1999")
	if err != nil {
		t.Fatalf("LoadNode error = %v", err)
	}
	params, _ := json.Marshal(map[string]string{
		"number":   "1999",
		"hangTime": "9000",
	})
	result, err := a.dispatch(context.Background(), "config.saveNode", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view := result.(*config.NodeView)
	if view.HangTime != "9000" {
		t.Errorf("HangTime = %q, want 9000", view.HangTime)
	}
	if view.RxChannel != before.RxChannel {
		t.Errorf("RxChannel = %q, want unchanged %q", view.RxChannel, before.RxChannel)
	}
	if view.Duplex != before.Duplex {
		t.Errorf("Duplex = %q, want unchanged %q", view.Duplex, before.Duplex)
	}
}

func TestActionConfigSaveNodeRejectsBadNumber(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"number": "not-a-node"})
	if _, err := a.dispatch(context.Background(), "config.saveNode", params); err == nil {
		t.Fatal("expected an error for an invalid node number")
	}
}

func TestActionConfigUpdateRadioSettings(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]any{
		"number":  "1999",
		"updates": map[string]string{"rxmixerset": "600"},
	})
	result, err := a.dispatch(context.Background(), "config.updateRadioSettings", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view := result.(*config.NodeView)
	if view.Radio == nil || view.Radio.RxMixerSet != "600" {
		t.Errorf("Radio = %+v, want RxMixerSet 600", view.Radio)
	}
}

func TestActionConfigSetCourtesyTones(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{
		"number": "1999", "unlinkedCT": "ct5", "remoteCT": "", "linkUnkeyCT": "ct7",
	})
	if _, err := a.dispatch(context.Background(), "config.setCourtesyTones", params); err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	loadParams, _ := json.Marshal(map[string]string{"number": "1999"})
	result, err := a.dispatch(context.Background(), "config.loadNode", loadParams)
	if err != nil {
		t.Fatalf("loadNode dispatch error = %v", err)
	}
	view := result.(*config.NodeView)
	if view.UnlinkedCT != "ct5" || view.LinkUnkeyCT != "ct7" {
		t.Errorf("UnlinkedCT=%q LinkUnkeyCT=%q, want ct5/ct7", view.UnlinkedCT, view.LinkUnkeyCT)
	}
}

func TestActionConfigListAndSetTelemetry(t *testing.T) {
	a := newConfigTestAgent(t)
	listParams, _ := json.Marshal(map[string]string{"number": "1999"})
	result, err := a.dispatch(context.Background(), "config.listTelemetry", listParams)
	if err != nil {
		t.Fatalf("list dispatch error = %v", err)
	}
	entries, ok := result.([]telemetryEntryResult)
	if !ok || len(entries) == 0 {
		t.Fatalf("result = %+v (%T), want non-empty []telemetryEntryResult", result, result)
	}
	foundCT2 := false
	for _, e := range entries {
		if e.Key == "ct2" {
			foundCT2 = true
			if e.Tone == nil {
				t.Errorf("ct2 should decompose as a single tone in the fixture, got raw: %q", e.Value)
			}
		}
	}
	if !foundCT2 {
		t.Fatal("ct2 not found in telemetry entries")
	}

	setParams, _ := json.Marshal(map[string]any{
		"number":  "1999",
		"updates": map[string]string{"ct2": "|t(100,200,50,1000)"},
	})
	if _, err := a.dispatch(context.Background(), "config.setTelemetry", setParams); err != nil {
		t.Fatalf("set dispatch error = %v", err)
	}
	result, err = a.dispatch(context.Background(), "config.listTelemetry", listParams)
	if err != nil {
		t.Fatalf("list dispatch error = %v", err)
	}
	for _, e := range result.([]telemetryEntryResult) {
		if e.Key == "ct2" && e.Value != "|t(100,200,50,1000)" {
			t.Errorf("ct2 = %q, want overridden value", e.Value)
		}
	}
}
