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
// Every object belongs to an account, and the CLI operates inside exactly one:
// the single account of the deployment, resolved at startup by
// store.ScopeForBootstrap. It cannot be pointed at another, and a deployment
// holding more than one makes the lookup fail rather than pick — "the single
// account" is not a question with an answer there.
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
