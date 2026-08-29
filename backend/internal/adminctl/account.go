package adminctl

import (
	"context"
	"fmt"
	"io"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// AccountCreate makes a team account and its first unrestricted member.
//
// It exists because design C10 removed the account the migration used to seed:
// a fresh deployment now has none at all, so something has to make the first
// one, and until a team account can be bought (task group 5) that something is
// the CLI. This is precisely the bootstrap case the package exists for — before
// anyone has console access.
//
// The owner is named by their identity-provider SUBJECT, not by an email or a
// display name, because that is the only identifier a sign-in will match. A
// subject that has never signed in is created here, which is deliberate: the
// account has to exist before its owner can reach it, and the alternative —
// "sign in first, then ask an operator" — is a chicken-and-egg for the first
// person on a new deployment.
//
// It is the ONE verb that does not take --account, for the obvious reason.
//
// credits is the opening balance. It is a parameter and not a zero because a
// team account is created by funding it (design C8): an account with no credit
// is suspended, so a CLI that made one would be handing an operator an account
// that does not work and no explanation of why.
//
// UNLESS NOTHING IS CHARGED. `monetized` is config.Config.Monetized, and where
// it is off the sentence above stops being true: an account at zero is not
// suspended, because store.Usable stops consulting the balance. --credits is
// then accepted at any value including the default, and simply recorded.
func AccountCreate(ctx context.Context, db *gorm.DB, out io.Writer, slug, name, ownerSubject string, credits int64, monetized bool) error {
	switch {
	case slug == "":
		return fmt.Errorf("--slug is required")
	case name == "":
		return fmt.Errorf("--name is required")
	case ownerSubject == "":
		return fmt.Errorf("--owner is required: the identity-provider subject of the person this account belongs to")
	case monetized && !store.IsOfferedAmount(credits):
		// The same fixed set the console's checkout takes, and for the same
		// reason (design C8): --credits stands in for a purchase, and an
		// amount this service does not sell would write an account.checkout
		// entry indistinguishable in shape from a real one for money that
		// could never have been paid. It also refuses zero and negatives,
		// which would leave the account suspended from the moment it exists.
		return fmt.Errorf("--credits must be one of %v EUR: %w", store.CheckoutAmounts, store.ErrAmountNotOffered)
	}
	user, err := store.FindOrCreateUser(ctx, db, ownerSubject, ownerSubject, "")
	if err != nil {
		return err
	}
	acc, err := store.CreateAccountWithOwner(ctx, db, slug, name, user.ID, credits, monetized)
	if err != nil {
		return err
	}
	// Audited from inside the account that was just created, which is the only
	// place the entry can live: audit rows carry an account, and this one has no
	// parent to be attributed to.
	//
	// The entry is written AFTER the account rather than inside the same
	// transaction, and the difference matters here. store.CreateAccountWithOwner
	// is indivisible on its own — account, roles and first membership commit
	// together or not at all — and the scope this needs cannot exist until it
	// has committed. An unrecorded creation is visible in the account list; a
	// half-created account would not be repairable by any route.
	sc, err := store.ScopeForSelection(ctx, db, acc.ID)
	if err != nil {
		return err
	}
	if err := mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		id := acc.ID
		return []authz.Event{{
			Action: authz.ActionAccountCreate, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: acc.Slug,
			After: map[string]any{"slug": acc.Slug, "name": acc.Name, "owner": ownerSubject},
		}, {
			// The opening credit as its own entry, in the same shape checkout
			// writes: the audit trail should not be able to tell where an
			// account's first credit came from by the shape of the row (C8).
			Action: authz.ActionAccountCheckout, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: acc.Slug,
			After: map[string]any{
				"amount": credits, "currency": "EUR",
				"balanceBefore": int64(0), "balanceAfter": acc.Credits,
			},
		}}, nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "created account %q (%s), owned by %q, with %d EUR of credit\n", acc.Slug, acc.ID, ownerSubject, acc.Credits)
	fmt.Fprintf(out, "seeded roles: ADMIN (unrestricted), CONFIG_ADMIN, KEY_ROTATION, READ_ONLY\n")
	fmt.Fprintf(out, "use it with: acmeservice adminctl --account %s <noun> <verb>\n", acc.Slug)
	return nil
}

// The three repair verbs below exist because of what an UPGRADE leaves behind,
// and they are the reason this file is not just AccountCreate.
//
// The internal edition's shared account survives an upgrade as a team account.
// Its members were derived from an identity-provider group claim rather than
// written down (P18's second branch, which design C10 removes), so
// `account_members` is empty however many people were administering it the day
// before; and it has no owner to find, so the upgrade left it with a stand-in
// user whose subject no identity provider issues. The account therefore holds the
// deployment's entire inventory and nobody at all can reach it: not by selecting
// it (mayAct reads account_members), not through the owner exception (the owner
// cannot sign in), and not through checkout (same).
//
// Nothing in the product could repair that. The console cannot, by construction —
// every route into the account requires being in it — so the repair belongs
// exactly where the rest of the break-glass path already is: a CLI whose
// authority is having the connection string. Without these verbs, restoring an
// upgraded deployment means hand-written SQL against a schema with a trigger and
// a partial unique index in it.

// AccountCredit adds credit to a team account (design C8).
//
// The amount is checked against the same offered set the console's checkout is,
// for the reason C8 gives: with no processor in the way, the only thing between
// the balance and an arbitrary number is that no arbitrary number is accepted.
// A staff tool is not an exception to that — it is the caller most likely to be
// scripted, and a `--credits 1000000` typo here is silent.
//
// The audit entry is the SAME row a console checkout writes, deliberately: the
// trail should not be able to tell where an account's credit came from by the
// shape of the entry. The actor distinguishes them, which is what the actor is
// for.
func AccountCredit(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, credits int64) error {
	if !store.IsOfferedAmount(credits) {
		return fmt.Errorf("--credits must be one of %v EUR: %w", store.CheckoutAmounts, store.ErrAmountNotOffered)
	}
	acc, err := store.GetAccount(ctx, db, sc)
	if err != nil {
		return err
	}
	before := acc.Credits
	var after *model.Account
	if err := mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		credited, err := store.Credit(ctx, tx, sc.AccountID(), credits)
		if err != nil {
			return nil, err
		}
		after = credited
		id := credited.ID
		return []authz.Event{{
			Action: authz.ActionAccountCheckout, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: credited.Slug,
			Before: map[string]any{"credits": before},
			After: map[string]any{
				"amount": credits, "currency": "EUR",
				"balanceBefore": before, "balanceAfter": credited.Credits,
			},
		}}, nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "credited %q with %d EUR: %d -> %d\n", after.Slug, credits, before, after.Credits)
	return nil
}

// AccountTransfer hands a team account to a real person, and makes them a member
// of it in the same transaction.
//
// The membership is not a convenience. An owner who is not a member is an owner
// who cannot open their own account — every route into it reads
// `account_members` — and after an upgrade that roster is empty, so the
// recipient would otherwise be an owner of something they still could not reach.
// store.TransferOwnership says in its own doc comment that establishing it is
// the caller's job, precisely so it lands in the caller's transaction.
//
// A personal account is refused, by store.TransferOwnership and, under it, by
// the schema's accounts_kind_is_immutable trigger (design C1).
func AccountTransfer(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, ownerSubject string) error {
	if ownerSubject == "" {
		return fmt.Errorf("--owner is required: the identity-provider subject of the person this account is to belong to")
	}
	acc, err := store.GetAccount(ctx, db, sc)
	if err != nil {
		return err
	}
	recipient, err := store.FindOrCreateUser(ctx, db, ownerSubject, ownerSubject, "")
	if err != nil {
		return err
	}
	if err := mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.TransferOwnership(ctx, tx, sc, recipient.ID); err != nil {
			return nil, err
		}
		// Unrestricted membership specifically — see the same call in
		// httpapi.transferAccount. "Is a member" is not the question; "can
		// administer what they now own" is.
		if err := store.EnsureUnrestrictedMember(ctx, tx, sc, recipient.ID); err != nil {
			return nil, err
		}
		id := acc.ID
		return []authz.Event{{
			Action: authz.ActionAccountTransfer, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: acc.Slug,
			Before: map[string]any{"ownerUserId": acc.OwnerUserID},
			After:  map[string]any{"ownerUserId": recipient.ID, "ownerSubject": ownerSubject},
		}}, nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "account %q now belongs to %q, who is an unrestricted member of it\n", acc.Slug, ownerSubject)
	return nil
}

// AccountMemberAdd puts a person into an account, in a named role.
//
// The role is named rather than identified, because roles are rows in this
// account now (design C10) and an operator repairing a deployment at three in
// the morning has the name in front of them and not the identifier. An unknown
// name lists what there is instead of failing blankly: after an upgrade the
// account holds the four seeded defaults, and after a customer has edited them
// it holds whatever they decided.
func AccountMemberAdd(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, subject, roleName string) error {
	switch {
	case subject == "":
		return fmt.Errorf("--subject is required: the identity-provider subject of the person to add")
	case roleName == "":
		return fmt.Errorf("--role is required: the name of a role in this account")
	}
	roles, err := store.RolesFor(ctx, db, sc)
	if err != nil {
		return err
	}
	var role *model.Role
	names := make([]string, 0, len(roles))
	for i := range roles {
		names = append(names, roles[i].Name)
		if roles[i].Name == roleName {
			role = &roles[i]
		}
	}
	if role == nil {
		return fmt.Errorf("no role named %q in this account; it has %v", roleName, names)
	}
	user, err := store.FindOrCreateUser(ctx, db, subject, subject, "")
	if err != nil {
		return err
	}
	if err := mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.AddMember(ctx, tx, sc, user.ID, role.ID); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionMemberAdd, ObjectType: authz.ObjectMember,
			ObjectID: &user.ID, ObjectLabel: subject,
			After: map[string]any{"subject": subject, "roleId": role.ID, "roleName": role.Name},
		}}, nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "added %q to this account as %q\n", subject, role.Name)
	return nil
}
