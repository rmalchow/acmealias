package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationProjectRoundTrip(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == model.NilID {
		t.Fatal("create did not assign an id")
	}

	got, err := GetProject(ctx, db, sc, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Slug != "acme-shop" {
		t.Fatalf("slug = %q, want acme-shop", got.Slug)
	}

	list, err := ListProjects(ctx, db, sc)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	if err := DeleteProject(ctx, db, sc, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetProject(ctx, db, sc, p.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get after delete: err = %v, want ErrRecordNotFound", err)
	}
}

func TestIntegrationDeleteProjectWithChildrenRefused(t *testing.T) {
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

	if err := DeleteProject(ctx, db, sc, p.ID); err == nil {
		t.Fatal("delete project with an alias still attached: want an error, got nil")
	}

	// The project must still be there.
	if _, err := GetProject(ctx, db, sc, p.ID); err != nil {
		t.Fatalf("project vanished despite refused delete: %v", err)
	}
}

// The listing reports what is IN each project, and it must do so in ONE query
// whatever the number of projects (design L5). Both halves are asserted here:
// the counts, and the statement count that stays flat between one project and
// five. A per-row count would pass the first assertion and fail the second,
// which is exactly the regression worth a test — it looks right on a screen and
// falls over on the account with forty.
func TestIntegrationListProjectsACLCountsWhatIsInThem(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	seed := func(slug string, nodes int, domains []string) model.ID {
		p := &model.Project{Slug: slug, Name: slug}
		if err := CreateProject(ctx, db, sc, p); err != nil {
			t.Fatalf("create project %s: %v", slug, err)
		}
		for i := 0; i < nodes; i++ {
			ipv4 := "203.0.113.1" + string(rune('0'+i))
			n := &model.LBNode{ProjectID: p.ID, Label: string(rune('a' + i)), IPv4: &ipv4}
			if err := CreateLBNode(ctx, db, sc, n); err != nil {
				t.Fatalf("create node: %v", err)
			}
		}
		if len(domains) > 0 {
			// One alias for the group: the apex and its wildcard share a
			// challenge name, so a count taken over aliases would report 1.
			alias := &model.Alias{
				ProjectID: p.ID, ChallengeFQDN: "_acme-challenge." + slug + ".example",
				Label: slug + "-example",
			}
			if err := CreateAlias(ctx, db, sc, alias); err != nil {
				t.Fatalf("create alias: %v", err)
			}
			for _, name := range domains {
				if err := CreateDomain(ctx, db, sc, &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: name}); err != nil {
					t.Fatalf("create domain %s: %v", name, err)
				}
			}
		}
		return p.ID
	}

	seed("acme-shop", 2, []string{"shop.example", "*.shop.example"})
	seed("acme-empty", 0, nil)

	all := ACL{All: true}
	got, total, err := ListProjectsACL(ctx, db, sc, all, "", 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("total = %d, len = %d, want 2 and 2", total, len(got))
	}
	if got[0].Slug != "acme-empty" || got[0].NodeCount != 0 || got[0].DomainCount != 0 {
		t.Errorf("%s: %d nodes, %d domains, want an empty project first",
			got[0].Slug, got[0].NodeCount, got[0].DomainCount)
	}
	if got[1].NodeCount != 2 || got[1].DomainCount != 2 {
		t.Errorf("%s: %d nodes, %d domains, want 2 and 2",
			got[1].Slug, got[1].NodeCount, got[1].DomainCount)
	}

	before := countStatements(t, db, func(tx *gorm.DB) {
		if _, _, err := ListProjectsACL(ctx, tx, sc, all, "", 50, 0); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
	for i := 0; i < 5; i++ {
		seed(fmt.Sprintf("filler-%d", i), 1, []string{fmt.Sprintf("f%d.example", i)})
	}
	after := countStatements(t, db, func(tx *gorm.DB) {
		if _, _, err := ListProjectsACL(ctx, tx, sc, all, "", 50, 0); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
	if after != before {
		t.Errorf("listing issued %d statements for 7 projects and %d for 2 — the counts are being "+
			"fetched per row, which is the shape that looks fine in development and falls over "+
			"on the account with forty", after, before)
	}
}

// countStatements runs fn against a handle that counts every statement GORM
// sends, which is the only way to assert "one query" from outside: the number of
// rows a listing returns says nothing about how many round trips produced them.
func countStatements(t *testing.T, db *gorm.DB, fn func(*gorm.DB)) int {
	t.Helper()
	n := 0
	tx := db.Session(&gorm.Session{NewDB: true})
	for _, p := range []struct {
		register func(string, func(*gorm.DB)) error
	}{
		{tx.Callback().Query().After("*").Register},
		{tx.Callback().Raw().After("*").Register},
		{tx.Callback().Row().After("*").Register},
	} {
		if err := p.register("test:count", func(*gorm.DB) { n++ }); err != nil {
			t.Fatalf("register counting callback: %v", err)
		}
	}
	fn(tx)
	return n
}
