package config

import (
	"fmt"
	"path/filepath"
)

// ScheduleEntry is one row of an app_rpt "schedule" stanza in rpt.conf --
// app_rpt's own native cron-like mechanism. The key is a macro number
// (referencing an entry in the node's own "macro" stanza -- see
// FunctionMacro/NodeView.Macro); Asterisk itself dials that macro's DTMF
// value when the current time matches TimeSpec, entirely on its own, no
// external process required. TimeSpec is "MM HH DayOfMonth MonthOfYear
// DayOfWeek" using only single numeric values or "*" wildcards --
// app_rpt's own docs are explicit that this is not real cron syntax: no
// ranges, lists, or step values are supported.
type ScheduleEntry struct {
	MacroNum string
	TimeSpec string
}

// ListScheduleEntries returns a schedule stanza's resolved entries, in
// first-defined order -- same resolution discipline as
// ListFunctionMacros/ListTelemetryEntries.
func (s *Store) ListScheduleEntries(section string) ([]ScheduleEntry, error) {
	rpt, err := s.loadRpt()
	if err != nil {
		return nil, err
	}
	r, err := rpt.Resolve(section)
	if err != nil {
		return nil, fmt.Errorf("config: schedule section %q not found in rpt.conf: %w", section, err)
	}

	var order []string
	values := map[string]string{}
	for _, p := range r.Pairs {
		if _, seen := values[p.Key]; !seen {
			order = append(order, p.Key)
		}
		values[p.Key] = p.Value
	}
	out := make([]ScheduleEntry, 0, len(order))
	for _, k := range order {
		out = append(out, ScheduleEntry{MacroNum: k, TimeSpec: values[k]})
	}
	return out, nil
}

// SetScheduleEntry adds or updates one schedule entry, creating section
// first if it doesn't exist yet (a node's own per-node scheduler
// section, on its first scheduled connection).
func (s *Store) SetScheduleEntry(section, macroNum, timeSpec string) error {
	path := filepath.Join(s.dir(), "rpt.conf")
	if err := s.ensureSectionExists(path, section); err != nil {
		return fmt.Errorf("config: set schedule entry %s.%s: %w", section, macroNum, err)
	}
	if err := s.setValues(path, section, map[string]string{macroNum: timeSpec}); err != nil {
		return fmt.Errorf("config: set schedule entry %s.%s: %w", section, macroNum, err)
	}
	return nil
}

// DeleteScheduleEntry removes one schedule entry from section's own
// body.
func (s *Store) DeleteScheduleEntry(section, macroNum string) error {
	if err := s.removeValue(filepath.Join(s.dir(), "rpt.conf"), section, macroNum); err != nil {
		return fmt.Errorf("config: delete schedule entry %s.%s: %w", section, macroNum, err)
	}
	return nil
}

// SetNodeScheduler updates just the "scheduler" key on node's own
// rpt.conf section -- the narrow-update counterpart to
// UpdateNodeSettings, used to self-heal a blank/shared
// NodeView.Scheduler to a correctly-named per-node section the first
// time a scheduled connection is saved for it, without risking any
// other field on the node's stanza.
func (s *Store) SetNodeScheduler(node, section string) error {
	if err := s.setValues(filepath.Join(s.dir(), "rpt.conf"), node, map[string]string{"scheduler": section}); err != nil {
		return fmt.Errorf("config: set scheduler for node %q: %w", node, err)
	}
	return nil
}
