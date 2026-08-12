package relay

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
	"hamvoipconfiggui-asl3/internal/system"
)

// Iax2Configurer applies (or reverts) chan_iax2's own bindport to match
// the relay's cloud-assigned external port -- a separate concern from
// Backend, which only ever touches the WireGuard interface itself (see
// wgctl.go). AllStarLink's HTTP-based registration self-reports
// whatever port.conf's own [general] bindport is currently set to (see
// res_rpt_http_registrations.c's get_bindport(), confirmed against a
// real deployment: `rpt show registrations`'s own Perceived field is
// exactly this self-reported value, not anything actually observed from
// the live connection) -- and inbound AllStarLink calls dial that same
// self-reported port, not chan_iax2's own default 4569, so the two have
// to match the cloud's DNAT rule for this device or inbound dial-in
// fails silently even though registration itself looks entirely
// healthy. Real implementation is realIax2Configurer below; tests
// inject a fake via SetIax2Configurer, the same shape as
// SetBackend/Backend.
type Iax2Configurer interface {
	// ApplyBindport ensures iax.conf's bindport matches port,
	// restarting Asterisk if a change was actually needed (chan_iax2
	// ignores a bindport change on `iax2 reload` -- confirmed against a
	// real node's own log line, "Ignoring bindport on reload" -- so
	// only a full process restart picks it up). A no-op, no restart, if
	// it already matches.
	ApplyBindport(ctx context.Context, port int) error
	// RestoreBindport reverts a previous ApplyBindport back to
	// whatever iax.conf's bindport held before this package first
	// touched it, restarting Asterisk if a change was actually needed.
	// A no-op if ApplyBindport was never called (or has already been
	// reverted).
	RestoreBindport(ctx context.Context) error
}

// realIax2Configurer is the production Iax2Configurer, editing
// iaxConfPath directly (see internal/asteriskconf) and restarting
// Asterisk via internal/system.
type realIax2Configurer struct {
	iaxConfPath string
	settings    *SettingsStore
}

const iaxGeneralSection = "general"
const iaxBindportKey = "bindport"

func readIaxBindport(path string) (value string, present bool, err error) {
	f, err := asteriskconf.Load(path)
	if err != nil {
		return "", false, err
	}
	sec, ok := f.Section(iaxGeneralSection)
	if !ok {
		return "", false, nil
	}
	value, present = sec.Value(iaxBindportKey)
	return value, present, nil
}

func (c *realIax2Configurer) ApplyBindport(ctx context.Context, port int) error {
	if c.iaxConfPath == "" {
		return fmt.Errorf("relay: no iax.conf path configured")
	}
	desired := strconv.Itoa(port)

	current, present, err := readIaxBindport(c.iaxConfPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", c.iaxConfPath, err)
	}
	if present && current == desired {
		return nil
	}

	settings, err := c.settings.Load()
	if err != nil {
		return fmt.Errorf("loading relay settings: %w", err)
	}
	if !settings.BindportOverridden {
		settings.BindportOverridden = true
		// current is "" when present is false, which OriginalBindport
		// also uses as its own "the key was absent" sentinel -- see
		// RestoreBindport.
		settings.OriginalBindport = current
		if err := c.settings.Save(settings); err != nil {
			return fmt.Errorf("saving relay settings: %w", err)
		}
	}

	if err := asteriskconf.SetValues(c.iaxConfPath, iaxGeneralSection, map[string]string{iaxBindportKey: desired}); err != nil {
		return fmt.Errorf("writing %s: %w", c.iaxConfPath, err)
	}
	log.Printf("relay: set iax2 bindport to %s (was %q) to match the cloud-assigned external port -- restarting asterisk, since a bindport change is ignored on reload", desired, current)
	if err := system.AsteriskRestart(ctx); err != nil {
		return fmt.Errorf("restarting asterisk: %w", err)
	}
	return nil
}

func (c *realIax2Configurer) RestoreBindport(ctx context.Context) error {
	if c.iaxConfPath == "" {
		return nil
	}
	settings, err := c.settings.Load()
	if err != nil {
		return fmt.Errorf("loading relay settings: %w", err)
	}
	if !settings.BindportOverridden {
		return nil
	}

	if settings.OriginalBindport == "" {
		if err := asteriskconf.RemoveValue(c.iaxConfPath, iaxGeneralSection, iaxBindportKey); err != nil {
			return fmt.Errorf("removing %s bindport override: %w", c.iaxConfPath, err)
		}
	} else if err := asteriskconf.SetValues(c.iaxConfPath, iaxGeneralSection, map[string]string{iaxBindportKey: settings.OriginalBindport}); err != nil {
		return fmt.Errorf("restoring %s bindport: %w", c.iaxConfPath, err)
	}

	settings.BindportOverridden = false
	settings.OriginalBindport = ""
	if err := c.settings.Save(settings); err != nil {
		return fmt.Errorf("saving relay settings: %w", err)
	}

	log.Printf("relay: restored iax2 bindport -- restarting asterisk, since a bindport change is ignored on reload")
	return system.AsteriskRestart(ctx)
}
