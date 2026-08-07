package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/skywarnplus"
)

// errSkywarnNotInstalled matches the local UI's own messaging -- this
// package never installs SkywarnPlus itself, only configures a copy
// that's already there.
func errSkywarnNotInstalled() error {
	return fmt.Errorf("SkywarnPlus isn't installed on this device -- re-run install.sh and choose to install it")
}

// actionSkywarnListCounties wraps skywarnplus.ListCounties -- this
// app's own bundled reference data, so it's available even before
// SkywarnPlus itself is installed.
func (a *Agent) actionSkywarnListCounties(_ context.Context, _ json.RawMessage) (any, error) {
	return skywarnplus.ListCounties(), nil
}

// actionSkywarnGetStatus wraps skywarnplus.GetStatus.
func (a *Agent) actionSkywarnGetStatus(ctx context.Context, _ json.RawMessage) (any, error) {
	if !skywarnplus.IsInstalled(a.skywarnDir) {
		return nil, errSkywarnNotInstalled()
	}
	return skywarnplus.GetStatus(ctx, a.skywarnDir)
}

type skywarnToggleParams struct {
	Key   string `json:"key"`
	Value bool   `json:"value"`
}

// actionSkywarnSetToggle wraps skywarnplus.SetToggle.
func (a *Agent) actionSkywarnSetToggle(ctx context.Context, params json.RawMessage) (any, error) {
	if !skywarnplus.IsInstalled(a.skywarnDir) {
		return nil, errSkywarnNotInstalled()
	}
	var p skywarnToggleParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	output, err := skywarnplus.SetToggle(ctx, a.skywarnDir, p.Key, p.Value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, output)
	}
	return map[string]bool{"ok": true}, nil
}

type skywarnCountiesParams struct {
	Codes []string `json:"codes"`
}

// actionSkywarnSetCounties wraps skywarnplus.SetCounties -- a full
// replace of the county-code list, same as the underlying function; the
// caller computes the add/remove diff itself before sending the new
// full list.
func (a *Agent) actionSkywarnSetCounties(ctx context.Context, params json.RawMessage) (any, error) {
	if !skywarnplus.IsInstalled(a.skywarnDir) {
		return nil, errSkywarnNotInstalled()
	}
	var p skywarnCountiesParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	output, err := skywarnplus.SetCounties(ctx, a.skywarnDir, p.Codes)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, output)
	}
	return map[string]bool{"ok": true}, nil
}

type skywarnAddNodeParams struct {
	Node string `json:"node"`
}

// actionSkywarnAddNode wraps skywarnplus.AddNode.
func (a *Agent) actionSkywarnAddNode(ctx context.Context, params json.RawMessage) (any, error) {
	if !skywarnplus.IsInstalled(a.skywarnDir) {
		return nil, errSkywarnNotInstalled()
	}
	var p skywarnAddNodeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	output, err := skywarnplus.AddNode(ctx, a.skywarnDir, p.Node)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, output)
	}
	return map[string]bool{"ok": true}, nil
}

type skywarnPushoverParams struct {
	Enable   bool   `json:"enable"`
	UserKey  string `json:"userKey"`
	APIToken string `json:"apiToken"`
	Debug    bool   `json:"debug"`
}

// actionSkywarnSetPushover wraps skywarnplus.SetPushover.
func (a *Agent) actionSkywarnSetPushover(ctx context.Context, params json.RawMessage) (any, error) {
	if !skywarnplus.IsInstalled(a.skywarnDir) {
		return nil, errSkywarnNotInstalled()
	}
	var p skywarnPushoverParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	output, err := skywarnplus.SetPushover(ctx, a.skywarnDir, p.Enable, p.UserKey, p.APIToken, p.Debug)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, output)
	}
	return map[string]bool{"ok": true}, nil
}

type skywarnSkyDescribeParams struct {
	APIKey   string `json:"apiKey"`
	Language string `json:"language"`
	Speed    int    `json:"speed"`
	Voice    string `json:"voice"`
	MaxWords int    `json:"maxWords"`
}

// actionSkywarnSetSkyDescribe wraps skywarnplus.SetSkyDescribe.
func (a *Agent) actionSkywarnSetSkyDescribe(ctx context.Context, params json.RawMessage) (any, error) {
	if !skywarnplus.IsInstalled(a.skywarnDir) {
		return nil, errSkywarnNotInstalled()
	}
	var p skywarnSkyDescribeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	output, err := skywarnplus.SetSkyDescribe(ctx, a.skywarnDir, p.APIKey, p.Language, p.Speed, p.Voice, p.MaxWords)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, output)
	}
	return map[string]bool{"ok": true}, nil
}
