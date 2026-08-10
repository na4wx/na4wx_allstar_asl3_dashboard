package wifi

import (
	"encoding/binary"
	"log"
	"net"
)

// captiveDNSInternalPort is where wildcardDNSServer actually listens --
// same reasoning as captivePortalInternalPort: NetworkManager's own
// dnsmasq (started automatically for the "shared" hotspot connection
// nmcliBackend.StartHotspot creates) already owns port 53 on wlan0, so
// this can't bind there directly either. A hotspot client's own DNS
// queries are instead funneled here with an iptables REDIRECT rule
// scoped to wlan0 alone (see addDNSRedirectRules), the same pattern
// captivePortalInternalPort already uses for port 80.
const captiveDNSInternalPort = ":8091"

// wildcardDNSServer answers every DNS query received on wlan0 with this
// node's own hotspot IP, regardless of what hostname was actually
// asked about. Combined with captivePortal's own port-80 redirect,
// this is what makes a phone/laptop that just joined the fallback
// hotspot automatically pop up a captive-portal sign-in prompt: every
// OS probes a fixed, unconfigurable hostname over plain HTTP
// (captive.apple.com, connectivitycheck.android.com, ...) the moment
// it joins a new network, and normally can't resolve that hostname at
// all (there's no real upstream internet -- that's the whole reason
// the hotspot exists) -- this makes it resolve to this node instead,
// where the port-80 redirect then serves the actual sign-in redirect.
// NetworkManager's own shared-connection dnsmasq (used for DHCP) has
// no plain-nmcli way to configure this kind of wildcard answer, so this
// package runs its own tiny DNS responder rather than relying on it --
// see docs/README's own note on this, or the unwired nmcli.go comment
// this replaces.
type wildcardDNSServer struct {
	conn *net.UDPConn
}

// startWildcardDNS starts the responder in the background. Best-effort,
// same as captive_portal.go's own server: a failure here doesn't fail
// hotspot startup, it just means no automatic captive-portal popup --
// manually browsing to the dashboard's real address still works.
//
// Binds every interface (no host in the address), not just loopback --
// confirmed on a real node this must: iptables' own REDIRECT target (see
// addDNSRedirectRules) rewrites a PREROUTING packet's destination to the
// *incoming interface's own real address* (hotspotStaticIP here), not
// 127.0.0.1 -- that mapping to loopback only applies to locally
// generated packets, not ones arriving from wlan0. A responder bound to
// 127.0.0.1 alone never receives a hotspot client's redirected query at
// all, so the captive-portal popup silently never fires even though the
// HTTP redirect (already interface-agnostic, see captivePortalInternalPort)
// works fine on its own -- exactly the "portal doesn't appear but the
// dashboard is reachable directly" symptom this bug produced.
func startWildcardDNS() *wildcardDNSServer {
	addr, err := net.ResolveUDPAddr("udp", captiveDNSInternalPort)
	if err != nil {
		log.Printf("wifi: captive-portal DNS: resolve %s: %v", captiveDNSInternalPort, err)
		return nil
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("wifi: captive-portal DNS: listen %s: %v", captiveDNSInternalPort, err)
		return nil
	}
	d := &wildcardDNSServer{conn: conn}
	go d.serve()
	return d
}

func (d *wildcardDNSServer) serve() {
	buf := make([]byte, 512)
	for {
		n, addr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			return // conn closed -- stop() was called
		}
		if resp := wildcardDNSResponse(buf[:n]); resp != nil {
			_, _ = d.conn.WriteToUDP(resp, addr)
		}
	}
}

// stop closes the listener. Safe to call on a nil receiver (e.g. if
// startWildcardDNS's own bind failed) or a nil conn.
func (d *wildcardDNSServer) stop() {
	if d == nil || d.conn == nil {
		return
	}
	_ = d.conn.Close()
}

// wildcardDNSResponse builds a reply to one DNS query message,
// answering an A-record question with hotspotStaticIP and every other
// question type with zero answers (so the client falls back to
// whatever it does next, e.g. an AAAA query getting no answer makes a
// well-behaved resolver fall back to its earlier A query rather than
// hanging). Returns nil for a malformed or truncated query -- the
// client just times out and moves on, same as any other dropped UDP
// packet; this never guesses at recovering a bad packet.
func wildcardDNSResponse(query []byte) []byte {
	const headerLen = 12
	if len(query) < headerLen {
		return nil
	}
	if binary.BigEndian.Uint16(query[4:6]) != 1 { // QDCOUNT must be exactly 1
		return nil
	}
	rd := query[2] & 0x01 // recursion-desired bit, echoed back in the reply

	// Walk the question's own QNAME (a sequence of length-prefixed
	// labels ending in a zero-length label) to find where it ends --
	// the name itself is never decoded, only echoed back verbatim plus
	// referenced via a compression pointer in the answer below.
	i := headerLen
	for {
		if i >= len(query) {
			return nil
		}
		length := int(query[i])
		i++
		if length == 0 {
			break
		}
		i += length
	}
	if i+4 > len(query) { // QTYPE(2) + QCLASS(2) must still follow
		return nil
	}
	qtype := binary.BigEndian.Uint16(query[i : i+2])
	questionEnd := i + 4

	answerThis := qtype == 1 // 1 = A record; anything else gets zero answers

	resp := make([]byte, 0, headerLen+(questionEnd-headerLen)+16)
	resp = append(resp, query[0:2]...) // ID, echoed
	resp = append(resp, 0x84|rd, 0x80) // QR=1 AA=1 RD=echoed; RA=1
	resp = append(resp, 0x00, 0x01)    // QDCOUNT=1
	if answerThis {
		resp = append(resp, 0x00, 0x01) // ANCOUNT=1
	} else {
		resp = append(resp, 0x00, 0x00) // ANCOUNT=0
	}
	resp = append(resp, 0x00, 0x00) // NSCOUNT=0
	resp = append(resp, 0x00, 0x00) // ARCOUNT=0
	resp = append(resp, query[headerLen:questionEnd]...)

	if answerThis {
		ip := net.ParseIP(hotspotStaticIP).To4()
		if ip == nil {
			return nil
		}
		resp = append(resp, 0xc0, 0x0c)             // NAME: compression pointer to the question's own name at offset 12
		resp = append(resp, 0x00, 0x01)             // TYPE=A
		resp = append(resp, 0x00, 0x01)             // CLASS=IN
		resp = append(resp, 0x00, 0x00, 0x00, 0x05) // TTL=5s -- short-lived, only matters while the hotspot is up
		resp = append(resp, 0x00, 0x04)             // RDLENGTH=4
		resp = append(resp, ip...)
	}
	return resp
}
