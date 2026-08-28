package store

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationProjectAndNodeRoundTrip(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ipv4, ipv6 := "203.0.113.10", "2001:db8:1::10"
	node := &model.LBNode{ProjectID: p.ID, Label: "a", IPv4: &ipv4, IPv6: &ipv6}
	if err := CreateLBNode(ctx, db, sc, node); err != nil {
		t.Fatalf("create lb node: %v", err)
	}

	got, err := GetProject(ctx, db, sc, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Label != "a" {
		t.Fatalf("nodes = %+v, want one node labelled a", got.Nodes)
	}

	newIPv4 := "203.0.113.20"
	node.IPv4 = &newIPv4
	if err := UpdateLBNode(ctx, db, sc, node); err != nil {
		t.Fatalf("update node: %v", err)
	}
	got, _ = GetProject(ctx, db, sc, p.ID)
	if *got.Nodes[0].IPv4 != newIPv4 {
		t.Fatalf("ipv4 after update = %q, want %q", *got.Nodes[0].IPv4, newIPv4)
	}

	list, err := ListLBNodesForProject(ctx, db, sc, p.ID)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	if err := DeleteLBNode(ctx, db, sc, node.ID); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	got, _ = GetProject(ctx, db, sc, p.ID)
	if len(got.Nodes) != 0 {
		t.Fatalf("nodes after delete = %+v, want none", got.Nodes)
	}
}

// A node cascades with its project rather than blocking its deletion. That is
// the half of the old load balancer's delete rule that survived: nodes carry no
// state of their own, so they are part of the thing being deleted.
func TestIntegrationDeleteProjectCascadesItsNodes(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "acme-shop", Name: "Acme Shop"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ipv4 := "203.0.113.10"
	node := &model.LBNode{ProjectID: p.ID, Label: "a", IPv4: &ipv4}
	if err := CreateLBNode(ctx, db, sc, node); err != nil {
		t.Fatalf("create lb node: %v", err)
	}

	if err := DeleteProject(ctx, db, sc, p.ID); err != nil {
		t.Fatalf("delete project holding only nodes: %v", err)
	}
	if _, err := GetLBNode(ctx, db, sc, node.ID); err == nil {
		t.Fatal("the node outlived its project")
	}
}
