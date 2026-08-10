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
//   - config.cloneNodeConfig/applyStandardCommandSet/normalizeNodeConfig/
//     recreateNodeDevice/syncExtensions -- HamVoIP's per-node private
//     command-set sections and shared-named-radio-device model; ASL3
//     nodes share one functions/macro/telemetry section by default and
//     store tuning directly on the node, so none of these apply
//   - config.listRadioDevices/loadRadioDevice/saveRadioDevice/
//     deleteRadioDevice -- same reason; already covered by
//     config.loadNode/config.updateRadioSettings here
//
// config.listFunctionMacros/saveFunctionMacro/deleteFunctionMacro,
// schedule.*, and wxTone.* were all previously omitted with the same
// reasoning as above, but that turned out to be wrong -- ASL3 does have
// all three (internal/config's own FunctionMacro support, internal/
// automation, internal/wxtone), they just hadn't been wired into this
// registry yet. Confirmed the hard way: the cloud dashboard's Commands/
// Scheduler/WX-tone sections all came back "unknown action" until these
// were added.
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
		"config.listFunctionMacros":  a.actionConfigListFunctionMacros,
		"config.saveFunctionMacro":   a.actionConfigSaveFunctionMacro,
		"config.deleteFunctionMacro": a.actionConfigDeleteFunctionMacro,

		"registration.load":   a.actionRegistrationLoad,
		"registration.save":   a.actionRegistrationSave,
		"registration.remove": a.actionRegistrationRemove,

		"soundSchedule.list":   a.actionSoundScheduleList,
		"soundSchedule.save":   a.actionSoundScheduleSave,
		"soundSchedule.delete": a.actionSoundScheduleDelete,

		"schedule.list":             a.actionScheduleList,
		"schedule.saveConnection":   a.actionScheduleSaveConnection,
		"schedule.deleteConnection": a.actionScheduleDeleteConnection,

		"wxTone.list":   a.actionWXToneList,
		"wxTone.save":   a.actionWXToneSave,
		"wxTone.delete": a.actionWXToneDelete,

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
