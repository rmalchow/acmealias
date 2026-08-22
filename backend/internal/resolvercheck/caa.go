package resolvercheck

import (
	"strings"

	"github.com/miekg/dns"
)

// CAAStatus is the outcome of checking a CAA record set. Unlike Status, an
// absent record is the good outcome here — design D5 and the spec
// requirement "CAA verification distinguishes absent from conflicting" both
// call this out because the intuitive reading is backwards.
type CAAStatus string

const (
	// CAACorrect means a CAA set exists and authorizes a configured issuer
	// for both plain and wildcard issuance.
	CAACorrect CAAStatus = "correct"
	// CAANotPublished means no CAA record exists — NOT a failure: absent CAA
	// permits any certificate authority to issue.
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
	// Found lists the authorities actually published, populated when Status
	// is CAABlocking.
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
func (c *Checker) CheckCAA(check CAACheck) CAAResult {
	resp, err := c.lookup(check.Domain, dns.TypeCAA)
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

	// issuewild governs wildcard issuance instead of issue when present.
	wildAuthority := issuewild
	if len(wildAuthority) == 0 {
		wildAuthority = issue
	}
	if authorizes(issue, check.Issuers) && authorizes(wildAuthority, check.Issuers) {
		return CAAResult{Status: CAACorrect}
	}
	return CAAResult{Status: CAABlocking, Found: strings.Join(append(append([]string{}, issue...), issuewild...), ", ")}
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
