package activation

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/resolvercheck"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// TestTargetMatchesThePlan is the smallest test in the package and the one
// worth the most.
//
// target() assembles the string the delegation must point at, and inventory
// assembles the same string to show the customer. If they ever differ, every
// check in the deployment answers "incorrect" for a delegation that is in fact
// correct, and nothing activates — for ever, silently. The trailing dot is the
// way that happens in practice: ACME_ZONE is a zone name, and a zone name is
// commonly written with the root dot.
func TestTargetMatchesThePlan(t *testing.T) {
	for _, tc := range []struct {
		zone, want string
	}{
		{"acmeservice.example.net", "shop-example-com.acmeservice.example.net"},
		{"acmeservice.example.net.", "shop-example-com.acmeservice.example.net"},
		{"ACMEservice.Example.NET.", "shop-example-com.acmeservice.example.net"},
	} {
		if got := target("shop-example-com", tc.zone); got != tc.want {
			t.Errorf("target(label, %q) = %q, want %q", tc.zone, got, tc.want)
		}
	}
}

// TestLookupAnswersInTheOrderItWasAsked pins the correspondence Pass depends on:
// results[i] is the answer about due[i]. The lookups run concurrently and write
// into a shared slice by index, so an off-by-one here would not fail — it would
// activate one customer's domain on another customer's delegation.
//
// The batch is deliberately larger than `parallelism`, so the answers do not
// arrive in the order they were asked.
func TestLookupAnswersInTheOrderItWasAsked(t *testing.T) {
	const batch = parallelism * 3
	const zone = "acmeservice.example.net"

	// Only the even-numbered delegations are published, and each points at its
	// own label — so a shifted mapping turns every answer wrong rather than
	// merely rearranging them.
	published := map[string]string{}
	due := make([]store.DueDelegation, batch)
	for i := range due {
		due[i] = store.DueDelegation{
			AliasID:       model.NewID(),
			ChallengeFQDN: "_acme-challenge." + label(i) + ".example.com",
			Label:         label(i),
		}
		if i%2 == 0 {
			published[dns.Fqdn(due[i].ChallengeFQDN)] = target(due[i].Label, zone)
		}
	}

	resolver := startTestServer(t, func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		q := r.Question[0]
		if to, ok := published[q.Name]; ok {
			answer, err := dns.NewRR(q.Name + " 300 IN CNAME " + dns.Fqdn(to))
			if err != nil {
				t.Errorf("NewRR: %v", err)
				return
			}
			m.Answer = append(m.Answer, answer)
		} else {
			m.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(m)
	})

	c := &resolvercheck.Checker{Resolver: resolver, Timeout: 2 * time.Second}
	got := lookup(context.Background(), c, zone, due)
	if len(got) != len(due) {
		t.Fatalf("lookup returned %d answers for %d questions", len(got), len(due))
	}
	for i := range due {
		want := resolvercheck.StatusMissing
		if i%2 == 0 {
			want = resolvercheck.StatusCorrect
		}
		if got[i] != want {
			t.Errorf("answer %d (%s) = %q, want %q — the results are not aligned with the batch",
				i, due[i].ChallengeFQDN, got[i], want)
		}
	}
}

func label(i int) string {
	return "lb" + string(rune('a'+i%26)) + string(rune('a'+i/26))
}

// startTestServer stands in for the customers' zones, answering over UDP on an
// ephemeral port. Same shape as resolvercheck's own test server; duplicated
// rather than exported, because a fake resolver is not part of that package's
// contract.
func startTestServer(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: handler}
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}
