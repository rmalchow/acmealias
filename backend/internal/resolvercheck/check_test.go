package resolvercheck

import (
	"context"
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
		got := c.CheckCNAME(context.Background(), CNAMECheck{Name: tc.name, Expected: tc.expected})
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
	got := c.CheckCNAME(context.Background(), CNAMECheck{Name: "_acme-challenge.example.com", Expected: "target.example.net."})
	if got.Status != StatusUndetermined {
		t.Fatalf("status = %q, want %q", got.Status, StatusUndetermined)
	}
	if got.Found == "" {
		t.Fatal("Found should describe the failure")
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
		case "pinned.example.com.":
			m.Answer = append(m.Answer,
				rr(t, q.Name+` 300 IN CAA 0 issue "letsencrypt.org; accounturi=https://acme-v02.api.letsencrypt.org/acme/acct/1"`),
				rr(t, q.Name+` 300 IN CAA 0 issuewild "letsencrypt.org; accounturi=https://acme-v02.api.letsencrypt.org/acme/acct/1"`))
		case "halfpinned.example.com.":
			// The wildcard is pinned and plain issuance is not. Anybody holding
			// the delegation can still get a certificate for the name itself.
			m.Answer = append(m.Answer,
				rr(t, q.Name+` 300 IN CAA 0 issue "letsencrypt.org"`),
				rr(t, q.Name+` 300 IN CAA 0 issuewild "letsencrypt.org; accounturi=https://acme-v02.api.letsencrypt.org/acme/acct/1"`))
		case "namedlikeaparam.example.com.":
			// An authority whose name merely contains the word: no parameter is
			// present, so nothing is pinned.
			m.Answer = append(m.Answer,
				rr(t, q.Name+` 300 IN CAA 0 issue "accounturi.example"`),
				rr(t, q.Name+` 300 IN CAA 0 issuewild "accounturi.example"`))
		case "emptyparam.example.com.":
			m.Answer = append(m.Answer,
				rr(t, q.Name+` 300 IN CAA 0 issue "letsencrypt.org; accounturi="`),
				rr(t, q.Name+` 300 IN CAA 0 issuewild "letsencrypt.org; accounturi="`))
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
		// Names the authority and nothing else: issuance works, and works for
		// every account at that authority — including this service's.
		{"authorized.example.com", CAAUnconstrained},
		{"pinned.example.com", CAACorrect},
		{"halfpinned.example.com", CAAUnconstrained},
		{"namedlikeaparam.example.com", CAABlocking},
		{"emptyparam.example.com", CAAUnconstrained},
		{"otherauthority.example.com", CAABlocking},
		{"absent.example.com", CAANotPublished},
	}
	for _, tc := range tests {
		got := c.CheckCAA(context.Background(), CAACheck{Domain: tc.domain, Issuers: []string{"letsencrypt.org"}})
		if got.Status != tc.wantStatus {
			t.Errorf("CheckCAA(%q) status = %q, want %q (found %q)", tc.domain, got.Status, tc.wantStatus, got.Found)
		}
	}

	// Absent and blocking stay different answers even though both are now shown
	// in red: one is "publish a record" and the other is "change the record you
	// have", which are instructions for different people on different days.
	if got := c.CheckCAA(context.Background(), CAACheck{Domain: "absent.example.com", Issuers: []string{"letsencrypt.org"}}); got.Status == CAABlocking {
		t.Fatal("an absent CAA record must never be reported as blocking")
	}

	// Found is populated in every state that has rdata to show, because the
	// console prints it beside the record it recommends.
	for _, d := range []string{"authorized.example.com", "pinned.example.com", "otherauthority.example.com"} {
		if got := c.CheckCAA(context.Background(), CAACheck{Domain: d, Issuers: []string{"letsencrypt.org"}}); got.Found == "" {
			t.Errorf("CheckCAA(%q): Found is empty, so the console has nothing to print in the second column", d)
		}
	}
	if got := c.CheckCAA(context.Background(), CAACheck{Domain: "otherauthority.example.com", Issuers: []string{"letsencrypt.org"}}); got.Found == "" {
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
	got := c.CheckCAA(context.Background(), CAACheck{Domain: "example.com", Issuers: []string{"letsencrypt.org"}})
	if got.Status != CAAUndetermined {
		t.Fatalf("status = %q, want %q", got.Status, CAAUndetermined)
	}
}
