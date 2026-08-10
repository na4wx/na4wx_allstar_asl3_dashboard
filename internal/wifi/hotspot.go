package wifi

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// hotspotStaticIP is the gateway address a client sees once joined to
// this node's fallback hotspot -- explicitly pinned via the connection
// profile's own ipv4.addresses (see nmcliBackend.StartHotspot), not
// left to whatever NetworkManager might default a "shared" connection
// to on its own. This used to just document NetworkManager's own
// assumed default instead of setting it -- confirmed on a real node
// that assumption doesn't hold on every NetworkManager version (same
// class of issue as StartHotspot's own password default not holding
// either): wlan0 came up with no usable address at all, breaking the
// static dashboardURL/dns.go's own wildcard-DNS answer, both of which
// hardcode this exact address.
const (
	hotspotStaticIP   = "10.42.0.1"
	hotspotStaticCIDR = hotspotStaticIP + "/24"
)

// runCmd runs name with args under a timeout, returning stderr's content
// wrapped into the error on failure. Used by captive_portal.go's
// iptables redirect rules -- the one piece of hotspot support that isn't
// itself an nmcli call.
func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, name, args...)
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, stderr.String())
	}
	return nil
}
