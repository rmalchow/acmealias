## Context

The service authorizes management requests with a global role check. `internal/authz` derives a
`RoleSet` from the session's group claim on every request, and `Can(roles, action)` consults a
`map[Action]RoleSet` literal holding seven rows. No object is passed, because none of the four roles
is scoped to anything: the package doc says so, and so does the `identity-and-roles` spec.

API keys are authorized differently and even more simply. `api_keys.load_balancer_id` is the scope,
there is no permission column, and the whole check is one comparison in `challenge.go`. It works
because a key can do exactly one thing.

The proposal explains why neither survives per-project keys, user-created projects, or real DNS
record management. This document is about how to replace them without breaking a running internal
deployment, and without making the tenancy boundary depend on the new code being correct.

Terms used throughout, defined once:

- **Account** — the owner of inventory and the anchor of every grant. Exactly one, seeded at
  migration, in the internal edition.
- **Subject** — something that holds grants and can be authorized: a person (through a membership in
  a role) or an API key.
- **Grant** — one `[type, id, action]` triple plus an `inheritable` flag, belonging to a role or a key.
- **Anchor** — the account a subject's grants are confined to. A wildcard means "within the anchor",
  never "everywhere".
- **Scope** — the query-layer value confining a database read or write to one account. Independent
  of grants, and the thing that actually keeps tenants apart.

Constraints that shaped everything below:

1. **The internal edition must not observably change.** Same screens, same answers, same roles by
   name, no account visible anywhere.
2. **The challenge path must not get slower.** `POST /present` is on a certificate issuance critical
   path and already does an alias lookup and a zone-view refresh. Authorization must add no query.
3. **A forgotten scope must not compile.** The failure mode being designed against is a new query
   written without a tenant filter, and conventions do not survive that.

## Goals / Non-Goals

**Goals:**

- Authorization that takes an object, expressive enough for per-project API keys and per-name DNS
  record grants, with roles the customer defines rather than four the code defines.
- Tenant isolation that does not depend on the permission evaluator being correct — two independent
  mechanisms, the outer one structural.
- A migration that is reversible and that runs while the only data belongs to us.
- No regression on the DNS or challenge answering path.

**Non-Goals:**

- The staff/administrative plane. Acting inside an account without being a member of it is a
  separate authorization system in a separate change.
- Storing zones or records. The `dns_record` selector is designed here; nothing writes one yet.
- Local accounts, signup, billing, editions, marketing. Those stay in
  `add-accounts-and-public-edition`.
- Postgres row-level security. Still wanted as defence in depth; still deferred.

## Decisions

### P1. Two mechanisms, not one: `Scope` separates tenants, grants separate powers

The single most important structural decision, and it is about what *not* to put in the permission
model.

Tenant isolation is **not** expressed as a permission. There is no grant that can name an object of
another account, because a grant's identifiers are resolved within its anchor. Isolation is enforced
one layer down, in the query layer: every inventory query takes a `Scope`, and a `Scope` confines it
to one account.

That means a bug in the permission evaluator — a wildcard that matches too much, an inheritance walk
that goes one level too far, a subset check that lets a grant through — produces *over-permission
inside one tenant*, which is bad, and not *cross-tenant disclosure*, which is unrecoverable. The two
mechanisms fail independently.

`Scope` keeps E3's shape from the in-flight change: no exported constructor from a bare integer, so
a handler cannot build one out of a path parameter. It is produced in exactly two places in this
change — the authenticated request path, from the caller's membership, and the internal edition's
bootstrap, from the single account. (A third producer arrives with the staff plane, deliberately as
the *only* bridge between the two authorization systems.)

**Rejected: expressing tenancy as a grant** (`[account, 42, *]`). It unifies the model into one
concept, which is aesthetically appealing. It was rejected because it makes cross-tenant access a
*writable value* — one row, one bug, one mistyped identifier away — rather than something with no
representation at all. The current asymmetry is the point: over-permission is a mistake, cross-tenant
is a breach, and they should not be reachable through the same mechanism.

### P2. Ownership is inherited — but `account_id` is materialized and made unforgeable by a composite foreign key

E2 of the in-flight change places the account above `projects`, has everything below inherit
ownership through its parent, and explicitly rejects putting `account_id` on every table, because a
row could then disagree with its parent and "the only way to keep them in step is a trigger or
discipline."

That rejection is correct about the risk and wrong about the remedy, and this change revises it —
because two things in this change need `account_id` to be a real column:

- **Per-account domain uniqueness.** `domains_name_key unique (name)` must become unique per account
  (see P8). Postgres cannot build a unique index across a join, so the account must be on the row.
- **Scope cost.** With ownership reachable only by join, `Scope.LoadBalancers` is
  `JOIN projects ON … WHERE projects.account_id = ?` on every query, and every index that should
  lead with the tenant cannot.

The remedy is neither a trigger nor discipline: **a composite foreign key makes disagreement
unrepresentable.**

```sql
-- the parent exposes (id, account_id) as a unique target
ALTER TABLE projects       ADD UNIQUE (id, account_id);
ALTER TABLE load_balancers ADD COLUMN account_id uuid NOT NULL;
ALTER TABLE load_balancers ADD FOREIGN KEY (project_id, account_id)
                               REFERENCES projects (id, account_id);

ALTER TABLE load_balancers ADD UNIQUE (id, account_id);
ALTER TABLE domains        ADD COLUMN account_id uuid NOT NULL;
ALTER TABLE domains        ADD FOREIGN KEY (load_balancer_id, account_id)
                               REFERENCES load_balancers (id, account_id);
```

A domain whose `account_id` differs from its load balancer's now fails to insert. The database
enforces the invariant that E2 feared needing discipline for, and it does so with a mechanism that
cannot be forgotten by a future query author. The column is a denormalization whose correctness is
structural rather than maintained.

Applied to `load_balancers`, `domains`, `aliases`, `api_keys` and `lb_nodes` — every table a `Scope`
must filter. `challenges` and `challenge_interactions` reach the account through the alias and load
balancer they already reference and are not scoped directly.

### P3. Every ancestor a grant can name is materialized on the row

Inheritance needs an object's ancestors: a node's load balancer, its project, its account. P2
materializes the account because tenancy needs it in indexes. This decision extends that to
**`project_id`** on `lb_nodes`, `domains`, `aliases` and `api_keys`, by the same composite-foreign-key
construction, so that a row cannot disagree with its parent about either ancestor.

The rule is: *a table carries a column for every ancestor type a grant is allowed to name.* Grants may
be anchored at an account, a project, a load balancer, or the object itself, so a leaf table carries
`account_id`, `project_id` and `load_balancer_id`, and nothing deeper is needed because nothing deeper
exists.

The reason is P13. Checking **one** loaded object needs no denormalization — the parent identifiers are
on the row and the chain is at most four long. Building a **predicate** for a listing is a different
problem: without `project_id`, a project-scoped grant reaching `lb_nodes` becomes

```sql
load_balancer_id IN (SELECT id FROM load_balancers WHERE project_id = 12)
```

— a correlated subquery per grant, composed with `OR` for every other grant the subject holds. With the
column, the same grant is `project_id = 12`, and every grant in the model reduces to a single-column
equality:

| Grant | Predicate |
| --- | --- |
| `[load_balancer, 7, view]` | `id = 7` |
| `[load_balancer, *, view]` | `account_id = :anchor` |
| `[project, 12, view]` inheritable | `project_id = 12` |
| `[account, 3, view]` inheritable | `account_id = 3` |

That uniformity is what makes the query builder in P13 a fold rather than a compiler, and it is the
same for every table.

**Rejected: walking the chain from loaded rows and filtering in memory.** This was the earlier decision
here, and it is wrong for listings: it requires reading rows the caller may not see in order to discard
them, which is both wasteful and the precise mechanism by which a paginated total leaks the existence
of invisible objects.

**Rejected: materializing deeper chains speculatively.** Only ancestor types a grant may name earn a
column. Adding one for an object type that cannot anchor a grant buys nothing and costs a composite key
on every insert.

### P13. Reads are ACL-gated in the query, and one function serves every read of an existing object

Permission enforcement for existing objects is not a check performed *after* fetching. The subject's
grants are compiled into a predicate that is part of the query, so a row the caller may not see is
never returned by the database.

The predicate is a disjunction: each grant matching the required action and object type contributes one
term (P3), and the terms are `OR`-ed. **This is tractable only because permissions are allow-only
(P11).** Deny rules would require correlated negation per grant, and the builder would stop being a
fold over a list.

The consequence that removes the most code: **a single-row read is a listing with one more predicate.**

```go
// List builds the ACL predicate and applies the caller's filters and paging.
func List[T any](ctx, subj Subject, perm Action, q Query) ([]T, int, error)

// GetWithPermission is List with `id = $1`, returning the row or nil.
func GetWithPermission[T any](ctx, subj Subject, perm Action, id uuid.UUID) (*T, error)
```

So every read, update and delete of an existing object authorizes through one function. There is no
second code path to keep in step, and the non-disclosure property the `accounts-and-tenancy` spec
requires — *"refused as though the object did not exist, and SHALL NOT reveal that it exists"* — is
structural rather than a discipline: `nil` is returned for an object that does not exist and for one the
caller may not see, because the query cannot tell them apart either.

**The count is gated by the same predicate.** A paginated listing reports a total; if that total counts
rows the caller cannot see, the existence of invisible objects leaks through a number, which undoes
the property the `nil` return was protecting. The count and the page run the same predicate.

**`dns_record` is the one type that does not reduce to equality.** Its selector matches by name pattern,
so its predicate is a suffix match (`name LIKE '%.staging.example.com'`, with the apex excluded per P9)
and a record-type set. A leading-wildcard `LIKE` cannot use an ordinary btree index, so record names are
stored reversed with a `text_pattern_ops` index and the predicate is built against the reversed column.
This special case is isolated in the record predicate builder and does not shape the general one.

### P14. The account is not an ACL object: operations *on* it are governed by membership, not grants

`GetWithPermission` covers every operation on an object that exists, and creation is authorized by a
`create` action on the **parent** — `domain.create` on the load balancer, `load_balancer.create` on the
project — fetched by that same restricted read, so creating under a parent the caller cannot see fails
identically to creating under one that does not exist.

That uniform rule stops at the account. **The account is the anchor of the grant model, not an object
inside it**, and the operations that act on the account itself are governed by membership:

| Operation | Governed by |
| --- | --- |
| Create an account | any authenticated user |
| Create or delete a project in it | the account's unrestricted role |
| Invite a user, or change a membership | the account's unrestricted role |
| Read the member roster | **any member of the account** |
| Everything inside a project | grants, uniformly |

The service SHALL NOT accept a grant naming an account as its object, and there is no account-typed
predicate in the query builder of P13.

The reason for holding this line is that the alternative dissolves it. Once `[account, …, project.create]`
is expressible, the account is an ACL-addressable object — and then reading its credit balance, changing
its plan, renaming it and managing its members all start to look like grants too, at which point the
boundary between the tenant plane and account administration is gone and the staff plane's reason for
existing is much harder to state.

**The roster is deliberately readable by every member**, not by grant. A person who shares a billing
entity with you is entitled to know who else does; hiding colleagues from each other inside one account
protects nothing and makes "who can see my project" unanswerable from inside the product. It is confined
by `Scope` like every other read — members of other accounts see nothing — but within an account it is
not gated further. Pending invitations appear in the same roster, because an invited address is about to
become a member and concealing that would defeat the purpose.

Two things follow that are worth stating so they are not read as inconsistencies:

- **A public-edition customer can still create projects**, because creating an account makes them its
  unrestricted member. "Only the admin may create projects" and "a user can create projects" are the same
  sentence for an account of one.
- **Creating an account** is now fully specified rather than deferred: it is a user-level action, outside
  the tenant model, available to any authenticated user (P18).

### P18. A user is a global entity; membership is what binds them to an account

A **user** is a person, and exists outside every account. They own nothing but their own identity, may
belong to several accounts or to none, and are not deleted when an account is. An **account** is the
billing entity that holds credits, owns projects and anchors grants. A **membership** is the row joining
one user to one account with a role.

The naming is a hazard and is fixed here: "user account" is never used for either. A person is a *user*;
the tenant is an *account*.

```
users                        ← global, no owner
  └── memberships ──┐
                    ├──> accounts        ← credits, projects, grant anchor
                    │      └── projects
                    │            └── load_balancers …
   invitations ─────┘
```

**A user with no accounts is a normal state**, not an error: it is where signup lands, and where a person
sits after their last membership is removed. They may sign in, create an account, and accept invitations.
Nothing else. The console needs a real empty state for it rather than a broken shell.

**Memberships have two sources, and the evaluator does not distinguish them.** In an identity-provider
edition they are *derived* per request from the group claim, exactly as today, and stored nowhere. In a
local-authentication edition they are rows in `account_members`. The evaluator asks for "the memberships
for this request" and receives either.

Materializing the derived ones into rows at sign-in would be simpler, and is rejected: the existing
`identity-and-roles` spec makes a group-to-role configuration change take effect when the service is
restarted, and sessions survive a restart because they are rows. Writing memberships at sign-in would
make that change invisible until every affected person signed in again — an observable difference in the
internal edition, which constraint 1 forbids.

**Creating an account is available to any authenticated user**, and the creator becomes its unrestricted
member in the same transaction. That is what satisfies P6's last-member invariant at the moment of
creation, rather than needing an exception for a freshly created account with nobody in it.

### P15. Two API surfaces: a versioned public one, and an unversioned internal one

The management API is split by audience, not by transport:

| Surface | Path | Versioned | Documented | Callers |
| --- | --- | --- | --- | --- |
| Public | `/api/v1/**` | yes | yes — an OpenAPI document served publicly | the console **and** customer programs alike |
| Internal | `/api/admin/**` | no | no | the staff plane only |

The public API is one contract with two kinds of caller. The console is not privileged: it is simply
the first client, and its use of the API is what demonstrates the API is usable. Paging, filtering and
counting are part of that contract — the `service-inventory` requirement that collection reads are
"filtered, paged and counted by the service" becomes a published guarantee rather than an
implementation note, and P13's ACL-gated count is what makes the total safe to publish.

The internal surface carries staff operations, which have no external callers and no stability
obligation, so versioning it would be ceremony. It is not merely undocumented — it is a separate route
tree with a separate authorization plane behind it, which is what keeps a staff-only operation from
being one forgotten annotation away from appearing in a public schema.

**Rejected: a private console API alongside a public one.** It would let the screens evolve without a
deprecation cycle. Rejected because it produces two implementations of the same reads, and the second
one is where a permission check eventually fails to get copied. One contract, and screen changes are
contract changes — which is a useful constraint on churn rather than a tax.

**Rejected: versioning the internal surface.** Nobody outside operations calls it, and a version number
would imply a compatibility promise that would then be half-kept.

### P16. One listener by path, and a second for mutually authenticated satellites

Everything except satellites is served on one listener and separated by path prefix, with per-group
middleware: `/api/v1/**` (session or key), `/api/admin/**` (staff plane), `/acme/httpreq/**` (API key),
`/healthz`, `/readyz`, `/metrics`.

This replaces the three-listener split of design D9, whose stated purpose was that "a stolen API key
reaches no management route even if a routing rule in front of us is wrong". That guarantee is now
carried by the permission model instead of by network topology: a key reaches only what its grants
permit. The trade is explicit — network-level exposure control moves to the reverse proxy — and the
tests asserting that no listener mounts another's routes become tests asserting each path group carries
its intended middleware.

The third-party challenge protocol does **not** force a path collision. `HTTPREQ_ENDPOINT` is a base URL
and the client appends the verbs itself (`docs/load-balancer-setup.md`: *"Trailing slash included.
Traefik appends `present` and `cleanup` itself."*), so the protocol can be namespaced under
`/acme/httpreq/` freely. Existing deployments have the old endpoint configured, so the root `/present`
and `/cleanup` paths keep serving alongside the namespaced ones. The only path class in this domain that
is genuinely fixed by specification is `/.well-known/*` (RFC 8615), which DNS-01 does not use.

**Satellites get their own listener**, because mutual TLS is the exception that paths cannot express: the
TLS handshake completes before any path is known, so client-certificate requirements are a property of a
listener, not of a route. Serving them separately keeps `ClientAuth` a plain listener setting, makes the
satellite surface's IP restrictions a firewall rule rather than middleware, and avoids terminating client
certificates at a proxy and forwarding them as headers — which would reintroduce exactly the trusted-peer
problem `internal/apikey/ipextractor.go` already solves for client addresses, with its fail-closed
startup check.

### P17. Every identifier is a UUIDv7

All primary keys and all foreign keys become `uuid`. No table keeps a sequence-backed integer
identifier. Specifically **UUIDv7** (RFC 9562), not v4.

The version matters and is not a detail. A v4 primary key is uniformly random, so every insert lands
at a random point in the B-tree: the index does not stay in cache, pages split constantly, and write
throughput degrades as the table grows. A v7 is a millisecond timestamp followed by randomness, so
inserts are append-mostly — the locality of a `bigserial` — while remaining globally unique. Postgres
18 provides `uuidv7()`; on earlier versions it is generated in Go (`google/uuid`) so the schema does
not depend on the server version.

Three consequences that are genuinely useful here, rather than merely tolerable:

**Identifiers stop being enumerable, which strengthens P13.** The whole non-disclosure argument —
absence and denial must be indistinguishable — is much stronger when an identifier cannot be guessed
in the first place. With sequential integers, `GET /api/v1/loadbalancers/41` is an invitation to walk
the range and read the response codes; the timing and status may be identical, but the *attempt* is
free. It also stops the identifier space from leaking business facts: how many customers exist, and
how fast they are arriving, are both readable off a serial primary key.

**Type confusion in the polymorphic grant table becomes impossible.** `role_grants.object_id` names an
object of whatever `object_type` says, with no foreign key to enforce the pairing (P4). With integers,
project `7` and load balancer `7` both exist, so a bug that compares the identifier without also
comparing the type produces a *plausible* match against the wrong object. With UUIDs the identifier
space is disjoint across tables, so the same bug produces no match at all. The failure mode changes
from silent mis-authorization to nothing happening.

**Identifiers can be generated before insertion.** The migration in P2 backfills materialized ancestor
columns, and the seeding in P5 writes roles and grants that reference each other; both are simpler when
the writer knows the identifier without a round trip.

The costs, stated plainly: 16 bytes rather than 8, on every key and on every one of the materialized
ancestor columns P2 and P3 add, which makes the composite foreign keys wider. That is real and it is
accepted — the tables in question hold inventory, not events, and their row counts are bounded by how
many domains customers have rather than by traffic. The event-shaped tables that could grow without
bound (`audit_entries`, `challenge_interactions`) are ordered by their timestamp columns and their
existing indexes are unaffected.

**Rejected: UUIDv4.** Non-enumerable, which was the goal, but with the index-locality behaviour above.
There is no reason to accept it once v7 exists.

**Rejected: keeping integer primary keys and exposing a separate public identifier.** Two identifier
spaces per object, a lookup to translate between them on every request, and a permanent question of
which one appears in a log line, an audit row or a grant. The benefit — narrower keys — is not worth a
second identity per object.

### P19. A session selects one account, and names it on every request anyway

A session is in one of exactly two states:

| State | Reachable |
| --- | --- |
| **No account selected** | the user's own profile, listing the accounts they belong to, creating an account, accepting an invitation |
| **Account selected** | everything that account's memberships and grants permit |

Selection is server-side state on the session row (`selected_account_id`, nullable), and a session may
switch mid-life without re-authenticating. A request to an account-scoped route from a session with no
selection is refused — not with an empty result, which would read as "you have nothing", but as a
statement that no account is selected.

**And the account is still named in the request.** Account-scoped resources are addressed as
`/api/v1/accounts/{accountId}/…`, and the server refuses when the named account is not the selected one.
That looks redundant and is not:

- **Ambient authority is what cross-site request forgery exploits.** If the target account comes only
  from the session, then any forged or confused-deputy request acts in whatever account happens to be
  selected, and the caller never had to know which. Naming it makes the target part of what an attacker
  would have to guess correctly, and makes a mismatch a refusal rather than a silent write to the wrong
  tenant.
- **The public API is used by programs** (P15), and a script should not have to establish selection state
  before it can address a resource. A path that identifies its own account is self-describing; a
  key-authenticated request skips selection entirely, because a key's anchor already fixes its account.

So selection is what the *console* uses to decide which account to put in the path, plus a server-side
gate; it is never the sole source of the answer.

Two properties worth having on purpose:

- **A session's blast radius is one account at a time.** A person in five accounts, whose session is
  stolen, exposes the one that was selected until the attacker performs an explicit switch — which is an
  auditable event rather than an invisible one.
- **Switching re-evaluates membership.** A membership revoked mid-session takes effect at the switch,
  which is a lever that did not exist when roles were global.

**The internal edition is unaffected**, which constraint 1 requires: it has exactly one account, so a
session selects it at sign-in and the console renders no switcher and no selection screen. "No account
selected" is a state that edition never enters.

**Rejected: selection only, no account in the path.** Fewer moving parts and the obvious reading of
"the session has an account". Rejected for the two reasons above — it makes every account-scoped write
depend on ambient state, and it forces a stateful handshake on programmatic callers.

**Rejected: the request's account overrides the selection.** It removes the mismatch case by making the
request authoritative, but then "selected account" stops gating anything and a single request can act in
any account the user belongs to — losing the blast-radius property for nothing.

## Risks / Trade-offs

- **A bug in the containment test grants authority** → single-grant, conservative coverage (P10);
  table-driven tests enumerating every combination of wildcard, identity, selector and inheritance;
  property tests asserting `g ⊑ e` implies every object matched by `g` is matched by `e`.
- **A pattern-matching off-by-one silently widens a grant** → `*` semantics fixed in the spec with a
  worked table (P9); infix wildcards refused at write time; normalization through the existing
  `internal/dnsname` rather than a second implementation.
- **A new store query forgets its `Scope`** → `Scope` is a parameter, not a convention, so omission
  does not compile; the composite foreign keys of P2 mean a wrongly-scoped *write* cannot even
  produce a row whose ownership disagrees.
- **Custom roles let an account build something incomprehensible** → the console shows effective
  permissions per object rather than only the grant list; `ADMIN` is uneditable, so there is always a
  way back.
- **An account strands itself with no administrator** → last-member guard under a row lock (P6);
  genuine recovery is a staff-plane operation and is out of scope here, which is a known gap between
  this change and the next.
- **The challenge hot path regresses** → grants preloaded with the key (P7), ancestor chain resolved
  from rows already in memory, no query added; the existing challenge benchmarks are the gate.
- **The migration is broad** → it runs while the only data is ours, every step is reversible, and the
  compiler enumerates the call sites that must change. This is the whole argument for sequencing this
  change first.
- **Two authorization concepts exist during rollout** — key-by-foreign-key and key-by-grant → the
  migration writes grants for every existing key in the same transaction that drops the column, so
  the two never coexist at runtime.

## Migration Plan

1. Create `accounts`; insert one row (slug `internal`, name from `ACME_ORG_NAME` or a default).
2. Add `projects.account_id`, backfill to that account, set `NOT NULL`, add `UNIQUE (id, account_id)`.
3. Add materialized `account_id` to `load_balancers`, `lb_nodes`, `domains`, `aliases`, `api_keys`;
   backfill by walking parents; add the composite foreign keys of P2; set `NOT NULL`.
4. Replace `domains_name_key` with `UNIQUE (account_id, name)`.
5. Create `roles`, `role_grants`, `account_members`, `api_key_grants`.
6. Seed the four roles with grants reproducing today's matrix; bind existing sessions' subjects by
   group mapping at next sign-in (no backfill needed — memberships are derived for IdP-authenticated
   editions).
7. For every `api_keys` row, insert `[load_balancer, <load_balancer_id>, challenge.publish]`; then
   drop `api_keys.load_balancer_id`.
8. Regenerate `internal/store/schema.sql` via the golden test.

**Rollback**: steps are reversible in reverse order; the only lossy step is 7, which is recoverable
because the dropped column's value is reconstructible from the grant it created.

## Open Questions

- **Does `account_members` bind to one role or several?** Additive roles are today's behaviour
  (`RolesFor` returns a union) and the simplest continuation, but a person holding three custom roles
  makes "why may they do this" harder to answer. Leaning additive, for continuity.
- **Where do satellites fit as subjects?** They authenticate like keys but are instance-scoped, not
  account-anchored. Deferred to `satellite-sync`; noted here because it may want a third grant table.
- **Should the internal edition expose the role editor at all?** Its four seeded roles are exactly
  what its operators need, and hiding the editor keeps constraint 1 ("no observable change") strictly
  true. Leaning: seed them, show the editor, change nothing by default.
- **Row-level security as defence in depth.** Deferred again, but P2's materialized `account_id`
  makes it dramatically easier than E3 assumed — a policy on a real column rather than one requiring
  a join.
