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
	return nil
}

// TeardownInterface removes relayIface entirely. Tolerant of it already
// being gone (disable-then-disable, or a fresh node that was never
// enabled).
func (wgBackend) TeardownInterface(ctx context.Context) error {
	if !interfaceExists(ctx, relayIface) {
		return nil
	}
	if _, err := runCmd(ctx, "ip", "link", "delete", relayIface); err != nil {
		return fmt.Errorf("removing %s: %w", relayIface, err)
	}
	return nil
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
