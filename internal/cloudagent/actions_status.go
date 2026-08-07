package cloudagent

import (
	"context"
	"encoding/json"

	"hamvoipconfiggui-asl3/internal/system"
)

// actionSystemStatus reports this device's overall Asterisk/system
// status — the read-only heartbeat action.
func (a *Agent) actionSystemStatus(ctx context.Context, _ json.RawMessage) (any, error) {
	return system.Snapshot(ctx, a.asteriskBin), nil
}
