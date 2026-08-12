package relay

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"hamvoipconfiggui-asl3/internal/asteriskconf"
	"hamvoipconfiggui-asl3/internal/system"
)

// relayReconcileInterval is how often Manager re-applies its
// last-known-good Grant to the backend — a self-heal against drift
// (e.g. the interface having been lost across a host reboot) rather
// than anything time-critical; a fresh helloAck grant (see ApplyGrant's
// own callers in client.go) is what actually drives this most of the
// time.
const relayReconcileInterval = 5 * time.Minute

// Manager owns this node's relay tunnel state: applying a Grant handed
// back by the cloud (bringing the WireGuard interface up, pointing
// chan_iax2's bindaddr at the tunnel IP via iax2.conf, and reloading the
// module so it takes effect), and reversing all of that when the
// operator disables the feature locally. Bringing chan_iax2's own
// registration and connection traffic through the tunnel is what
// actually makes this work — see AllStarLink's own directory model
// (package doc comment) for why nothing less than that suffices.
type Manager struct {
	mu           sync.Mutex
	backend      Backend
	settings     *SettingsStore
	asteriskDir  string
	asteriskBin  string
	currentGrant Grant
	applied      bool
}

// NewManager builds a Manager with backend = unavailableBackend{} --
// SetBackend swaps in the real detected backend later (see
// (*server.Server).StartRelayManager), so constructing a Manager never
// shells out, matching internal/wifi.NewManager's own contract.
// asteriskDir/asteriskBin are the same values already threaded through
// server.New for every other Asterisk-config-touching feature.
func NewManager(settings *SettingsStore, asteriskDir, asteriskBin string) *Manager {
	return &Manager{
		backend:     unavailableBackend{},
		settings:    settings,
		asteriskDir: asteriskDir,
		asteriskBin: asteriskBin,
	}
}

func (m *Manager) SetBackend(b Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backend = b
}

func (m *Manager) Backend() Backend {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backend
}

// Settings exposes the settings store so internal/server's relay
// handlers can read/write it without this package depending on
// net/http, same pattern as cloudagent.Agent.Settings.
func (m *Manager) Settings() *SettingsStore { return m.settings }

// Status reports whether a grant is currently applied, and what it is
// -- for the System page's relay status card. The zero Grant when
// active is false.
func (m *Manager) Status() (active bool, grant Grant) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applied, m.currentGrant
}

// PublicKeyForHello returns this device's WireGuard public key to send
// in the cloudagent hello envelope, generating and persisting a keypair
// on first use if relay is enabled locally but no keypair exists yet
// (see Settings.PrivateKey's own doc comment for why it's never
// regenerated after that). The second return value is false when relay
// is disabled locally, in which case the caller must not send
// RelayPublicKey at all.
func (m *Manager) PublicKeyForHello(ctx context.Context) (string, bool, error) {
	settings, err := m.settings.Load()
	if err != nil {
		return "", false, err
	}
	if !settings.Enabled {
		return "", false, nil
	}
	if settings.PrivateKey == "" || settings.PublicKey == "" {
		priv, pub, err := GenerateKeypair(ctx)
		if err != nil {
			return "", false, err
		}
		settings.PrivateKey = priv
		settings.PublicKey = pub
		if err := m.settings.Save(settings); err != nil {
			return "", false, err
		}
	}
	return settings.PublicKey, true, nil
}

func (m *Manager) iax2ConfPath() string {
	return filepath.Join(m.asteriskDir, "iax2.conf")
}

// ApplyGrant brings the relay tunnel up (or updates it in place) using
// grant, points chan_iax2 at the tunnel IP, and reloads it -- called
// from client.go whenever a fresh helloAck carries a Relay grant, and
// from Run's own periodic reconcile to self-heal drift. Idempotent:
// re-applying the same grant is cheap and safe.
func (m *Manager) ApplyGrant(ctx context.Context, grant Grant) error {
	m.mu.Lock()
	backend := m.backend
	m.mu.Unlock()

	settings, err := m.settings.Load()
	if err != nil {
		return fmt.Errorf("loading relay settings: %w", err)
	}
	if settings.PrivateKey == "" {
		return fmt.Errorf("no local wireguard keypair -- relay must be enabled locally before a grant can be applied")
	}

	if err := backend.ApplyTunnel(ctx, settings.PrivateKey, grant); err != nil {
		return fmt.Errorf("applying wireguard tunnel: %w", err)
	}
	if err := asteriskconf.SetValues(m.iax2ConfPath(), "general", map[string]string{"bindaddr": grant.TunnelIP}); err != nil {
		return fmt.Errorf("writing iax2.conf bindaddr: %w", err)
	}
	if err := system.AsteriskReloadIax2(ctx, m.asteriskBin); err != nil {
		return fmt.Errorf("reloading chan_iax2: %w", err)
	}

	m.mu.Lock()
	m.currentGrant = grant
	m.applied = true
	m.mu.Unlock()
	return nil
}

// Disable reverses ApplyGrant: tears down the tunnel interface, resets
// chan_iax2's bindaddr back to listening on every interface, and
// reloads it. A no-op if nothing was ever applied. Called from the
// System page's explicit "disable relay" action, and whenever the
// operator turns the Settings toggle off (see internal/server's relay
// handler) -- this is a purely local action, independent of whatever
// the cloud side still thinks, matching how internal/cloudagent's own
// Reload always cuts a live connection immediately rather than waiting
// for the cloud to agree.
func (m *Manager) Disable(ctx context.Context) error {
	m.mu.Lock()
	backend := m.backend
	applied := m.applied
	m.mu.Unlock()

	if !applied {
		return nil
	}

	if err := backend.TeardownInterface(ctx); err != nil {
		return fmt.Errorf("tearing down relay interface: %w", err)
	}
	if err := asteriskconf.SetValues(m.iax2ConfPath(), "general", map[string]string{"bindaddr": "0.0.0.0"}); err != nil {
		return fmt.Errorf("resetting iax2.conf bindaddr: %w", err)
	}
	if err := system.AsteriskReloadIax2(ctx, m.asteriskBin); err != nil {
		return fmt.Errorf("reloading chan_iax2: %w", err)
	}

	m.mu.Lock()
	m.applied = false
	m.currentGrant = Grant{}
	m.mu.Unlock()
	return nil
}

// Run blocks until ctx is cancelled, periodically re-applying the
// current grant (if any) as a self-heal against drift -- same "single
// supervised goroutine for the life of the process" shape as
// internal/wifi's own Manager.Run. Started once from
// (*server.Server).StartRelayManager.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(relayReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			applied := m.applied
			grant := m.currentGrant
			m.mu.Unlock()
			if !applied {
				continue
			}
			if err := m.ApplyGrant(ctx, grant); err != nil {
				log.Printf("relay: reconcile failed: %v", err)
			}
		}
	}
}
