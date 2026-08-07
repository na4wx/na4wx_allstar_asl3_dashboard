// Registration actions -- ASL3's replacement for the original HamVoIP
// app's actions_iax.go: ASL3 uses HTTP registration
// (rpt_http_registrations.conf), not IAX2 peers, so there's no separate
// "peer" concept to save alongside a registration here.
package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hamvoipconfiggui-asl3/internal/config"
)

// registrationDefaultServer mirrors node_edit.html's own pre-filled
// default -- a registration saved through the cloud with a blank server
// ends up configured identically to one saved locally.
const registrationDefaultServer = "register.allstarlink.org"

type registrationLoadParams struct {
	Number string `json:"number"`
}

// actionRegistrationLoad wraps config.Store.ListRegistrations, returning
// just the one entry matching Number (or null if this node has never
// been registered) -- same lookup internal/server's own
// loadNodeEditData does.
func (a *Agent) actionRegistrationLoad(_ context.Context, params json.RawMessage) (any, error) {
	var p registrationLoadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	regs, err := a.store.ListRegistrations()
	if err != nil {
		return nil, err
	}
	for _, r := range regs {
		if r.Node == p.Number {
			return r, nil
		}
	}
	return nil, nil
}

type registrationSaveParams struct {
	Number   string `json:"number"`
	Password string `json:"password"`
	Server   string `json:"server"`
}

// actionRegistrationSave wraps config.Store.SetRegistration, applying
// the same default server node_edit.html's own registration form does
// when left blank.
func (a *Agent) actionRegistrationSave(_ context.Context, params json.RawMessage) (any, error) {
	var p registrationSaveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if strings.TrimSpace(p.Password) == "" {
		return nil, fmt.Errorf("a registration password is required")
	}
	server := strings.TrimSpace(p.Server)
	if server == "" {
		server = registrationDefaultServer
	}
	reg := config.Registration{Node: p.Number, Password: p.Password, Server: server}
	if err := a.store.SetRegistration(reg); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

type registrationRemoveParams struct {
	Number string `json:"number"`
}

// actionRegistrationRemove wraps config.Store.RemoveRegistration.
func (a *Agent) actionRegistrationRemove(_ context.Context, params json.RawMessage) (any, error) {
	var p registrationRemoveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if err := a.store.RemoveRegistration(p.Number); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
