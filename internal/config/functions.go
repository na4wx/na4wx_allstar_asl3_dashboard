package config

import (
	"fmt"
	"path/filepath"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
)

// FunctionMacro is one DTMF command mapping from an app_rpt "functions"
// (or "macro") stanza in rpt.conf: the digit sequence a user dials on
// the radio (after the node's funcchar) mapped to the app_rpt command it
// runs, e.g. "1" -> "ilink,3" (a generic connect macro). Digits are only
// unique within the section they're defined in -- a node picks which
// functions/macro stanza it uses via its own fields (see NodeView.
// Functions/Macro), so callers pass that section name explicitly rather
// than assuming a fixed section.
type FunctionMacro struct {
	Digits  string
	Command string
}

// ListFunctionMacros returns a functions/macro stanza's resolved
// (template-inheritance flattened) DTMF mappings, in first-defined
// order -- same resolution discipline as ListTelemetryEntries, since a
// real node's own "functions" section inherits from a shared
// "functions-main" template the same way "telemetry" does.
func (s *Store) ListFunctionMacros(section string) ([]FunctionMacro, error) {
	rpt, err := s.loadRpt()
	if err != nil {
		return nil, err
	}
	r, err := rpt.Resolve(section)
	if err != nil {
		return nil, fmt.Errorf("config: section %q not found in rpt.conf: %w", section, err)
	}

	var order []string
	values := map[string]string{}
	for _, p := range r.Pairs {
		if _, seen := values[p.Key]; !seen {
			order = append(order, p.Key)
		}
		values[p.Key] = p.Value
	}
	out := make([]FunctionMacro, 0, len(order))
	for _, k := range order {
		out = append(out, FunctionMacro{Digits: k, Command: values[k]})
	}
	return out, nil
}

// SetFunctionMacro adds or updates one DTMF mapping, overriding whatever
// template default the section inherited for that digit (same
// per-section override mechanism as SetTelemetryEntries). Creates
// section first if it doesn't exist yet -- e.g. a node's own per-node
// scheduler/macro section, on its first use.
func (s *Store) SetFunctionMacro(section, digits, command string) error {
	path := filepath.Join(s.dir(), "rpt.conf")
	if err := s.ensureSectionExists(path, section); err != nil {
		return fmt.Errorf("config: set %s.%s: %w", section, digits, err)
	}
	if err := s.setValues(path, section, map[string]string{digits: command}); err != nil {
		return fmt.Errorf("config: set %s.%s: %w", section, digits, err)
	}
	return nil
}

// ensureSectionExists creates section in path (a brand-new, empty
// section) if it isn't already there.
func (s *Store) ensureSectionExists(path, section string) error {
	exists, err := asteriskconf.SectionExists(path, section)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.createSection(path, section, nil, nil)
}

// DeleteFunctionMacro removes one DTMF mapping from section's own body.
// If digits was only ever inherited from a template (never overridden
// on this section directly), there's nothing to remove here -- see
// asteriskconf.RemoveValue's own doc comment: a no-op, not an error.
func (s *Store) DeleteFunctionMacro(section, digits string) error {
	if err := s.removeValue(filepath.Join(s.dir(), "rpt.conf"), section, digits); err != nil {
		return fmt.Errorf("config: delete %s.%s: %w", section, digits, err)
	}
	return nil
}
