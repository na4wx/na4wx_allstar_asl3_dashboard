package relay

import (
	"bytes"
	"context"
	"fmt"
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
	if err := applyPolicyRouting(ctx); err != nil {
		return fmt.Errorf("routing asterisk's own traffic through %s: %w", relayIface, err)
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
	if !interfaceExists(ctx, relayIface) {
		return nil
	}
	if _, err := runCmd(ctx, "ip", "link", "delete", relayIface); err != nil {
		return fmt.Errorf("removing %s: %w", relayIface, err)
	}
	return nil
}

// iax2Port is chan_iax2's own well-known port -- explicitly exempted
// from policy routing (see applyPolicyRouting) so ordinary outbound
// dialing to other real nodes is never affected by this package at
// all, only Asterisk's non-IAX2 traffic (chiefly ASL3's HTTP-based node
// registration).
const iax2Port = "4569"

// applyPolicyRouting routes the asterisk user's own outbound traffic
// through relayIface, *except* its own IAX2 traffic -- needed because
// ASL3's node registration is a separate HTTP-based mechanism
// (res_rpt_http_registrations.so) with no per-application bindaddr
// option of its own; confirmed directly against that module's source
// (AllStarLink/app_rpt) that it never sets libcurl's CURLOPT_INTERFACE,
// so it always uses whatever the OS's normal routing table would pick.
// Marking by uid (rather than by destination IP) covers this without
// needing to track register.allstarlink.org's own, possibly-changing,
// resolved address.
//
// The IAX2 exemption is load-bearing, not an optimization: confirmed
// the hard way on real hardware that marking *all* of the asterisk
// user's traffic (an earlier version of this function) broke ordinary
// outbound calls to other nodes -- chan_iax2 itself runs as the same
// asterisk user, so its own already-working direct-dial traffic got
// swept into the tunnel right along with the registration traffic this
// was actually meant to fix. The exemption rule (`-j RETURN`) has to
// come before the general MARK rule in the OUTPUT chain, since iptables
// evaluates rules in order and the first match wins.
//
// Idempotent: deletes any matching rule left over from a previous run
// first, same "safe to call again after a crash/restart" pattern as
// internal/wifi's own captive-portal iptables rules.
func applyPolicyRouting(ctx context.Context) error {
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "udp", "--dport", iax2Port, "-j", "RETURN")
	if _, err := runCmd(ctx, "iptables", "-t", "mangle", "-I", "OUTPUT", "-p", "udp", "--dport", iax2Port, "-j", "RETURN"); err != nil {
		return fmt.Errorf("exempting IAX2 traffic from policy routing: %w", err)
	}
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark)
	if _, err := runCmd(ctx, "iptables", "-t", "mangle", "-A", "OUTPUT", "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark); err != nil {
		return fmt.Errorf("marking asterisk's outbound traffic: %w", err)
	}
	_, _ = runCmd(ctx, "ip", "rule", "del", "fwmark", policyFwMark, "table", policyRouteTable)
	if _, err := runCmd(ctx, "ip", "rule", "add", "fwmark", policyFwMark, "table", policyRouteTable, "priority", policyRulePriority); err != nil {
		return fmt.Errorf("adding policy routing rule: %w", err)
	}
	if _, err := runCmd(ctx, "ip", "route", "replace", "default", "dev", relayIface, "table", policyRouteTable); err != nil {
		return fmt.Errorf("adding policy route: %w", err)
	}
	return nil
}

func removePolicyRouting(ctx context.Context) {
	_, _ = runCmd(ctx, "ip", "route", "del", "default", "dev", relayIface, "table", policyRouteTable)
	_, _ = runCmd(ctx, "ip", "rule", "del", "fwmark", policyFwMark, "table", policyRouteTable)
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-m", "owner", "--uid-owner", asteriskUser, "-j", "MARK", "--set-mark", policyFwMark)
	_, _ = runCmd(ctx, "iptables", "-t", "mangle", "-D", "OUTPUT", "-p", "udp", "--dport", iax2Port, "-j", "RETURN")
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
