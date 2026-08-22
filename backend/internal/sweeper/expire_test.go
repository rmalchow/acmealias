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
	db := migratedDB(t)
	ctx := t.Context()

	p, err := inventory.CreateProject(ctx, db, "acme", "Acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb, err := inventory.CreateLoadBalancer(ctx, db, p.ID, "eu", "EU")
	if err != nil {
		t.Fatalf("create load balancer: %v", err)
	}

	lapsed, _, err := apikey.New()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	lapsed.LoadBalancerID, lapsed.ExpiresAt = lb.ID, &past
	lapsed.Networks = []model.APIKeyNetwork{{CIDR: "203.0.113.0/24"}}
	if err := store.CreateAPIKey(ctx, db, lapsed); err != nil {
		t.Fatalf("create lapsed key: %v", err)
	}

	// A key that has not lapsed must be left entirely alone.
	live, _, err := apikey.New()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	future := time.Now().UTC().Add(time.Hour)
	live.LoadBalancerID, live.ExpiresAt = lb.ID, &future
	if err := store.CreateAPIKey(ctx, db, live); err != nil {
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

	es, err := store.ListAuditEntries(ctx, db, store.AuditFilter{Action: authz.ActionKeyExpire})
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
	if e.ProjectID == nil || *e.ProjectID != p.ID {
		t.Errorf("projectId %v, want %d", e.ProjectID, p.ID)
	}
	// The spec asks every key action to carry the restrictions in effect.
	if !strings.Contains(string(e.After), "203.0.113.0/24") {
		t.Errorf("the entry does not carry the CIDR list: %s", e.After)
	}

	// Idempotent: a second sweep, or a second instance, records nothing more.
	if n, err := ExpireKeys(ctx, db, m); err != nil || n != 0 {
		t.Fatalf("second sweep expired %d keys (err %v), want 0", n, err)
	}
	es, _ = store.ListAuditEntries(ctx, db, store.AuditFilter{Action: authz.ActionKeyExpire})
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
