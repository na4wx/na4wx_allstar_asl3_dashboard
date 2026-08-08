package config

import (
	"fmt"
	"path/filepath"
)

// GetMorseIDFrequency reads section's own "idfrequency" -- the CW/Morse
// station ID's own audio tone (Hz), read from whichever [morse] section
// the node points at (see NodeView.Morse). Distinct from that same
// section's plain "frequency", which sets the tone for Morse telemetry
// (e.g. touch-tone command feedback) instead -- confirmed both live
// side by side in a real node's own [morse-main] template.
func (s *Store) GetMorseIDFrequency(section string) (string, error) {
	entries, err := s.ListTelemetryEntries(section)
	if err != nil {
		return "", fmt.Errorf("config: read morse section %q: %w", section, err)
	}
	for _, e := range entries {
		if e.Key == "idfrequency" {
			return e.Value, nil
		}
	}
	return "", nil
}

// SetMorseIDFrequency writes section's own "idfrequency" override --
// the same per-section override mechanism as SetTelemetryEntries (this
// section may be shared by other nodes, exactly like the telemetry
// section), just with its own accurate error message since a [morse]
// section isn't semantically a telemetry one.
func (s *Store) SetMorseIDFrequency(section, freq string) error {
	if err := s.setValues(filepath.Join(s.dir(), "rpt.conf"), section, map[string]string{"idfrequency": freq}); err != nil {
		return fmt.Errorf("config: update morse section %q: %w", section, err)
	}
	return nil
}
