package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Scope confines a database read or write to exactly one account (design P1).
//
// It is the outer of the two mechanisms that keep tenants apart, and it is
// deliberately not a permission. No grant can name an object of another account,
// because a grant's identifiers are resolved within its anchor; isolation lives
// one layer down, here, so that a bug in the permission evaluator over-permits
// *inside* one tenant — bad — rather than disclosing *across* tenants, which is
// not recoverable.
//
// # What this type guarantees, and what it does not
//
// It has no exported constructor taking a bare identifier, so a handler cannot
// build one out of a path parameter: the only ways to obtain a Scope are from a
// Membership, which only this package mints, or from the single-account bootstrap.
// Every store read takes one as a parameter, so a query written without it does
// not compile.
//
// It does NOT guarantee that a query which HAS a Scope actually used it. That is a
// property of each query's WHERE clause, and it is why Scope sets a per-connection
// account for the row-level security policies of migration 000008 — see Run. With
// the policy in place, a query that forgets its predicate returns nothing anyway.
type Scope struct {
	accountID model.ID
}

// Membership binds a person to one account. Its fields are unexported so that a
// caller outside this package cannot assemble one out of request data: the only
// sources are a row in account_members, or the derived membership the
// identity-provider edition resolves from a group claim, both of which are built
// here from data the database supplied.
type Membership struct {
	accountID model.ID
	userID    model.ID
	roleIDs   []model.ID
}

// AccountID reports the account this membership binds to.
func (m Membership) AccountID() model.ID { return m.accountID }

// UserID reports the person. It is the nil identifier for a membership derived
// from a group claim, which names no stored user.
func (m Membership) UserID() model.ID { return m.userID }

// RoleIDs reports the roles held in this account.
func (m Membership) RoleIDs() []model.ID { return m.roleIDs }

// ScopeFor confines queries to the account a membership names. This is the
// producer used by the authenticated request path.
func ScopeFor(m Membership) Scope { return Scope{accountID: m.accountID} }

// ScopeForBootstrap confines queries to the single account of an internal-edition
// deployment. It reads the account rather than accepting one, so that a caller
// cannot pass an identifier of its own choosing; it fails when the deployment has
// more than one account, because then "the single account" is not a question with
// an answer.
func ScopeForBootstrap(ctx context.Context, db *gorm.DB) (Scope, error) {
	var accs []model.Account
	if err := db.WithContext(ctx).Order("created_at").Find(&accs).Error; err != nil {
		return Scope{}, fmt.Errorf("store: bootstrap scope: %w", err)
	}
	switch len(accs) {
	case 0:
		return Scope{}, errors.New("store: bootstrap scope: no account exists")
	case 1:
		return Scope{accountID: accs[0].ID}, nil
	default:
		return Scope{}, fmt.Errorf("store: bootstrap scope: %d accounts exist, expected one", len(accs))
	}
}

// ScopeForKey confines queries to the account an API key is anchored to.
//
// This is the third producer, and it exists because the challenge path
// authenticates a machine rather than a person: there is no session and no
// membership, and the key row itself is the evidence. The account comes off the
// key that the database returned — never off the request — which is the same
// property the other two producers have.
func ScopeForKey(k *model.APIKey) Scope {
	if k == nil {
		return Scope{}
	}
	return Scope{accountID: k.AccountID}
}

// ScopeForSelection confines queries to the account a session has selected.
//
// It reads the account rather than accepting the identifier as gospel, so a
// selection pointing at an account that has since been deleted fails here
// instead of producing a scope over nothing. The caller's right to act in it was
// established when the selection was made (design P19).
func ScopeForSelection(ctx context.Context, db *gorm.DB, accountID model.ID) (Scope, error) {
	var acc model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.First(&acc, "id = ?", accountID).Error
	})
	if err != nil {
		return Scope{}, fmt.Errorf("store: scope for selected account %s: %w", accountID, err)
	}
	return Scope{accountID: acc.ID}, nil
}

// AccountID reports the account this scope confines to.
func (s Scope) AccountID() model.ID { return s.accountID }

// Valid reports whether the scope names an account. The zero Scope does not, and
// every store function refuses it rather than silently reading everything.
func (s Scope) Valid() bool { return s.accountID != model.NilID }

// ErrNoScope is returned when a store call is handed the zero Scope. It exists so
// the failure is loud: the alternative — treating an empty scope as "no filter" —
// is the exact bug this type is designed to prevent.
var ErrNoScope = errors.New("store: query has no scope")

// Where narrows a query to the scope's account. Callers use it on tables carrying
// a materialized account_id, which since migration 000006 is every table a Scope
// applies to.
func (s Scope) Where(db *gorm.DB) *gorm.DB {
	return db.Where("account_id = ?", s.accountID)
}

// Run executes fn inside a transaction whose app.account_id is set to this
// scope's account, which is what the row-level security policies read (design
// P20).
//
// The transaction is not optional and not an optimisation. SET LOCAL is discarded
// at commit, so the setting cannot survive into the next borrower of a pooled
// connection; a plain SET outside a transaction would leave one account's tenant
// setting on a connection that then serves another account's request, which is the
// same class of bug the policies exist to prevent, arriving by a new route.
//
// PgBouncer in transaction mode is therefore supported and statement mode is not:
// statement mode may route the SET and the query to different connections.
func (s Scope) Run(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	if !s.Valid() {
		return ErrNoScope
	}
	// Inside an already-open transaction this joins it rather than nesting — but
	// it still issues the SET LOCAL, and that is the whole point.
	//
	// An earlier version skipped the setting when it found a transaction,
	// reasoning that whoever opened it had already set the tenant. That is false
	// for every caller that opens a transaction to group several writes and
	// their audit entry: those transactions are opened from a plain handle, so
	// the tenant was never set, and every write inside them was refused by the
	// policy. It did not show up in tests because the test connection was a
	// superuser, which bypasses policies entirely; the dev stack found it within
	// a minute of connecting as an ordinary role.
	//
	// Setting it again for the same account is harmless. Setting it for a
	// DIFFERENT account inside someone else's transaction would be a bug, and it
	// is one this makes visible — the statements after it move to the new tenant
	// — rather than one that silently writes nothing.
	setTenant := func(tx *gorm.DB) error {
		if err := tx.Exec("select set_config('app.account_id', ?, true)", s.accountID.String()).Error; err != nil {
			return fmt.Errorf("store: set scope: %w", err)
		}
		return nil
	}
	if committer, ok := db.Statement.ConnPool.(gorm.TxCommitter); ok && committer != nil {
		tx := db.WithContext(ctx)
		if err := setTenant(tx); err != nil {
			return err
		}
		return fn(tx)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := setTenant(tx); err != nil {
			return err
		}
		return fn(tx)
	})
}

// CrossAccount runs fn with the row-level security policies lifted, for the
// components that must read across accounts by definition: the zone view the
// nameserver answers from, the expiry sweep, the daily inventory snapshot, and
// the process-level gauges.
//
// It is deliberately verbose to call and deliberately easy to grep for. The set
// of cross-tenant readers should be short enough to enumerate in a review, and
// naming it at each call site is what keeps it that way — the failure this
// guards against is not a hostile caller but an ordinary one that quietly grew a
// second meaning.
//
// Design P20 wants this to be a separate database role holding BYPASSRLS.
// Migration 000007 records why it is a session setting instead, and what that
// gives up.
func CrossAccount(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("select set_config('app.cross_account', 'on', true)").Error; err != nil {
			return fmt.Errorf("store: enter cross-account read: %w", err)
		}
		return fn(tx)
	})
}
