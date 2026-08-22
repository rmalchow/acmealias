package store

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationAPIKeyRoundTrip(t *testing.T) {
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

	// The key's authority is its grants now, and the per-load-balancer listing
	// asks a question about them rather than about a foreign key — so a key with
	// no grants appears on no load balancer's screen, correctly.
	key := &model.APIKey{
		Identifier:    "k_7f3a91c4",
		SecretHash:    []byte("digest"),
		DisplayPrefix: "7f3a91",
		Networks: []model.APIKeyNetwork{
			{CIDR: "203.0.113.0/24"},
		},
		Grants: []model.APIKeyGrant{
			{ObjectType: "load_balancer", ObjectID: &lb.ID, Action: "challenge.publish", Inheritable: true},
		},
	}
	if err := CreateAPIKey(ctx, db, sc, key); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := ListAPIKeysForLoadBalancer(ctx, db, sc, lb.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || len(list[0].Networks) != 1 {
		t.Fatalf("list = %+v, want one key with one network", list)
	}

	found, err := FindAPIKeyByIdentifier(ctx, db, "k_7f3a91c4")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(found.Networks) != 1 || found.Networks[0].CIDR != "203.0.113.0/24" {
		t.Fatalf("networks = %+v, want the /24", found.Networks)
	}
	if found.LastUsedAt != nil {
		t.Fatalf("last_used_at = %v, want nil before first use", found.LastUsedAt)
	}

	if err := TouchLastUsed(ctx, db, key.ID); err != nil {
		t.Fatalf("touch: %v", err)
	}
	found, _ = FindAPIKeyByIdentifier(ctx, db, "k_7f3a91c4")
	if found.LastUsedAt == nil {
		t.Fatal("last_used_at still nil after touch")
	}

	if found.DisabledAt != nil {
		t.Fatalf("disabled_at = %v, want nil before revoke", found.DisabledAt)
	}
	if err := RevokeAPIKey(ctx, db, sc, key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	found, _ = FindAPIKeyByIdentifier(ctx, db, "k_7f3a91c4")
	if found.DisabledAt == nil {
		t.Fatal("disabled_at still nil after revoke")
	}

	// Revoking an already-revoked key is refused, not a silent success.
	if err := RevokeAPIKey(ctx, db, sc, key.ID); err == nil {
		t.Fatal("re-revoke: want an error, got nil")
	}
}
