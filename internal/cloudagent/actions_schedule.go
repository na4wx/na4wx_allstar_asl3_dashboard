// Native app_rpt connect/disconnect scheduler actions -- the cloud
// equivalent of the local Scheduler tab's own "Scheduled connections"
// half (see internal/server/automation.go). Both that file and this one
// call straight through to internal/automation's shared
// BuildRows/SaveConnection/DeleteConnection, so the actual algorithm
// exists in exactly one place -- see that package's own doc comment.
package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/automation"
)

type scheduleListParams struct {
	Number string `json:"number"`
}

// actionScheduleList wraps automation.BuildRows.
func (a *Agent) actionScheduleList(_ context.Context, params json.RawMessage) (any, error) {
	var p scheduleListParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	view, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	rows, err := automation.BuildRows(a.store, view)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []automation.Row{}
	}
	return rows, nil
}

type scheduleSaveConnectionParams struct {
	Number   string   `json:"number"`
	Action   string   `json:"action"`
	Target   string   `json:"target"`
	Minute   string   `json:"minute"`
	Hour     string   `json:"hour"`
	Dom      string   `json:"dom"`
	Month    string   `json:"month"`
	Weekdays []string `json:"weekdays"`
}

// actionScheduleSaveConnection wraps automation.SaveConnection.
func (a *Agent) actionScheduleSaveConnection(_ context.Context, params json.RawMessage) (any, error) {
	var p scheduleSaveConnectionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	view, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	err = automation.SaveConnection(a.store, view, automation.SaveConnectionParams{
		ActionKey:  p.Action,
		Target:     p.Target,
		Minute:     p.Minute,
		Hour:       p.Hour,
		DayOfMonth: p.Dom,
		Month:      p.Month,
		Weekdays:   p.Weekdays,
	})
	if err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

type scheduleDeleteConnectionParams struct {
	Number   string `json:"number"`
	MacroNum string `json:"macroNum"`
}

// actionScheduleDeleteConnection wraps automation.DeleteConnection.
func (a *Agent) actionScheduleDeleteConnection(_ context.Context, params json.RawMessage) (any, error) {
	var p scheduleDeleteConnectionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	view, err := a.store.LoadNode(p.Number)
	if err != nil {
		return nil, err
	}
	if err := automation.DeleteConnection(a.store, view, p.MacroNum); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
