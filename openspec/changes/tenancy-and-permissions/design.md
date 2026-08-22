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
3. **A forgotten scope must not compile, and a mis-aimed one must not return rows.** The failure
   mode being designed against is a new query written without a tenant filter, and conventions do
   not survive that. Making `Scope` a parameter covers the first half — a store method that omits it
   does not build. It does not cover a *valid* `Scope` pointed at another account's row, which is a
   property of each query's `WHERE` clause rather than of its signature, nor any read that does not
   go through the store package at all. P20 puts the second half in the database.

## Goals / Non-Goals

**Goals:**

- Authorization that takes an object, expressive enough for per-project API keys and per-name DNS
  record grants, with roles the customer defines rather than four the code defines.
- Tenant isolation that does not depend on the permission evaluator being correct — two independent
  mechanisms, the outer one structural, and the outer one enforced by the database rather than by
  the shape of the Go code alone (P20).
- A migration that is reversible and that runs while the only data belongs to us.
- No regression on the DNS or challenge answering path.

**Non-Goals:**

- The staff/administrative plane. Acting inside an account without being a member of it is a
  separate authorization system in a separate change.
- Storing zones or records. The `dns_record` selector is designed here; nothing writes one yet.
- Local accounts, signup, billing, editions, marketing. Those stay in `public-edition`.
- DNSSEC, zone storage, satellites. Named here only because P20's bypass role is what the jobs in
  those changes will connect as.

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

### P20. Row-level security is the second half of tenant isolation, and `P2` already paid for it

`Scope` is a Go parameter. That gets one guarantee and not the one the earlier draft claimed.

**What it does guarantee.** A new store method written without a `Scope` argument does not compile,
and a wrongly-scoped *write* cannot produce a row whose ownership disagrees with its parent, because
of P2's composite foreign keys.

**What it does not.** Consider a handler for `GET /api/v1/accounts/acme/load-balancers/{id}`. It
builds a `Scope` from the caller's membership — account `acme`, correct, unforgeable, exactly as
designed — and calls `store.LoadBalancer(ctx, scope, id)` with the `id` from the path. If that `id`
belongs to another account, whether the caller sees the row depends entirely on whether this
particular method's SQL remembered its second predicate:

```sql
SELECT * FROM load_balancers
 WHERE id = $2
   AND account_id = $1   -- omit this line and it still compiles, and returns the other account's row
```

The compiler sees `scope` being passed. It cannot see whether the SQL used it. So the fence is
really "every store method's SQL is correct", upheld by review.

There is a second gap with a larger blast radius: **code that legitimately reads across accounts and
therefore cannot take a `Scope` at all.** By the end of this design's sequence that includes the
satellite feed builder in `satellite-sync`, the zone-time tick job in `metering-and-credits`, the
check-report ingest path in `health-checks`, every migration, and `internal/adminctl` — which its own
documentation calls *"the surface with the most authority, not the least."* The satellite feed is the
one that matters most, because a partitioning bug there does not leak a row into an API response; it
ships one account's zone to a satellite, which then answers the internet with it.

**The decision: enable Postgres row-level security on every table that carries `account_id`, with the
tenant read from a per-connection setting.**

```sql
ALTER TABLE load_balancers ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON load_balancers
  USING (account_id = current_setting('app.account_id')::uuid);
```

`current_setting('app.account_id')` reads a variable set on the connection. The single place that
already produces a `Scope` sets it when the connection is checked out:

```go
// inside the transaction the scoped query runs in
tx.Exec(ctx, "SET LOCAL app.account_id = $1", scope.AccountID())
```

Now the broken query above returns nothing, not because the Go code was right but because the
database refused. Isolation stops being a property of every query and becomes a property of every
table.

**This is cheap only because of P2.** A policy can hold any boolean expression, including a join to
the parent:

```sql
CREATE POLICY tenant_isolation ON lb_nodes USING (
  EXISTS (SELECT 1 FROM load_balancers lb JOIN projects p ON p.id = lb.project_id
           WHERE lb.id = lb_nodes.load_balancer_id
             AND p.account_id = current_setting('app.account_id')::uuid));
```

and that form is a trap. A subquery inside a policy is evaluated *as the querying user*, so the
policies on `load_balancers` and `projects` apply inside it, and Postgres can refuse the whole query
with `infinite recursion detected in policy for relation "load_balancers"`. The usual escape is to
hide the lookup in a `SECURITY DEFINER` function owned by a role that bypasses RLS — which puts the
isolation rule inside a function that deliberately ignores isolation. It is also slow: the predicate
is evaluated against every row the plan touches, before the query's own selective `WHERE` gets a
chance, which is precisely the cost P2 identified as *"every index that should lead with the tenant
cannot."*

P2 already put `account_id` on the row and made it unforgeable, so none of that applies: every policy
is a plain column comparison against the same tenant-leading index the design already wanted. The
denormalization that made `Scope` cheap makes RLS cheap. That is not a coincidence — both mechanisms
want the same thing, the tenant on the row.

**`challenges` and `challenge_interactions` are a deliberate exception.** P2 leaves them without an
`account_id`, because they reach the account through the alias and load balancer they already
reference. They therefore get no policy, and the join form is not used for them. Three reasons, in
order of weight: a challenge row is only ever reached through an alias that was itself fetched under
a `Scope`, so the fence sits one level up rather than absent; `POST /present` is on a certificate
issuance critical path and constraint 2 forbids making it slower; and the alternative is exactly the
`SECURITY DEFINER` helper described above, which is the one construction that pays both costs at
once. This exception is written down here so that a later reader finds a decision rather than an
oversight.

**Cross-account jobs connect as a role with `BYPASSRLS`.** With policies in place they would
otherwise see nothing, which is the point: the set of code that can read across tenants stops being
"whatever does not happen to go through the store package" and becomes a short list of components
that explicitly asked for the privilege, greppable by the role name in their connection string. A
future author adding a cross-account query has to reach for it consciously.

**The failure mode to test first is the connection pool.** `SET LOCAL` is scoped to a transaction
and discarded at commit, so it cannot survive into the next borrower of that connection — but only if
the scoped query genuinely runs inside an explicit transaction. A plain `SET` outside one, on a
pooled connection, leaves account `acme`'s tenant setting on a connection that then serves a request
for another account: the same class of bug this decision exists to prevent, arriving by a new route.
So every scoped query runs in a transaction, and the pooler is verified early — pgx's own pool and
PgBouncer in transaction mode are both fine, PgBouncer in statement mode is not.

**Rejected: `Scope` alone, with the design text corrected.** The cheapest option, and it was the
previous position ("still wanted as defence in depth; still deferred"). Rejected because it leaves
the satellite feed — the one path that can publish another account's data to the internet — with no
mechanism in front of it at all.

**Rejected: `Scope` alone plus cross-tenant tests.** A fixture with two accounts, and an assertion
per store method that account `acme`'s `Scope` cannot reach account `globex`'s row. This is a genuine
improvement and catches the missing-`WHERE` bug, so it is kept as a task regardless. It is not
sufficient on its own: it tests the store package, and the exposure is outside it.

**Rejected: doing this after the first customers arrive.** RLS on an empty database is a migration.
RLS on a live one is a change to what running queries return, applied to tables people are reading,
with no safe way to rehearse the failure. The whole argument for sequencing this change first is that
its data is still only ours.

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
- **A store query carries a valid `Scope` but omits the tenant predicate in its SQL** → the RLS
  policy of P20 applies the predicate at the table, so the query returns nothing; cross-tenant tests
  over every store method catch it in CI as well.
- **A read that never goes through the store package — the satellite feed builder, the zone-time
  tick, check ingest, `adminctl`, a migration — crosses accounts** → those connect as the `BYPASSRLS`
  role by name, which makes the set of cross-tenant readers enumerable and reviewable rather than
  implicit. Everything else sees policies.
- **The tenant setting leaks between pooled connections** → `SET LOCAL` inside the explicit
  transaction the scoped query already runs in, discarded at commit; a pooler test asserting that a
  connection returned to the pool and re-borrowed carries no tenant setting; PgBouncer statement mode
  documented as unsupported.
- **`challenges` and `challenge_interactions` have no policy** → accepted and reasoned in P20: they
  are reachable only through an alias fetched under a `Scope`, and the alternative costs both the
  challenge hot path and a `SECURITY DEFINER` bypass.
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
8. Create the `BYPASSRLS` role the cross-account jobs connect as; grant it the privileges those
   jobs need and nothing more.
9. `ENABLE ROW LEVEL SECURITY` and create the `tenant_isolation` policy on `load_balancers`,
   `lb_nodes`, `domains`, `aliases` and `api_keys`. Not on `challenges` or
   `challenge_interactions` (P20).
10. Regenerate `internal/store/schema.sql` via the golden test.

**Rollback**: steps are reversible in reverse order; the only lossy step is 7, which is recoverable
because the dropped column's value is reconstructible from the grant it created. Steps 8 and 9 are
`DROP POLICY` / `DISABLE ROW LEVEL SECURITY`, which restore exactly the previous behaviour, since
`Scope` is doing the filtering either way.

## Open Questions

- **Does `account_members` bind to one role or several?** Additive roles are today's behaviour
  (`RolesFor` returns a union) and the simplest continuation, but a person holding three custom roles
  makes "why may they do this" harder to answer. Leaning additive, for continuity.
- **Where do satellites fit as subjects?** They authenticate like keys but are instance-scoped, not
  account-anchored. Deferred to `satellite-sync`; noted here because it may want a third grant table.
- **Should the internal edition expose the role editor at all?** Its four seeded roles are exactly
  what its operators need, and hiding the editor keeps constraint 1 ("no observable change") strictly
  true. Leaning: seed them, show the editor, change nothing by default.
- **Should the cross-account jobs share one `BYPASSRLS` role or hold one each?** One role is
  simpler and is what the migration creates. One role per job — `svc_satellite_feed`,
  `svc_metering` — would make an audit of who read what across tenants sharper, at the cost of more
  credentials to rotate. Leaning: one role now, split if the list grows past a handful.
