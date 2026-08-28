package store

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/queryobs"
)

// The interaction history, tested where its two non-obvious rules live: the
// overlap attribution in ApplyQueryObservations, and the reuse of a live row in
// OpenInteraction. Everything else here is a filter, and filters are tested
// because a console screen is only as honest as the WHERE clause behind it.

// interactionEnv is a project, a load balancer and an alias, plus the ids the
// tests below hang interactions off.
type interactionEnv struct {
	t         *testing.T
	db        *gorm.DB
	sc        Scope
	projectID model.ID
	alias     *model.Alias
}

func newInteractionEnv(t *testing.T) *interactionEnv {
	t.Helper()
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme", Name: "Acme"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	alias := &model.Alias{ProjectID: p.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "demo-example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	return &interactionEnv{t: t, db: db, sc: sc, projectID: p.ID, alias: alias}
}

// interaction builds a row for this env's alias, with the fields the filters
// read filled in. Callers override what they care about.
func (v *interactionEnv) interaction(value string, publishedAt time.Time) *model.ChallengeInteraction {
	keyID := "k_7f3a91c4"
	ip := "203.0.113.7"
	return &model.ChallengeInteraction{
		ProjectID:     &v.projectID,
		AliasID:       &v.alias.ID,
		AliasLabel:    v.alias.Label,
		ChallengeFQDN: v.alias.ChallengeFQDN,
		DomainNames:   model.StringArray{"example.com", "*.example.com"},
		KeyID:         &keyID,
		ClientIP:      &ip,
		FQDNForm:      "alias",
		ValueDigest:   ValueDigest(value),
		PublishedAt:   publishedAt,
	}
}

// insert writes a row directly, bypassing OpenInteraction, so a test can place
// an interaction anywhere in time.
func (v *interactionEnv) insert(in *model.ChallengeInteraction) *model.ChallengeInteraction {
	v.t.Helper()
	if err := v.db.Create(in).Error; err != nil {
		v.t.Fatalf("insert interaction: %v", err)
	}
	return in
}

func (v *interactionEnv) reload(id model.ID) model.ChallengeInteraction {
	v.t.Helper()
	var got model.ChallengeInteraction
	if err := v.db.First(&got, id).Error; err != nil {
		v.t.Fatalf("reload interaction %d: %v", id, err)
	}
	return got
}

// --- the overlap rule ------------------------------------------------------

// TestIntegrationQueryObservationsCreditARowThatLivedBetweenTwoFlushes is the
// reason ApplyQueryObservations attributes by overlap rather than by "is this
// row live now".
//
// A DNS-01 validation is over in seconds — publish, get queried, clean up —
// while the flush runs on the zone-view interval, tens of seconds apart. The
// row here is published AND withdrawn strictly inside one flush window, so an
// implementation that credits only rows still live at flush time credits
// nothing and reports a perfectly successful validation as "never queried",
// which is precisely the signal this history exists to give. That naive
// implementation must fail this test.
func TestIntegrationQueryObservationsCreditARowThatLivedBetweenTwoFlushes(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	from := time.Now().Add(-30 * time.Second).UTC()
	to := time.Now().UTC()
	published := from.Add(10 * time.Second)
	queried := from.Add(11 * time.Second)
	withdrawn := from.Add(12 * time.Second)

	in := v.interaction("gfj9Xq", published)
	in.WithdrawnAt = &withdrawn
	v.insert(in)

	obs := map[model.ID]queryobs.Observation{
		v.alias.ID: {Count: 2, First: queried, Last: queried.Add(time.Second)},
	}
	unmatched, err := ApplyQueryObservations(ctx, v.db, obs, from, to)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if unmatched != 0 {
		t.Errorf("unmatched = %d, want 0: the row overlapped the window", unmatched)
	}

	got := v.reload(in.ID)
	if got.QueryCount != 2 {
		t.Fatalf("queryCount = %d, want 2 — a row that was published, queried and "+
			"withdrawn entirely between two flushes was not credited", got.QueryCount)
	}
	if got.FirstQueriedAt == nil || got.LastQueriedAt == nil {
		t.Fatalf("first/last queried = %v/%v, want both stamped", got.FirstQueriedAt, got.LastQueriedAt)
	}
	// The stamps are the counter's own, not the flush's: an operator reading
	// "first queried at 14:02:07" must not be looking at when the flusher ran.
	if d := got.FirstQueriedAt.Sub(queried); d > time.Second || d < -time.Second {
		t.Errorf("firstQueriedAt = %s, want the counter's stamp %s", got.FirstQueriedAt, queried)
	}
}

// A row that closed BEFORE the window opened, and one published after it
// closed, are both outside it — and an alias whose rows all miss the window is
// counted as unmatched, which is what the caller reports separately from a
// label that no longer resolves at all.
func TestIntegrationQueryObservationsSkipRowsOutsideTheWindow(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	from := time.Now().Add(-30 * time.Second).UTC()
	to := time.Now().UTC()

	earlyWithdrawn := from.Add(-5 * time.Second)
	early := v.interaction("early", from.Add(-20*time.Second))
	early.WithdrawnAt = &earlyWithdrawn
	v.insert(early)

	late := v.insert(v.interaction("late", to.Add(10*time.Second)))

	unmatched, err := ApplyQueryObservations(ctx, v.db,
		map[model.ID]queryobs.Observation{v.alias.ID: {Count: 1, First: from, Last: to}}, from, to)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if unmatched != 1 {
		t.Errorf("unmatched = %d, want 1: no row overlapped the window", unmatched)
	}
	for name, id := range map[string]model.ID{"withdrawn before the window": early.ID, "published after it": late.ID} {
		if got := v.reload(id); got.QueryCount != 0 {
			t.Errorf("%s: queryCount = %d, want 0", name, got.QueryCount)
		}
	}
}

// TestIntegrationOneQueryCreditsEveryLiveValueAtTheName is the wildcard case: a
// certificate covering example.com and *.example.com validates two
// authorizations at ONE challenge name, a single query returns both TXT values,
// and both rows were genuinely observed by it.
//
// The counts are asserted PER ROW rather than as a total, because a total of
// two also passes an implementation that credits the most recent row twice —
// which is the wrong answer for the screen this feeds.
func TestIntegrationOneQueryCreditsEveryLiveValueAtTheName(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	from := time.Now().Add(-30 * time.Second).UTC()
	to := time.Now().UTC()

	apex := v.insert(v.interaction("apex-value", from.Add(2*time.Second)))
	wildcard := v.insert(v.interaction("wildcard-value", from.Add(3*time.Second)))

	unmatched, err := ApplyQueryObservations(ctx, v.db,
		map[model.ID]queryobs.Observation{v.alias.ID: {Count: 1, First: from.Add(4 * time.Second), Last: from.Add(4 * time.Second)}},
		from, to)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if unmatched != 0 {
		t.Errorf("unmatched = %d, want 0", unmatched)
	}
	for name, id := range map[string]model.ID{"apex": apex.ID, "wildcard": wildcard.ID} {
		got := v.reload(id)
		if got.QueryCount != 1 {
			t.Errorf("%s authorization: queryCount = %d, want 1 — one query at a name "+
				"with two live values credits both", name, got.QueryCount)
		}
	}
}

// A batch with no timestamps — a counter drained before any query carried one —
// falls back to the window's end rather than leaving the stamps null, so a
// credited row never reads as "queried, but we cannot say when".
func TestIntegrationQueryObservationsFallBackToTheWindowEnd(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	from := time.Now().Add(-30 * time.Second).UTC()
	to := time.Now().UTC()
	in := v.insert(v.interaction("gfj9Xq", from.Add(time.Second)))

	if _, err := ApplyQueryObservations(ctx, v.db,
		map[model.ID]queryobs.Observation{v.alias.ID: {Count: 1}}, from, to); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := v.reload(in.ID)
	if got.FirstQueriedAt == nil || got.LastQueriedAt == nil {
		t.Fatalf("first/last queried = %v/%v, want the window end", got.FirstQueriedAt, got.LastQueriedAt)
	}

	// A second batch must not move first_queried_at: "did the CA ever come, and
	// when did it first come" is the question, and coalesce is what keeps the
	// answer stable.
	firstSeen := *got.FirstQueriedAt
	later := to.Add(10 * time.Second)
	if _, err := ApplyQueryObservations(ctx, v.db,
		map[model.ID]queryobs.Observation{v.alias.ID: {Count: 3, First: later, Last: later}}, to, later); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	got = v.reload(in.ID)
	if !got.FirstQueriedAt.Equal(firstSeen) {
		t.Errorf("firstQueriedAt moved from %s to %s on a later batch", firstSeen, got.FirstQueriedAt)
	}
	if got.QueryCount != 4 {
		t.Errorf("queryCount = %d, want 4 — counts accumulate across flushes", got.QueryCount)
	}
}

// --- opening and closing ----------------------------------------------------

// /present is retried, so OpenInteraction must be idempotent in the way
// /present is: a retry of a value that is still live reuses the row and leaves
// its publication time alone. The interaction began when the CA first asked for
// it, and a moved published_at would silently reset the age of everything the
// console shows.
func TestIntegrationOpenInteractionReusesALiveRow(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	first := v.interaction("gfj9Xq", time.Now().Add(-time.Minute))
	if err := OpenInteraction(ctx, v.db, first); err != nil {
		t.Fatalf("open: %v", err)
	}
	publishedAt := v.reload(first.ID).PublishedAt

	retry := v.interaction("gfj9Xq", time.Now())
	if err := OpenInteraction(ctx, v.db, retry); err != nil {
		t.Fatalf("retried open: %v", err)
	}
	if n := v.count(); n != 1 {
		t.Fatalf("%d interactions after a retried publication of a live value, want 1", n)
	}
	if got := v.reload(first.ID); !got.PublishedAt.Equal(publishedAt) {
		t.Errorf("publishedAt moved from %s to %s on a retry", publishedAt, got.PublishedAt)
	}

	// A different value at the same alias is a different authorization and gets
	// its own row — this is the apex-and-wildcard pair, not a duplicate.
	if err := OpenInteraction(ctx, v.db, v.interaction("second-value", time.Now())); err != nil {
		t.Fatalf("open a second value: %v", err)
	}
	if n := v.count(); n != 2 {
		t.Fatalf("%d interactions after a second distinct value, want 2", n)
	}

	// And once a value has been withdrawn, publishing it again is a NEW
	// interaction: the next renewal is not the previous one continuing.
	if _, _, err := CloseInteraction(ctx, v.db, v.alias.ID, "gfj9Xq"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := OpenInteraction(ctx, v.db, v.interaction("gfj9Xq", time.Now())); err != nil {
		t.Fatalf("reopen after withdrawal: %v", err)
	}
	if n := v.count(); n != 3 {
		t.Fatalf("%d interactions after republishing a withdrawn value, want 3", n)
	}
}

func (v *interactionEnv) count() int64 {
	v.t.Helper()
	var n int64
	if err := v.db.Model(&model.ChallengeInteraction{}).Count(&n).Error; err != nil {
		v.t.Fatalf("count interactions: %v", err)
	}
	return n
}

// Clients retry cleanups, and a cleanup can arrive for a value whose row was
// already closed or never opened at all (the history write is best effort). None
// of that is an error, and none of it may invent a row.
func TestIntegrationCloseInteractionWithoutAMatchIsNotAnError(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	validated, unqueried, err := CloseInteraction(ctx, v.db, v.alias.ID, "never-published")
	if err != nil {
		t.Fatalf("close with no matching row: %v", err)
	}
	if validated != 0 || unqueried != 0 {
		t.Errorf("closing nothing reported %d validated and %d unqueried, want 0 and 0", validated, unqueried)
	}
	if _, _, err := CloseInteraction(ctx, v.db, model.NewID(), "never-published"); err != nil {
		t.Fatalf("close for an unknown alias: %v", err)
	}
	if n := v.count(); n != 0 {
		t.Fatalf("%d interactions after closing nothing, want 0", n)
	}

	// Closing twice is fine too, and the second close must not move the
	// withdrawal stamp the first one wrote.
	in := v.interaction("gfj9Xq", time.Now())
	if err := OpenInteraction(ctx, v.db, in); err != nil {
		t.Fatalf("open: %v", err)
	}
	// The row was never queried, so it closes as unqueried — the count the
	// caller turns into a metric, read off the rows actually closed.
	validated, unqueried, err = CloseInteraction(ctx, v.db, v.alias.ID, "gfj9Xq")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if validated != 0 || unqueried != 1 {
		t.Errorf("closing one never-queried row reported %d validated / %d unqueried, want 0 / 1", validated, unqueried)
	}
	withdrawn := v.reload(in.ID).WithdrawnAt
	if withdrawn == nil {
		t.Fatal("withdrawnAt is still null after a close")
	}
	if validated, unqueried, err = CloseInteraction(ctx, v.db, v.alias.ID, "gfj9Xq"); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
	if validated != 0 || unqueried != 0 {
		t.Errorf("a repeated cleanup counted %d validated / %d unqueried rows again", validated, unqueried)
	}
	if got := v.reload(in.ID); !got.WithdrawnAt.Equal(*withdrawn) {
		t.Errorf("withdrawnAt moved from %s to %s on a repeated cleanup", withdrawn, got.WithdrawnAt)
	}

	// And the other side of the count: a row the CA did query closes as
	// validated. The counts come off the rows being closed, in the same UPDATE,
	// so a query-observation flush landing between a read and the write cannot
	// make the metric disagree with the history it was derived from.
	queried := v.interaction("was-queried", time.Now())
	if err := OpenInteraction(ctx, v.db, queried); err != nil {
		t.Fatalf("open a row to be queried: %v", err)
	}
	if err := v.db.Model(&model.ChallengeInteraction{}).Where("id = ?", queried.ID).
		Update("query_count", 5).Error; err != nil {
		t.Fatalf("seed query count: %v", err)
	}
	if validated, unqueried, err = CloseInteraction(ctx, v.db, v.alias.ID, "was-queried"); err != nil {
		t.Fatalf("close a queried row: %v", err)
	}
	if validated != 1 || unqueried != 0 {
		t.Errorf("closing a row queried 5 times reported %d validated / %d unqueried, want 1 / 0", validated, unqueried)
	}
}

// --- the console's listing --------------------------------------------------

// Every field the `q` filter claims to search is searched, the unqueried filter
// narrows to the rows an operator acts on, and the total is the count BEFORE
// paging — a pager that reports the size of the page it is on tells the
// operator nothing about what is behind it.
func TestIntegrationListInteractionsFiltersAndPages(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	other := &model.Alias{ProjectID: v.projectID, ChallengeFQDN: "_acme-challenge.shop.example.org", Label: "shop-example-org"}
	if err := CreateAlias(ctx, v.db, v.sc, other); err != nil {
		t.Fatalf("create second alias: %v", err)
	}

	base := time.Now().Add(-time.Hour)
	for i := range 5 {
		v.insert(v.interaction("value-"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Minute)))
	}
	// One row on the other alias, with its own key, address and domain, so each
	// clause of the filter can be told apart from the rest.
	otherKey := "k_deadbeef"
	otherIP := "198.51.100.42"
	shop := v.interaction("shop-value", base.Add(10*time.Minute))
	shop.AliasID = &other.ID
	shop.AliasLabel = other.Label
	shop.ChallengeFQDN = other.ChallengeFQDN
	shop.DomainNames = model.StringArray{"shop.example.org"}
	shop.KeyID = &otherKey
	shop.ClientIP = &otherIP
	shop.QueryCount = 3
	v.insert(shop)

	// A row belonging to another project must never appear.
	stranger := v.interaction("stranger", base)
	strangerProject := model.NewID()
	stranger.ProjectID = &strangerProject
	v.insert(stranger)

	list := func(q string, unqueried bool, limit, offset int) ([]model.ChallengeInteraction, int64) {
		t.Helper()
		got, total, err := ListInteractionsForProject(ctx, v.db, v.sc, v.projectID, q, unqueried, limit, offset)
		if err != nil {
			t.Fatalf("list(%q, %v, %d, %d): %v", q, unqueried, limit, offset, err)
		}
		return got, total
	}

	if got, total := list("", false, 100, 0); len(got) != 6 || total != 6 {
		t.Fatalf("unfiltered: %d rows, total %d, want 6 and 6 (and nothing from another project)", len(got), total)
	}

	for name, q := range map[string]string{
		"alias label": "shop-example-org",
		"fqdn":        "shop.example.org",
		"key id":      "deadbeef",
		"client ip":   "198.51.100.42",
		"domain name": "shop.example.org",
	} {
		got, total := list(q, false, 100, 0)
		if total != 1 || len(got) != 1 || got[0].ID != shop.ID {
			t.Errorf("q=%q (%s): %d rows, total %d, want just the shop row", q, name, len(got), total)
		}
	}

	// Newest first, and the total is the filtered count, not the page size.
	page, total := list("", false, 2, 0)
	if total != 6 {
		t.Errorf("total on a 2-row page = %d, want the pre-paging count 6", total)
	}
	if len(page) != 2 || page[0].ID != shop.ID {
		t.Fatalf("first page %v, want two rows newest first", ids(page))
	}
	second, _ := list("", false, 2, 2)
	if len(second) != 2 || second[0].ID == page[0].ID {
		t.Fatalf("second page %v overlaps the first %v", ids(second), ids(page))
	}

	// The unqueried filter: the one row anybody ever asked for drops out, and
	// the total drops with it.
	unqueried, total := list("", true, 100, 0)
	if total != 5 || len(unqueried) != 5 {
		t.Fatalf("unqueried: %d rows, total %d, want 5", len(unqueried), total)
	}
	for _, in := range unqueried {
		if in.QueryCount != 0 {
			t.Errorf("unqueried listing returned a row with %d queries", in.QueryCount)
		}
	}

	// A filter matching a row only on a later page still finds it — the filter
	// runs in SQL, not over the page.
	if got, total := list("shop", true, 100, 0); len(got) != 0 || total != 0 {
		t.Errorf("q=shop with unqueried: %d rows, want none — the shop row was queried", len(got))
	}

	// `_` is a LIKE wildcard and must be matched literally, or a filter for
	// "shop_" would quietly return everything.
	if got, total := list("shop_", false, 100, 0); len(got) != 0 || total != 0 {
		t.Errorf(`q="shop_": %d rows, total %d, want none — _ must not act as a wildcard`, len(got), total)
	}
	if got, _ := list("k_deadbeef", false, 100, 0); len(got) != 1 {
		t.Errorf(`q="k_deadbeef": %d rows, want the one key whose id contains a literal underscore`, len(got))
	}
}

func ids(ins []model.ChallengeInteraction) []model.ID {
	out := make([]model.ID, len(ins))
	for i, in := range ins {
		out[i] = in.ID
	}
	return out
}

// AbandonStaleInteractions is what stops a client that died mid-validation from
// reading as "live" forever. It closes only rows past the cutoff, flags them,
// and leaves the query count alone — an abandoned row that WAS queried is a
// different story from one that never was.
func TestIntegrationAbandonStaleInteractions(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	stale := v.insert(v.interaction("stale", time.Now().Add(-2*time.Hour)))
	if err := v.db.Model(&model.ChallengeInteraction{}).Where("id = ?", stale.ID).
		Update("query_count", 2).Error; err != nil {
		t.Fatalf("seed query count: %v", err)
	}
	fresh := v.insert(v.interaction("fresh", time.Now()))

	// And an acme-dns publication of the same age, which has no withdrawal in
	// its protocol and must NOT be called abandoned for behaving as specified
	// (design N4, task 5.2).
	expired := v.insert(v.interaction("expired", time.Now().Add(-2*time.Hour)))
	if err := v.db.Model(&model.ChallengeInteraction{}).Where("id = ?", expired.ID).
		Updates(map[string]any{"fqdn_form": FormAcmeDNS, "query_count": 3}).Error; err != nil {
		t.Fatalf("seed acme-dns row: %v", err)
	}
	silent := v.insert(v.interaction("silent", time.Now().Add(-2*time.Hour)))
	if err := v.db.Model(&model.ChallengeInteraction{}).Where("id = ?", silent.ID).
		Update("fqdn_form", FormAcmeDNS).Error; err != nil {
		t.Fatalf("seed acme-dns row: %v", err)
	}

	closed, err := AbandonStaleInteractions(ctx, v.db, time.Hour)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if closed.Abandoned != 1 {
		t.Fatalf("abandoned %d rows, want 1", closed.Abandoned)
	}
	if closed.Validated != 1 || closed.Unqueried != 1 {
		t.Fatalf("acme-dns rows closed as %d validated and %d unqueried, want one of each",
			closed.Validated, closed.Unqueried)
	}
	// Closed, so the console stops reporting them as still in flight — but not
	// abandoned, so it does not report a correct renewal as a fault. That
	// distinction is made here, once, rather than in the two places that derive
	// an outcome from the flag.
	for _, id := range []model.ID{expired.ID, silent.ID} {
		got := v.reload(id)
		if got.WithdrawnAt == nil {
			t.Errorf("acme-dns row %s was left open; nothing will ever close it", id)
		}
		if got.Abandoned {
			t.Errorf("acme-dns row %s was marked abandoned. Its protocol has no withdrawal "+
				"operation, so expiring on the timer is its normal ending — flagging that would "+
				"paint every correct renewal red and train somebody to ignore the column", id)
		}
	}
	got := v.reload(stale.ID)
	if got.WithdrawnAt == nil || !got.Abandoned {
		t.Errorf("stale row: withdrawnAt %v, abandoned %v, want both set", got.WithdrawnAt, got.Abandoned)
	}
	if got.QueryCount != 2 {
		t.Errorf("stale row: queryCount = %d, want the observations left intact", got.QueryCount)
	}
	if got := v.reload(fresh.ID); got.WithdrawnAt != nil {
		t.Errorf("a row inside the cutoff was closed: withdrawnAt %s", got.WithdrawnAt)
	}
}

// AliasIDsByLabel drops labels that no longer resolve — a label can be deleted
// between a query being answered from the in-memory view and the observation
// being flushed — and REPORTS how many it dropped, because silently discarding
// them would look exactly like "the CA never queried anything".
func TestIntegrationAliasIDsByLabelReportsWhatItDropped(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	obs := map[string]queryobs.Observation{
		v.alias.Label: {Count: 4},
		"gone-away":   {Count: 9},
	}
	byID, dropped, err := AliasIDsByLabel(ctx, v.db, obs)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if len(byID) != 1 || byID[v.alias.ID].Count != 4 {
		t.Errorf("resolved %v, want the live alias with its count", byID)
	}
}
