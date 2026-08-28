package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationDomainRoundTrip(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	alias := &model.Alias{ProjectID: p.ID, ChallengeFQDN: "_acme-challenge.shop.example.com", Label: "shop-example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	d := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "shop.example.com"}
	if err := CreateDomain(ctx, db, sc, d); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	got, err := GetDomain(ctx, db, sc, d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "shop.example.com" {
		t.Fatalf("name = %q, want shop.example.com", got.Name)
	}

	byName, err := FindDomainByName(ctx, db, sc, "shop.example.com")
	if err != nil {
		t.Fatalf("find by name: %v", err)
	}
	if byName.ID != d.ID {
		t.Fatalf("id = %d, want %s", byName.ID, d.ID)
	}

	forProject, err := ListDomainsForProject(ctx, db, sc, p.ID)
	if err != nil {
		t.Fatalf("list for lb: %v", err)
	}
	if len(forProject) != 1 {
		t.Fatalf("len(forProject) = %d, want 1", len(forProject))
	}

	forAlias, err := ListDomainsForAlias(ctx, db, sc, alias.ID)
	if err != nil {
		t.Fatalf("list for alias: %v", err)
	}
	if len(forAlias) != 1 {
		t.Fatalf("len(forAlias) = %d, want 1", len(forAlias))
	}

	if err := DeleteDomain(ctx, db, sc, d.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetDomain(ctx, db, sc, d.ID); err == nil {
		t.Fatal("get after delete: want an error, got nil")
	}
}

func TestIntegrationDomainNameIsUnique(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	alias := &model.Alias{ProjectID: p.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	first := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, sc, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	dup := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, sc, dup); err == nil {
		t.Fatal("create duplicate domain name: want an error, got nil")
	}
}

// TestIntegrationActivationQueueAndStamps covers the three properties task
// group 2 rests on: the due query finds a never-checked domain and stops
// finding it once it has been stamped, one observation activates every domain
// on the shared delegation (domain-activation A1), and a later failed check
// leaves the activation alone (A3).
func TestIntegrationActivationQueueAndStamps(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme", Name: "Acme"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	// One alias, two domains: the apex and its wildcard validate at the same
	// name and share the one CNAME the customer publishes.
	alias := &model.Alias{ProjectID: p.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	for _, name := range []string{"example.com", "*.example.com"} {
		if err := CreateDomain(ctx, db, sc, &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: name}); err != nil {
			t.Fatalf("create domain %q: %v", name, err)
		}
	}

	fresh, settled := 5*time.Minute, 24*time.Hour
	now := time.Now().UTC()

	due, err := DueDelegations(ctx, db, now, fresh, settled, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].AliasID != alias.ID {
		t.Fatalf("due = %+v, want the one alias — two domains share one delegation and it is asked about once", due)
	}
	if due[0].ChallengeFQDN != alias.ChallengeFQDN || due[0].Label != alias.Label {
		t.Fatalf("due carries %q -> %q, want %q -> %q", due[0].ChallengeFQDN, due[0].Label, alias.ChallengeFQDN, alias.Label)
	}

	n, err := RecordDelegationSeen(ctx, db, alias.ID, now)
	if err != nil {
		t.Fatalf("record seen: %v", err)
	}
	if n != 2 {
		t.Fatalf("the observation reached %d domains, want both of them (A1)", n)
	}
	for _, name := range []string{"example.com", "*.example.com"} {
		d, err := FindDomainByName(ctx, db, sc, name)
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if d.LastSuccessAt == nil {
			t.Errorf("%s is still inactive after the delegation it shares was seen", name)
		}
	}

	// Just stamped, so neither cadence is up yet.
	if due, err = DueDelegations(ctx, db, now, fresh, settled, 10); err != nil || len(due) != 0 {
		t.Fatalf("due immediately after a check = %+v (err %v), want nothing", due, err)
	}
	// A day later the settled cadence brings it back.
	if due, err = DueDelegations(ctx, db, now.Add(25*time.Hour), fresh, settled, 10); err != nil || len(due) != 1 {
		t.Fatalf("due a day later = %+v (err %v), want the one alias", due, err)
	}

	// A3: the re-check fails, and it changes nothing about being activated.
	// Repeated, because "it survives one failure" is not the promise.
	for i := 0; i < 3; i++ {
		if _, err := RecordDelegationMissing(ctx, db, alias.ID, now.Add(time.Duration(i+1)*26*time.Hour)); err != nil {
			t.Fatalf("record missing: %v", err)
		}
	}
	for _, name := range []string{"example.com", "*.example.com"} {
		d, err := FindDomainByName(ctx, db, sc, name)
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if d.LastSuccessAt == nil {
			t.Errorf("%s was deactivated by failed re-checks — activation is one-way (A3)", name)
		}
		if d.LastCheckedAt == nil || !d.LastCheckedAt.After(*d.LastSuccessAt) {
			t.Errorf("%s: last_checked_at %v does not record the later look", name, d.LastCheckedAt)
		}
	}
}

// TestIntegrationDomainJoiningAnActivatedAliasIsActivated covers the invariant
// that keeps the gate and the console from deriving one fact two ways: every
// domain on an alias carries the same activation stamps, including one added
// after the delegation was seen (domain-activation A1).
//
// The case is ordinary. A customer activates example.com, then adds
// *.example.com — which collapses onto the same challenge name, the same alias
// and the same published CNAME. store.DelegationSeen asks the alias, so
// /present publishes for the new domain immediately; if its own row were left
// NULL the console would call it "waiting for delegation" and offer a check for
// something that already works.
func TestIntegrationDomainJoiningAnActivatedAliasIsActivated(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme", Name: "Acme"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	alias := &model.Alias{ProjectID: p.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	if err := CreateDomain(ctx, db, sc, &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "example.com"}); err != nil {
		t.Fatalf("create apex: %v", err)
	}

	// The first domain begins offline: nothing has looked at it.
	apex, err := FindDomainByName(ctx, db, sc, "example.com")
	if err != nil {
		t.Fatalf("find apex: %v", err)
	}
	if apex.LastSuccessAt != nil || apex.LastCheckedAt != nil {
		t.Fatalf("a brand new domain on a brand new alias arrived stamped: %+v", apex)
	}

	seen := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := RecordDelegationSeen(ctx, db, alias.ID, seen); err != nil {
		t.Fatalf("record seen: %v", err)
	}

	// Now the wildcard joins the alias whose CNAME is already published.
	wild := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "*.example.com"}
	if err := CreateDomain(ctx, db, sc, wild); err != nil {
		t.Fatalf("create wildcard: %v", err)
	}
	if wild.LastSuccessAt == nil {
		t.Fatal("the wildcard joined an activated alias and arrived inactive; /present would " +
			"publish for it while the console showed it waiting (A1)")
	}
	stored, err := FindDomainByName(ctx, db, sc, "*.example.com")
	if err != nil {
		t.Fatalf("find wildcard: %v", err)
	}
	if stored.LastSuccessAt == nil || !stored.LastSuccessAt.Equal(seen) {
		t.Errorf("wildcard last_success_at = %v, want the alias's %v", stored.LastSuccessAt, seen)
	}
	if stored.LastCheckedAt == nil || !stored.LastCheckedAt.Equal(seen) {
		t.Errorf("wildcard last_checked_at = %v, want the alias's %v", stored.LastCheckedAt, seen)
	}

	// The queue is not disturbed by the new row: the alias was just checked, so
	// neither cadence is up.
	due, err := DueDelegations(ctx, db, seen, 5*time.Minute, 24*time.Hour, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("due right after the check = %+v, want nothing — the joining domain must not "+
			"drag its alias back to the front of the queue", due)
	}
}

// TestIntegrationDomainObservationSwitch is design G8 at the store: what the
// switch does, what it refuses, and what it deliberately leaves alone (change
// domain-diagnostics, tasks 2.2a, 2.3, 2.5).
func TestIntegrationDomainObservationSwitch(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "obs", Name: "Observed"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	alias := &model.Alias{ProjectID: p.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	apex := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "example.com"}
	wild := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "*.example.com"}
	for _, d := range []*model.Domain{apex, wild} {
		if err := CreateDomain(ctx, db, sc, d); err != nil {
			t.Fatalf("create %s: %v", d.Name, err)
		}
		if d.ObserveTLS || d.TLSTestHost != nil {
			t.Fatalf("%s arrived with observation on (%v, %v) — nothing may be "+
				"connected to until somebody switches it on", d.Name, d.ObserveTLS, d.TLSTestHost)
		}
	}

	t.Run("the apex needs no test hostname", func(t *testing.T) {
		got, err := UpdateDomainObservation(ctx, db, sc, apex.ID, true, nil)
		if err != nil {
			t.Fatalf("enable on the apex: %v", err)
		}
		if !got.ObserveTLS || got.TLSTestHost != nil {
			t.Errorf("apex = %v/%v, want on with no hostname — example.com IS a TLS server name",
				got.ObserveTLS, got.TLSTestHost)
		}
	})

	t.Run("a wildcard without one is refused by the database", func(t *testing.T) {
		// The check constraint, not a Go guard: a future write path that does not
		// go through inventory would otherwise store work nothing can perform,
		// since "*.example.com" cannot be sent as a TLS server name.
		if _, err := UpdateDomainObservation(ctx, db, sc, wild.ID, true, nil); err == nil {
			t.Fatal("a wildcard with observation on and no test hostname was accepted")
		}
		got, err := GetDomain(ctx, db, sc, wild.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ObserveTLS {
			t.Error("the refused update left the switch on")
		}
	})

	t.Run("a wildcard with one is accepted, and disabling keeps it", func(t *testing.T) {
		host := "www.example.com"
		got, err := UpdateDomainObservation(ctx, db, sc, wild.ID, true, &host)
		if err != nil {
			t.Fatalf("enable with a hostname: %v", err)
		}
		if !got.ObserveTLS || got.TLSTestHost == nil || *got.TLSTestHost != host {
			t.Fatalf("wildcard = %v/%v, want on with %q", got.ObserveTLS, got.TLSTestHost, host)
		}

		// G8: turning the switch off does not clear the hostname. Somebody who
		// turns it back on gets theirs back rather than being asked again.
		got, err = UpdateDomainObservation(ctx, db, sc, wild.ID, false, nil)
		if err != nil {
			t.Fatalf("disable: %v", err)
		}
		if got.ObserveTLS {
			t.Error("the switch stayed on")
		}
		if got.TLSTestHost == nil || *got.TLSTestHost != host {
			t.Errorf("hostname after disabling = %v, want it kept as %q", got.TLSTestHost, host)
		}
	})

	t.Run("another account's domain is not found", func(t *testing.T) {
		other := seedAccount(t, db, "elsewhere")
		if _, err := UpdateDomainObservation(ctx, db, other, apex.ID, false, nil); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("err = %v, want ErrRecordNotFound", err)
		}
	})
}
