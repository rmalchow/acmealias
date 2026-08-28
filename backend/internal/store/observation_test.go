package store

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// seedObservationProject builds the shape every case below needs and that a real
// project has: one dual-stack node, one v4-only node, and an apex and a wildcard
// sharing one alias.
func seedObservationProject(t *testing.T, db *gorm.DB, sc Scope) (apex, wild *model.Domain, dual, v4only *model.LBNode) {
	t.Helper()
	ctx := context.Background()

	p := &model.Project{Slug: "obs", Name: "Observed"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	v4a, v6a, v4b := "192.0.2.10", "2001:db8::10", "192.0.2.11"
	dual = &model.LBNode{ProjectID: p.ID, Label: "a", IPv4: &v4a, IPv6: &v6a}
	v4only = &model.LBNode{ProjectID: p.ID, Label: "b", IPv4: &v4b}
	for _, n := range []*model.LBNode{dual, v4only} {
		if err := CreateLBNode(ctx, db, sc, n); err != nil {
			t.Fatalf("create node %s: %v", n.Label, err)
		}
	}
	alias := &model.Alias{ProjectID: p.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	apex = &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "example.com"}
	wild = &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "*.example.com"}
	for _, d := range []*model.Domain{apex, wild} {
		if err := CreateDomain(ctx, db, sc, d); err != nil {
			t.Fatalf("create %s: %v", d.Name, err)
		}
	}
	return apex, wild, dual, v4only
}

// TestIntegrationDueObservationsRespectTheSwitch is task 12.13, asserted on the
// QUERY rather than by counting dials: the switch is enforced here, so this is
// where it can go wrong.
func TestIntegrationDueObservationsRespectTheSwitch(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	apex, wild, dual, v4only := seedObservationProject(t, db, sc)

	due, err := DueObservations(ctx, db, now, time.Hour, 100)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("with every switch off, %d handshakes were due — nothing may be "+
			"connected to until somebody opts in: %+v", len(due), due)
	}

	if _, err := UpdateDomainObservation(ctx, db, sc, apex.ID, true, nil); err != nil {
		t.Fatalf("enable on the apex: %v", err)
	}

	due, err = DueObservations(ctx, db, now, time.Hour, 100)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	// Three: the dual-stack node twice (a separate attempt per family, G6) and
	// the v4-only node once. The absent v6 address is NOT a row — there was
	// nothing to attempt, which is different from an attempt that failed.
	if len(due) != 3 {
		t.Fatalf("due = %d rows, want 3 (dual-stack node × 2 families + v4-only node × 1): %+v", len(due), due)
	}
	families := map[string]string{}
	for _, d := range due {
		if d.DomainID != apex.ID {
			t.Errorf("the wildcard has its switch off and appeared anyway: %+v", d)
		}
		if d.ServerName != "example.com" {
			t.Errorf("server name = %q, want the domain's own name", d.ServerName)
		}
		families[d.NodeID.String()+"/"+d.Family] = d.Address
	}
	if got := families[dual.ID.String()+"/AAAA"]; got != "2001:db8::10" {
		t.Errorf("the v6 attempt on the dual-stack node = %q, want 2001:db8::10", got)
	}
	if _, ok := families[v4only.ID.String()+"/AAAA"]; ok {
		t.Error("a v6 attempt was queued for a node with no v6 address")
	}

	// The wildcard needs a hostname to be enabled at all, and once it has one
	// that hostname — not the wildcard — is what gets presented.
	host := "www.example.com"
	if _, err := UpdateDomainObservation(ctx, db, sc, wild.ID, true, &host); err != nil {
		t.Fatalf("enable on the wildcard: %v", err)
	}
	due, err = DueObservations(ctx, db, now, time.Hour, 100)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 6 {
		t.Fatalf("due = %d rows, want 6 (two domains × three addresses)", len(due))
	}
	for _, d := range due {
		if d.DomainID == wild.ID && d.ServerName != host {
			t.Errorf("the wildcard's server name = %q, want its test host %q — "+
				"%q is not a name a TLS client can ask for", d.ServerName, host, wild.Name)
		}
	}
}

// TestIntegrationDueObservationsQueueOrder is design G13: a domain somebody just
// switched on outranks one due for routine re-observation, so a batch too small
// for the backlog delays the settled work and never the person watching.
func TestIntegrationDueObservationsQueueOrder(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	apex, wild, dual, _ := seedObservationProject(t, db, sc)
	host := "www.example.com"
	if _, err := UpdateDomainObservation(ctx, db, sc, apex.ID, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateDomainObservation(ctx, db, sc, wild.ID, true, &host); err != nil {
		t.Fatal(err)
	}

	// The apex has been observed on every address; the wildcard never has.
	for _, fam := range []struct{ family, address string }{
		{"A", "192.0.2.10"}, {"AAAA", "2001:db8::10"},
	} {
		record(t, db, apex, dual, fam.family, fam.address, now.Add(-2*time.Hour))
	}

	due, err := DueObservations(ctx, db, now, time.Hour, 100)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) == 0 {
		t.Fatal("nothing was due")
	}
	// Everything never observed comes first; the two the apex has a stamp for
	// come last.
	seenObserved := false
	for _, d := range due {
		neverObserved := !(d.DomainID == apex.ID && d.NodeID == dual.ID)
		if neverObserved && seenObserved {
			t.Fatalf("a never-observed address sorted behind an already-observed one:\n%+v", due)
		}
		if !neverObserved {
			seenObserved = true
		}
	}

	// And a stamp inside the interval takes the row out of the queue entirely.
	record(t, db, apex, dual, "A", "192.0.2.10", now)
	due, err = DueObservations(ctx, db, now, time.Hour, 100)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	for _, d := range due {
		if d.DomainID == apex.ID && d.NodeID == dual.ID && d.Family == "A" {
			t.Fatal("an address observed a moment ago is still due — the interval is not being applied")
		}
	}
}

// TestIntegrationObservationReads covers the two request-path reads: the latest
// per address, and the capped history.
func TestIntegrationObservationReads(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	apex, _, dual, _ := seedObservationProject(t, db, sc)
	if _, err := UpdateDomainObservation(ctx, db, sc, apex.ID, true, nil); err != nil {
		t.Fatal(err)
	}

	// Three observations of the v4 address and one of the v6 address, so
	// "latest per address" has something to choose between.
	record(t, db, apex, dual, "A", "192.0.2.10", now.Add(-2*time.Hour))
	record(t, db, apex, dual, "A", "192.0.2.10", now.Add(-time.Hour))
	newest := record(t, db, apex, dual, "A", "192.0.2.10", now)
	record(t, db, apex, dual, "AAAA", "2001:db8::10", now.Add(-3*time.Hour))

	latest, err := LatestObservations(ctx, db, sc, apex.ID)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("latest = %d rows, want one per (node, family)", len(latest))
	}
	for _, o := range latest {
		if o.Family == "A" && !o.ObservedAt.Equal(newest) {
			t.Errorf("latest v4 observation is from %v, want the newest at %v", o.ObservedAt, newest)
		}
	}

	history, err := ObservationHistory(ctx, db, sc, apex.ID, 100)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("history = %d rows, want all 4", len(history))
	}
	for i := 1; i < len(history); i++ {
		if history[i].ObservedAt.After(history[i-1].ObservedAt) {
			t.Fatalf("history is not newest-first at %d: %v then %v",
				i, history[i-1].ObservedAt, history[i].ObservedAt)
		}
	}

	// A limit is honoured, and "everything" is not a question this answers.
	if got, err := ObservationHistory(ctx, db, sc, apex.ID, 2); err != nil || len(got) != 2 {
		t.Fatalf("history(limit 2) = %d rows, err %v, want 2", len(got), err)
	}
	if got, err := ObservationHistory(ctx, db, sc, apex.ID, 0); err != nil || len(got) != 4 {
		t.Fatalf("history(limit 0) = %d rows, err %v — a caller that did not choose "+
			"gets the ceiling, not nothing", len(got), err)
	}
}

// record appends one successful observation and returns the stamp it was written
// with, so a test can assert on it without reading the row back.
func record(t *testing.T, db *gorm.DB, d *model.Domain, n *model.LBNode, family, address string, at time.Time) time.Time {
	t.Helper()
	at = at.Truncate(time.Microsecond)
	notAfter := at.Add(60 * 24 * time.Hour)
	issuer, serial := "Test CA", "01"
	yes := true
	err := RecordObservation(context.Background(), db, model.CertificateObservation{
		AccountID: d.AccountID, ProjectID: d.ProjectID, DomainID: d.ID, NodeID: n.ID,
		Family: family, Address: address, ServerName: d.Name, ObservedAt: at,
		Outcome: "observed", NotAfter: &notAfter, Issuer: &issuer, Serial: &serial,
		SANs: model.StringArray{d.Name}, CoversServerName: &yes, ChainTrusted: &yes,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}
	return at
}
