package relay

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// relayReconcileInterval is how often Manager re-applies its
// last-known-good Grant to the backend — a self-heal against drift
// (e.g. the interface having been lost across a host reboot) rather
// than anything time-critical; a fresh helloAck grant (see ApplyGrant's
// own callers in client.go) is what actually drives this most of the
// time.
const relayReconcileInterval = 5 * time.Minute

// Manager owns this node's relay tunnel state: applying a Grant handed
// back by the cloud (bringing the WireGuard interface up and routing
// Asterisk's non-IAX2 outbound traffic through it — see wgctl.go's
// applyPolicyRouting for exactly what that covers and why), and
// reversing all of that when the operator disables the feature
// locally. Deliberately does NOT touch chan_iax2's own bindaddr (an
// earlier version of this pointed iax.conf's bindaddr at the tunnel IP
// — removed after confirming on real hardware that it broke ordinary
// outbound dialing to other nodes: binding chan_iax2's own socket to
// the tunnel's private IP couples ALL of its traffic, not just
// registration, to the tunnel, with no way to selectively exempt normal
// calls at that layer. chan_iax2 listening on its default 0.0.0.0
// receives inbound tunnel traffic fine on its own, but *replying* to it
// needed its own fix too, since a wildcard-bound socket doesn't
// automatically send a reply back out the interface a request arrived
// on — see wgctl.go's replyFwMark for the CONNMARK-based routing that
// makes that work). It does, however, own iax.conf's bindport — see
// iax2bindport.go's own doc comment for why that one has to change:
// AllStarLink's own directory dials whatever port is set on the node's
// own profile at allstarlink.org (confirmed on a real deployment — not
// anything self-reported over the wire, an earlier theory that turned
// out to be wrong), so chan_iax2 has to actually be listening on that
// same port for a real inbound call to land anywhere.
type Manager struct {
	mu           sync.Mutex
	backend      Backend
	iax2         Iax2Configurer
	settings     *SettingsStore
	currentGrant Grant
	applied      bool
}

// NewManager builds a Manager with backend = unavailableBackend{} --
// SetBackend swaps in the real detected backend later (see
// (*server.Server).StartRelayManager), so constructing a Manager never
// shells out, matching internal/wifi.NewManager's own contract.
// iaxConfPath is the node's own iax.conf (ordinarily
// /etc/asterisk/iax.conf); pass "" in tests that never apply a Grant
// with a nonzero ExternalPort, since ApplyBindport is skipped entirely
// in that case.
func NewManager(settings *SettingsStore, iaxConfPath string) *Manager {
	return &Manager{
		backend:  unavailableBackend{},
		iax2:     &realIax2Configurer{iaxConfPath: iaxConfPath, settings: settings},
		settings: settings,
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

// SetIax2Configurer overrides the default iax.conf bindport handling —
// exposed only for tests (see manager_test.go's fakeIax2Configurer);
// production code always uses the realIax2Configurer NewManager builds.
func (m *Manager) SetIax2Configurer(c Iax2Configurer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.iax2 = c
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

// ApplyGrant brings the relay tunnel up (or updates it in place) using
// grant -- called from client.go whenever a fresh helloAck carries a
// Relay grant, and from Run's own periodic reconcile to self-heal
// drift. Idempotent: re-applying the same grant is cheap and safe.
func (m *Manager) ApplyGrant(ctx context.Context, grant Grant) error {
	m.mu.Lock()
	backend := m.backend
	iax2 := m.iax2
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

	if grant.ExternalPort != 0 {
		if err := iax2.ApplyBindport(ctx, grant.ExternalPort); err != nil {
			return fmt.Errorf("configuring iax2's bindport for the relay: %w", err)
		}
	}

	m.mu.Lock()
	m.currentGrant = grant
	m.applied = true
	m.mu.Unlock()
	return nil
}

// Disable reverses ApplyGrant: tears down the tunnel interface and its
// policy-routing rules. A no-op if nothing was ever applied. Called
// from the System page's explicit "disable relay" action, and whenever
// the operator turns the Settings toggle off (see internal/server's
// relay handler) -- this is a purely local action, independent of
// whatever the cloud side still thinks, matching how
// internal/cloudagent's own Reload always cuts a live connection
// immediately rather than waiting for the cloud to agree.
func (m *Manager) Disable(ctx context.Context) error {
	m.mu.Lock()
	backend := m.backend
	iax2 := m.iax2
	applied := m.applied
	m.mu.Unlock()

	if !applied {
		return nil
	}

	if err := backend.TeardownInterface(ctx); err != nil {
		return fmt.Errorf("tearing down relay interface: %w", err)
	}

	if err := iax2.RestoreBindport(ctx); err != nil {
		return fmt.Errorf("restoring iax2's bindport: %w", err)
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
