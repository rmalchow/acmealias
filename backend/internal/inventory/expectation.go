package inventory

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// DefaultTTL is the TTL suggested for the records the customer publishes in
// *their* zone. It has nothing to do with the TTL we answer challenges with.
const DefaultTTL = 300

// PlanOptions is the configuration an expectation depends on: the zone this
// service serves (ACME_ZONE), and the CA identifiers CAA entries should
// authorize (ACME_CAA_ISSUER) — empty means expect no CAA at all.
type PlanOptions struct {
	Zone       string
	CAAIssuers []string
}

// DomainExpectation is everything one domain needs published in somebody
// else's zone, expressed per domain rather than as a set of records.
//
// The plan this replaced built a deduplicated record set and then joined it
// back onto domains, which is what made a per-domain answer awkward to give
// (change domain-diagnostics, G2). The deduplication that mattered has not
// gone away — it has moved to the reader: two domains that share an alias
// carry the identical ChallengeName and Target, and every domain under one
// registrable name carries the identical ExpectedCAA, so a caller that wants
// one lookup per distinct value groups on the value rather than on the domain.
//
// Address records are deliberately absent and are not coming back: the
// customer typed those addresses into the Nodes panel themselves, and grading
// their DNS against them is a check whose only possible finding is that the
// customer disagrees with the customer (G2).
type DomainExpectation struct {
	DomainID      model.ID
	Name          string   // "shop.example.com", "*.example.com"
	ChallengeName string   // where the CNAME must be published
	AliasID       model.ID // shared by a wildcard and its apex
	Target        string   // what the CNAME must point at, "" when no alias exists yet
	Registrable   string   // the name CAA for this domain lives at, "" when it has none
	ExpectedCAA   []string // rendered rdata, `0 issue "letsencrypt.org"`; nil with no issuer configured
}

// Expectations returns one DomainExpectation per domain in a project, in the
// project's own domain order.
//
// It is still authorized with authz.ActionDNSPlan: the permission gates
// verification, which is the one caller left, and renaming a permission string
// means migrating role_grants rows for a spelling.
func Expectations(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, projectID model.ID, opts PlanOptions) ([]DomainExpectation, error) {
	project, err := store.GetProject(ctx, db, sc, projectID)
	if err != nil {
		return nil, err
	}
	if err := authorize(subj, authz.ActionDNSPlan, authz.ObjectOf(project)); err != nil {
		return nil, err
	}
	domains, err := store.ListDomainsForProject(ctx, db, sc, projectID)
	if err != nil {
		return nil, err
	}
	aliases, err := store.ListAliasesForProject(ctx, db, sc, projectID)
	if err != nil {
		return nil, err
	}
	return buildExpectations(domains, aliases, opts), nil
}

// buildExpectations is Expectations without the database, which is also what
// makes the rules testable without one.
func buildExpectations(domains []model.Domain, aliases []model.Alias, opts PlanOptions) []DomainExpectation {
	byID := make(map[model.ID]model.Alias, len(aliases))
	for _, a := range aliases {
		byID[a.ID] = a
	}

	out := make([]DomainExpectation, 0, len(domains))
	for _, d := range domains {
		// Every domain gets a row, including one whose alias has gone missing:
		// the row still exists on the screen and still deserves an answer, and
		// an empty Target says "we have nothing to ask you for" rather than
		// dropping the domain out of the verification silently.
		e := DomainExpectation{
			DomainID: d.ID, Name: d.Name,
			ChallengeName: dnsname.ChallengeName(d.Name), AliasID: d.AliasID,
		}
		if a, ok := byID[d.AliasID]; ok {
			// Taken from the alias, not recomputed, because ChallengeName is the
			// key a result is joined back on. The two agree — an alias is created
			// from ChallengeName(d.Name) and never re-labelled — but agreeing by
			// construction beats agreeing by an equality some other test has to
			// keep defending, and the failure if they ever diverged would be
			// silent: every row reading "not checked" for a delegation that is
			// correct.
			e.ChallengeName = a.ChallengeFQDN
			e.Target = DelegationTarget(a, opts.Zone)
		}
		if r, err := dnsname.RegistrableDomain(d.Name); err == nil {
			// Recorded even when no CAA is expected (no issuer configured): this
			// says where CAA for the domain lives, not that we checked it.
			e.Registrable = r
			e.ExpectedCAA = caaLines(r, opts.CAAIssuers)
		}
		out = append(out, e)
	}
	return out
}

// DelegationTarget renders what a domain's `_acme-challenge` CNAME must point
// at: the alias's label in the zone this service serves.
//
// Exported because the domain read renders the same string from the same alias
// (change domain-diagnostics, 10.1) and two spellings of it would be two
// spellings a customer could be shown, one of which would be wrong.
func DelegationTarget(a model.Alias, zone string) string {
	return a.Label + "." + strings.TrimSuffix(strings.ToLower(zone), ".")
}

// caaLines renders the CAA rdata expected at one registrable name.
//
// It is the registrable name and never the host: CAA is inherited and the
// nearest ancestor wins, so a set at shop.example.com would shadow the one at
// example.com for that host alone. Both tags are emitted, since a present
// issuewild governs wildcard issuance *instead of* issue, and wildcards are the
// case this service exists for.
//
// No issuer configured returns nil rather than a guess — naming a CA we do not
// use would lock issuance out entirely — and nil is also what lets the console
// hide the CAA dot rather than showing a grey one for a check nobody asked for.
func caaLines(registrable string, issuers []string) []string {
	if registrable == "" || len(issuers) == 0 {
		return nil
	}
	var out []string
	for _, tag := range []string{"issue", "issuewild"} {
		for _, issuer := range issuers {
			out = append(out, fmt.Sprintf("0 %s %q", tag, issuer))
		}
	}
	return out
}
