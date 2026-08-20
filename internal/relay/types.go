// Package relay lets this node accept inbound AllStarLink IAX2
// connections even behind NAT with no forwarded ports, by tunneling
// through the cloud service over WireGuard. AllStarLink's own directory
// resolves "where is node N" purely from the observed source address of
// the request that registered it, so the fix is to make that
// registration traffic egress through the tunnel instead of this
// node's real, unreachable address.
//
// ASL3 doesn't use classic IAX2-level registration at all -- it uses a
// separate HTTP-based mechanism (res_rpt_http_registrations.so,
// configured via rpt_http_registrations.conf) with no per-application
// "bind to this interface" option of its own (confirmed directly
// against that module's source, AllStarLink/app_rpt: it never sets
// libcurl's CURLOPT_INTERFACE, so it always uses whatever the OS's
// normal routing table would pick). wgctl.go's applyPolicyRouting
// routes that traffic, plus all of chan_iax2's own UDP (an allowlist,
// not a blocklist -- see that function's own doc comment for why an
// earlier blocklist version kept finding new gaps on real hardware),
// through the tunnel via OS-level policy routing.
//
// Replying to an inbound call needed one more piece, discovered only
// after an extensive, evidence-driven investigation on a real
// deployment. A userspace relay was tried for *both* directions at one
// point (routing chan_iax2's inbound delivery through a cloud-owned
// rendezvous socket instead of kernel DNAT) and worked at the IAX2
// protocol level, but broke a real, higher-layer requirement: app_rpt's
// own parse_caller check (app_rpt.c) rejects a call whose channel peer
// address doesn't match the calling node's own real, directory-
// registered IP -- confirmed on a real deployment ("Node N IP a.b.c.d
// does not match link IP e.f.g.h!!"). Inbound delivery has to stay on
// kernel-level DNAT (the cloud's own PREROUTING rule, which preserves
// the caller's real source address and has never been the unreliable
// part of any of this) so chan_iax2 genuinely sees the real caller as
// its peer.
//
// That in turn means chan_iax2's own reply is naturally addressed back
// to that real caller -- and sending that reply, unmodified, out this
// node's own tunnel interface was proven, via a controlled, isolating
// test, to be lost between this node and the cloud, every single time,
// while a genuinely new, outbound-initiated call over the exact same
// tunnel worked perfectly. wgctl.go's applyDirectReplyRedirect
// sidesteps this rather than solving it: such a reply (identified by a
// destination-port heuristic, not conntrack -- see that function's own
// doc comment for why) is redirected to leave over this node's own
// ordinary internet connection instead of the tunnel, addressed to a
// dedicated port on the cloud -- see the cloud repo's
// relayDirectReply.ts, which receives it and re-sends it to the correct
// caller. The root cause of the original in-tunnel loss was never
// conclusively identified, though two later, unrelated discoveries on
// the cloud side (a stale-NAT-rule accumulation bug and a missing
// INPUT-chain allow, both now fixed) may well explain at least some of
// what made this so hard to pin down.
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
