package cloudagent

import (
	"context"
	"encoding/json"
	"testing"

	"hamvoipconfiggui-asl3/internal/config"
)

func TestActionConfigGetMorseIDFrequency(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"section": "morse"})
	result, err := a.dispatch(context.Background(), "config.getMorseIDFrequency", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	got := result.(morseIDFrequencyResult)
	if got.Frequency == "" {
		t.Fatalf("Frequency = %q, want a non-empty default from the fixture's [morse] section", got.Frequency)
	}
}

func TestActionConfigSaveStationIDCW(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{
		"number":          "1999",
		"mode":            "cw",
		"text":            "N0CALL",
		"frequency":       "800",
		"intervalMinutes": "10",
	})
	result, err := a.dispatch(context.Background(), "config.saveStationID", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view := result.(*config.NodeView)
	if view.IDRecording != "|iN0CALL" {
		t.Errorf("IDRecording = %q, want |iN0CALL", view.IDRecording)
	}
	if view.IDTime != "600000" {
		t.Errorf("IDTime = %q, want 600000 (10 minutes)", view.IDTime)
	}

	freqParams, _ := json.Marshal(map[string]string{"section": "morse"})
	freqResult, err := a.dispatch(context.Background(), "config.getMorseIDFrequency", freqParams)
	if err != nil {
		t.Fatalf("dispatch error (frequency check) = %v", err)
	}
	if got := freqResult.(morseIDFrequencyResult).Frequency; got != "800" {
		t.Errorf("morse frequency = %q, want 800", got)
	}
}

func TestActionConfigSaveStationIDSound(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{
		"number": "1999",
		"mode":   "sound",
		"sound":  "custom/mysound.wav",
	})
	result, err := a.dispatch(context.Background(), "config.saveStationID", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	view := result.(*config.NodeView)
	if view.IDRecording != "custom/mysound.wav" {
		t.Errorf("IDRecording = %q, want custom/mysound.wav", view.IDRecording)
	}
}

func TestActionConfigSaveStationIDRejectsBlankCWText(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"number": "1999", "mode": "cw", "text": "  "})
	if _, err := a.dispatch(context.Background(), "config.saveStationID", params); err == nil {
		t.Fatal("expected an error for blank CW text")
	}
}

func TestActionConfigSaveStationIDRejectsBadMode(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"number": "1999", "mode": "bogus"})
	if _, err := a.dispatch(context.Background(), "config.saveStationID", params); err == nil {
		t.Fatal("expected an error for an unrecognized mode")
	}
}
