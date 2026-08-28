// Package authz decides what a subject may do, and records what it did.
//
// The model is grants: a subject — a person through the roles they hold in one
// account, or an API key — carries a set of [object, action] grants, and the
// evaluator in evaluate.go answers one question against them. There is no deny
// rule and no ordering: authority is granted and nothing takes it away, which is
// what makes the answer explainable.
//
// A KNOWN GAP, recorded here because this is where the grant model is
// described and because it is invisible everywhere else (api-key-kinds task
// 6.1). A grant's object_id is POLYMORPHIC: one column whose referent is decided
// by the object_type beside it, across nine tables. A SQL foreign key names
// exactly one table, so role_grants.object_id and api_key_grants.object_id
// declare none — and NOTHING DELETES A GRANT WHEN THE OBJECT IT NAMES IS
// DELETED. Every deleted project has been leaving grants behind; the
// project/load-balancer collapse migration found two of them and aborted.
//
// The consequence is bounded but real: an orphaned grant names an identifier
// nothing can match, so it confers nothing, and identifiers are not reused. It
// is clutter and a confusing role editor rather than an authority hole. What it
// would become, if identifiers were ever reused or if a grant's object type were
// ever widened, is worse.
//
// api-key-kinds K2 declined to fix it and declined to make it worse: a project
// key names its project with a REAL foreign key that cascades, precisely because
// it is not polymorphic and can therefore say what it means. Sweeping the
// existing orphans is a separate decision about a separate table, and burying it
// in a change about keys is how a deletion nobody reviewed gets shipped.
//
// It used to be smaller and wrong in an interesting way. Roles were four
// constants named by four environment variables, mapped from an identity
// provider's group claim, and a matrix in permissions.go said which of them
// could do what. That model could express "an administrator" and nothing else:
// no object, no account, no key narrower than its holder. Design C10 removed it.
// What survives of it is the Action vocabulary below and the four role NAMES,
// which are now rows seeded into each new team account by store.DefaultRoles.
package authz
