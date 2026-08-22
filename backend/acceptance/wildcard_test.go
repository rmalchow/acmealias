//go:build acceptance

package acceptance

import (
	"slices"
	"testing"

	"github.com/miekg/dns"
)

// Task 2.2: the wildcard invariant (D6). Two domain rows, one challenge name,
// one alias, one CNAME — and therefore two TXT values that have to be
// answerable at that single name AT THE SAME TIME, because the CA validates
// both authorizations there.
//
// The two halves are asserted separately because they fail for different
// reasons: the SAN list says the order covered both names, the TXT observation
// says our DNS could actually answer for both at once. An implementation that
// replaced the value on each /present instead of adding to it would still
// produce a two-SAN certificate request and would fail here.
func TestWildcardAndApexShareOneChallengeName(t *testing.T) {
	iss := forcedIssuance(t)

	chain, err := servedChain(demoDomain)
	if err != nil {
		t.Fatalf("reading the served certificate: %v", err)
	}
	sans := chain[0].DNSNames
	for _, want := range []string{demoDomain, wildcardDomain} {
		if !slices.Contains(sans, want) {
			t.Errorf("the served certificate does not cover %q; its SANs are %v", want, sans)
		}
	}

	// The customer publishes one CNAME for both names. Confirm the fake
	// internet really serves exactly that single alias, so the "one shared
	// name" claim is not just an assumption about the seed.
	r, err := query(fakeNetDNS, challengeName, dns.TypeCNAME)
	if err != nil {
		t.Fatalf("resolving %s through the stack's DNS: %v", challengeName, err)
	}
	var targets []string
	for _, rr := range r.Answer {
		if c, ok := rr.(*dns.CNAME); ok {
			targets = append(targets, c.Target)
		}
	}
	if len(targets) != 1 || targets[0] != aliasFQDN {
		t.Fatalf("%s: got CNAME targets %v, want exactly [%s]", challengeName, targets, aliasFQDN)
	}

	// The observation taken from the service's own authoritative listener
	// while the order was in flight. Fewer than two values means the two
	// authorizations were never answerable together.
	if iss.maxTXT < 2 {
		t.Fatalf("only %d TXT value(s) were ever answerable at %s during the issuance (%v); "+
			"the apex and the wildcard must both be present at once",
			iss.maxTXT, aliasFQDN, iss.values)
	}
}
