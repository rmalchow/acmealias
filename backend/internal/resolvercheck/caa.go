package resolvercheck

import (
	"context"
	"strings"

	"github.com/miekg/dns"
)

// CAAStatus is the outcome of checking a CAA record set.
//
// THIS VOCABULARY WAS REVERSED, and the reversal is the point of it. It used to
// read absence as the good outcome, on the grounds that a domain publishing no
// CAA lets any authority issue and therefore lets OURS issue. That is true and
// it is the wrong question. The question a customer of this service should be
// asking is not "may my authority issue" but "who else may", and the honest
// answer to that on a delegated domain includes US: a customer publishes one
// CNAME pointing the challenge name into our zone, and from that moment we can
// complete a DNS-01 validation for that name — and for its wildcard, since a
// wildcard and its base share one challenge name (dnsname.ChallengeName). Every
// hosted responder for DNS-01 holds that power over the domains delegated to it.
// It is what the customer is buying and it is not something we can give back.
//
// What a customer CAN do is bound it, and there is exactly one record that does:
// RFC 8657's accounturi parameter, which pins issuance to one ACME account. A
// CAA set that names our configured authority without accounturi does not
// restrict us at all — any account at that authority may issue, and ours is one.
// A set that carries accounturi does: an order from any other account is refused
// by the authority itself, whatever this service does or says.
//
// So the three outcomes below grade a domain by how much of that is left open,
// which is why absence is now the WORST of them rather than the best.
type CAAStatus string

const (
	// CAACorrect means a CAA set exists, authorizes a configured issuer for
	// both plain and wildcard issuance, AND pins every authorization it grants
	// to a specific ACME account. This is the only state in which holding the
	// customer's delegation does not also mean being able to obtain their
	// certificates.
	//
	// It does NOT mean the account named is the customer's. Nothing visible in
	// DNS says whose an account URI is, and a check that claimed otherwise
	// would be asserting something it cannot see.
	CAACorrect CAAStatus = "correct"
	// CAAUnconstrained means the set authorizes a configured issuer but names
	// no account: issuance works, and works for anybody holding the delegation.
	// The remedy is a parameter on a record the customer already publishes,
	// which is why this is its own state and not a variety of CAACorrect.
	CAAUnconstrained CAAStatus = "unconstrained"
	// CAANotPublished means no CAA record exists. Any authority may issue, by
	// any method, from any account.
	//
	// It was previously reported as the good outcome and is not one. Nothing is
	// BROKEN in this state — this is the state most domains on the internet are
	// in, and certificates issue perfectly well — but nothing is bounded in it
	// either, and it is the one state a delegating customer can leave by
	// publishing a single record.
	CAANotPublished CAAStatus = "not-published"
	// CAABlocking means a CAA set exists but does not authorize a configured
	// issuer — issuance will fail outright until it is fixed.
	CAABlocking CAAStatus = "blocking"
	// CAAUndetermined means the lookup itself failed or timed out.
	CAAUndetermined CAAStatus = "undetermined"
)

// CAAResult is what checking one registrable domain's CAA set produced.
type CAAResult struct {
	Status CAAStatus
	// Found is the issue and issuewild rdata actually published, in the order
	// the answer carried it, and it is populated in EVERY state that has
	// something to show — not only in CAABlocking as it once was. A screen that
	// prints the record a domain needs beside the record it has cannot fill the
	// second column from a field that is only set when the news is bad.
	Found string
}

// CAACheck is one row: the registrable domain design D5 places CAA records
// at (dnsname.RegistrableDomain), and the issuer identifiers configured via
// ACME_CAA_ISSUER that must be authorized.
type CAACheck struct {
	Domain  string   // e.g. "example.com"
	Issuers []string // e.g. []string{"letsencrypt.org"}
}

// CheckCAA verifies the CAA set at Domain.
//
// ponytail: this queries exactly Domain rather than walking every ancestor
// up to the public suffix (the full RFC 8659 tree-climbing algorithm).
// Design D5 only ever publishes CAA at the registrable domain, and Domain
// here is expected to already be one (dnsname.RegistrableDomain) — so the
// only "above" a real plan needs is covered. Add the walk if a scenario ever
// needs CAA authority inherited from a grandparent zone.
func (c *Checker) CheckCAA(ctx context.Context, check CAACheck) CAAResult {
	resp, err := c.lookup(ctx, check.Domain, dns.TypeCAA)
	if err != nil {
		return CAAResult{Status: CAAUndetermined, Found: err.Error()}
	}
	if resp.Rcode == dns.RcodeNameError || len(resp.Answer) == 0 {
		return CAAResult{Status: CAANotPublished}
	}

	var issue, issuewild []string
	for _, rr := range resp.Answer {
		caa, ok := rr.(*dns.CAA)
		if !ok {
			continue
		}
		switch caa.Tag {
		case "issue":
			issue = append(issue, caa.Value)
		case "issuewild":
			issuewild = append(issuewild, caa.Value)
		}
	}
	if len(issue) == 0 && len(issuewild) == 0 {
		// Only unrelated tags were published (e.g. iodef) — no issue or
		// issuewild constraint means any CA may issue, same as no record.
		return CAAResult{Status: CAANotPublished}
	}

	found := strings.Join(append(append([]string{}, issue...), issuewild...), ", ")

	// issuewild governs wildcard issuance instead of issue when present.
	wildAuthority := issuewild
	if len(wildAuthority) == 0 {
		wildAuthority = issue
	}
	if !authorizes(issue, check.Issuers) || !authorizes(wildAuthority, check.Issuers) {
		return CAAResult{Status: CAABlocking, Found: found}
	}
	// Both halves are pinned or neither counts. A wildcard authorized by an
	// account-pinned issuewild beside a bare issue is still a domain anybody
	// holding the delegation can get a certificate for — just not a wildcard
	// one — and calling that constrained would be reporting the better half.
	if pinned(issue) && pinned(wildAuthority) {
		return CAAResult{Status: CAACorrect, Found: found}
	}
	return CAAResult{Status: CAAUnconstrained, Found: found}
}

// authorizes reports whether tags is empty (no restriction of that kind) or
// names one of the configured issuers. A CAA value may carry parameters
// after a ';' (e.g. "letsencrypt.org; validationmethods=dns-01"); only the
// authority identifier before it is compared.
func authorizes(tags []string, issuers []string) bool {
	if len(tags) == 0 {
		return true
	}
	for _, t := range tags {
		identifier := strings.TrimSpace(strings.SplitN(t, ";", 2)[0])
		for _, want := range issuers {
			if strings.EqualFold(identifier, want) {
				return true
			}
		}
	}
	return false
}

// pinned reports whether EVERY value in tags carries an accounturi parameter
// (RFC 8657), which is what limits issuance to one ACME account.
//
// Every value and not any: a set is only as tight as its loosest entry, and one
// unpinned `issue "letsencrypt.org"` beside a pinned one leaves issuance open to
// every account at that authority.
//
// An empty tags list is NOT pinned, which is the case that matters most and
// reads backwards next to authorizes() above. No issue tag means no restriction
// on plain issuance at all — every authority, every account — so the two
// functions disagree about the empty list on purpose: it authorizes everyone and
// pins no one.
func pinned(tags []string) bool {
	if len(tags) == 0 {
		return false
	}
	for _, t := range tags {
		if !hasAccountURI(t) {
			return false
		}
	}
	return true
}

// hasAccountURI reports whether one CAA value carries a non-empty accounturi
// parameter. Parameters follow the authority identifier after a ';' and are
// themselves ';'-separated `name=value` pairs, so this walks the tail rather
// than searching the whole string — `issue "accounturi.example.com"` names an
// authority that happens to be spelled like the parameter and constrains
// nothing.
func hasAccountURI(value string) bool {
	parts := strings.Split(value, ";")
	for _, p := range parts[1:] {
		name, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "accounturi") && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}
