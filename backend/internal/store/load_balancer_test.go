package store

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationLoadBalancerAndNodeRoundTrip(t *testing.T) {
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

	ipv4, ipv6 := "203.0.113.10", "2001:db8:1::10"
	node := &model.LBNode{LoadBalancerID: lb.ID, Label: "a", IPv4: &ipv4, IPv6: &ipv6}
	if err := CreateLBNode(ctx, db, sc, node); err != nil {
		t.Fatalf("create lb node: %v", err)
	}

	got, err := GetLoadBalancer(ctx, db, sc, lb.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Label != "a" {
		t.Fatalf("nodes = %+v, want one node labelled a", got.Nodes)
	}

	list, err := ListLoadBalancersForProject(ctx, db, sc, p.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	newIPv4 := "203.0.113.20"
	node.IPv4 = &newIPv4
	if err := UpdateLBNode(ctx, db, sc, node); err != nil {
		t.Fatalf("update node: %v", err)
	}
	got, _ = GetLoadBalancer(ctx, db, sc, lb.ID)
	if *got.Nodes[0].IPv4 != newIPv4 {
		t.Fatalf("ipv4 after update = %q, want %q", *got.Nodes[0].IPv4, newIPv4)
	}

	if err := DeleteLBNode(ctx, db, sc, node.ID); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	got, _ = GetLoadBalancer(ctx, db, sc, lb.ID)
	if len(got.Nodes) != 0 {
		t.Fatalf("nodes after delete = %+v, want none", got.Nodes)
	}
}

func TestIntegrationDeleteLoadBalancerWithDomainRefused(t *testing.T) {
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
	domain := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "example.com"}
	if err := CreateDomain(ctx, db, sc, domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	if err := DeleteLoadBalancer(ctx, db, sc, lb.ID); err == nil {
		t.Fatal("delete load balancer with a domain still attached: want an error, got nil")
	}
}

// The project screen shows a domain COUNT per load balancer, and it read 0 for
// every load balancer that had any: ListLoadBalancersForProject preloaded Nodes
// and not Domains, so the association came back empty from the query rather than
// from the data. Counting through the listing — not through
// ListDomainsForLoadBalancer — is what makes this a test of the screen's answer.
func TestIntegrationListLoadBalancersForProjectCarriesTheirDomains(t *testing.T) {
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
	// One alias, two domains: the apex and its wildcard share a challenge name,
	// so a listing that counted aliases instead would report 1.
	alias := &model.Alias{LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	for _, name := range []string{"example.com", "*.example.com"} {
		if err := CreateDomain(ctx, db, sc, &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: name}); err != nil {
			t.Fatalf("create domain %s: %v", name, err)
		}
	}

	list, err := ListLoadBalancersForProject(ctx, db, sc, p.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if len(list[0].Domains) != 2 {
		t.Fatalf("load balancer %q reports %d domains through the project listing, want 2",
			list[0].Slug, len(list[0].Domains))
	}
}
