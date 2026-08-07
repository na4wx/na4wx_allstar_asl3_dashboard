package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/config"
)

// requireRawConfigEdit gates every rawconfig.* action (reads included --
// this is this app's single most powerful, least-guarded capability, so
// it gets its own explicit opt-in on top of Cloud Sync itself, same as
// the local raw-config-editor page's own AllowedRawConfigFiles gate).
func (a *Agent) requireRawConfigEdit() error {
	settings, err := a.settings.Load()
	if err != nil {
		return err
	}
	if !settings.AllowRawConfigEdit {
		return errCapabilityDisabled("Remote raw config editing")
	}
	return nil
}

// rawConfigKV is one section's key/value line, JSON-friendly.
type rawConfigKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type rawConfigSection struct {
	Name string        `json:"name"`
	Keys []rawConfigKV `json:"keys"`
}

type rawConfigFileResult struct {
	Sections []rawConfigSection `json:"sections"`
}

// actionRawConfigListFiles wraps config.AllowedRawConfigFiles -- the
// list itself isn't sensitive (it's just file names), so this one is
// not gated behind AllowRawConfigEdit; the cloud UI needs it to know
// what to even offer before the operator has necessarily turned the
// capability on.
func (a *Agent) actionRawConfigListFiles(_ context.Context, _ json.RawMessage) (any, error) {
	return config.AllowedRawConfigFiles, nil
}

type rawConfigFileParams struct {
	File string `json:"file"`
}

// actionRawConfigGetFile wraps config.Store.RawSections, returning every
// section's key/value pairs in file order -- the same shape
// internal/server's own /config page builds for its template.
func (a *Agent) actionRawConfigGetFile(_ context.Context, params json.RawMessage) (any, error) {
	if err := a.requireRawConfigEdit(); err != nil {
		return nil, err
	}
	var p rawConfigFileParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if !config.IsAllowedRawConfigFile(p.File) {
		return nil, fmt.Errorf("%q is not one of this app's editable config files", p.File)
	}
	sections, err := a.store.RawSections(p.File)
	if err != nil {
		return nil, err
	}
	// Both Sections and each section's Keys stay non-nil even with zero
	// entries -- a nil Go slice marshals to JSON null, and the browser
	// expects to always call .length/.map on both (see live.go's
	// snapshotLiveNode for the exact same bug, already found and fixed
	// once in this package).
	result := rawConfigFileResult{Sections: []rawConfigSection{}}
	for _, sec := range sections {
		keys := []rawConfigKV{}
		for _, pair := range sec.Pairs {
			keys = append(keys, rawConfigKV{Key: pair.Key, Value: pair.Value})
		}
		result.Sections = append(result.Sections, rawConfigSection{Name: sec.Name, Keys: keys})
	}
	return result, nil
}

type rawConfigSetKeyParams struct {
	File    string `json:"file"`
	Section string `json:"section"`
	Index   int    `json:"index"`
	Value   string `json:"value"`
}

// actionRawConfigSetKey wraps config.Store.SetRawKey -- addresses one
// key/value line by its position within the section (see
// SetRawKey's own doc comment for why position rather than key name),
// matching internal/server's own raw config editor.
func (a *Agent) actionRawConfigSetKey(_ context.Context, params json.RawMessage) (any, error) {
	if err := a.requireRawConfigEdit(); err != nil {
		return nil, err
	}
	var p rawConfigSetKeyParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if !config.IsAllowedRawConfigFile(p.File) {
		return nil, fmt.Errorf("%q is not one of this app's editable config files", p.File)
	}
	ok, err := a.store.SetRawKey(p.File, p.Section, p.Index, p.Value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no key at position %d in section %q", p.Index, p.Section)
	}
	return map[string]bool{"ok": true}, nil
}

type rawConfigAddKeyParams struct {
	File    string `json:"file"`
	Section string `json:"section"`
	Key     string `json:"key"`
	Value   string `json:"value"`
}

// actionRawConfigAddKey wraps config.Store.AddRawKey (adds if absent,
// overwrites in place if the key already exists in that section).
func (a *Agent) actionRawConfigAddKey(_ context.Context, params json.RawMessage) (any, error) {
	if err := a.requireRawConfigEdit(); err != nil {
		return nil, err
	}
	var p rawConfigAddKeyParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if !config.IsAllowedRawConfigFile(p.File) {
		return nil, fmt.Errorf("%q is not one of this app's editable config files", p.File)
	}
	if err := a.store.AddRawKey(p.File, p.Section, p.Key, p.Value); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

type rawConfigAddSectionParams struct {
	File    string `json:"file"`
	Section string `json:"section"`
}

// actionRawConfigAddSection wraps config.Store.AddRawSection.
func (a *Agent) actionRawConfigAddSection(_ context.Context, params json.RawMessage) (any, error) {
	if err := a.requireRawConfigEdit(); err != nil {
		return nil, err
	}
	var p rawConfigAddSectionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if !config.IsAllowedRawConfigFile(p.File) {
		return nil, fmt.Errorf("%q is not one of this app's editable config files", p.File)
	}
	if err := a.store.AddRawSection(p.File, p.Section); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
