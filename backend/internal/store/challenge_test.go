package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// challengeFixture creates one project, one load balancer and n aliases, returning the
// alias ids in order. Rows are inserted directly rather than through the inventory
// repositories, so this test only depends on the schema.
func challengeFixture(t *testing.T, db *gorm.DB, sc Scope, n int) []model.ID {
	t.Helper()
	ctx := context.Background()

	// Fixtures go through the store rather than around it: row-level security is
	// forced, so a raw insert with no account set is refused by the database —
	// which is the point of the policy, and makes a test that bypasses the store
	// a test of nothing.
	p := model.Project{Slug: "proj", Name: "Project"}
	if err := CreateProject(ctx, db, sc, &p); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ids := make([]model.ID, n)
	for i := range ids {
		a := model.Alias{
			ProjectID:     p.ID,
			Label:         fmt.Sprintf("a%d", i),
			ChallengeFQDN: fmt.Sprintf("a%d.acme.example.net", i),
		}
		if err := CreateAlias(ctx, db, sc, &a); err != nil {
			t.Fatalf("create alias %d: %v", i, err)
		}
		ids[i] = a.ID
	}
	return ids
}

func countChallenges(t *testing.T, db *gorm.DB, aliasID model.ID) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.Challenge{}).Where("alias_id = ?", aliasID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestIntegrationPresentIsAnIdempotentUpsert covers "re-storing refreshes rather
// than duplicates": one row, expiry extended, first publish time kept.
func TestIntegrationPresentIsAnIdempotentUpsert(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	alias := challengeFixture(t, db, sc, 1)[0]

	first, err := Present(ctx, db, sc, alias, "tok-1", "k_aaa", time.Minute)
	if err != nil {
		t.Fatalf("first present: %v", err)
	}
	second, err := Present(ctx, db, sc, alias, "tok-1", "k_bbb", time.Hour)
	if err != nil {
		t.Fatalf("second present: %v", err)
	}

	if n := countChallenges(t, db, alias); n != 1 {
		t.Fatalf("rows after repeat present = %d, want 1", n)
	}
	if second.ID != first.ID {
		t.Fatalf("upsert produced a new row: %d then %d", first.ID, second.ID)
	}

	var got model.Challenge
	if err := db.First(&got, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.After(first.ExpiresAt) {
		t.Fatalf("expiry not refreshed: %s not after %s", got.ExpiresAt, first.ExpiresAt)
	}
	if got.PublishedBy == nil || *got.PublishedBy != "k_bbb" {
		t.Fatalf("published_by = %v, want k_bbb", got.PublishedBy)
	}
	if d := got.CreatedAt.Sub(first.CreatedAt); d > time.Second || d < -time.Second {
		t.Fatalf("created_at moved by %s, want the original publish time kept", d)
	}
}

// TestIntegrationPresentAddsRatherThanReplaces is the wildcard case: example.com
// and *.example.com validate at one challenge name and both values must be
// answerable at once.
func TestIntegrationPresentAddsRatherThanReplaces(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	alias := challengeFixture(t, db, sc, 1)[0]

	for _, v := range []string{"tok-a", "tok-b"} {
		if _, err := Present(ctx, db, sc, alias, v, "k_aaa", time.Minute); err != nil {
			t.Fatalf("present %s: %v", v, err)
		}
	}
	if n := countChallenges(t, db, alias); n != 2 {
		t.Fatalf("rows = %d, want 2 — a present must add, not replace", n)
	}
}

// TestIntegrationCleanupIsExact covers the sibling-authorization case: cleaning up
// one value must leave the other in place, and cleaning up an absent value must
// succeed.
func TestIntegrationCleanupIsExact(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	ids := challengeFixture(t, db, sc, 2)
	alias, other := ids[0], ids[1]

	for _, v := range []string{"tok-a", "tok-b"} {
		if _, err := Present(ctx, db, sc, alias, v, "k_aaa", time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Present(ctx, db, sc, other, "tok-a", "k_aaa", time.Minute); err != nil {
		t.Fatal(err)
	}

	if err := Cleanup(ctx, db, sc, alias, "tok-a"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if err := Cleanup(ctx, db, sc, alias, "tok-a"); err != nil {
		t.Fatalf("cleanup of an absent value must succeed, got %v", err)
	}
	if err := Cleanup(ctx, db, sc, alias, "never-published"); err != nil {
		t.Fatalf("cleanup of a value never published must succeed, got %v", err)
	}

	var left []model.Challenge
	if err := db.Where("alias_id = ?", alias).Find(&left).Error; err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Value != "tok-b" {
		t.Fatalf("after cleanup the alias holds %v, want only tok-b", left)
	}
	if n := countChallenges(t, db, other); n != 1 {
		t.Fatalf("cleanup touched another alias: %d rows left, want 1", n)
	}
}

// TestIntegrationExpiryIsReadTime covers "expired value is not answered, before
// the sweep has run", then that the sweep deletes it and reports the count.
func TestIntegrationExpiryIsReadTime(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	alias := challengeFixture(t, db, sc, 1)[0]

	if _, err := Present(ctx, db, sc, alias, "live", "k_aaa", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := Present(ctx, db, sc, alias, "dead", "k_aaa", time.Hour); err != nil {
		t.Fatal(err)
	}
	// Backdate one row rather than sleeping.
	past := time.Now().Add(-time.Second)
	if err := db.Model(&model.Challenge{}).
		Where("alias_id = ? AND value = ?", alias, "dead").
		Update("expires_at", past).Error; err != nil {
		t.Fatal(err)
	}

	byAlias, err := ActiveByAlias(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if vs := byAlias["a0"]; len(vs) != 1 || vs[0].Value != "live" {
		t.Fatalf("ActiveByAlias = %v, want only the unexpired value, before any sweep", vs)
	}
	active, err := ListActive(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Value != "live" {
		t.Fatalf("ListActive = %v, want only the unexpired value", active)
	}
	if active[0].Alias == nil || active[0].Alias.Label != "a0" {
		t.Fatalf("ListActive did not preload the alias: %+v", active[0].Alias)
	}

	n, err := DeleteExpired(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("DeleteExpired swept %d rows, want 1", n)
	}
	if got := countChallenges(t, db, alias); got != 1 {
		t.Fatalf("rows after sweep = %d, want 1", got)
	}
}

// TestIntegrationActiveByAliasIncludesEmptyAliases: a label with no values must
// still be a key in the map (NOERROR-empty), or DNS answers NXDOMAIN after every
// successful cleanup.
func TestIntegrationActiveByAliasIncludesEmptyAliases(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	ids := challengeFixture(t, db, sc, 2)

	if _, err := Present(ctx, db, sc, ids[0], "tok", "k_aaa", time.Minute); err != nil {
		t.Fatal(err)
	}

	byAlias, err := ActiveByAlias(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(byAlias) != 2 {
		t.Fatalf("labels = %d, want both aliases present: %v", len(byAlias), byAlias)
	}
	vs, ok := byAlias["a1"]
	if !ok {
		t.Fatal("alias with no values is missing from the map — DNS would answer NXDOMAIN")
	}
	if len(vs) != 0 {
		t.Fatalf("alias a1 has values %v, want none", vs)
	}
}

// TestIntegrationPublisherSurvivesKeyRevocation: published_by is plain text, not a
// foreign key, so deleting the key leaves the record answerable and attributed.
func TestIntegrationPublisherSurvivesKeyRevocation(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	alias := challengeFixture(t, db, sc, 1)[0]

	key := model.APIKey{Identifier: "k_7f3a91c4", SecretHash: []byte{1, 2, 3}, DisplayPrefix: "k_7f3a"}
	if err := CreateAPIKey(ctx, db, sc, &key, AllowAnyGrant); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if _, err := Present(ctx, db, sc, alias, "tok", key.Identifier, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&model.APIKey{}, key.ID).Error; err != nil {
		t.Fatalf("delete api key: %v", err)
	}

	active, err := ListActive(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("records after key deletion = %d, want the value still answerable", len(active))
	}
	if active[0].PublishedBy == nil || *active[0].PublishedBy != "k_7f3a91c4" {
		t.Fatalf("attribution lost: %v", active[0].PublishedBy)
	}
}

// TestIntegrationConcurrentIdenticalPresents: the unique constraint, not a
// read-then-write, is what makes a repeated present idempotent. Exactly one row,
// and no goroutine may see an error.
func TestIntegrationConcurrentIdenticalPresents(t *testing.T) {
	db, sc := scopedDB(t)
	alias := challengeFixture(t, db, sc, 1)[0]

	const n = 16
	errs := make([]error, n)
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, errs[i] = Present(context.Background(), db, sc, alias, "same-token", "k_aaa", time.Minute)
		}()
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("present %d: %v", i, err)
		}
	}
	if got := countChallenges(t, db, alias); got != 1 {
		t.Fatalf("rows = %d, want exactly 1", got)
	}
}

// TestIntegrationConcurrentDistinctPresents: concurrent publishes of different
// values at one alias must all survive — none may overwrite another.
func TestIntegrationConcurrentDistinctPresents(t *testing.T) {
	db, sc := scopedDB(t)
	alias := challengeFixture(t, db, sc, 1)[0]

	const n = 16
	errs := make([]error, n)
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, errs[i] = Present(context.Background(), db, sc, alias, fmt.Sprintf("tok-%d", i), "k_aaa", time.Minute)
		}()
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("present %d: %v", i, err)
		}
	}
	if got := countChallenges(t, db, alias); got != n {
		t.Fatalf("rows = %d, want %d", got, n)
	}
}

// TestIntegrationCleanupRacingPresent: a withdraw of one value racing publishes of
// another must not deadlock, error, or take the other value with it.
func TestIntegrationCleanupRacingPresent(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	alias := challengeFixture(t, db, sc, 1)[0]

	const rounds = 40
	var wg sync.WaitGroup
	wg.Add(2)
	presentErrs := make([]error, rounds)
	cleanupErrs := make([]error, rounds)

	go func() {
		defer wg.Done()
		for i := range rounds {
			_, presentErrs[i] = Present(ctx, db, sc, alias, "keep-me", "k_aaa", time.Minute)
		}
	}()
	go func() {
		defer wg.Done()
		for i := range rounds {
			if _, err := Present(ctx, db, sc, alias, "drop-me", "k_bbb", time.Minute); err != nil {
				cleanupErrs[i] = err
				continue
			}
			cleanupErrs[i] = Cleanup(ctx, db, sc, alias, "drop-me")
		}
	}()
	wg.Wait()

	for i := range rounds {
		if presentErrs[i] != nil {
			t.Errorf("present round %d: %v", i, presentErrs[i])
		}
		if cleanupErrs[i] != nil {
			t.Errorf("cleanup round %d: %v", i, cleanupErrs[i])
		}
	}

	var left []model.Challenge
	if err := db.Where("alias_id = ?", alias).Find(&left).Error; err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Value != "keep-me" {
		t.Fatalf("after the race the alias holds %v, want only keep-me", left)
	}
}

// TestPresentRejectsBadInput needs no database: the guards are before the query.
func TestPresentRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		aliasID  model.ID
		value    string
		lifetime time.Duration
	}{
		{"no alias", model.NilID, "tok", time.Minute},
		{"empty value", model.NewID(), "", time.Minute},
		{"zero lifetime", model.NewID(), "tok", 0},
		{"negative lifetime", model.NewID(), "tok", -time.Minute},
	} {
		if _, err := Present(ctx, nil, Scope{}, tc.aliasID, tc.value, "k_aaa", tc.lifetime); err == nil {
			t.Errorf("%s: want an error", tc.name)
		}
	}
}
