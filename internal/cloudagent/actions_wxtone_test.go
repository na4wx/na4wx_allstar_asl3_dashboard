package cloudagent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"hamvoipconfiggui-asl3/internal/wxtone"
)

func newWXToneTestAgent(t *testing.T) *Agent {
	t.Helper()
	store := tempStoreFromFixtures(t)
	a := New(
		"", "wss://cloud.example.com/agent", store, "asterisk",
		nil, nil,
		wxtone.New(filepath.Join(t.TempDir(), "wx-tones.json")),
		"", "", "", "",
	)
	return a
}

func TestActionWXToneSaveListDelete(t *testing.T) {
	a := newWXToneTestAgent(t)

	saveParams, _ := json.Marshal(wxtone.Entry{
		Node:       "1999",
		CTKey:      "ct2",
		NormalType: wxtone.TypeTone,
		NormalTone: "|t(660,880,150,2048)",
		WXType:     wxtone.TypeTone,
		WXTone:     "|t(440,0,400,2048)",
	})
	result, err := a.dispatch(context.Background(), "wxTone.save", saveParams)
	if err != nil {
		t.Fatalf("save error = %v", err)
	}
	saved, ok := result.(wxtone.Entry)
	if !ok {
		t.Fatalf("result type = %T, want wxtone.Entry", result)
	}
	if saved.ID == "" {
		t.Error("saved.ID is empty, want a generated id")
	}
	if saved.Mode != wxtone.ModeNormal {
		t.Errorf("saved.Mode = %q, want %q", saved.Mode, wxtone.ModeNormal)
	}

	listParams, _ := json.Marshal(map[string]string{"node": "1999"})
	result, err = a.dispatch(context.Background(), "wxTone.list", listParams)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	entries := result.([]wxtone.Entry)
	if len(entries) != 1 || entries[0].ID != saved.ID {
		t.Fatalf("entries = %+v, want exactly the saved entry", entries)
	}

	deleteParams, _ := json.Marshal(map[string]string{"id": saved.ID})
	if _, err := a.dispatch(context.Background(), "wxTone.delete", deleteParams); err != nil {
		t.Fatalf("delete error = %v", err)
	}
	result, err = a.dispatch(context.Background(), "wxTone.list", listParams)
	if err != nil {
		t.Fatalf("list after delete error = %v", err)
	}
	if entries := result.([]wxtone.Entry); len(entries) != 0 {
		t.Fatalf("entries after delete = %+v, want empty", entries)
	}
}

func TestActionWXToneListEmptyIsNotError(t *testing.T) {
	a := newWXToneTestAgent(t)
	listParams, _ := json.Marshal(map[string]string{"node": "1999"})
	result, err := a.dispatch(context.Background(), "wxTone.list", listParams)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	if entries := result.([]wxtone.Entry); len(entries) != 0 {
		t.Fatalf("entries = %+v, want empty", entries)
	}
}
