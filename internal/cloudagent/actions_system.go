package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/system"
)

// errCapabilityDisabled is returned by a gated action when its
// capability flag isn't on — worded for the operator, since this
// message is what ends up surfaced in the cloud UI's error toast.
func errCapabilityDisabled(what string) error {
	return fmt.Errorf("%s is disabled — turn it on in this node's own Cloud Sync settings first", what)
}

// actionSystemRestartAsterisk wraps system.AsteriskRestart, gated by
// Settings.AllowRemoteReboot (the same flag covers both — a full device
// reboot and an Asterisk-only restart are both "the radio drops
// everything it's doing," just at different severity, so they share one
// opt-in rather than being two separate checkboxes).
func (a *Agent) actionSystemRestartAsterisk(ctx context.Context, _ json.RawMessage) (any, error) {
	settings, err := a.settings.Load()
	if err != nil {
		return nil, err
	}
	if !settings.AllowRemoteReboot {
		return nil, errCapabilityDisabled("Remote restart/reboot")
	}
	if err := system.AsteriskRestart(ctx); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

// actionSystemReboot wraps system.Reboot, gated the same way as
// actionSystemRestartAsterisk.
func (a *Agent) actionSystemReboot(ctx context.Context, _ json.RawMessage) (any, error) {
	settings, err := a.settings.Load()
	if err != nil {
		return nil, err
	}
	if !settings.AllowRemoteReboot {
		return nil, errCapabilityDisabled("Remote restart/reboot")
	}
	if err := system.Reboot(ctx); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
