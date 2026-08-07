package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hamvoipconfiggui-asl3/internal/sa818"
)

// sa818ProgramResult is what actionSA818Program returns -- the module's
// own success flag and raw transcript are both surfaced, mirroring
// internal/server/sa818.go's handleNodeSA818Apply (a non-nil err means
// the serial connection itself couldn't be made; ok=false with err=nil
// means the module rejected the settings -- two different failure modes
// the caller needs to tell apart).
type sa818ProgramResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}

// actionSA818Last wraps sa818.LoadLast -- the closest thing to a "read"
// this feature can offer (the module itself has no way to report back
// its currently-programmed values). A missing state file isn't an
// error, it just means nothing has been sent from this device yet, so
// the result is nil rather than an error in that case -- callers should
// treat a nil result as "no record yet", not "read failed".
func (a *Agent) actionSA818Last(_ context.Context, _ json.RawMessage) (any, error) {
	if a.sa818StatePath == "" {
		return nil, nil
	}
	last, err := sa818.LoadLast(a.sa818StatePath)
	if err != nil {
		return nil, err
	}
	if last == nil {
		// A typed nil *LastApplied boxed into `any` would be a non-nil
		// interface -- return an untyped nil explicitly so callers
		// checking result == nil get what they expect.
		return nil, nil
	}
	return last, nil
}

// actionSA818Program wraps sa818.Program (direct serial AT-command
// programming, no external tool involved), params decoded directly as
// sa818.Settings, and records the attempt via sa818.SaveLast the same
// way the local Radio tab's own handler does -- so "last sent" displays
// agree regardless of which UI triggered the send.
func (a *Agent) actionSA818Program(ctx context.Context, params json.RawMessage) (any, error) {
	var settings sa818.Settings
	if err := json.Unmarshal(params, &settings); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}

	output, ok, err := sa818.Program(ctx, a.sa818Port, settings)

	if a.sa818StatePath != "" {
		last := &sa818.LastApplied{
			Settings:  settings,
			Port:      a.sa818Port,
			AppliedAt: time.Now(),
			Success:   ok,
			Output:    output,
		}
		_ = sa818.SaveLast(a.sa818StatePath, last)
	}

	if err != nil {
		return nil, fmt.Errorf("could not reach the radio module: %w", err)
	}
	return sa818ProgramResult{OK: ok, Output: output}, nil
}
