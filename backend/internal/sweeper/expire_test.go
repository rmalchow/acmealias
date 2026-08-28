package sweeper

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// TestIntegrationExpiredKeyIsRecordedOnceWithASystemActor covers the spec
// scenario "a key passes its expiry without human action": there is an entry,
// it is attributed to the service rather than to a person, and running the
// sweep again does not write a second one.
func TestIntegrationExpiredKeyIsRecordedOnceWithASystemActor(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := t.Context()

	p, err := inventory.CreateProject(ctx, db, sc, authz.CLISubject(sc.AccountID()), inventory.Limits{}, "acme", "Acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	lapsed, _, err := apikey.New()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	lapsed.ExpiresAt = &past
	lapsed.Kind = model.APIKeyKindProject
	lapsed.ProjectID = &p.ID
	lapsed.Networks = []model.APIKeyNetwork{{CIDR: "203.0.113.0/24"}}
	if err := store.CreateAPIKey(ctx, db, sc, lapsed, store.AllowAnyGrant); err != nil {
		t.Fatalf("create lapsed key: %v", err)
	}

	// A key that has not lapsed must be left entirely alone.
	live, _, err := apikey.New()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	future := time.Now().UTC().Add(time.Hour)
	live.ExpiresAt = &future
	live.Kind = model.APIKeyKindProject
	live.ProjectID = &p.ID
	if err := store.CreateAPIKey(ctx, db, sc, live, store.AllowAnyGrant); err != nil {
		t.Fatalf("create live key: %v", err)
	}

	m := obs.New()
	n, err := ExpireKeys(ctx, db, m)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired %d keys, want 1", n)
	}

	// The entry is counted, and under the credential class: apikey.expire is a
	// credential event, and counting it as config would put it in front of the
	// wrong reviewer.
	if got := counterValue(t, m.AuditEntries.WithLabelValues(obs.ActionClassCredential)); got != 1 {
		t.Errorf("credential-class audit entries = %v, want 1", got)
	}
	if got := counterValue(t, m.AuditEntries.WithLabelValues(obs.ActionClassConfig)); got != 0 {
		t.Errorf("config-class audit entries = %v, want 0", got)
	}

	es, err := store.ListAuditEntries(ctx, db, sc, store.AuditFilter{Action: authz.ActionKeyExpire})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(es) != 1 {
		t.Fatalf("%d %s entries, want 1", len(es), authz.ActionKeyExpire)
	}
	e := es[0]
	if e.ActorSource != authz.SourceSystem || e.ActorSubject != nil {
		t.Errorf("actor is %q/%v, want the system actor with no subject", e.ActorSource, e.ActorSubject)
	}
	if e.ObjectLabel != lapsed.Identifier {
		t.Errorf("objectLabel %q, want %q", e.ObjectLabel, lapsed.Identifier)
	}
	// No project: a key is anchored to the account and reaches whatever its
	// grants name, so there is no single project this expiry belongs to.
	if e.ProjectID != nil {
		t.Errorf("projectId %v, want none — a key has no project", e.ProjectID)
	}
	// The spec asks every key action to carry the restrictions in effect.
	if !strings.Contains(string(e.After), "203.0.113.0/24") {
		t.Errorf("the entry does not carry the CIDR list: %s", e.After)
	}

	// Idempotent: a second sweep, or a second instance, records nothing more.
	if n, err := ExpireKeys(ctx, db, m); err != nil || n != 0 {
		t.Fatalf("second sweep expired %d keys (err %v), want 0", n, err)
	}
	es, _ = store.ListAuditEntries(ctx, db, sc, store.AuditFilter{Action: authz.ActionKeyExpire})
	if len(es) != 1 {
		t.Fatalf("%d entries after a second sweep, want 1", len(es))
	}
	if got := counterValue(t, m.AuditEntries.WithLabelValues(obs.ActionClassCredential)); got != 1 {
		t.Errorf("credential-class audit entries after a second sweep = %v, want 1 — the counter must follow the rows, not the sweeps", got)
	}

	var stillLive model.APIKey
	if err := db.First(&stillLive, live.ID).Error; err != nil {
		t.Fatalf("re-read the live key: %v", err)
	}
	if stillLive.DisabledAt != nil {
		t.Error("the sweep disabled a key that had not expired")
	}
}

// counterValue reads one counter, the way obs's own tests do — client_golang's
// testutil would do it in one call but pulls in a dependency this module does
// not otherwise have.
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

// TestIntegrationExpiredInvitationIsRecordedOnceWithASystemActor is the
// invitation half of the test above, and covers account-invitations task 9.3.
//
// The gap it closes is a silent one. Every other outcome an invitation can have
// writes an audit entry — created, resent, revoked, accepted, declined — and
// lapsing wrote nothing, so an account reading its own history saw an
// invitation appear and then stop, with no way to distinguish "it expired" from
// "the trail is incomplete". `invitation.expire` had existed in the vocabulary
// since the change was designed, with a comment saying the system actor writes
// it, and nothing ever did.
//
// Three properties, and the second is the one worth the test: the entry exists,
// running the sweep again does not write a second one, and an invitation that
// was DECLINED before it lapsed is left alone — its outcome already happened
// and saying it also expired would make one invitation's history read as two
// contradictory events.
func TestIntegrationExpiredInvitationIsRecordedOnceWithASystemActor(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := t.Context()

	inviter, err := store.FindOrCreateUser(ctx, db, "sweeper-inviter", "Inviter", "inviter@corp.example")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}

	past := time.Now().UTC().Add(-time.Hour)
	lapsed, _, err := store.IssueInvitation(ctx, db, sc, "lapsed@corp.example", inviter.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue lapsed invitation: %v", err)
	}
	if err := db.Model(&model.Invitation{}).Where("id = ?", lapsed.ID).
		Update("expires_at", past).Error; err != nil {
		t.Fatalf("age the invitation: %v", err)
	}

	// Declined before it lapsed: already answered, and must not also be
	// reported as having expired.
	declined, _, err := store.IssueInvitation(ctx, db, sc, "declined@corp.example", inviter.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue declined invitation: %v", err)
	}
	if err := db.Model(&model.Invitation{}).Where("id = ?", declined.ID).
		Updates(map[string]any{"expires_at": past, "declined_at": past}).Error; err != nil {
		t.Fatalf("age and decline: %v", err)
	}

	// Still live: untouched.
	if _, _, err := store.IssueInvitation(ctx, db, sc, "live@corp.example", inviter.ID, time.Now().UTC()); err != nil {
		t.Fatalf("issue live invitation: %v", err)
	}

	m := obs.New()
	n, err := RecordExpiredInvitations(ctx, db, m)
	if err != nil {
		t.Fatalf("record expired invitations: %v", err)
	}
	if n != 1 {
		t.Fatalf("recorded %d invitations, want 1 — only the lapsed one", n)
	}

	// A SECOND PASS MUST WRITE NOTHING. Without the bookkeeping mark the
	// predicate is "expired", which is every invitation that ever lapsed,
	// forever, and every tick would rewrite the same entries.
	again, err := RecordExpiredInvitations(ctx, db, m)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if again != 0 {
		t.Errorf("second pass recorded %d, want 0 — the expiry is accounted for once", again)
	}

	es, err := store.ListAuditEntries(ctx, db, sc, store.AuditFilter{Action: authz.ActionInvitationExpire})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(es) != 1 {
		t.Fatalf("%d %s entries, want exactly 1", len(es), authz.ActionInvitationExpire)
	}
	e := es[0]
	if e.ActorSource != authz.SourceSystem || e.ActorSubject != nil {
		t.Errorf("actor is %q/%v, want the system actor with no subject — nobody did this, time did",
			e.ActorSource, e.ActorSubject)
	}
	if e.ObjectLabel != "lapsed@corp.example" {
		t.Errorf("objectLabel %q, want the invited address", e.ObjectLabel)
	}
	if strings.Contains(strings.ToLower(string(e.Before)), "token") {
		t.Errorf("the entry mentions a token: %s — an audit entry outlives the row it describes", e.Before)
	}
}
