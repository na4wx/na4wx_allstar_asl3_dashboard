package relay

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// relayIface is hardcoded, not operator-configurable — same reasoning as
// internal/wifi's wlan0Iface: this package manages exactly one tunnel,
// and getting the interface name wrong here has no graceful failure
// mode.
const relayIface = "wg-relay"

const cmdTimeout = 10 * time.Second

// Policy-routing constants for routing Asterisk's own outbound traffic
// through the tunnel -- see applyPolicyRouting's own doc comment for
// why this exists at all (chan_iax2's bindaddr, set elsewhere in this
// package, only covers IAX2 itself; ASL3's own HTTP-based node
// registration is a separate code path with no bindaddr equivalent).
// policyRouteTable/policyFwMark are arbitrary but need to not collide
// with anything else already using custom routing tables/marks on this
// host -- 200/0x2a were picked as values unlikely to already be in use,
// not because they mean anything. policyRulePriority has to be lower
// than the kernel's own default main-table rule (32766) or it would
// never actually be consulted, since the main table's own default
// route would already have answered the lookup first.
const (
	asteriskUser       = "asterisk"
	policyRouteTable   = "200"
	policyFwMark       = "0x2a"
	policyRulePriority = "100"

	// wgTransportPriority prevents a routing loop: WireGuard's own outer
	// encrypted packets, addressed to the peer's endpoint (the cloud's
	// public IP), are ordinary locally-generated UDP traffic from the
	// kernel's point of view, with nothing inherently exempting them
	// from policy routing. Without an explicit exemption, they can end
	// up matching the very policy route meant for Asterisk's traffic and
	// get sent back into relayIface -- which re-encrypts and re-sends
	// them, forever. Confirmed the hard way on real hardware: hundreds
	// of thousands of packets and over a gigabyte transferred in
	// minutes, each one larger than the last (repeated re-encapsulation
	// overhead), the unmistakable signature of exactly this loop.
	//
	// WireGuard's own documented fix for this (tagging the interface's
	// outer traffic with a dedicated fwmark, then routing anything
	// carrying it via the main table) did not reliably prevent the loop
	// here despite being configured correctly (confirmed via `wg show`
	// and `ip rule show`) -- rather than keep depending on exactly when
	// and how that mark gets attached at the kernel level, applyPolicyRouting
	// instead routes by *destination* (the cloud's own IP, extracted
	// from the grant's own endpoint) via the main table -- unambiguous,
	// and correct regardless of any WireGuard-internal marking behavior.
	// The priority has to be lower than policyRulePriority so it's
	// evaluated first.
	wgTransportPriority = "50"
)

// DetectBackend probes the running system once and returns the Backend
// to use for every subsequent relay operation. Never returns nil — falls
// back to unavailableBackend{} so callers never need a nil check, same
// convention as internal/wifi.DetectBackend.
func DetectBackend(ctx context.Context) Backend {
	if _, err := exec.LookPath("wg"); err != nil {
		return unavailableBackend{}
	}
	if _, err := exec.LookPath("ip"); err != nil {
		return unavailableBackend{}
	}
	return wgBackend{}
}

type wgBackend struct{}

func (wgBackend) Name() string { return "wireguard-tools" }

// runCmd follows internal/system's own convention exactly: explicit,
// separate argv elements (never a shell string), a context.WithTimeout
// on every call, and stderr captured and %w-wrapped into the returned
// error.
func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

func interfaceExists(ctx context.Context, iface string) bool {
	_, err := runCmd(ctx, "ip", "link", "show", iface)
	return err == nil
}

// writeTempKey writes privateKey to a private (0600) temp file — `wg
// set ... private-key` reads a filename, not inline data, and this
// package never pipes secret data through a shell string.
func writeTempKey(privateKey string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "wg-relay-")
	if err != nil {
		return "", nil, err
	}
	path = filepath.Join(dir, "privatekey")
	if err := os.WriteFile(path, []byte(privateKey), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return path, func() { os.RemoveAll(dir) }, nil
}

// ApplyTunnel brings relayIface up if it doesn't already exist, then
// (re-)applies privateKey and grant to it. "ip address replace" (not
// "add") and re-running "wg set" with the same values are both
// idempotent, so this is safe to call on every reconcile tick even when
// nothing actually changed.
func (wgBackend) ApplyTunnel(ctx context.Context, privateKey string, grant Grant) error {
	if !interfaceExists(ctx, relayIface) {
		if _, err := runCmd(ctx, "ip", "link", "add", relayIface, "type", "wireguard"); err != nil {
			return fmt.Errorf("creating %s: %w", relayIface, err)
		}
	}

	keyPath, cleanup, err := writeTempKey(privateKey)
	if err != nil {
		return fmt.Errorf("writing wireguard private key: %w", err)
	}
	defer cleanup()

	if _, err := runCmd(ctx, "wg", "set", relayIface,
		"private-key", keyPath,
		"peer", grant.CloudPublicKey,
		"endpoint", grant.Endpoint,
		"allowed-ips", "0.0.0.0/0",
		"persistent-keepalive", "25",
	); err != nil {
		return fmt.Errorf("configuring %s: %w", relayIface, err)
	}

	if _, err := runCmd(ctx, "ip", "address", "replace", grant.TunnelIP+grant.TunnelCIDR, "dev", relayIface); err != nil {
		return fmt.Errorf("assigning tunnel address: %w", err)
	}
	if _, err := runCmd(ctx, "ip", "link", "set", relayIface, "up"); err != nil {
		return fmt.Errorf("bringing up %s: %w", relayIface, err)
	}
	cloudHost, _, err := net.SplitHostPort(grant.Endpoint)
	if err != nil {
		return fmt.Errorf("parsing cloud endpoint %q: %w", grant.Endpoint, err)
	}
	if err := applyPolicyRouting(ctx, cloudHost); err != nil {
		return fmt.Errorf("routing asterisk's own traffic through %s: %w", relayIface, err)
	}
	if grant.ExternalPort != 0 {
		if err := pinIax2SourcePort(ctx, grant.TunnelIP, grant.ExternalPort); err != nil {
			return fmt.Errorf("pinning chan_iax2's own source port through %s: %w", relayIface, err)
		}
	}
	return nil
}

// TeardownInterface removes relayIface entirely, and the policy-routing
// rules from applyPolicyRouting alongside it (best-effort, tolerant of
// them already being gone -- same "disable-then-disable is a no-op"
// contract as the interface removal itself). The rules are removed
// regardless of whether the interface still exists, since they aren't
// tied to it existing and would otherwise linger, silently blackholing
// the asterisk user's traffic (routed at a table that no longer has a
// working default route) even after the tunnel itself is gone.
func (wgBackend) TeardownInterface(ctx context.Context) error {
	removePolicyRouting(ctx)
	removeStaleIax2SNAT(ctx)
	if !interfaceExists(ctx, relayIface) {
		return nil
	}
	if _, err := runCmd(ctx, "ip", "link", "delete", relayIface); err != nil {
		return fmt.Errorf("removing %s: %w", relayIface, err)
	}
	return nil
}

// registrationPort is the destination port ASL3's HTTP-based node
// registration actually uses (HTTPS).
const registrationPort = "443"

// dnsPort is excluded even though everything else the asterisk user
// sends over UDP is now routed through the tunnel (see
// applyPolicyRouting's own doc comment) -- confirmed on a real
// deployment as a real gap in an earlier, narrower version of this
// exclusion list: a query to a *public* DNS resolver isn't caught by
// privateDestinationRanges below, and has no business going through the
// tunnel to a cloud with no reason to answer it.
const dnsPort = "53"

// privateDestinationRanges are loopback + every RFC1918 block -- for
// anything that doesn't belong going through the tunnel to a cloud that
// has no route back into a private network it was never part of (e.g. a
// local DNS resolver on the LAN).
var privateDestinationRanges = []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

// applyPolicyRouting routes the asterisk user's registration traffic
// (tcp/443) *and* all of chan_iax2's own UDP traffic through relayIface
// -- an allowlist (mark only these two, explicitly, rather than marking
// everything from the asterisk user and carving out exemptions for what
// shouldn't go) that has grown by one entry, for two different reasons:
//
// Registration needs it because ASL3's node registration is a separate
// HTTP-based mechanism (res_rpt_http_registrations.so) with no
// per-application bindaddr option of its own; confirmed directly
// against that module's source (AllStarLink/app_rpt) that it never sets
// libcurl's CURLOPT_INTERFACE, so it always uses whatever the OS's
// normal routing table would pick.
//
// IAX2 itself needs it for a completely different reason, confirmed the
// hard way on a real deployment: chan_iax2's own socket, bound to
// 0.0.0.0, *receives* an inbound call that arrived via the tunnel just
// fine regardless of interface -- but *replying* to it doesn't work for
// free. A wildcard-bound UDP socket's outbound packets get their source
// address chosen by an ordinary destination-based routing lookup before
// any netfilter/mangle processing runs at all, so an arbitrary calling
// station's real address was never going to route toward relayIface,
// and the reply left (if it left at all) carrying this node's real LAN
// address, not the tunnel's -- meaningless to a caller whose own NAT is
// only expecting a reply from the cloud's public IP. A CONNMARK-based
// fix (mark the inbound connection, restore that mark onto the reply)
// was tried first and didn't work either: conntrack evaluates the
// reply's addressing *before* mangle OUTPUT even runs, using whatever
// source address the kernel already committed to, so it never matches
// the inbound connection's own tracked tuple in the first place --
// confirmed via `conntrack -L`, which showed the reply as a distinct,
// unrelated, unmarked connection entirely. Marking chan_iax2's UDP
// traffic unconditionally sidesteps the problem: it uses the exact same
// mechanism already proven to work for registration (a static match,
// not anything tied to an existing conntrack entry), for both an
// initial reply *and* a self-initiated outbound call alike. The
// tradeoff: this node's own calls to other, directly-reachable
// AllStarLink nodes now also route via the cloud relay while relay is
// enabled, rather than going direct -- an accepted cost, not a
// regression, since the cloud already forwards arbitrary destinations
// for the whole tunnel subnet (see relayNetwork.ts's ensureForwarding
// on the cloud side).
//
// Idempotent: deletes any matching rule left over from a previous run
// first, same "safe to call again after a crash/restart" pattern as
// internal/wifi's own captive-portal iptables rules.
func applyPolicyRouting(ctx context.Context, cloudHost string) error {
	// Must be applied before the general policy route below is even
	// useful -- see wgTransportPriority's own doc comment for why this
	// specific rule (checked first, at a lower priority number, matched
	// by destination rather than any WireGuard-internal marking) is what
	// stops WireGuard's own transport traffic from looping back into
	// itself via that route. Deleted by priority alone (not by its match
	// criteria) so cleanup works even if cloudHost has changed since the
	// rule was added -- e.g. across a reconnect that lands on a
	// different relay slot.
	_, _ = runCmd(ctx, "ip", "rule", "del", "priority", wgTransportPriority)
	if _, err := runCmd(ctx, "ip", "rule", "add", "to", cloudHost, "table", "main", "priority", wgTransportPriority); err != nil {
		return fmt.Errorf("exempting the cloud endpoint %s from policy routing: %w", cloudHost, err)
	}
	for _, cidr := range privateDestinationRanges {
		_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-d", cidr, "-j", "RETURN")
		if _, err := runCmd(ctx, "iptables", "-t", "mangle", "-I", "OUTPUT", "-d", cidr, "-j", "RETURN"); err != nil {
			return fmt.Errorf("exempting %s from policy routing: %w", cidr, err)
		}
	}
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "udp", "--dport", dnsPort, "-j", "RETURN")
	if _, err := runCmd(ctx, "iptables", "-t", "mangle", "-I", "OUTPUT", "-p", "udp", "--dport", dnsPort, "-j", "RETURN"); err != nil {
		return fmt.Errorf("exempting DNS from policy routing: %w", err)
	}
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "tcp", "--dport", registrationPort, "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark)
	if _, err := runCmd(ctx, "iptables", "-t", "mangle", "-A", "OUTPUT", "-p", "tcp", "--dport", registrationPort, "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark); err != nil {
		return fmt.Errorf("marking asterisk's registration traffic: %w", err)
	}
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "udp", "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark)
	if _, err := runCmd(ctx, "iptables", "-t", "mangle", "-A", "OUTPUT", "-p", "udp", "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark); err != nil {
		return fmt.Errorf("marking asterisk's own UDP (IAX2) traffic: %w", err)
	}
	_, _ = runCmd(ctx, "ip", "rule", "del", "fwmark", policyFwMark, "table", policyRouteTable)
	if _, err := runCmd(ctx, "ip", "rule", "add", "fwmark", policyFwMark, "table", policyRouteTable, "priority", policyRulePriority); err != nil {
		return fmt.Errorf("adding policy routing rule: %w", err)
	}

	if _, err := runCmd(ctx, "ip", "route", "replace", "default", "dev", relayIface, "table", policyRouteTable); err != nil {
		return fmt.Errorf("adding policy route: %w", err)
	}
	// Marking and routing alone aren't enough: a packet policy-routed
	// out relayIface still carries whatever source address the kernel
	// picked during the *original* (pre-mark) routing lookup -- this
	// node's real LAN address, not the tunnel's own. Confirmed on real
	// hardware via tcpdump directly on wg-relay: the captured packet's
	// source was the node's real 10.x LAN IP, not its tunnel IP.
	// Without rewriting it, the cloud's own MASQUERADE (scoped to the
	// tunnel subnet) never matches it either, so it leaves the cloud's
	// uplink still carrying a private, non-internet-routable address
	// with nowhere for a reply to come back to. MASQUERADE (not a fixed
	// SNAT) so this keeps working automatically if the tunnel's
	// assigned address ever changes across a reconnect.
	_, _ = runCmd(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING", "-o", relayIface, "-j", "MASQUERADE")
	if _, err := runCmd(ctx, "iptables", "-t", "nat", "-A", "POSTROUTING", "-o", relayIface, "-j", "MASQUERADE"); err != nil {
		return fmt.Errorf("masquerading traffic leaving %s: %w", relayIface, err)
	}
	return nil
}

func removePolicyRouting(ctx context.Context) {
	_, _ = runCmd(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING", "-o", relayIface, "-j", "MASQUERADE")
	_, _ = runCmd(ctx, "ip", "route", "del", "default", "dev", relayIface, "table", policyRouteTable)
	_, _ = runCmd(ctx, "ip", "rule", "del", "fwmark", policyFwMark, "table", policyRouteTable)
	_, _ = runCmd(ctx, "ip", "rule", "del", "priority", wgTransportPriority)
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "udp", "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark)
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "tcp", "--dport", registrationPort, "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark)
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "udp", "--dport", dnsPort, "-j", "RETURN")
	for _, cidr := range privateDestinationRanges {
		_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-d", cidr, "-j", "RETURN")
	}
}

// pinIax2SourcePort keeps chan_iax2's own outbound UDP traffic through
// relayIface sourced from tunnelIP:iaxPort -- its single bound local
// port, matching what the cloud's own PREROUTING DNAT rule expects and
// what a calling station's own NAT is only willing to accept a reply
// from. The general MASQUERADE rule below (applied by
// applyPolicyRouting) isn't enough on its own: confirmed on a real
// deployment that it doesn't reliably preserve the original source
// port for this traffic, instead picking a different one seemingly at
// random on each retry -- the same class of bug already fixed once on
// the cloud side's own general subnet-wide MASQUERADE (see
// relayNetwork.ts's addNat there). Inserted at position 1 in
// POSTROUTING (not appended), so it's matched before the general
// MASQUERADE rule, the same "explicit beats general" ordering used
// there too.
//
// Checks whether the exact rule already exists before touching
// anything, rather than unconditionally deleting and re-inserting on
// every call -- this runs on every hello/reconcile (every few minutes,
// or more often across a reconnect), and a blind delete+insert resets
// the rule's own packet counter each time even though the rule itself
// never stopped working, which made that counter useless for
// diagnosing real hardware: it read "0 packets matched" after every
// routine reconcile, indistinguishable from a rule that had never
// fired at all. Only reaches for removeStaleIax2SNAT (clearing out a
// *different*, no-longer-correct pin -- e.g. after a reconnect lands on
// a different relay slot's port) when the current one isn't already
// exactly right.
func pinIax2SourcePort(ctx context.Context, tunnelIP string, iaxPort int) error {
	port := fmt.Sprintf("%d", iaxPort)
	spec := []string{"-o", relayIface, "-p", "udp", "--sport", port, "-j", "SNAT", "--to-source", tunnelIP + ":" + port}
	if _, err := runCmd(ctx, "iptables", append([]string{"-t", "nat", "-C", "POSTROUTING"}, spec...)...); err == nil {
		return nil
	}
	removeStaleIax2SNAT(ctx)
	if _, err := runCmd(ctx, "iptables", append([]string{"-t", "nat", "-I", "POSTROUTING", "1"}, spec...)...); err != nil {
		return fmt.Errorf("pinning chan_iax2's own source port: %w", err)
	}
	return nil
}

// removeStaleIax2SNAT deletes every POSTROUTING SNAT rule this package
// has ever added for relayIface, regardless of which port it pinned --
// used at teardown (TeardownInterface has no Grant to know which port
// was last in effect) and lets a reconnect that lands on a different
// relay slot's port clean up the previous port's rule too, rather than
// leaving it stacked underneath the new one. Lists rules in their exact
// addable form (`-S`) and deletes each match by that same exact spec --
// `-D` only removes a rule identical to what's given, so guessing at
// the shape wouldn't reliably find one added with a different port.
func removeStaleIax2SNAT(ctx context.Context) {
	out, err := runCmd(ctx, "iptables", "-t", "nat", "-S", "POSTROUTING")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "-o "+relayIface) || !strings.Contains(line, "-j SNAT") {
			continue
		}
		args := strings.Fields(line)
		if len(args) == 0 || args[0] != "-A" {
			continue
		}
		args[0] = "-D"
		delArgs := append([]string{"-t", "nat"}, args...)
		_, _ = runCmd(ctx, "iptables", delArgs...)
	}
}

// GenerateKeypair returns a fresh WireGuard private/public keypair via
// `wg genkey`/`wg pubkey`, the same two-command form wg-quick itself
// uses internally. Called once, the first time relay is enabled — the
// keypair is then persisted (see settings.go) and reused across
// restarts and reconnects, not regenerated on every hello.
func GenerateKeypair(ctx context.Context) (privateKey, publicKey string, err error) {
	genCtx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	var privOut bytes.Buffer
	genCmd := exec.CommandContext(genCtx, "wg", "genkey")
	genCmd.Stdout = &privOut
	if err := genCmd.Run(); err != nil {
		return "", "", fmt.Errorf("wg genkey: %w", err)
	}
	privateKey = strings.TrimSpace(privOut.String())

	pubCtx, cancel2 := context.WithTimeout(ctx, cmdTimeout)
	defer cancel2()
	var pubOut bytes.Buffer
	pubCmd := exec.CommandContext(pubCtx, "wg", "pubkey")
	pubCmd.Stdin = strings.NewReader(privateKey)
	pubCmd.Stdout = &pubOut
	if err := pubCmd.Run(); err != nil {
		return "", "", fmt.Errorf("wg pubkey: %w", err)
	}
	return privateKey, strings.TrimSpace(pubOut.String()), nil
}
