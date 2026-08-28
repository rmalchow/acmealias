//go:build acceptance

package acceptance

import (
	"fmt"
	"testing"
	"time"
)

type interactionRow struct {
	ID          string    `json:"id"`
	AliasLabel  string    `json:"aliasLabel"`
	DomainNames []string  `json:"domainNames"`
	ClientIP    *string   `json:"clientIp"`
	PublishedAt time.Time `json:"publishedAt"`
	QueryCount  int64     `json:"queryCount"`
	Outcome     string    `json:"outcome"`
}

// Task 2.8: the overlap-attribution rule, end to end.
//
// A certificate covering demo.example.com and *.demo.example.com validates TWO
// authorizations at ONE challenge name. That produces two /present calls, two
// interaction rows — and only ONE round of TXT queries, because the CA asks the
// shared name once and gets both values back. The rule
// (specs/challenge-history/spec.md, "One query serves several concurrent
// authorizations") is that the query is credited to BOTH open interactions, not
// to whichever was published last.
//
// The unit tests exercise the SQL overlap predicate against hand-written rows.
// They cannot reach this: it takes a real lego publishing two values at one
// name and a real resolver querying it once.
//
// Note the assertion shape. "Both rows have a non-zero count" is deliberately
// not phrased as a total: an implementation that credited only the most recent
// interaction would pass any assertion about a sum.
func TestBothAuthorizationsAreCreditedForTheOneQuery(t *testing.T) {
	iss := forcedIssuance(t)

	s := mustSignInSeeded(t, adminUser)
	project := demoProject(t, s)

	var rows []interactionRow
	// The window of the forced issuance. Scoping by time rather than taking
	// "the last two" keeps this independent of what else in the suite has
	// published to this alias, and of any earlier `./scripts/dev.sh issue`.
	eventually(t, 2*time.Minute, "two closed interactions for "+aliasLabel, func() error {
		var all []interactionRow
		if err := s.getJSON(s.acct("/projects/"+project+"/interactions?limit=500"), &all); err != nil {
			return err
		}
		rows = rows[:0]
		for _, in := range all {
			if in.AliasLabel != aliasLabel {
				continue
			}
			if in.PublishedAt.Before(iss.start) || in.PublishedAt.After(iss.end.Add(time.Minute)) {
				continue
			}
			rows = append(rows, in)
		}
		if len(rows) != 2 {
			return fmt.Errorf("got %d, want 2", len(rows))
		}
		// Query counts are not written on the query path — the DNS server must
		// keep answering when the datastore does not — so they arrive with the
		// next flush, one ACME_VIEW_REFRESH_INTERVAL (30s in development)
		// later. Waiting for them here rather than asserting immediately is
		// the difference between testing the rule and testing the timer.
		for _, in := range rows {
			if in.QueryCount == 0 {
				return fmt.Errorf("interaction %s has no queries credited yet", in.ID)
			}
		}
		return nil
	})

	for _, in := range rows {
		if in.QueryCount == 0 {
			t.Errorf("interaction %s (%v) was never credited with a query, "+
				"although the one TXT query at %s answered both values",
				in.ID, in.DomainNames, aliasFQDN)
		}
		if in.Outcome != "validated" {
			t.Errorf("interaction %s (%v): outcome %q, want \"validated\" "+
				"(withdrawn after being queried at least once)", in.ID, in.DomainNames, in.Outcome)
		}
	}
}
