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
// deployment: chan_iax2's own reply to an inbound call, sent directly
// to the calling station's real address (as it always has been -- the
// caller's address survives the cloud's own inbound DNAT unchanged),
// was reliably lost somewhere between this node and the caller, no
// matter how many different ways it was made to leave this node or the
// cloud -- while a genuinely new, outbound-initiated call over the
// exact same tunnel worked perfectly, and the caller's own inbound
// request always arrived here intact. Every kernel-level NAT/SNAT
// mechanism tried for this leg failed identically, on both this node
// and the cloud: the rule matched (confirmed via zeroed, precisely-
// windowed iptables counters), yet the packet never showed up in a
// simultaneous, unfiltered capture on the egress interface -- across
// three structurally different mechanisms (an in-tunnel SNAT reply, a
// separate out-of-band relay, and that relay again with the colliding
// conntrack entry explicitly evicted first). That consistent pattern
// pointed at something outside this package's own rules entirely
// (plausibly a hypervisor- or provider-level filter on the cloud host,
// invisible to a local tcpdump), not at any one rule's own shape.
//
// The fix sidesteps kernel-level NAT for this leg entirely, rather than
// continuing to chase it: chan_iax2 never addresses the real caller at
// all anymore. Its inbound delivery is unchanged (the cloud's own
// PREROUTING DNAT to this device's tunnel IP, which has never been the
// unreliable part). Its replies simply go, via chan_iax2's own default
// routing, to whichever peer sent the inbound packet -- the cloud's own
// dedicated rendezvous socket on the tunnel interface, not the caller
// directly -- ordinary WireGuard-routed UDP to a directly-connected
// tunnel address, the one traffic pattern proven reliable throughout
// this whole investigation. The cloud's own relayProxy.ts owns both
// ends of the real, public-facing leg: it remembers which caller is
// currently talking to this device and relays payloads between that
// caller and the rendezvous socket, entirely in userspace, with no
// DNAT/SNAT/conntrack tricks of its own either.
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
