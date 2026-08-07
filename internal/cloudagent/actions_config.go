// Node configuration actions, scoped to what ASL3's own internal/config
// actually supports -- see this package's own (run.go) doc comment for
// what the original HamVoIP app's cloudagent offered here that ASL3
// doesn't (per-node private functions/macro sections, a separate named
// "radio device" entity, node cloning/normalizing/standard-command-set,
// tune-file recovery, dialplan sync). Every action below wraps the exact
// same config.Store methods internal/server's own node-edit handlers
// use, so a cloud-driven change and a local one are indistinguishable
// to the config files themselves.
package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/config"
)

// actionConfigListNodes wraps config.Store.ListNodes.
func (a *Agent) actionConfigListNodes(_ context.Context, _ json.RawMessage) (any, error) {
	return a.store.ListNodes()
}

type configLoadNodeParams struct {
	Number string `json:"number"`
}

// actionConfigLoadNode wraps config.Store.LoadNode.
func (a *Agent) actionConfigLoadNode(_ context.Context, params json.RawMessage) (any, error) {
	var p configLoadNodeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	return a.store.LoadNode(p.Number)
}

type configCreateNodeParams struct {
	Number    string `json:"number"`
	RXChannel string `json:"rxChannel"`
	Duplex    string `json:"duplex"`
}

// actionConfigCreateNode wraps config.Store.CreateNode.
func (a *Agent) actionConfigCreateNode(_ context.Context, params json.RawMessage) (any, error) {
	var p configCreateNodeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if err := a.store.CreateNode(p.Number, p.RXChannel, p.Duplex); err != nil {
		return nil, err
	}
	return a.store.LoadNode(p.Number)
}

type configDeleteNodeParams struct {
	Number string `json:"number"`
}

// actionConfigDeleteNode wraps config.Store.DeleteNode.
func (a *Agent) actionConfigDeleteNode(_ context.Context, params json.RawMessage) (any, error) {
	var p configDeleteNodeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if err := a.store.DeleteNode(p.Number); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

type configUpdateNodeSettingsParams struct {
	Number  string            `json:"number"`
	Updates map[string]string `json:"updates"`
}

// actionConfigUpdateNodeSettings wraps config.Store.UpdateNodeSettings --
// the node-main-tier field updates (rxchannel, duplex, and anything else
// that lives directly on the node's own rpt.conf stanza), matching
// handleNodeUpdate's own field set. Updates is a map rather than fixed
// fields so this stays in sync with whatever UpdateNodeSettings itself
// accepts without a wire-format change every time that set grows.
func (a *Agent) actionConfigUpdateNodeSettings(_ context.Context, params json.RawMessage) (any, error) {
	var p configUpdateNodeSettingsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if err := a.store.UpdateNodeSettings(p.Number, p.Updates); err != nil {
		return nil, err
	}
	return a.store.LoadNode(p.Number)
}

type configUpdateRadioSettingsParams struct {
	Number  string            `json:"number"`
	Updates map[string]string `json:"updates"`
}

// actionConfigUpdateRadioSettings wraps config.Store.UpdateRadioSettings
// -- the node's simpleusb.conf/usbradio.conf tuning fields, matching
// handleNodeRadioTuningUpdate's own field set.
func (a *Agent) actionConfigUpdateRadioSettings(_ context.Context, params json.RawMessage) (any, error) {
	var p configUpdateRadioSettingsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if err := a.store.UpdateRadioSettings(p.Number, p.Updates); err != nil {
		return nil, err
	}
	return a.store.LoadNode(p.Number)
}

type configSetCourtesyTonesParams struct {
	Number      string `json:"number"`
	UnlinkedCT  string `json:"unlinkedCT"`
	RemoteCT    string `json:"remoteCT"`
	LinkUnkeyCT string `json:"linkUnkeyCT"`
}

// actionConfigSetCourtesyTones wraps
// config.Store.SetCourtesyToneAssignments.
func (a *Agent) actionConfigSetCourtesyTones(_ context.Context, params json.RawMessage) (any, error) {
	var p configSetCourtesyTonesParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if err := a.store.SetCourtesyToneAssignments(p.Number, p.UnlinkedCT, p.RemoteCT, p.LinkUnkeyCT); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

// telemetrySection resolves a node's telemetry stanza name the same way
// internal/server's own populateNodeTelemetry does: the node's own
// Telemetry field if set, otherwise the bare "telemetry" default --
// never a client-supplied section string (a relayed action must never
// let the cloud side point a config write at an arbitrary section name).
func telemetrySection(n *config.NodeView) string {
	if n.Telemetry != "" {
		return n.Telemetry
	}
	return "telemetry"
}

type toneSpecResult struct {
	Freq1      int `json:"freq1"`
	Freq2      int `json:"freq2"`
	DurationMS int `json:"durationMs"`
	Amplitude  int `json:"amplitude"`
}

type telemetryEntryResult struct {
	Key   string          `json:"key"`
	Value string          `json:"value"`
	Tone  *toneSpecResult `json:"tone,omitempty"`
}

type configListTelemetryParams struct {
	Number string `json:"number"`
}

// actionConfigListTelemetry wraps config.Store.ListTelemetryEntries,
// resolving the section from the node itself rather than trusting a
// client-supplied section name. Each entry includes both its raw value
// and, when it parses as a single tone-generator segment, the friendly
// per-field breakdown -- mirroring the local Sounds & Tones tab's own
// either/or editor so the cloud client can offer the same thing.
func (a *Agent) actionConfigListTelemetry(_ context.Context, params json.RawMessage) (any, error) {
	var p configListTelemetryParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	node, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	entries, err := a.store.ListTelemetryEntries(telemetrySection(node))
	if err != nil {
		return nil, err
	}
	out := make([]telemetryEntryResult, 0, len(entries))
	for _, e := range entries {
		r := telemetryEntryResult{Key: e.Key, Value: e.Value}
		if tone, ok := config.ParseSingleTone(e.Value); ok {
			r.Tone = &toneSpecResult{Freq1: tone.Freq1, Freq2: tone.Freq2, DurationMS: tone.DurationMS, Amplitude: tone.Amplitude}
		}
		out = append(out, r)
	}
	return out, nil
}

type configSetTelemetryParams struct {
	Number  string            `json:"number"`
	Updates map[string]string `json:"updates"`
}

// actionConfigSetTelemetry wraps config.Store.SetTelemetryEntries,
// section resolved the same way actionConfigListTelemetry does --
// updates several keys in one edit, matching the local Sounds & Tones
// tab's own batch save.
func (a *Agent) actionConfigSetTelemetry(_ context.Context, params json.RawMessage) (any, error) {
	var p configSetTelemetryParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	node, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	if err := a.store.SetTelemetryEntries(telemetrySection(node), p.Updates); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
