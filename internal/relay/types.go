// Package relay lets this node accept inbound AllStarLink IAX2
// connections even behind NAT with no forwarded ports, by tunneling
// through the cloud service over WireGuard. AllStarLink's own directory
// resolves "where is node N" purely from the observed source address of
// the request that registered it, so the fix is to make that traffic
// egress through the tunnel instead of this node's real, unreachable
// address. Two separate mechanisms handle two separate registration
// paths on ASL3, both wired up here:
//   - chan_iax2 itself: bindaddr in iax.conf, see Manager's own doc
//     comment.
//   - ASL3's own HTTP-based node registration (res_rpt_http_registrations.so,
//     configured via rpt_http_registrations.conf) — a completely
//     separate code path with no bindaddr equivalent of its own
//     (confirmed directly against that module's source: it never sets
//     libcurl's CURLOPT_INTERFACE), so it's routed through the tunnel
//     instead via OS-level policy routing keyed to the asterisk user —
//     see wgctl.go's applyPolicyRouting. Missing this piece entirely
//     is why an early version of this feature could bring the tunnel
//     up and pass IAX2 call traffic through it just fine, while
//     `iax2 show registry` kept showing 0 registrations and the node
//     stayed unreachable by number from anywhere else on the network.
//
// Provisioning rides the existing cloudagent hello/helloAck handshake
// (see internal/cloudagent/protocol.go's RelayPublicKey/Relay fields)
// rather than a separate protocol — this package only owns what happens
// once a Grant has actually been handed back.
package relay

import (
	"context"
	"errors"
)

// Grant is what the cloud hands back once it has provisioned this
// device's relay slot — see internal/cloudagent's Envelope.Relay field,
// populated on a successful helloAck.
type Grant struct {
	CloudPublicKey string
	Endpoint       string // "<cloudIp>:<wgPort>" -- what this node's WireGuard client dials
	TunnelIP       string
	TunnelCIDR     string // e.g. "/24"
	ExternalHost   string // display only -- what other AllStar stations end up dialing
	ExternalPort   int
}

// Backend is the thing that actually shells out to bring the relay's
// WireGuard interface up, apply a Grant to it, or tear it down — see
// wgctl.go's real implementation and unavailableBackend below for what's
// used when wireguard-tools isn't installed.
type Backend interface {
	// Name identifies which backend this is, for display on the System
	// page.
	Name() string
	// ApplyTunnel brings the relay interface up (creating it first if
	// necessary) and configures it with privateKey and grant. Idempotent
	// — safe to call repeatedly, including with an unchanged Grant, so
	// Manager's periodic reconcile can just re-apply the current state
	// rather than diffing it first.
	ApplyTunnel(ctx context.Context, privateKey string, grant Grant) error
	// TeardownInterface removes the relay interface entirely. Tolerant
	// of it already being gone.
	TeardownInterface(ctx context.Context) error
}

// ErrUnavailable is returned by every unavailableBackend method —
// callers check for it with errors.Is rather than needing a nil Backend
// check anywhere, same convention as internal/wifi.ErrUnavailable.
var ErrUnavailable = errors.New("relay: wireguard-tools is not installed on this system")

// unavailableBackend is what DetectBackend returns when wireguard-tools
// isn't present. Every method fails the same way, so callers never need
// a nil check — they just get ErrUnavailable back, and the System page
// shows "the relay isn't available on this system" instead of crashing.
type unavailableBackend struct{}

func (unavailableBackend) Name() string { return "unavailable" }
func (unavailableBackend) ApplyTunnel(context.Context, string, Grant) error {
	return ErrUnavailable
}
func (unavailableBackend) TeardownInterface(context.Context) error {
	return ErrUnavailable
}
