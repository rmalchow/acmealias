package resolvercheck

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// startTestServer stands in for a customer's zone: it answers exactly the
// records the tests below assert against, over UDP on an ephemeral port.
func startTestServer(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: handler}
	go func() {
		_ = srv.ActivateAndServe()
	}()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}

func rr(t *testing.T, s string) dns.RR {
	t.Helper()
	r, err := dns.NewRR(s)
	if err != nil {
		t.Fatalf("NewRR(%q): %v", s, err)
	}
	return r
}

func TestCheckCNAME(t *testing.T) {
	handler := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		q := r.Question[0]
		switch q.Name {
		case "_acme-challenge.correct.example.com.":
			m.Answer = append(m.Answer, rr(t, q.Name+" 300 IN CNAME target.acmeservice.cloudpowered.services."))
		case "_acme-challenge.wrong.example.com.":
			m.Answer = append(m.Answer, rr(t, q.Name+" 300 IN CNAME somewhere-else.example.net."))
		case "_acme-challenge.missing.example.com.":
			m.Rcode = dns.RcodeNameError
		default:
			m.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(m)
	}
	resolver := startTestServer(t, handler)
	c := &Checker{Resolver: resolver, Timeout: 2 * time.Second}

	tests := []struct {
		name       string
		expected   string
		wantStatus Status
		wantFound  string
	}{
		{"_acme-challenge.correct.example.com", "target.acmeservice.cloudpowered.services.", StatusCorrect, ""},
		{"_acme-challenge.wrong.example.com", "target.acmeservice.cloudpowered.services.", StatusIncorrect, "somewhere-else.example.net."},
		{"_acme-challenge.missing.example.com", "target.acmeservice.cloudpowered.services.", StatusMissing, ""},
	}
	for _, tc := range tests {
		got := c.CheckCNAME(CNAMECheck{Name: tc.name, Expected: tc.expected})
		if got.Status != tc.wantStatus {
			t.Errorf("CheckCNAME(%q) status = %q, want %q", tc.name, got.Status, tc.wantStatus)
		}
		if tc.wantFound != "" && got.Found != tc.wantFound {
			t.Errorf("CheckCNAME(%q) found = %q, want %q", tc.name, got.Found, tc.wantFound)
		}
	}
}

func TestCheckCNAMEUndetermined(t *testing.T) {
	// A resolver address nothing is listening on: the exchange itself must
	// fail (connection refused / timeout), reported as undetermined and
	// distinct from missing.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	resolver := pc.LocalAddr().String()
	_ = pc.Close() // nothing will answer at this address now

	c := &Checker{Resolver: resolver, Timeout: 300 * time.Millisecond}
	got := c.CheckCNAME(CNAMECheck{Name: "_acme-challenge.example.com", Expected: "target.example.net."})
	if got.Status != StatusUndetermined {
		t.Fatalf("status = %q, want %q", got.Status, StatusUndetermined)
	}
	if got.Found == "" {
		t.Fatal("Found should describe the failure")
	}
}

func TestCheckAddress(t *testing.T) {
	handler := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		q := r.Question[0]
		switch {
		case q.Name == "correct.example.com." && q.Qtype == dns.TypeA:
			m.Answer = append(m.Answer,
				rr(t, q.Name+" 300 IN A 203.0.113.10"),
				rr(t, q.Name+" 300 IN A 203.0.113.11"))
		case q.Name == "correct.example.com." && q.Qtype == dns.TypeAAAA:
			m.Answer = append(m.Answer, rr(t, q.Name+" 300 IN AAAA 2001:db8:1::10"))
		case q.Name == "wrong.example.com." && q.Qtype == dns.TypeA:
			m.Answer = append(m.Answer, rr(t, q.Name+" 300 IN A 198.51.100.5"))
		case q.Name == "missing.example.com.":
			m.Rcode = dns.RcodeNameError
		default:
			m.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(m)
	}
	resolver := startTestServer(t, handler)
	c := &Checker{Resolver: resolver, Timeout: 2 * time.Second}

	tests := []struct {
		name       string
		family     string
		want       []net.IP
		wantStatus Status
	}{
		{"correct.example.com", "A", []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("203.0.113.11")}, StatusCorrect},
		{"correct.example.com", "AAAA", []net.IP{net.ParseIP("2001:db8:1::10")}, StatusCorrect},
		{"wrong.example.com", "A", []net.IP{net.ParseIP("203.0.113.10")}, StatusIncorrect},
		{"missing.example.com", "A", []net.IP{net.ParseIP("203.0.113.10")}, StatusMissing},
	}
	for _, tc := range tests {
		got := c.CheckAddress(AddressCheck{Name: tc.name, Family: tc.family, Want: tc.want})
		if got.Status != tc.wantStatus {
			t.Errorf("CheckAddress(%q,%s) status = %q, want %q (found %q)", tc.name, tc.family, got.Status, tc.wantStatus, got.Found)
		}
	}
}

func TestCheckCAA(t *testing.T) {
	handler := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		q := r.Question[0]
		switch q.Name {
		case "authorized.example.com.":
			m.Answer = append(m.Answer,
				rr(t, q.Name+` 300 IN CAA 0 issue "letsencrypt.org"`),
				rr(t, q.Name+` 300 IN CAA 0 issuewild "letsencrypt.org"`))
		case "otherauthority.example.com.":
			m.Answer = append(m.Answer, rr(t, q.Name+` 300 IN CAA 0 issue "someotherca.example"`))
		case "absent.example.com.":
			// NOERROR, no answer: no CAA record at all.
		default:
			m.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(m)
	}
	resolver := startTestServer(t, handler)
	c := &Checker{Resolver: resolver, Timeout: 2 * time.Second}

	tests := []struct {
		domain     string
		wantStatus CAAStatus
	}{
		{"authorized.example.com", CAACorrect},
		{"otherauthority.example.com", CAABlocking},
		{"absent.example.com", CAANotPublished},
	}
	for _, tc := range tests {
		got := c.CheckCAA(CAACheck{Domain: tc.domain, Issuers: []string{"letsencrypt.org"}})
		if got.Status != tc.wantStatus {
			t.Errorf("CheckCAA(%q) status = %q, want %q (found %q)", tc.domain, got.Status, tc.wantStatus, got.Found)
		}
	}

	// The one place this table is backwards on purpose: absent is fine,
	// present-but-wrong is fatal (design D5).
	if got := c.CheckCAA(CAACheck{Domain: "absent.example.com", Issuers: []string{"letsencrypt.org"}}); got.Status == CAABlocking {
		t.Fatal("an absent CAA record must never be reported as blocking")
	}
	if got := c.CheckCAA(CAACheck{Domain: "otherauthority.example.com", Issuers: []string{"letsencrypt.org"}}); got.Found == "" {
		t.Fatal("a blocking CAA result must report what was found")
	}
}

func TestCheckCAAUndetermined(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	resolver := pc.LocalAddr().String()
	_ = pc.Close()

	c := &Checker{Resolver: resolver, Timeout: 300 * time.Millisecond}
	got := c.CheckCAA(CAACheck{Domain: "example.com", Issuers: []string{"letsencrypt.org"}})
	if got.Status != CAAUndetermined {
		t.Fatalf("status = %q, want %q", got.Status, CAAUndetermined)
	}
}
