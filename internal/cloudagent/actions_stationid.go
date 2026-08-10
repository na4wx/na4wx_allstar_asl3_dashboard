// Station ID actions -- wraps the exact same config.Store calls
// internal/server's own handleNodeStationIDUpdate/populateNodeTelemetry
// use, so the cloud's Station ID card behaves identically to the local
// app's Sounds & Tones tab.
package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"hamvoipconfiggui-asl3/internal/config"
)

type configGetMorseIDFrequencyParams struct {
	Section string `json:"section"`
}

type morseIDFrequencyResult struct {
	Frequency string `json:"frequency"`
}

// actionConfigGetMorseIDFrequency wraps config.Store.GetMorseIDFrequency
// -- the CW ID tone's own frequency, which lives in the node's [morse]
// section rather than on the node itself, so it isn't part of
// config.NodeView. The caller resolves which section to ask for the
// same way the local app does (the node's own Morse field, or "morse"
// if blank) -- this action never picks a default itself.
func (a *Agent) actionConfigGetMorseIDFrequency(_ context.Context, params json.RawMessage) (any, error) {
	var p configGetMorseIDFrequencyParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	freq, err := a.store.GetMorseIDFrequency(p.Section)
	if err != nil {
		return nil, err
	}
	return morseIDFrequencyResult{Frequency: freq}, nil
}

type configSaveStationIDParams struct {
	Number          string `json:"number"`
	Mode            string `json:"mode"` // "cw" | "sound"
	Text            string `json:"text"`
	Sound           string `json:"sound"`
	Frequency       string `json:"frequency"`
	IntervalMinutes string `json:"intervalMinutes"`
}

// actionConfigSaveStationID wraps the same three config.Store calls
// handleNodeStationIDUpdate makes: UpdateNodeSettings for idrecording
// (formatted via config.FormatCWID for CW mode, or the raw sound
// reference) and idtime (converted from the caller's minutes to
// app_rpt's own milliseconds), plus SetMorseIDFrequency for the CW
// tone's own frequency -- only when Mode is "cw" and Frequency was
// given, matching the local form's own "CW mode only" behavior exactly.
func (a *Agent) actionConfigSaveStationID(_ context.Context, params json.RawMessage) (any, error) {
	var p configSaveStationIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}

	var value string
	switch p.Mode {
	case "cw":
		text := strings.TrimSpace(p.Text)
		if text == "" {
			return nil, fmt.Errorf("enter the callsign/text to send as CW")
		}
		value = config.FormatCWID(text)
	case "sound":
		value = strings.TrimSpace(p.Sound)
		if value == "" {
			return nil, fmt.Errorf("choose a sound file for the station ID")
		}
	default:
		return nil, fmt.Errorf("mode must be \"cw\" or \"sound\"")
	}

	nodeUpdates := map[string]string{"idrecording": value}
	if v := strings.TrimSpace(p.IntervalMinutes); v != "" {
		minutes, err := strconv.Atoi(v)
		if err != nil || minutes < 0 {
			return nil, fmt.Errorf("ID interval must be a non-negative number of minutes")
		}
		nodeUpdates["idtime"] = strconv.Itoa(minutes * 60000)
	}
	if err := a.store.UpdateNodeSettings(p.Number, nodeUpdates); err != nil {
		return nil, err
	}

	if p.Mode == "cw" {
		if v := strings.TrimSpace(p.Frequency); v != "" {
			hz, err := strconv.Atoi(v)
			if err != nil || hz <= 0 {
				return nil, fmt.Errorf("CW ID tone frequency must be a positive number of Hz")
			}
			view, err := a.store.LoadNode(p.Number)
			if err != nil {
				return nil, err
			}
			morseSection := view.Morse
			if morseSection == "" {
				morseSection = "morse"
			}
			if err := a.store.SetMorseIDFrequency(morseSection, v); err != nil {
				return nil, err
			}
		}
	}

	return a.store.LoadNode(p.Number)
}
