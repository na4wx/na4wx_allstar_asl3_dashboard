// Function/macro (DTMF command table) actions -- the cloud equivalent
// of the local "Commands" tab (see internal/server/commands.go). kind
// picks which of a node's two sections (Functions or Macro) an action
// targets, matching node_edit.html's own "Command list"/"Saved macros"
// tables and the cloud client's own FunctionMacroKind enum -- never a
// raw section name over the wire, so a caller can't be pointed at an
// arbitrary rpt.conf section.
package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/config"
)

// functionMacroSection resolves kind ("functions" or "macro") against
// view's own section names -- the same two-tier "shared by default,
// overridable per node" sections NodeView.Functions/Macro already
// describe.
func functionMacroSection(view *config.NodeView, kind string) (string, error) {
	switch kind {
	case "functions":
		return view.Functions, nil
	case "macro":
		return view.Macro, nil
	default:
		return "", fmt.Errorf("unrecognized kind %q, want \"functions\" or \"macro\"", kind)
	}
}

type functionMacroListParams struct {
	Number string `json:"number"`
	Kind   string `json:"kind"`
}

// actionConfigListFunctionMacros wraps config.Store.ListFunctionMacros.
func (a *Agent) actionConfigListFunctionMacros(_ context.Context, params json.RawMessage) (any, error) {
	var p functionMacroListParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	view, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	section, err := functionMacroSection(view, p.Kind)
	if err != nil {
		return nil, err
	}
	return a.store.ListFunctionMacros(section)
}

type functionMacroSaveParams struct {
	Number  string `json:"number"`
	Kind    string `json:"kind"`
	Digits  string `json:"digits"`
	Command string `json:"command"`
}

// actionConfigSaveFunctionMacro wraps config.Store.SetFunctionMacro.
func (a *Agent) actionConfigSaveFunctionMacro(_ context.Context, params json.RawMessage) (any, error) {
	var p functionMacroSaveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	view, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	section, err := functionMacroSection(view, p.Kind)
	if err != nil {
		return nil, err
	}
	if p.Digits == "" {
		return nil, fmt.Errorf("digits is required")
	}
	if err := a.store.SetFunctionMacro(section, p.Digits, p.Command); err != nil {
		return nil, err
	}
	return config.FunctionMacro{Digits: p.Digits, Command: p.Command}, nil
}

type functionMacroDeleteParams struct {
	Number string `json:"number"`
	Kind   string `json:"kind"`
	Digits string `json:"digits"`
}

// actionConfigDeleteFunctionMacro wraps config.Store.DeleteFunctionMacro.
func (a *Agent) actionConfigDeleteFunctionMacro(_ context.Context, params json.RawMessage) (any, error) {
	var p functionMacroDeleteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	view, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	section, err := functionMacroSection(view, p.Kind)
	if err != nil {
		return nil, err
	}
	if err := a.store.DeleteFunctionMacro(section, p.Digits); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
