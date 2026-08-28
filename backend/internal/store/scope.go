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
// build one out of a path parameter: every producer below reads a row first and
// takes the account off what the database returned. Every store read takes one as
// a parameter, so a query written without it does not compile.
//
// It does NOT guarantee that a query which HAS a Scope actually used it. That is a
// property of each query's WHERE clause, and it is why Scope sets a per-connection
// account for the tenant_isolation policies to read — see Run. With
// the policy in place, a query that forgets its predicate returns nothing anyway.
type Scope struct {
	accountID model.ID
}

// Membership binds a person to one account. Its fields are unexported so that a
// caller outside this package cannot assemble one out of request data: the only
// source is a row in account_members, built here from data the database supplied.
//
// There used to be a second source — a membership derived per request from an
// identity provider's group claim, for the internal edition. Removing it (design
// C10) turns P18's rule from one with an exception into one without: a membership
// is a row we wrote, always.
type Membership struct {
	accountID model.ID
	userID    model.ID
	roleIDs   []model.ID
}

// AccountID reports the account this membership binds to.
func (m Membership) AccountID() model.ID { return m.accountID }

// UserID reports the person.
func (m Membership) UserID() model.ID { return m.userID }

// RoleIDs reports the roles held in this account.
func (m Membership) RoleIDs() []model.ID { return m.roleIDs }

// ScopeFor confines queries to the account a membership names. This is the
// producer used by the authenticated request path.
func ScopeFor(m Membership) Scope { return Scope{accountID: m.accountID} }

// ScopeForSlug confines queries to the account a slug names.
//
// It is the CLI's producer, and it exists because `adminctl` has no session and
// no membership: its authority is the database connection string. Before design
// C10 it took the deployment's single account, which is not a question with an
// answer any more — so the account is now named on the command line, and read
// back before it becomes a Scope. Naming it is the point: the break-glass path
// should have to say which tenant it is about to write into, rather than
// inheriting one.
func ScopeForSlug(ctx context.Context, db *gorm.DB, slug string) (Scope, error) {
	var acc model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.First(&acc, "slug = ?", slug).Error
	})
	if err != nil {
		return Scope{}, fmt.Errorf("store: scope for account %q: %w", slug, err)
	}
	return Scope{accountID: acc.ID}, nil
}

// ScopeForAccount confines queries to an account row the caller already read or
// wrote.
//
// It takes the ROW rather than an identifier, which is what keeps it honest: the
// only way to hold one is to have gone through the database for it, so this
// cannot be built from a path parameter any more than the others can. It exists
// for checkout-that-creates (design C8), which has the account in hand from the
// insert it just performed and cannot go back through ScopeForSelection — the
// selection has not happened yet, and the transaction has not committed.
func ScopeForAccount(acc *model.Account) Scope {
	if acc == nil {
		return Scope{}
	}
	return Scope{accountID: acc.ID}
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
	sc, _, err := AccountForSelection(ctx, db, accountID)
	return sc, err
}

// AccountForSelection is ScopeForSelection for the callers that need the account
// ROW as well as the confinement — every request that has to ask whether the
// account is usable (design C3), which since task 4.3 is every account-scoped
// one.
//
// It exists so that question costs ONE read. The obvious spelling —
// ScopeForSelection followed by GetAccount — reads the same row twice through
// two separate cross-account transactions, on the hot path of every management
// request; ScopeForSelection had already read it and thrown it away.
func AccountForSelection(ctx context.Context, db *gorm.DB, accountID model.ID) (Scope, *model.Account, error) {
	var acc model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.First(&acc, "id = ?", accountID).Error
	})
	if err != nil {
		return Scope{}, nil, fmt.Errorf("store: scope for selected account %s: %w", accountID, err)
	}
	return Scope{accountID: acc.ID}, &acc, nil
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
// a materialized account_id, which is every table a Scope applies to.
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
// Design P20 wants this to be a separate database role holding BYPASSRLS. The
// schema baseline records why it is a session setting instead, and what that
// gives up.
// Called INSIDE an already-open transaction, the lifting is undone again when fn
// returns, and that is not tidiness. `SET LOCAL` is scoped to the transaction
// and not to the savepoint: gorm turns a nested Transaction into a savepoint,
// and PostgreSQL does not undo a SET LOCAL at RELEASE SAVEPOINT. So without the
// restore, every statement after a nested CrossAccount runs with the policies
// still off for the rest of the OUTER transaction — which is what happened the
// first time this was nested under a mutation (transferAccount, checkout).
// Nothing leaked: both carry their own account_id predicates. But the
// database-level net that four shipped bugs paid for was silently down for the
// remainder of every transfer and every checkout, and the next statement written
// inside one would have had nothing under it.
//
// At the TOP level the two extra statements are skipped, and skipped rather than
// written for symmetry: the transaction ends when this returns, and a SET LOCAL
// cannot outlive it — that is the whole reason Scope.Run insists on a
// transaction in the first place. This is the case that matters for cost,
// because /present resolves its key and its alias through two of these on the
// certificate-issuance path, and a test pins the statement count there.
//
// The restore runs only on success. When fn fails, gorm rolls back to the
// savepoint — which DOES undo a SET LOCAL — and the transaction may be aborted,
// so issuing another statement would replace the caller's error with a
// "current transaction is aborted" from the driver.
func CrossAccount(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	committer, nested := db.Statement.ConnPool.(gorm.TxCommitter)
	nested = nested && committer != nil
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous string
		if nested {
			if err := tx.Raw("select coalesce(current_setting('app.cross_account', true), '')").Scan(&previous).Error; err != nil {
				return fmt.Errorf("store: read cross-account setting: %w", err)
			}
		}
		if err := tx.Exec("select set_config('app.cross_account', 'on', true)").Error; err != nil {
			return fmt.Errorf("store: enter cross-account read: %w", err)
		}
		if err := fn(tx); err != nil {
			return err
		}
		if nested {
			if err := tx.Exec("select set_config('app.cross_account', ?, true)", previous).Error; err != nil {
				return fmt.Errorf("store: leave cross-account read: %w", err)
			}
		}
		return nil
	})
}
