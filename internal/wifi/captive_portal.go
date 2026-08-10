package wifi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

// captivePortalInternalPort is where this package's own redirect server
// actually listens. Deliberately not :80 -- confirmed on a real node:
// HamVoIP's own stock httpd already permanently owns :80 system-wide
// (its own unrelated admin pages, on every interface), so a second Go
// server can never bind there directly -- ListenAndServe just fails
// with "address already in use". Traffic arriving on the *hotspot's*
// own interface at port 80 is instead funneled here with an iptables
// REDIRECT rule scoped to wlan0 alone (see addRedirectRule), leaving
// the stock httpd's own binding on every other interface untouched.
const captivePortalInternalPort = ":8090"

// captivePortalProbePort is the standard, unconfigurable port every
// OS's captive-portal detection probe uses (Apple's captive.apple.com,
// Android's connectivitycheck.android.com/generate_204, Windows'
// www.msftconnecttest.com, Firefox's detectportal.firefox.com --
// deliberately plain HTTP by design on every one of these, specifically
// so a captive portal network can intercept them, even as ordinary web
// browsing has moved almost entirely to HTTPS).
const captivePortalProbePort = "80"

// captivePortal is a tiny HTTP server plus wildcardDNSServer, reached
// via iptables REDIRECT rules that funnel the hotspot interface's own
// port-80 and port-53 traffic to them. The DNS side answers every
// hostname with this node's own IP (see dns.go) -- since NetworkManager's
// own shared-connection dnsmasq has no plain-nmcli way to configure that
// kind of wildcard answer, this package runs its own responder instead
// of relying on it. Combined, this is what makes a phone/laptop that
// just joined the hotspot automatically pop up a sign-in prompt pointed
// at the dashboard, rather than requiring the operator to already know
// (or look up) an address to browse to.
type captivePortal struct {
	srv *http.Server
	dns *wildcardDNSServer
}

// startCaptivePortal starts the redirect server and wildcard DNS
// responder in the background and adds the iptables rules that route
// the hotspot interface's port-80 and port-53 traffic to them. All of
// this is best-effort: a failure here doesn't fail hotspot startup --
// the AP itself and manually browsing straight to the dashboard's real
// address both still work fine without it, just without the automatic
// popup.
func startCaptivePortal(dashboardURL string) *captivePortal {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dashboardURL, http.StatusFound)
	})
	srv := &http.Server{Addr: captivePortalInternalPort, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("wifi: captive-portal redirect server failed to start: %v", err)
		}
	}()
	if err := addRedirectRule(); err != nil {
		log.Printf("wifi: could not add captive-portal iptables redirect: %v", err)
	}

	dns := startWildcardDNS()
	if err := addDNSRedirectRules(); err != nil {
		log.Printf("wifi: could not add captive-portal DNS iptables redirect: %v", err)
	}

	return &captivePortal{srv: srv, dns: dns}
}

// stop shuts the redirect server and DNS responder down and removes
// both iptables rules, on its own fresh timeout budget regardless of
// the caller's own context state (this is best-effort cleanup, not
// something that should inherit an already-cancelled context and skip
// straight to a force-close). Safe to call on a nil *captivePortal
// (e.g. if startCaptivePortal's own bind failed) or a nil receiver.
func (c *captivePortal) stop() {
	if c == nil {
		return
	}
	if err := removeRedirectRule(); err != nil {
		log.Printf("wifi: could not remove captive-portal iptables redirect: %v", err)
	}
	if err := removeDNSRedirectRules(); err != nil {
		log.Printf("wifi: could not remove captive-portal DNS iptables redirect: %v", err)
	}
	c.dns.stop()
	if c.srv == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.srv.Shutdown(shutdownCtx); err != nil {
		_ = c.srv.Close() // best-effort force-close if graceful shutdown didn't finish in time
	}
}

// redirectRuleArgs builds iptables' own match arguments for the
// wlan0-scoped port-80-to-captivePortalInternalPort redirect. Shared
// between add and remove -- iptables' -D (delete) takes the exact same
// match arguments as -A (add), just swapping the action flag, so a
// rule can only ever be found and removed if the two stay in sync.
func redirectRuleArgs(action string) []string {
	return []string{
		"-t", "nat", action, "PREROUTING",
		"-i", wlan0Iface,
		"-p", "tcp",
		"--dport", captivePortalProbePort,
		"-j", "REDIRECT",
		"--to-port", captivePortalInternalPort[1:], // strip the leading ":" http.Server's Addr needs
	}
}

// addRedirectRule deletes any matching rule left over from a previous
// run first (best-effort -- iptables errors with "Bad rule" when
// there's nothing to delete, which is the expected, ignored case on a
// clean first start), so a crash or process restart while the hotspot
// was already active never leaves duplicate rules piling up.
func addRedirectRule() error {
	ctx := context.Background()
	_ = runCmd(ctx, 5*time.Second, "iptables", redirectRuleArgs("-D")...)
	return runCmd(ctx, 5*time.Second, "iptables", redirectRuleArgs("-A")...)
}

func removeRedirectRule() error {
	return runCmd(context.Background(), 5*time.Second, "iptables", redirectRuleArgs("-D")...)
}

// dnsRedirectRuleArgs is redirectRuleArgs' own counterpart for port 53
// -- proto is "udp" or "tcp" (a DNS client may use either, so both need
// their own rule; iptables doesn't have a single "either protocol"
// match). Shared between add and remove for the same reason
// redirectRuleArgs is.
func dnsRedirectRuleArgs(action, proto string) []string {
	return []string{
		"-t", "nat", action, "PREROUTING",
		"-i", wlan0Iface,
		"-p", proto,
		"--dport", "53",
		"-j", "REDIRECT",
		"--to-port", captiveDNSInternalPort[1:], // strip the leading ":" net.ListenUDP's addr needs
	}
}

// addDNSRedirectRules adds both the udp and tcp variants, deleting any
// matching rule left over from a previous run first -- same "safe to
// call again after a crash/restart" reasoning as addRedirectRule.
func addDNSRedirectRules() error {
	ctx := context.Background()
	var firstErr error
	for _, proto := range []string{"udp", "tcp"} {
		_ = runCmd(ctx, 5*time.Second, "iptables", dnsRedirectRuleArgs("-D", proto)...)
		if err := runCmd(ctx, 5*time.Second, "iptables", dnsRedirectRuleArgs("-A", proto)...); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func removeDNSRedirectRules() error {
	ctx := context.Background()
	var firstErr error
	for _, proto := range []string{"udp", "tcp"} {
		if err := runCmd(ctx, 5*time.Second, "iptables", dnsRedirectRuleArgs("-D", proto)...); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
