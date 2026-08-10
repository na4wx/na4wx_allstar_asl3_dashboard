package wifi

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

// buildDNSQuery builds a minimal, well-formed DNS query message asking
// for name (a dotted hostname, e.g. "captive.apple.com") with the given
// QTYPE -- just enough of the wire format for wildcardDNSResponse's own
// tests to exercise it without pulling in a real DNS library.
func buildDNSQuery(id uint16, name string, qtype uint16) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], id)
	msg[2] = 0x01                           // RD=1
	binary.BigEndian.PutUint16(msg[4:6], 1) // QDCOUNT=1

	for _, label := range strings.Split(name, ".") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0x00) // root label

	qtypeBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(qtypeBytes, qtype)
	msg = append(msg, qtypeBytes...)
	msg = append(msg, 0x00, 0x01) // QCLASS=IN
	return msg
}

// TestWildcardDNSResponseAnswersARecordWithHotspotIP is the direct
// regression test for the real gap this file exists to close: without
// it, a hotspot client's DNS query for each OS's own fixed captive-
// portal probe hostname (captive.apple.com, connectivitycheck.android.com,
// ...) has nothing to resolve it -- there's no real upstream internet,
// that's the whole reason the hotspot is up -- so the automatic
// "sign in to network" popup never fires. Confirms an A-record query
// gets back exactly one answer pointing at hotspotStaticIP.
func TestWildcardDNSResponseAnswersARecordWithHotspotIP(t *testing.T) {
	query := buildDNSQuery(0x1234, "captive.apple.com", 1) // A
	resp := wildcardDNSResponse(query)
	if resp == nil {
		t.Fatal("wildcardDNSResponse returned nil for a well-formed A query")
	}

	if got := binary.BigEndian.Uint16(resp[0:2]); got != 0x1234 {
		t.Errorf("response ID = %#x, want %#x (must echo the query's own ID)", got, 0x1234)
	}
	if resp[2]&0x80 == 0 {
		t.Error("QR bit not set -- this must be marked as a response")
	}
	if resp[2]&0x01 == 0 {
		t.Error("RD bit not echoed back (query set it)")
	}
	if ancount := binary.BigEndian.Uint16(resp[6:8]); ancount != 1 {
		t.Fatalf("ANCOUNT = %d, want 1", ancount)
	}

	// The answer's own RDATA is the last 4 bytes of a single-answer
	// response with a 2-byte compression-pointer name.
	rdata := resp[len(resp)-4:]
	wantIP := []byte{10, 42, 0, 1} // hotspotStaticIP
	if !reflect.DeepEqual(rdata, wantIP) {
		t.Errorf("answer RDATA = %v, want %v (hotspotStaticIP)", rdata, wantIP)
	}
}

// TestWildcardDNSResponseNonARecordGetsNoAnswer confirms a query type
// this responder can't meaningfully answer (e.g. AAAA) still gets a
// well-formed reply with zero answers, rather than either an IPv4
// address stuffed into an AAAA answer (which would be wrong/malformed)
// or no reply at all (which would just make the client wait out a
// timeout instead of falling back immediately).
func TestWildcardDNSResponseNonARecordGetsNoAnswer(t *testing.T) {
	query := buildDNSQuery(0x0001, "connectivitycheck.android.com", 28) // AAAA
	resp := wildcardDNSResponse(query)
	if resp == nil {
		t.Fatal("wildcardDNSResponse returned nil for a well-formed AAAA query")
	}
	if ancount := binary.BigEndian.Uint16(resp[6:8]); ancount != 0 {
		t.Errorf("ANCOUNT = %d, want 0 for a non-A query", ancount)
	}
}

// TestWildcardDNSResponseRejectsMalformedInput confirms garbage/
// truncated input never panics and is simply dropped (nil), matching
// how a real DNS server treats an unparseable packet -- the client just
// times out and moves on.
func TestWildcardDNSResponseRejectsMalformedInput(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		{0x00, 0x01, 0x02},                      // shorter than a 12-byte header
		buildDNSQuery(1, "example.com", 1)[:15], // header + partial QDCOUNT-1 name, truncated mid-question
	}
	for i, c := range cases {
		if resp := wildcardDNSResponse(c); resp != nil {
			t.Errorf("case %d: wildcardDNSResponse(%v) = non-nil, want nil for malformed input", i, c)
		}
	}
}

// TestDNSRedirectRuleArgs mirrors TestRedirectRuleArgs -- confirms the
// iptables rule that funnels wlan0's own port-53 traffic (both UDP and
// TCP; a DNS client may use either) to the wildcard responder is built
// correctly, and that add/remove use identical match arguments (iptables'
// -D only finds a rule if its match arguments are byte-for-byte the same
// ones -A used to create it).
func TestDNSRedirectRuleArgs(t *testing.T) {
	for _, proto := range []string{"udp", "tcp"} {
		add := dnsRedirectRuleArgs("-A", proto)
		want := []string{
			"-t", "nat", "-A", "PREROUTING",
			"-i", "wlan0",
			"-p", proto,
			"--dport", "53",
			"-j", "REDIRECT",
			"--to-port", "8091",
		}
		if !reflect.DeepEqual(add, want) {
			t.Errorf("dnsRedirectRuleArgs(\"-A\", %q) = %v, want %v", proto, add, want)
		}

		del := dnsRedirectRuleArgs("-D", proto)
		if del[2] != "-D" {
			t.Errorf("dnsRedirectRuleArgs(\"-D\", %q)[2] = %q, want \"-D\"", proto, del[2])
		}
		del[2] = "-A"
		if !reflect.DeepEqual(del, add) {
			t.Errorf("-D and -A must share identical match arguments for proto %q, got -D=%v vs -A=%v", proto, del, add)
		}
	}
}
