package cloudagent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func newRawConfigTestAgent(t *testing.T, allowEdit bool) *Agent {
	t.Helper()
	store := tempStoreFromFixtures(t)
	a := newTestAgent(t, filepath.Join(t.TempDir(), "settings.json"), store, "asterisk")
	if err := a.Settings().Save(Settings{Enabled: true, AllowRawConfigEdit: allowEdit}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestActionRawConfigListFilesWorksEvenWhenDisabled(t *testing.T) {
	a := newRawConfigTestAgent(t, false)
	result, err := a.dispatch(context.Background(), "rawconfig.listFiles", nil)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	files, ok := result.([]string)
	if !ok || len(files) == 0 {
		t.Fatalf("result = %v (%T), want a non-empty []string", result, result)
	}
}

func TestActionRawConfigGetFileRefusedWhenDisabled(t *testing.T) {
	a := newRawConfigTestAgent(t, false)
	params, _ := json.Marshal(map[string]string{"file": "rpt.conf"})
	if _, err := a.dispatch(context.Background(), "rawconfig.getFile", params); err == nil {
		t.Fatal("dispatch() error = nil, want refusal when AllowRawConfigEdit is off")
	}
}

func TestActionRawConfigGetFileRejectsDisallowedFile(t *testing.T) {
	a := newRawConfigTestAgent(t, true)
	params, _ := json.Marshal(map[string]string{"file": "/etc/passwd"})
	if _, err := a.dispatch(context.Background(), "rawconfig.getFile", params); err == nil {
		t.Fatal("dispatch() error = nil, want rejection of a file not on the allowlist")
	}
}

func TestActionRawConfigGetFileWhenEnabled(t *testing.T) {
	a := newRawConfigTestAgent(t, true)
	params, _ := json.Marshal(map[string]string{"file": "rpt.conf"})
	result, err := a.dispatch(context.Background(), "rawconfig.getFile", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	fileResult, ok := result.(rawConfigFileResult)
	if !ok {
		t.Fatalf("result type = %T, want rawConfigFileResult", result)
	}
	found := false
	for _, sec := range fileResult.Sections {
		if sec.Name != "1999" {
			continue
		}
		found = true
		hasRxChannel := false
		for _, kv := range sec.Keys {
			if kv.Key == "rxchannel" {
				hasRxChannel = true
			}
		}
		if !hasRxChannel {
			t.Error("[1999]'s own keys should include rxchannel")
		}
	}
	if !found {
		t.Fatal("section [1999] not found in the relayed result")
	}
}

// TestActionRawConfigGetFileSectionWithNoKeysIsNonNil confirms a section
// with zero key/value lines gets back a non-nil empty Keys slice, not
// nil -- a nil slice marshals to JSON null, and this result goes
// straight to the browser as JSON.
func TestActionRawConfigGetFileSectionWithNoKeysIsNonNil(t *testing.T) {
	a := newRawConfigTestAgent(t, true)
	if err := a.store.AddRawSection("rpt.conf", "empty-section-for-test"); err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]string{"file": "rpt.conf"})
	result, err := a.dispatch(context.Background(), "rawconfig.getFile", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	fileResult, ok := result.(rawConfigFileResult)
	if !ok {
		t.Fatalf("result type = %T, want rawConfigFileResult", result)
	}
	for _, sec := range fileResult.Sections {
		if sec.Name != "empty-section-for-test" {
			continue
		}
		if sec.Keys == nil {
			t.Error("Keys = nil, want a non-nil empty slice")
		}
		if len(sec.Keys) != 0 {
			t.Errorf("Keys = %+v, want empty", sec.Keys)
		}
		return
	}
	t.Fatal("empty-section-for-test not found in the relayed result")
}

func TestActionRawConfigSetKeyRefusedWhenDisabled(t *testing.T) {
	a := newRawConfigTestAgent(t, false)
	params, _ := json.Marshal(rawConfigSetKeyParams{File: "rpt.conf", Section: "1999", Index: 0, Value: "3"})
	if _, err := a.dispatch(context.Background(), "rawconfig.setKey", params); err == nil {
		t.Fatal("dispatch() error = nil, want refusal when AllowRawConfigEdit is off")
	}
}

func TestActionRawConfigSetKeyActuallyChangesTheFile(t *testing.T) {
	a := newRawConfigTestAgent(t, true)
	sections, err := a.store.RawSections("rpt.conf")
	if err != nil {
		t.Fatal(err)
	}
	var idx int
	var found bool
	for _, sec := range sections {
		if sec.Name != "1999" {
			continue
		}
		for i, p := range sec.Pairs {
			if p.Key == "rxchannel" {
				idx = i
				found = true
			}
		}
	}
	if !found {
		t.Fatal("rxchannel not found in [1999]'s own pairs")
	}

	params, _ := json.Marshal(rawConfigSetKeyParams{File: "rpt.conf", Section: "1999", Index: idx, Value: "Radio/1999"})
	if _, err := a.dispatch(context.Background(), "rawconfig.setKey", params); err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view, err := a.store.LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.RxChannel != "Radio/1999" {
		t.Fatalf("RxChannel = %q, want Radio/1999 (the raw edit)", view.RxChannel)
	}
}

func TestActionRawConfigAddKeyAndAddSection(t *testing.T) {
	a := newRawConfigTestAgent(t, true)

	addKeyParams, _ := json.Marshal(rawConfigAddKeyParams{File: "rpt.conf", Section: "1999", Key: "custom_test_key", Value: "hello"})
	if _, err := a.dispatch(context.Background(), "rawconfig.addKey", addKeyParams); err != nil {
		t.Fatalf("addKey error = %v", err)
	}
	sections, err := a.store.RawSections("rpt.conf")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sec := range sections {
		if sec.Name != "1999" {
			continue
		}
		for _, p := range sec.Pairs {
			if p.Key == "custom_test_key" && p.Value == "hello" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("custom_test_key not found after addKey")
	}

	addSectionParams, _ := json.Marshal(rawConfigAddSectionParams{File: "rpt.conf", Section: "brand-new-section"})
	if _, err := a.dispatch(context.Background(), "rawconfig.addSection", addSectionParams); err != nil {
		t.Fatalf("addSection error = %v", err)
	}
	sections, err = a.store.RawSections("rpt.conf")
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, sec := range sections {
		if sec.Name == "brand-new-section" {
			found = true
		}
	}
	if !found {
		t.Fatal("brand-new-section not found after addSection")
	}
}

func TestActionRawConfigAddKeyAndAddSectionRefusedWhenDisabled(t *testing.T) {
	a := newRawConfigTestAgent(t, false)
	addKeyParams, _ := json.Marshal(rawConfigAddKeyParams{File: "rpt.conf", Section: "1999", Key: "custom_test_key", Value: "hello"})
	if _, err := a.dispatch(context.Background(), "rawconfig.addKey", addKeyParams); err == nil {
		t.Fatal("addKey: dispatch() error = nil, want refusal when AllowRawConfigEdit is off")
	}
	addSectionParams, _ := json.Marshal(rawConfigAddSectionParams{File: "rpt.conf", Section: "brand-new-section"})
	if _, err := a.dispatch(context.Background(), "rawconfig.addSection", addSectionParams); err == nil {
		t.Fatal("addSection: dispatch() error = nil, want refusal when AllowRawConfigEdit is off")
	}
}
