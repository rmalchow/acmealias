// Package reporting turns the challenge-interaction history into the two numbers
// the console reports, and writes the one thing that history cannot answer.
//
// Two numbers, never conflated (design D5):
//
//	challenge attempts   one published TXT value — the raw row count.
//	issuance attempts    challenge attempts at ONE alias whose live windows
//	                     overlap, grouped into one.
//
// Neither is a count of certificates. This service never sees a certificate:
// finalization happens between the load balancer's ACME client and the CA, long
// after the TXT value it asked us to publish has been withdrawn. A figure
// labelled "certificates issued" would be believed, so nothing here is labelled
// that way.
package reporting

import (
	"sort"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Issuance is a group of challenge attempts treated as one certificate request.
type Issuance struct {
	// AliasLabel is the challenge name all members were published at.
	AliasLabel string
	// Start is the earliest publication in the group; End the latest end of a
	// member's live window, or the zero time while a member is still published.
	Start time.Time
	End   time.Time
	// ChallengeAttempts is how many values the group holds — 2 for the wildcard
	// case below, 1 for an ordinary single-name certificate.
	ChallengeAttempts int
	// Outcome is the best outcome among the members (see betterOutcome).
	Outcome string
}

// GroupIssuances groups interactions at one alias whose live windows overlap.
//
// The wildcard case is the reason this exists. A certificate covering
// example.com and *.example.com needs TWO DNS-01 authorizations, and both are
// answered at ONE challenge name — the ACME client publishes two TXT values at
// _acme-challenge.example.com within a second of each other and withdraws both
// when the certificate is issued. Counting rows there reports two renewals for
// one certificate, so every wildcard customer's usage is doubled. Grouping the
// overlap reports one.
//
// Sequential publications at the same alias are NOT grouped: publish, clean up,
// publish again is two separate attempts at issuance and reads as two.
//
// A row that is still live has an open-ended window, so anything published at
// that alias afterwards joins its group — which is right, because a value that
// is still answerable is still part of whatever is being validated now.
//
// Computed on read and never materialized: the overlap rule may want tuning, and
// a stored grouping would then be wrong for every row already written.
//
// ponytail: O(n log n) over the rows of one range, held in memory. At the volume
// this service records — a few hundred domains renewing every sixty days is
// single-digit rows a day — that is nothing. Upgrade path if it ever is: the
// same grouping as a window function in SQL, which is much harder to read and
// buys nothing until the row count has four more digits.
func GroupIssuances(ins []model.ChallengeInteraction) []Issuance {
	byAlias := map[string][]model.ChallengeInteraction{}
	for _, in := range ins {
		byAlias[in.AliasLabel] = append(byAlias[in.AliasLabel], in)
	}

	// Sorted alias order so the result is deterministic — two calls on the same
	// data produce the same series, which a chart and a test both rely on.
	labels := make([]string, 0, len(byAlias))
	for label := range byAlias {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	var out []Issuance
	for _, label := range labels {
		rows := byAlias[label]
		sort.Slice(rows, func(i, j int) bool { return rows[i].PublishedAt.Before(rows[j].PublishedAt) })

		cur := -1        // index into out of the group being built, -1 before the first
		curOpen := false // some member is still published: the window has no end
		for _, in := range rows {
			end, open := liveWindowEnd(in)
			// Overlap, given the rows are sorted by publication: this row starts
			// before the group's window closes. An open group swallows anything
			// published after it.
			if cur >= 0 && (curOpen || !in.PublishedAt.After(out[cur].End)) {
				out[cur].ChallengeAttempts++
				out[cur].Outcome = betterOutcome(out[cur].Outcome, outcomeOf(in))
				switch {
				case open:
					curOpen = true
					out[cur].End = time.Time{}
				case !curOpen && end.After(out[cur].End):
					out[cur].End = end
				}
				continue
			}
			out = append(out, Issuance{
				AliasLabel: label, Start: in.PublishedAt, End: end,
				ChallengeAttempts: 1, Outcome: outcomeOf(in),
			})
			cur, curOpen = len(out)-1, open
		}
	}
	return out
}

// liveWindowEnd is when a publication stopped being answerable. open reports a
// row that has not stopped: still published, so its window has no end yet.
func liveWindowEnd(in model.ChallengeInteraction) (time.Time, bool) {
	if in.WithdrawnAt != nil {
		return *in.WithdrawnAt, false
	}
	return time.Time{}, true
}

// The five outcomes are DERIVED, not stored, and this is the same derivation the
// interactions list makes (httpapi.outcomeOf). Duplicated rather than exported
// across packages because httpapi imports reporting and not the other way round;
// if a sixth value ever appears, both move to one place.
const (
	OutcomeValidated = "validated" // withdrawn, and queried at least once — the normal life
	OutcomeUnqueried = "unqueried" // withdrawn without ever being queried; suspect the customer's CNAME
	OutcomeAbandoned = "abandoned" // never cleaned up; the sweep closed it
	OutcomeLive      = "live"      // still published, queried already
	OutcomePending   = "pending"   // still published, nobody has asked yet
)

// Outcomes is the vocabulary in report order, best first. Published in the API
// response so a client renders every series even for a bucket where one is zero.
var Outcomes = []string{OutcomeValidated, OutcomeLive, OutcomePending, OutcomeUnqueried, OutcomeAbandoned}

func outcomeOf(in model.ChallengeInteraction) string {
	switch {
	case in.Abandoned:
		return OutcomeAbandoned
	case in.WithdrawnAt != nil && in.QueryCount > 0:
		return OutcomeValidated
	case in.WithdrawnAt != nil:
		return OutcomeUnqueried
	case in.QueryCount > 0:
		return OutcomeLive
	default:
		return OutcomePending
	}
}

// betterOutcome picks an issuance attempt's outcome from its members': the best
// one wins.
//
// "Best" and not "worst" because of what the members are. The wildcard and the
// apex authorization are two halves of ONE certificate request, and the CA does
// not have to query both values to be satisfied by the pair — so one member
// queried means the CA came and looked, which is the thing the outcome reports.
// Taking the worst would report every wildcard renewal as half-failed.
//
// The order runs validated, live, pending, unqueried, abandoned: an attempt
// still in flight (live, pending) is not a failure, "withdrawn but never
// queried" points at the customer's CNAME, and "abandoned" — never cleaned up —
// is the least informative end and therefore last.
func betterOutcome(a, b string) string {
	if outcomeRank(b) < outcomeRank(a) {
		return b
	}
	return a
}

func outcomeRank(o string) int {
	for i, v := range Outcomes {
		if v == o {
			return i
		}
	}
	return len(Outcomes)
}
