package wifi

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// DetectBackend probes the running system once and returns the Backend to
// use for every subsequent WiFi operation. Never returns nil -- falls
// back to unavailableBackend{} so callers never need a nil check.
// NetworkManager-only on ASL3: confirmed on a real node that it's the
// standard, systemd-managed network stack there (with netplan generating
// its connection profiles on top -- this package only ever talks to
// NetworkManager via nmcli, the same layer netplan's own generated
// profiles already live on, so it doesn't touch or need to know about
// netplan itself). Unlike HamVoIP, there is no wpa_supplicant/hostapd
// fallback path to detect here.
func DetectBackend(ctx context.Context) Backend {
	if networkManagerActive(ctx) {
		return newNmcliBackend()
	}
	return unavailableBackend{}
}

// networkManagerActive reports whether NetworkManager's systemd unit is
// currently active. A missing unit exits non-zero with stdout "unknown";
// a stopped one exits non-zero with "inactive" -- both must be treated as
// "not this backend", not just the exit code, since "systemctl is-active"
// can print "activating"/"deactivating" as a transient state a bare
// exit-code check would misclassify.
func networkManagerActive(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	c := exec.CommandContext(ctx, "systemctl", "is-active", "NetworkManager")
	c.Stdout = &out
	err := c.Run()
	return err == nil && strings.TrimSpace(out.String()) == "active"
}
