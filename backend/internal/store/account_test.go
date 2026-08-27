package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// personalAccount inserts a personal account for a user the way the
// materialization path of C1 will: no slug the tenant chose, no roles, a name
// derived from the person.
func personalAccount(t *testing.T, db *gorm.DB, owner model.ID, slug string) model.Account {
	t.Helper()
	acc := model.Account{
		ID: model.NewID(), Slug: slug, Name: "Personal",
		Kind: model.AccountKindPersonal, OwnerUserID: owner,
	}
	if err := CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Create(&acc).Error
	}); err != nil {
		t.Fatalf("create personal account: %v", err)
	}
	return acc
}

// TestIntegrationOnePersonalAccountPerUser is design C2's limit, asserted where
// it is actually enforced. The personal account is implicit — no request asks
// for one — so the only way a second could appear is a race between two first
// sign-ins, and a check in Go loses that race by construction. This is the
// partial unique index doing the work instead.
func TestIntegrationOnePersonalAccountPerUser(t *testing.T) {
	db, _ := isolatedDB(t)
	owner := seedUser(t, db, "u-personal")

	personalAccount(t, db, owner, "personal-one")

	err := CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Create(&model.Account{
			ID: model.NewID(), Slug: "personal-two", Name: "Personal",
			Kind: model.AccountKindPersonal, OwnerUserID: owner,
		}).Error
	})
	if err == nil {
		t.Fatal("a second personal account was created; the partial unique index is not enforcing C2")
	}

	// Team accounts are deliberately unlimited: paying for one must not consume
	// the free slot.
	for _, slug := range []string{"team-one", "team-two"} {
		if _, err := CreateAccountWithOwner(context.Background(), db, slug, slug, owner, 25); err != nil {
			t.Fatalf("create team account %q: %v", slug, err)
		}
	}
}

// TestIntegrationConcurrentPersonalAccounts is the race C1 names: two first
// sign-ins for the same person at the same moment. One insert wins, the other is
// refused by the database, and the user ends with exactly one account.
func TestIntegrationConcurrentPersonalAccounts(t *testing.T) {
	db, _ := isolatedDB(t)
	owner := seedUser(t, db, "u-racing")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
				return tx.Create(&model.Account{
					ID: model.NewID(), Slug: "racing-" + string(rune('a'+i)), Name: "Personal",
					Kind: model.AccountKindPersonal, OwnerUserID: owner,
				}).Error
			})
		}(i)
	}
	wg.Wait()

	created := 0
	for _, err := range errs {
		if err == nil {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("%d of 2 concurrent materializations succeeded, want exactly 1 (errors: %v)", created, errs)
	}

	var n int64
	if err := CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).
			Where("owner_user_id = ? AND kind = ?", owner, model.AccountKindPersonal).
			Count(&n).Error
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("user owns %d personal accounts, want 1", n)
	}
}

// TestIntegrationAccountKindIsImmutable covers C1's other half. Nobody may write
// kind after creation — not the service, not the CLI, not a repair script — so
// the refusal is the database's.
func TestIntegrationAccountKindIsImmutable(t *testing.T) {
	db, _ := isolatedDB(t)
	owner := seedUser(t, db, "u-immutable")
	acc := personalAccount(t, db, owner, "personal-immutable")

	err := CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).
			Where("id = ?", acc.ID).
			Update("kind", model.AccountKindTeam).Error
	})
	if err == nil {
		t.Fatal("a personal account was reclassified as a team account")
	}

	team, err2 := CreateAccountWithOwner(context.Background(), db, "team-immutable", "Team", owner, 25)
	if err2 != nil {
		t.Fatalf("create team account: %v", err2)
	}
	err = CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).
			Where("id = ?", team.ID).
			Update("kind", model.AccountKindPersonal).Error
	})
	if err == nil {
		t.Fatal("a team account was reclassified as a personal account")
	}
}

// TestIntegrationPersonalAccountIsNotTransferable is C1's immutable owner, at
// both levels: the sentence a caller reads, and the trigger under it that a
// caller writing raw SQL still meets.
func TestIntegrationPersonalAccountIsNotTransferable(t *testing.T) {
	db, _ := isolatedDB(t)
	ctx := context.Background()
	owner := seedUser(t, db, "u-owner")
	recipient := seedUser(t, db, "u-recipient")
	acc := personalAccount(t, db, owner, "personal-transfer")
	sc := Scope{accountID: acc.ID}

	if err := TransferOwnership(ctx, db, sc, recipient); !errors.Is(err, ErrPersonalAccountFixed) {
		t.Fatalf("transfer = %v, want ErrPersonalAccountFixed", err)
	}

	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).
			Where("id = ?", acc.ID).
			Update("owner_user_id", recipient).Error
	})
	if err == nil {
		t.Fatal("a personal account changed hands through a direct write")
	}
}

// TestIntegrationTeamAccountTransfers is the case that is allowed, so that the
// refusals above are not passing because transfer is broken outright.
func TestIntegrationTeamAccountTransfers(t *testing.T) {
	db, _ := isolatedDB(t)
	ctx := context.Background()
	owner := seedUser(t, db, "u-team-owner")
	recipient := seedUser(t, db, "u-team-recipient")

	acc, err := CreateAccountWithOwner(ctx, db, "team-transfer", "Team", owner, 25)
	if err != nil {
		t.Fatalf("create team account: %v", err)
	}
	sc := Scope{accountID: acc.ID}
	if err := TransferOwnership(ctx, db, sc, recipient); err != nil {
		t.Fatalf("transfer: %v", err)
	}

	after, err := GetAccount(ctx, db, sc)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.OwnerUserID != recipient {
		t.Fatalf("owner = %s, want %s", after.OwnerUserID, recipient)
	}
}

// TestIntegrationPersonalAccountHasNoBalance is C3's "personal accounts are
// outside the state machine entirely": free is what they are, so there is no
// balance to credit rather than a balance that happens to be zero.
func TestIntegrationPersonalAccountHasNoBalance(t *testing.T) {
	db, _ := isolatedDB(t)
	owner := seedUser(t, db, "u-balance")
	acc := personalAccount(t, db, owner, "personal-balance")

	err := CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).Where("id = ?", acc.ID).Update("credits", 25).Error
	})
	if err == nil {
		t.Fatal("a personal account was credited")
	}
}
