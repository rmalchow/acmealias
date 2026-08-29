// Package resolvercheck resolves a DNS plan's records (design D5) against a
// configured resolver — never the process's implicit system resolver — and
// reports what is actually published. Results are advisory only: DNS is
// somebody else's mutable state, so nothing here is ever persisted as fact
// (spec "Required DNS records can be verified live").
//
// The plan itself is generated elsewhere (task group 7); this package only
// consumes the small per-row types below, which a plan generator builds one
// of per record it wants checked.
package resolvercheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// Status is the outcome of checking one CNAME or address row.
type Status string

const (
	// StatusCorrect means the record matches what the plan requires.
	StatusCorrect Status = "correct"
	// StatusMissing means the name does not resolve at all for the record type checked.
	StatusMissing Status = "missing"
	// StatusIncorrect means the name resolves, but to something else. Found
	// carries what was actually there.
	StatusIncorrect Status = "incorrect"
	// StatusUndetermined means the lookup itself failed or timed out — distinct
	// from absence, and retryable.
	StatusUndetermined Status = "undetermined"
)

// Result is what checking one record produced.
type Result struct {
	Status Status
	// Found describes what was actually resolved, populated when it differs
	// from what was required (Incorrect) or when the lookup failed (Undetermined).
	Found string
}

// CNAMECheck is one challenge-delegation row of a DNS plan: Name must CNAME
// to Expected (design D5's `_acme-challenge.<base>` row).
type CNAMECheck struct {
	Name     string // e.g. "_acme-challenge.shop.example.com"
	Expected string // e.g. "shop-example-com.acmeservice.cloudpowered.services."
}

// Checker resolves plan rows against one configured resolver and caches raw
// answers briefly (cache.go) — configurable per design D5 (the DNS plan) so a
// check can run against a public resolver as well as the system default.
//
// One Checker is meant to be kept and shared: the cache is what makes the
// per-domain check affordable now that it runs whenever a screen opens
// (domain-setup-feedback D1), and a Checker built per request would carry an
// empty cache every time. It is safe for concurrent use — the cache is the
// only mutable state and takes a lock — and the fields must not be written
// after the first lookup.
type Checker struct {
	// Resolver is the "host:port" address queried, e.g. "1.1.1.1:53" or a
	// test server's ephemeral loopback address.
	Resolver string
	// Timeout bounds each lookup; zero uses defaultTimeout.
	Timeout time.Duration

	cache cache
}

const defaultTimeout = 5 * time.Second

func (c *Checker) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeout
}

// exchange sends one question to the configured resolver. A transport error
// or a resolver-side failure (SERVFAIL and friends) is returned as an error;
// NXDOMAIN is a normal, successful answer meaning "does not exist".
//
// The context is the caller's — for the management API, the HTTP request's. A
// customer who closes the tab must not leave a blackholing resolver being
// waited on for the full timeout per question: the check runs on page load
// now, so the work nobody is waiting for is work that piles up
// (domain-setup-feedback D1's cost argument only holds if abandoned checks
// stop).
func (c *Checker) exchange(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	client := &dns.Client{Timeout: c.timeout()}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = true
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	resp, _, err := client.ExchangeContext(ctx, m, c.Resolver)
	if err != nil {
		return nil, fmt.Errorf("query %s %s: %w", dns.TypeToString[qtype], name, err)
	}
	if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
		return nil, fmt.Errorf("query %s %s: resolver returned %s", dns.TypeToString[qtype], name, dns.RcodeToString[resp.Rcode])
	}
	return resp, nil
}

// lookup is exchange with the short result cache (cache.go) in front.
func (c *Checker) lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	key := cacheKey{resolver: c.Resolver, name: strings.ToLower(dns.Fqdn(name)), qtype: qtype}
	if e, ok := c.cache.get(key); ok {
		return e.msg, e.err
	}
	msg, err := c.exchange(ctx, name, qtype)
	c.cache.put(key, cacheEntry{msg: msg, err: err})
	return msg, err
}

// CheckCNAME verifies one challenge-delegation row.
func (c *Checker) CheckCNAME(ctx context.Context, check CNAMECheck) Result {
	resp, err := c.lookup(ctx, check.Name, dns.TypeCNAME)
	if err != nil {
		return Result{Status: StatusUndetermined, Found: err.Error()}
	}
	if resp.Rcode == dns.RcodeNameError || len(resp.Answer) == 0 {
		return Result{Status: StatusMissing}
	}

	want := strings.ToLower(dns.Fqdn(check.Expected))
	var found []string
	for _, rr := range resp.Answer {
		cname, ok := rr.(*dns.CNAME)
		if !ok {
			// Something other than a CNAME sits directly at the name (e.g. a
			// TXT record) — a valid, if surprising, thing to find.
			found = append(found, rr.String())
			continue
		}
		if strings.ToLower(cname.Target) == want {
			// Found is filled in on the CORRECT path too, and it is not
			// redundant with Expected: a screen that shows what a name needs
			// beside what it has would otherwise print an empty second column
			// for every row that is right, which reads as "we did not look".
			return Result{Status: StatusCorrect, Found: cname.Target}
		}
		found = append(found, cname.Target)
	}
	return Result{Status: StatusIncorrect, Found: strings.Join(found, ", ")}
}
