package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationAliasRoundTrip(t *testing.T) {
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

	got, err := FindAliasByChallengeName(ctx, db, "_acme-challenge.example.com")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ID != alias.ID {
		t.Fatalf("id = %d, want %s", got.ID, alias.ID)
	}

	if _, err := FindAliasByChallengeName(ctx, db, "_acme-challenge.nowhere.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("find missing: err = %v, want ErrRecordNotFound", err)
	}

	// Apex and wildcard share the alias.
	apex := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "example.com"}
	wild := &model.Domain{ProjectID: p.ID, AliasID: alias.ID, Name: "*.example.com"}
	if err := CreateDomain(ctx, db, sc, apex); err != nil {
		t.Fatalf("create apex domain: %v", err)
	}
	if err := CreateDomain(ctx, db, sc, wild); err != nil {
		t.Fatalf("create wildcard domain: %v", err)
	}

	list, err := ListAliasesForProject(ctx, db, sc, p.ID)
	if err != nil {
		t.Fatalf("list for lb: %v", err)
	}
	if len(list) != 1 || len(list[0].Domains) != 2 {
		t.Fatalf("list = %+v, want one alias with two domains", list)
	}

	all, err := ListAllAliases(ctx, db)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len(all) = %d, want 1", len(all))
	}

	// Alias can't be deleted while domains still reference it.
	if err := DeleteAlias(ctx, db, sc, alias.ID); err == nil {
		t.Fatal("delete alias with domains attached: want an error, got nil")
	}

	if err := DeleteDomain(ctx, db, sc, apex.ID); err != nil {
		t.Fatalf("delete apex domain: %v", err)
	}
	if err := DeleteDomain(ctx, db, sc, wild.ID); err != nil {
		t.Fatalf("delete wildcard domain: %v", err)
	}
	if err := DeleteAlias(ctx, db, sc, alias.ID); err != nil {
		t.Fatalf("delete alias after domains removed: %v", err)
	}
}
