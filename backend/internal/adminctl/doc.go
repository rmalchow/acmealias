// Package adminctl implements the acmeservice command-line administration
// verbs (design D-cli): projects, load balancers, nodes, domains and API keys,
// operated directly against the datastore with no server process running.
// It exists for bootstrap and emergencies — before anyone has console
// access, and when the console is down.
//
// Every mutation goes through internal/inventory, the same service layer the
// HTTP admin API uses, so validation cannot diverge between the console and
// the CLI. Each function here does the inventory call, prints an
// operator-facing result to the given io.Writer, and appends one audit entry
// attributing the change to a CLI actor (the OS user and host).
//
// # Accounts
//
// Every object belongs to an account, and the CLI operates inside exactly one,
// named by --account SLUG (or ACME_ADMINCTL_ACCOUNT) and resolved by
// store.ScopeForSlug.
//
// It used to be resolved without being asked for: the deployment had a single
// account and store.ScopeForBootstrap found it. Design C10 removed that edition,
// so there is nothing to fall back on, and every verb but one refuses rather
// than guessing. The exception is AccountCreate, which is what makes the first
// account on a fresh deployment — a job that had no owner once the migration
// stopped seeding one.
//
// AccountCreate makes a TEAM account, and it funds it in the same breath,
// because a team account works only while its balance is above zero (design C3)
// and one created empty would be suspended from the moment it existed. In the
// console the same act is a purchase; --credits is what stands in for that on a
// deployment where nobody can reach the console yet (design C8).
//
// AccountCredit, AccountTransfer and AccountMemberAdd are the repair verbs. They
// exist for the state an UPGRADE leaves behind rather than for day-to-day
// administration: migration 000013 turns the internal edition's shared account
// into a team account whose owner is a stand-in nobody can authenticate as and
// whose roster is empty, because membership used to be derived from a group
// claim rather than written down (design C10). The console cannot mend that by
// construction — every route into an account requires already being in it — so
// the repair belongs with the rest of the break-glass path. They are also what
// restores a suspended account when nobody can reach checkout.
//
// There is no verb that makes a PERSONAL account, and there never will be: a
// user has one because they are a user, materialized at their first sign-in
// (design C1). Nothing here can create one, and neither the transfer nor the
// credit will touch one — store.TransferOwnership and store.Credit both refuse
// it, and migration 000013's trigger is the floor under the first.
//
// That confinement is real but it is not a security boundary against the person
// running the CLI. The authority behind a CLI change is "had the connection
// string", so the actor it records carries no roles (see actor.go) and its
// subject is unrestricted within that account (authz.CLISubject). A narrower
// subject would obstruct the honest path while leaving `psql` open, and the
// audit trail would claim a constraint that did not exist.
//
// # Roles are data
//
// Roles used to be four constants in Go. They are rows in an account now, with
// their grants alongside, and a customer may create their own — so nothing here
// enumerates them or decides what one may do. Permission is a [type, id,
// action] grant evaluated against the object being acted on, which is why every
// service-layer call takes a subject rather than a role name.
//
// # Keys hold grants
//
// An API key used to belong to one load balancer, and that foreign key WAS its
// authorization: the whole check was one comparison. A key is a subject now,
// anchored to the account and holding grants, which is what lets one key cover a
// whole project — the request that started the change. KeyCreate takes
// --loadbalancer for the common case and --grant for anything else; see
// GrantSpec for the form.
package adminctl
