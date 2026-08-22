package reporting

import (
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

// The case the whole grouping rule exists for: a certificate covering
// example.com AND *.example.com needs two DNS-01 authorizations, both answered
// at ONE challenge name. The client publishes two TXT values seconds apart and
// withdraws both when the certificate is issued. That is one renewal, and
// counting the rows would report two.
func TestWildcardAndApexAreOneIssuanceAttempt(t *testing.T) {
	ins := []model.ChallengeInteraction{
		{AliasLabel: "abc123", PublishedAt: at("2026-08-20T14:02:00Z"),
			WithdrawnAt: ptr(at("2026-08-20T14:02:31Z")), QueryCount: 2},
		{AliasLabel: "abc123", PublishedAt: at("2026-08-20T14:02:01Z"),
			WithdrawnAt: ptr(at("2026-08-20T14:02:31Z")), QueryCount: 0},
	}

	got := GroupIssuances(ins)
	if len(got) != 1 {
		t.Fatalf("two overlapping publications at one alias: want 1 issuance attempt, got %d: %+v", len(got), got)
	}
	if got[0].ChallengeAttempts != 2 {
		t.Errorf("want 2 challenge attempts in the group, got %d", got[0].ChallengeAttempts)
	}
	// The CA satisfied itself by querying once; the pair is one validated
	// request, not one validated and one failed half.
	if got[0].Outcome != OutcomeValidated {
		t.Errorf("want the group validated, got %q", got[0].Outcome)
	}

	// And the two numbers stay distinct in the series.
	buckets := bucketize(ins, at("2026-08-20T00:00:00Z"), at("2026-08-20T23:59:59Z"), BucketDay)
	if len(buckets) != 1 {
		t.Fatalf("want one daily bucket, got %d", len(buckets))
	}
	if buckets[0].ChallengeAttemptsTotal != 2 || buckets[0].IssuanceAttemptsTotal != 1 {
		t.Errorf("want 2 challenge attempts and 1 issuance attempt, got %d and %d",
			buckets[0].ChallengeAttemptsTotal, buckets[0].IssuanceAttemptsTotal)
	}
}

// The other half of the rule: same alias, but the first value was withdrawn
// before the second appeared. Two genuine attempts at issuance.
func TestSequentialPublicationsAreNotGrouped(t *testing.T) {
	got := GroupIssuances([]model.ChallengeInteraction{
		{AliasLabel: "abc123", PublishedAt: at("2026-08-20T10:00:00Z"),
			WithdrawnAt: ptr(at("2026-08-20T10:00:20Z")), QueryCount: 0},
		{AliasLabel: "abc123", PublishedAt: at("2026-08-20T11:00:00Z"),
			WithdrawnAt: ptr(at("2026-08-20T11:00:20Z")), QueryCount: 1},
	})
	if len(got) != 2 {
		t.Fatalf("want 2 issuance attempts, got %d: %+v", len(got), got)
	}
	if got[0].Outcome != OutcomeUnqueried || got[1].Outcome != OutcomeValidated {
		t.Errorf("outcomes want unqueried then validated, got %q then %q", got[0].Outcome, got[1].Outcome)
	}
}

// Different aliases never group, however closely they overlap: two customers
// renewing at the same second are two issuance attempts.
func TestDifferentAliasesNeverGroup(t *testing.T) {
	got := GroupIssuances([]model.ChallengeInteraction{
		{AliasLabel: "abc123", PublishedAt: at("2026-08-20T10:00:00Z"), WithdrawnAt: ptr(at("2026-08-20T10:00:20Z"))},
		{AliasLabel: "def456", PublishedAt: at("2026-08-20T10:00:00Z"), WithdrawnAt: ptr(at("2026-08-20T10:00:20Z"))},
	})
	if len(got) != 2 {
		t.Fatalf("want 2 issuance attempts, got %d", len(got))
	}
}

// A still-published row has an open window, so anything published at that alias
// afterwards belongs to the same in-flight attempt.
func TestOpenWindowSwallowsLaterPublication(t *testing.T) {
	got := GroupIssuances([]model.ChallengeInteraction{
		{AliasLabel: "abc123", PublishedAt: at("2026-08-20T10:00:00Z")},
		{AliasLabel: "abc123", PublishedAt: at("2026-08-20T10:05:00Z"), QueryCount: 3},
	})
	if len(got) != 1 || got[0].ChallengeAttempts != 2 {
		t.Fatalf("want one group of two, got %+v", got)
	}
	if got[0].Outcome != OutcomeLive {
		t.Errorf("want live, got %q", got[0].Outcome)
	}
}

// Empty buckets are emitted rather than omitted: a two-week gap must draw as two
// weeks of nothing, not as two adjacent days.
func TestEmptyBucketsAreFilledIn(t *testing.T) {
	got := bucketize(nil, at("2026-08-01T00:00:00Z"), at("2026-08-05T23:59:59Z"), BucketDay)
	if len(got) != 5 {
		t.Fatalf("want 5 daily buckets, got %d", len(got))
	}
	for _, b := range got {
		if b.ChallengeAttemptsTotal != 0 || len(b.ChallengeAttempts) != len(Outcomes) {
			t.Errorf("bucket %s: want a zero for every outcome, got %+v", b.Start, b.ChallengeAttempts)
		}
	}
}
