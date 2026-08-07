package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// actionFunc is one relayed action's implementation: decode params,
// call exactly one specific internal/* function, and return its result
// (marshaled back to the cloud as Data) or an error.
type actionFunc func(ctx context.Context, params json.RawMessage) (any, error)

// actions returns this Agent's action registry -- a fixed map literal,
// never built via reflection or any other "call this internal method by
// name" dynamic dispatch. Each entry is individually written out and
// reviewed; a relayed action name that isn't a key here can never reach
// any internal/* call. See this package's doc comment (run.go) for why
// that property matters.
//
// Narrower than the original HamVoIP app's own registry -- the
// following action families exist there but have no ASL3 counterpart
// here, deliberately omitted rather than faked against a config model
// ASL3 doesn't have:
//   - config.listFunctionMacros/saveFunctionMacro/deleteFunctionMacro --
//     no DTMF function/macro editing built in ASL3's config package yet
//   - config.cloneNodeConfig/applyStandardCommandSet/normalizeNodeConfig/
//     recreateNodeDevice/syncExtensions -- HamVoIP's per-node private
//     command-set sections and shared-named-radio-device model; ASL3
//     nodes share one functions/macro/telemetry section by default and
//     store tuning directly on the node, so none of these apply
//   - config.listRadioDevices/loadRadioDevice/saveRadioDevice/
//     deleteRadioDevice -- same reason; already covered by
//     config.loadNode/config.updateRadioSettings here
//   - schedule.* (DTMF-triggered connect/disconnect scheduling) --
//     internal/automation is ported and wired into the local UI (see
//     internal/server/automation.go), but not relayed here
//   - wxTone.* (alert-driven courtesy-tone swap) -- internal/wxtone is
//     ported and wired into the local UI (see internal/server/wxtone.go),
//     but not relayed here
func (a *Agent) actions() map[string]actionFunc {
	return map[string]actionFunc{
		"system.status":          a.actionSystemStatus,
		"system.restartAsterisk": a.actionSystemRestartAsterisk,
		"system.reboot":          a.actionSystemReboot,
		"system.dtmf":            a.actionSystemDTMF,
		"system.nodeStats":       a.actionSystemNodeStats,

		"config.listNodes":           a.actionConfigListNodes,
		"config.loadNode":            a.actionConfigLoadNode,
		"config.createNode":          a.actionConfigCreateNode,
		"config.deleteNode":          a.actionConfigDeleteNode,
		"config.updateNodeSettings":  a.actionConfigUpdateNodeSettings,
		"config.updateRadioSettings": a.actionConfigUpdateRadioSettings,
		"config.setCourtesyTones":    a.actionConfigSetCourtesyTones,
		"config.listTelemetry":       a.actionConfigListTelemetry,
		"config.setTelemetry":        a.actionConfigSetTelemetry,

		"registration.load":   a.actionRegistrationLoad,
		"registration.save":   a.actionRegistrationSave,
		"registration.remove": a.actionRegistrationRemove,

		"soundSchedule.list":   a.actionSoundScheduleList,
		"soundSchedule.save":   a.actionSoundScheduleSave,
		"soundSchedule.delete": a.actionSoundScheduleDelete,

		"sa818.last":    a.actionSA818Last,
		"sa818.program": a.actionSA818Program,

		"skywarn.listCounties":   a.actionSkywarnListCounties,
		"skywarn.getStatus":      a.actionSkywarnGetStatus,
		"skywarn.setToggle":      a.actionSkywarnSetToggle,
		"skywarn.setCounties":    a.actionSkywarnSetCounties,
		"skywarn.addNode":        a.actionSkywarnAddNode,
		"skywarn.setPushover":    a.actionSkywarnSetPushover,
		"skywarn.setSkyDescribe": a.actionSkywarnSetSkyDescribe,

		"sounds.listAll": a.actionSoundsListAll,
		"sounds.upload":  a.actionSoundsUpload,
		"sounds.delete":  a.actionSoundsDelete,
		"sounds.preview": a.actionSoundsPreview,

		"rawconfig.listFiles":  a.actionRawConfigListFiles,
		"rawconfig.getFile":    a.actionRawConfigGetFile,
		"rawconfig.setKey":     a.actionRawConfigSetKey,
		"rawconfig.addKey":     a.actionRawConfigAddKey,
		"rawconfig.addSection": a.actionRawConfigAddSection,
	}
}

// dispatch looks up action in the registry and runs it, turning an
// unrecognized name into an error result rather than panicking or
// silently dropping the call. Every attempt -- known or unknown action,
// success or failure -- is independently recorded via a.audit (see
// audit.go), deliberately without the params themselves: several
// actions carry secrets (a SkywarnPlus Pushover API token, etc.) that
// have no business sitting in a plaintext log file just for an audit
// trail that only needs to answer "what was asked of this device, and
// did it work" -- not "with what exact values".
func (a *Agent) dispatch(ctx context.Context, action string, params json.RawMessage) (any, error) {
	fn, ok := a.actions()[action]
	if !ok {
		err := fmt.Errorf("unknown action %q", action)
		a.audit.log(auditEntry{Time: time.Now(), Action: action, OK: false, Error: err.Error()})
		return nil, err
	}
	result, err := fn(ctx, params)
	entry := auditEntry{Time: time.Now(), Action: action, OK: err == nil}
	if err != nil {
		entry.Error = err.Error()
	}
	a.audit.log(entry)
	return result, err
}
