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
// this node's fallback hotspot. Not an arbitrary choice -- it's
// NetworkManager's own hardcoded default gateway for a "shared"
// connection (which is what nmcliBackend.StartHotspot creates), so this
// constant just names what nmcli already does rather than configuring
// anything itself.
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
