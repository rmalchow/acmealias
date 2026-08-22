package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationProjectRoundTrip(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("create did not assign an id")
	}

	got, err := GetProject(ctx, db, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Slug != "acme-shop" {
		t.Fatalf("slug = %q, want acme-shop", got.Slug)
	}

	list, err := ListProjects(ctx, db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	if err := DeleteProject(ctx, db, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetProject(ctx, db, p.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get after delete: err = %v, want ErrRecordNotFound", err)
	}
}

func TestIntegrationDeleteProjectWithChildrenRefused(t *testing.T) {
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

	if err := DeleteProject(ctx, db, p.ID); err == nil {
		t.Fatal("delete project with a load balancer still attached: want an error, got nil")
	}

	// The project must still be there.
	if _, err := GetProject(ctx, db, p.ID); err != nil {
		t.Fatalf("project vanished despite refused delete: %v", err)
	}
}

func TestIntegrationListProjectsWithLoadBalancers(t *testing.T) {
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
	ipv4 := "203.0.113.10"
	if err := CreateLBNode(ctx, db, &model.LBNode{LoadBalancerID: lb.ID, Label: "a", IPv4: &ipv4}); err != nil {
		t.Fatalf("create lb node: %v", err)
	}
	alias := &model.Alias{LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	domain := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	got, err := ListProjectsWithLoadBalancers(ctx, db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].LoadBalancers) != 1 {
		t.Fatalf("len(lbs) = %d, want 1", len(got[0].LoadBalancers))
	}
	pr := got[0].LoadBalancers[0]
	if len(pr.Nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(pr.Nodes))
	}
	if len(pr.Domains) != 1 || pr.Domains[0].Name != "example.com" {
		t.Fatalf("domains = %+v, want one domain example.com", pr.Domains)
	}
}
