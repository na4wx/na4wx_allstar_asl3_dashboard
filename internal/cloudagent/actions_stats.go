package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/rptstatus"
	"hamvoipconfiggui-asl3/internal/system"
)

type nodeStatsParams struct {
	Number string `json:"number"`
}

// nodeStatsResult is a live-only counterpart to a local "Node N stats"
// view -- the full "rpt stats" field dump plus who's connected right
// now. Deliberately has no history array (unlike a background-poller-fed
// link history) -- this package has no *Server to borrow that poller/
// buffer from without an import cycle, and giving every connected
// device's own cloudagent an unconditional polling loop just for an
// optional history view is a real, permanent resource cost this pass
// isn't taking on. snapshotLiveNode (live.go) already covers Receiving/
// Connected for the live-watch case; this action additionally surfaces
// the full field table, which that one doesn't.
type nodeStatsResult struct {
	StatsOK   bool                      `json:"statsOk"`
	Stats     []rptstatus.StatField     `json:"stats"`
	Receiving bool                      `json:"receiving"`
	Connected []rptstatus.ConnectedNode `json:"connected"`
}

// actionSystemNodeStats runs "rpt stats <number>" and "rpt nodes
// <number>" directly against this device's own Asterisk instance.
func (a *Agent) actionSystemNodeStats(ctx context.Context, params json.RawMessage) (any, error) {
	var p nodeStatsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}

	if !a.validNodeNumber(p.Number) {
		return nil, fmt.Errorf("node %s not found", p.Number)
	}

	result := nodeStatsResult{Connected: []rptstatus.ConnectedNode{}}
	statsOut, err := system.AsteriskRX(ctx, a.asteriskBin, "rpt stats "+p.Number)
	if err != nil {
		return nil, fmt.Errorf("could not read node status: %w", err)
	}
	result.Stats, result.StatsOK = rptstatus.ParseRptStats(statsOut)
	result.Receiving = rptstatus.NodeReceiving(result.Stats)

	nodesOut, _ := system.AsteriskRX(ctx, a.asteriskBin, "rpt nodes "+p.Number)
	for _, num := range rptstatus.ParseConnectedNodes(nodesOut) {
		result.Connected = append(result.Connected, rptstatus.DescribeNode(nil, num))
	}
	if len(result.Connected) > 0 {
		if out, err := system.AsteriskRX(ctx, a.asteriskBin, "rpt show variables "+p.Number); err == nil {
			keyed := rptstatus.KeyedNodes(out)
			for i := range result.Connected {
				if keyed[result.Connected[i].Number] {
					result.Connected[i].Keyed = true
				}
			}
		}
	}

	return result, nil
}
