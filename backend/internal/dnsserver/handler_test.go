package dnsserver

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/queryobs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

const zone = "acme.example.test."

// Two maximum-length TXT strings at one name overflow the 512-byte classic UDP
// limit — the wildcard-plus-apex case D7 calls out, in its largest form.
var longA, longB = strings.Repeat("a", 255), strings.Repeat("b", 255)

// start brings up the server on ephemeral ports and returns the UDP and TCP
// addresses. Port 0 gives the two listeners different ports; in production they
// share one configured address.
func start(t *testing.T) (udpAddr, tcpAddr string) {
	t.Helper()

	future := time.Now().Add(time.Hour)
	view := zoneview.New(func(context.Context) (map[string][]store.ChallengeValue, error) {
		return map[string][]store.ChallengeValue{
			"one":     {{Value: "single-value", ExpiresAt: future}},
			"two":     {{Value: longA, ExpiresAt: future}, {Value: longB, ExpiresAt: future}},
			"cleaned": {}, // alias exists, challenge was cleaned up
			"expired": {{Value: "stale", ExpiresAt: time.Now().Add(-time.Minute)}},
		}, nil
	})
	if err := view.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Zone:          strings.TrimSuffix(zone, "."),
		SOAMName:      "ns1.example.test",
		SOARName:      "hostmaster.example.test",
		SOASerial:     42,
		SOARefresh:    time.Hour,
		SOARetry:      15 * time.Minute,
		SOAExpire:     7 * 24 * time.Hour,
		SOAMinimum:    30 * time.Second,
		NSNames:       []string{"ns1.example.test", "ns2.example.test"},
		DNSListenAddr: "127.0.0.1:0",
		ChallengeTTL:  30 * time.Second,
	}

	// A nil counter on purpose here: these tests are about the answers, and the
	// handler must treat "no observation counter" as a no-op rather than as a
	// reason to guard every call site. TestAnsweredQueryIsObserved covers the
	// counting itself.
	s := New(cfg, view, obs.New(), nil, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return s.UDPAddr().String(), s.TCPAddr().String()
}

func query(t *testing.T, addr, network, name string, qtype uint16, udpSize uint16) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = true
	if udpSize > 0 {
		m.SetEdns0(udpSize, false)
	}
	c := &dns.Client{Net: network, Timeout: 5 * time.Second, UDPSize: udpSize}
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("%s query %s %s: %v", network, name, dns.Type(qtype), err)
	}
	return resp
}

func txtValues(m *dns.Msg) []string {
	var out []string
	for _, rr := range m.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			out = append(out, strings.Join(txt.Txt, ""))
		}
	}
	return out
}

func hasSOA(m *dns.Msg) bool {
	for _, rr := range m.Ns {
		if _, ok := rr.(*dns.SOA); ok {
			return true
		}
	}
	return false
}

func TestDecisionTable(t *testing.T) {
	udpAddr, _ := start(t)

	cases := []struct {
		name     string
		qname    string
		qtype    uint16
		rcode    int
		aa       bool
		wantSOA  bool
		wantTXT  []string
		wantAnsN int // -1: don't care
	}{
		{"txt known label with values", "one." + zone, dns.TypeTXT, dns.RcodeSuccess, true, false, []string{"single-value"}, 1},
		{"txt known label, cleaned up", "cleaned." + zone, dns.TypeTXT, dns.RcodeSuccess, true, true, nil, 0},
		{"txt known label, all expired", "expired." + zone, dns.TypeTXT, dns.RcodeSuccess, true, true, nil, 0},
		{"other type at known label", "one." + zone, dns.TypeA, dns.RcodeSuccess, true, true, nil, 0},
		{"unknown name in zone", "nosuchalias." + zone, dns.TypeTXT, dns.RcodeNameError, true, true, nil, 0},
		{"deep name in zone", "a.b." + zone, dns.TypeTXT, dns.RcodeNameError, true, true, nil, 0},
		{"out of zone", "www.example.org.", dns.TypeTXT, dns.RcodeRefused, false, false, nil, 0},
		{"apex soa", zone, dns.TypeSOA, dns.RcodeSuccess, true, false, nil, 1},
		{"apex ns", zone, dns.TypeNS, dns.RcodeSuccess, true, false, nil, 2},
		{"apex other type", zone, dns.TypeA, dns.RcodeSuccess, true, true, nil, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := query(t, udpAddr, "udp", tc.qname, tc.qtype, 0)

			if resp.Rcode != tc.rcode {
				t.Errorf("rcode = %s, want %s", dns.RcodeToString[resp.Rcode], dns.RcodeToString[tc.rcode])
			}
			if resp.Authoritative != tc.aa {
				t.Errorf("AA = %v, want %v", resp.Authoritative, tc.aa)
			}
			if resp.RecursionAvailable {
				t.Error("RA set; an authoritative-only server must never offer recursion")
			}
			if resp.Truncated {
				t.Error("TC set on an answer that fits")
			}
			if got := hasSOA(resp); got != tc.wantSOA {
				t.Errorf("SOA in authority = %v, want %v", got, tc.wantSOA)
			}
			if tc.wantAnsN >= 0 && len(resp.Answer) != tc.wantAnsN {
				t.Errorf("answer count = %d, want %d: %v", len(resp.Answer), tc.wantAnsN, resp.Answer)
			}
			if got := txtValues(resp); tc.wantTXT != nil && strings.Join(got, ",") != strings.Join(tc.wantTXT, ",") {
				t.Errorf("txt = %v, want %v", got, tc.wantTXT)
			}
		})
	}
}

// The distinction the whole zoneview exists for: a cleaned-up alias still exists.
func TestCleanedAliasIsNoerrorNotNXDomain(t *testing.T) {
	udpAddr, _ := start(t)

	cleaned := query(t, udpAddr, "udp", "cleaned."+zone, dns.TypeTXT, 0)
	unknown := query(t, udpAddr, "udp", "nosuchalias."+zone, dns.TypeTXT, 0)

	if cleaned.Rcode != dns.RcodeSuccess {
		t.Errorf("cleaned alias rcode = %s, want NOERROR", dns.RcodeToString[cleaned.Rcode])
	}
	if unknown.Rcode != dns.RcodeNameError {
		t.Errorf("unknown name rcode = %s, want NXDOMAIN", dns.RcodeToString[unknown.Rcode])
	}
}

func TestChallengeTTLAndSOAFields(t *testing.T) {
	udpAddr, _ := start(t)

	answer := query(t, udpAddr, "udp", "one."+zone, dns.TypeTXT, 0)
	if ttl := answer.Answer[0].Header().Ttl; ttl != 30 {
		t.Errorf("challenge TTL = %d, want 30", ttl)
	}

	apex := query(t, udpAddr, "udp", zone, dns.TypeSOA, 0)
	soa := apex.Answer[0].(*dns.SOA)
	if soa.Serial != 42 {
		t.Errorf("serial = %d, want 42", soa.Serial)
	}
	if soa.Minttl != 30 {
		t.Errorf("SOA minimum = %d, want 30; negative caching is bounded by this field", soa.Minttl)
	}
	if soa.Ns != "ns1.example.test." || soa.Mbox != "hostmaster.example.test." {
		t.Errorf("soa = %v", soa)
	}
}

// Two 255-byte TXT values at one name exceed 512: TC over plain UDP, and the
// full answer once the client retries over TCP.
func TestTruncationOverUDPSucceedsOverTCP(t *testing.T) {
	udpAddr, tcpAddr := start(t)

	udp := query(t, udpAddr, "udp", "two."+zone, dns.TypeTXT, 0)
	if !udp.Truncated {
		t.Fatalf("TC not set on an oversized UDP answer (%d answers)", len(udp.Answer))
	}

	tcp := query(t, tcpAddr, "tcp", "two."+zone, dns.TypeTXT, 0)
	if tcp.Truncated {
		t.Error("TC set over TCP")
	}
	if got := txtValues(tcp); len(got) != 2 || got[0] != longA || got[1] != longB {
		t.Errorf("tcp txt = %d values, want both", len(got))
	}
}

// The same answer fits once the client advertises a bigger EDNS(0) buffer.
func TestEDNS0BufferIsHonoured(t *testing.T) {
	udpAddr, _ := start(t)

	resp := query(t, udpAddr, "udp", "two."+zone, dns.TypeTXT, 4096)
	if resp.Truncated {
		t.Error("TC set although the client advertised 4096 bytes")
	}
	if len(txtValues(resp)) != 2 {
		t.Errorf("got %d values, want 2", len(txtValues(resp)))
	}
	if resp.IsEdns0() == nil {
		t.Error("no OPT in the reply to an EDNS(0) query")
	}
}

// A reply to a non-EDNS query must not carry an OPT record.
func TestNoOPTWithoutEDNS(t *testing.T) {
	udpAddr, _ := start(t)
	if resp := query(t, udpAddr, "udp", "one."+zone, dns.TypeTXT, 0); resp.IsEdns0() != nil {
		t.Error("OPT in a reply to a query that sent none")
	}
}

// A datagram whose header parses but whose body does not: FORMERR, and the
// server keeps serving afterwards.
func TestMalformedQueryGetsFORMERRAndServerSurvives(t *testing.T) {
	udpAddr, _ := start(t)

	m := new(dns.Msg)
	m.SetQuestion("one."+zone, dns.TypeTXT)
	wire, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("udp", udpAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(wire[:len(wire)-1]); err != nil { // header intact, question truncated mid-qclass
		t.Fatal(err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	var resp dns.Msg
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatal(err)
	}
	if resp.Rcode != dns.RcodeFormatError {
		t.Errorf("rcode = %s, want FORMERR", dns.RcodeToString[resp.Rcode])
	}

	if ok := query(t, udpAddr, "udp", "one."+zone, dns.TypeTXT, 0); ok.Rcode != dns.RcodeSuccess {
		t.Errorf("server did not keep serving: %s", dns.RcodeToString[ok.Rcode])
	}
}

// TestAnsweredQueryIsObserved pins the rule the interaction history depends on:
// a query is counted when, and only when, it is answered with a live value.
//
// The three negative cases matter as much as the positive one. An empty answer,
// an NXDOMAIN and a non-TXT question all say nothing about a certificate
// authority checking a validation, and counting them would turn "was this
// challenge actually queried" — the signal that distinguishes a missing customer
// CNAME from a working one — into noise that is always non-zero.
func TestAnsweredQueryIsObserved(t *testing.T) {
	future := time.Now().Add(time.Hour)
	view := zoneview.New(func(context.Context) (map[string][]store.ChallengeValue, error) {
		return map[string][]store.ChallengeValue{
			"one":     {{Value: "single-value", ExpiresAt: future}},
			"cleaned": {},
		}, nil
	})
	if err := view.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Zone:         strings.TrimSuffix(zone, "."),
		SOAMName:     "ns1.example.test",
		SOARName:     "hostmaster.example.test",
		SOAMinimum:   30 * time.Second,
		ChallengeTTL: 30 * time.Second,
	}

	for _, tc := range []struct {
		name  string
		qname string
		qtype uint16
		want  int64
	}{
		{"answered TXT is counted", "one." + zone, dns.TypeTXT, 1},
		{"empty answer is not", "cleaned." + zone, dns.TypeTXT, 0},
		{"nxdomain is not", "absent." + zone, dns.TypeTXT, 0},
		{"non-TXT at a live name is not", "one." + zone, dns.TypeA, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counter := queryobs.NewCounter()
			h := NewHandler(cfg, view, obs.New(), counter)

			m := new(dns.Msg)
			h.answer(m, dns.Question{Name: tc.qname, Qtype: tc.qtype, Qclass: dns.ClassINET})

			got, _ := counter.Drain()
			if got["one"].Count != tc.want {
				t.Errorf("observations for %q = %d, want %d", "one", got["one"].Count, tc.want)
			}
		})
	}
}
