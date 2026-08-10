// WX courtesy-tone actions -- the cloud equivalent of the local
// SkywarnPlus tab's own WX courtesy tone half (see internal/server/wxtone.go).
// Reads/writes the same wxtone.Store instance the device's own local WX-
// tone poller reads (see cmd/asl3-gui/main.go's StartWXTonePoller), so a
// mapping saved here takes effect on its own next poll tick, no restart.
package cloudagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"hamvoipconfiggui-asl3/internal/wxtone"
)

type wxToneListParams struct {
	Node string `json:"node"`
}

// actionWXToneList wraps wxtone.Store.ListForNode.
func (a *Agent) actionWXToneList(_ context.Context, params json.RawMessage) (any, error) {
	var p wxToneListParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	entries, err := a.wxTones.ListForNode(p.Node)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []wxtone.Entry{}
	}
	return entries, nil
}

// actionWXToneSave wraps wxtone.Store.Save, params decoded directly as a
// wxtone.Entry -- the client's own save mutation never sends an id
// (every save creates a new entry; editing is delete-then-recreate, see
// WXToneSection.tsx), so the ID is always generated fresh here, the same
// way Store.Save's own blank-ID branch would, but done up front so the
// generated ID/defaulted Mode can be returned in the response (Save
// itself only reports success/failure, not the entry it produced).
func (a *Agent) actionWXToneSave(_ context.Context, params json.RawMessage) (any, error) {
	var e wxtone.Entry
	if err := json.Unmarshal(params, &e); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	id, err := wxToneID()
	if err != nil {
		return nil, err
	}
	e.ID = id
	if e.Mode == "" {
		e.Mode = wxtone.ModeNormal
	}
	if err := a.wxTones.Save(e); err != nil {
		return nil, err
	}
	return e, nil
}

func wxToneID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type wxToneDeleteParams struct {
	ID string `json:"id"`
}

// actionWXToneDelete wraps wxtone.Store.Delete.
func (a *Agent) actionWXToneDelete(_ context.Context, params json.RawMessage) (any, error) {
	var p wxToneDeleteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	if err := a.wxTones.Delete(p.ID); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
