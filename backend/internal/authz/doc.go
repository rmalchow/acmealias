// Package authz decides what a subject may do, and records what it did.
//
// The model is grants: a subject — a person through the roles they hold in one
// account, or an API key — carries a set of [object, action] grants, and the
// evaluator in evaluate.go answers one question against them. There is no deny
// rule and no ordering: authority is granted and nothing takes it away, which is
// what makes the answer explainable.
//
// It used to be smaller and wrong in an interesting way. Roles were four
// constants named by four environment variables, mapped from an identity
// provider's group claim, and a matrix in permissions.go said which of them
// could do what. That model could express "an administrator" and nothing else:
// no object, no account, no key narrower than its holder. Design C10 removed it.
// What survives of it is the Action vocabulary below and the four role NAMES,
// which are now rows seeded into each new team account by store.DefaultRoles.
package authz
