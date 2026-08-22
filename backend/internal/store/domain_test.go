package store

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationDomainRoundTrip(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb := &model.LoadBalancer{ProjectID: p.ID, Slug: "eu-prod", Name: "EU Prod"}
	if err := CreateLoadBalancer(ctx, db, lb); err != nil {
		t.Fatalf("create load balancer: %v", err)
	}
	alias := &model.Alias{LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge.shop.example.com", Label: "shop-example-com"}
	if err := CreateAlias(ctx, db, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	d := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "shop.example.com"}
	if err := CreateDomain(ctx, db, d); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	got, err := GetDomain(ctx, db, d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "shop.example.com" {
		t.Fatalf("name = %q, want shop.example.com", got.Name)
	}

	byName, err := FindDomainByName(ctx, db, "shop.example.com")
	if err != nil {
		t.Fatalf("find by name: %v", err)
	}
	if byName.ID != d.ID {
		t.Fatalf("id = %d, want %d", byName.ID, d.ID)
	}

	forLoadBalancer, err := ListDomainsForLoadBalancer(ctx, db, lb.ID)
	if err != nil {
		t.Fatalf("list for lb: %v", err)
	}
	if len(forLoadBalancer) != 1 {
		t.Fatalf("len(forLoadBalancer) = %d, want 1", len(forLoadBalancer))
	}

	forAlias, err := ListDomainsForAlias(ctx, db, alias.ID)
	if err != nil {
		t.Fatalf("list for alias: %v", err)
	}
	if len(forAlias) != 1 {
		t.Fatalf("len(forAlias) = %d, want 1", len(forAlias))
	}

	if err := DeleteDomain(ctx, db, d.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetDomain(ctx, db, d.ID); err == nil {
		t.Fatal("get after delete: want an error, got nil")
	}
}

func TestIntegrationDomainNameIsUnique(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb := &model.LoadBalancer{ProjectID: p.ID, Slug: "eu-prod", Name: "EU Prod"}
	if err := CreateLoadBalancer(ctx, db, lb); err != nil {
		t.Fatalf("create load balancer: %v", err)
	}
	alias := &model.Alias{LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	first := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	dup := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, dup); err == nil {
		t.Fatal("create duplicate domain name: want an error, got nil")
	}
}
