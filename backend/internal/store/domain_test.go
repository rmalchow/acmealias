package store

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationDomainRoundTrip(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb := &model.LoadBalancer{ProjectID: p.ID, Slug: "eu-prod", Name: "EU Prod"}
	if err := CreateLoadBalancer(ctx, db, sc, lb); err != nil {
		t.Fatalf("create load balancer: %v", err)
	}
	alias := &model.Alias{LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge.shop.example.com", Label: "shop-example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	d := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "shop.example.com"}
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

	forLoadBalancer, err := ListDomainsForLoadBalancer(ctx, db, sc, lb.ID)
	if err != nil {
		t.Fatalf("list for lb: %v", err)
	}
	if len(forLoadBalancer) != 1 {
		t.Fatalf("len(forLoadBalancer) = %d, want 1", len(forLoadBalancer))
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
	lb := &model.LoadBalancer{ProjectID: p.ID, Slug: "eu-prod", Name: "EU Prod"}
	if err := CreateLoadBalancer(ctx, db, sc, lb); err != nil {
		t.Fatalf("create load balancer: %v", err)
	}
	alias := &model.Alias{LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	first := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, sc, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	dup := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, sc, dup); err == nil {
		t.Fatal("create duplicate domain name: want an error, got nil")
	}
}
