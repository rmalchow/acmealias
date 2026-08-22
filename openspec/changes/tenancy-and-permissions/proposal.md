## Why

### The question authorization cannot currently answer

Today the service asks "what may this person do". It has no way to ask "what may this person do
*to this thing*". That is not an oversight — it is written down as a decision, in
`internal/authz/permissions.go`:

> *"There is one Action per row of the permission matrix in design D10 and in the
> identity-and-roles spec, and no Action takes a project: no role is project-scoped, so there is
> nothing to scope a check to."*

So `Can(roles, action)` takes a verb and nothing else. `CONFIG_ADMIN` means "may write inventory",
everywhere, to everything. For one organization administering its own fleet, where everyone who
can sign in is trusted with everything, that is correct and pleasantly small.

Three things we now want are each impossible inside it, and the third is dangerous inside it.

### One: an API key scoped to a project is strictly more powerful than one scoped to a load balancer

An API key today carries no permission field at all. Its entire authorization is one comparison,
in `internal/httpapi/challenge.go`:

```go
if alias.LoadBalancerID != key.LoadBalancerID {
    h.refuse(ctx, key, name, form, "alias belongs to another load balancer")
    return nil, errRefused
}
```

The scope *is* the foreign key `api_keys.load_balancer_id`, and the capability is implicit and
singular: publish or withdraw a challenge TXT value. That works precisely because there is exactly
one thing a key can do, so "which load balancer" is the whole question.

Move the key up to a project and it covers every load balancer in that project — the same absent
permission model, applied to a wider set of objects. A customer who asks for "one key for the
project instead of four keys for four load balancers" is asking for convenience; what the current
model would hand them is an escalation. So per-project keys and fine-grained permissions are not
two features that happen to be in the same change. The first cannot be built safely without the
second.

### Two: if users create projects, `ADMIN` stops being the right gate

`ActionProjectAdmin` is held by `ADMIN` alone, and `ADMIN` is conferred by membership in an
identity-provider group named by the `ACME_ADMIN_GROUP` environment variable. Nobody can grant it
to themselves; that is the point.

If an ordinary user may create a project, project creation must become a right held inside a
tenant rather than a global operator privilege — and the person who created a project needs
standing *in the project they just made*, which is a per-object fact about a person. The current
model has nowhere to put it.

### Three: real DNS management turns a small blast radius into a total one

This is the forcing function. The worst thing a stolen API key can do today is publish a TXT
record under a label we generated, in a zone we own — say `k4m2p9.acme.example.net`. It cannot
touch the customer's own zone, because we hold no credentials for it. That is a genuinely small
blast radius, and it is small by accident of what the service does, not by design of its
authorization.

Once the service manages real DNS records, the same key with the same (absent) permission model
can rewrite `A` records to point traffic at an attacker's host, change `MX` records to intercept
mail — including password-reset mail for every other service the customer uses — or add `NS`
records delegating a subtree away entirely. There is no second gate behind us: we are authoritative,
so whatever we serve is the truth as far as the internet is concerned.

The gap between "may publish one TXT under a name we control" and "may say anything about any of
the customer's names" is the entire distance between an inconvenience and a takeover, and today
both sit behind the same check, which is no check at all.

### Why now

The permission model is the load-bearing decision. Satellites, real DNS records, health checks and
credits all reference objects — zones, recordsets, nodes, checks — that must be grantable before
any of them can be exposed to a tenant. Every one of those changes is cheaper to write against a
model that takes an object than to retrofit afterwards, and "retrofit afterwards" means auditing
every route ever written, where a miss is a cross-tenant disclosure or a hijacked domain.

## What Changes

### A permission is an anchored triple

A permission becomes `[type, id, action]`, each slot admitting a wildcard, plus a flag saying
whether it reaches into contained objects:

```
grant := (type | *, id | * | selector, action | *, inheritable)
```

Read as: *"on objects of this type, with this identity, you may do this."* Some worked examples,
using the object names this service already has:

| Grant | Means |
| --- | --- |
| `[load_balancer, 7, challenge.publish]` | publish challenge values for load balancer 7 — what every API key can do today |
| `[project, 12, *]` inheritable | anything, to project 12 and everything inside it |
| `[project, 12, project.rename]` **not** inheritable | rename project 12; may not touch anything in it |
| `[lb_node, *, status.read]` inheritable | read status of every node in this grant's account |
| `[domain, 88, domain.delete]` | delete exactly one domain |

Identifiers are shown abbreviated above for readability; in reality every one is a UUIDv7 (see
*Impact*), so `7` is really `018f3a2b-9c41-7e6d-8b02-3f5a1c9d4e77`.

### The wildcard is anchored, and that is what preserves tenancy

Read literally, `[load_balancer, *, challenge.publish]` says *every load balancer that exists*,
which would be a tenancy hole large enough to drive the entire product through.

It is not free-floating. A grant lives in a role or on an API key; a role belongs to an account; a
key is anchored to a project, which belongs to an account. **`*` means "any object of that type
within this grant's own account", never "any object anywhere."** No grant, however written, can
name an object of another account, because there is no syntax that reaches outside the anchor.

Tenant isolation therefore is *not* expressed in the permission language. It stays in the query
layer, where a `Scope` value that every inventory query must accept confines reads and writes to
one account. The permission model runs strictly inside that boundary and cannot widen it. Two
independent mechanisms, and the outer one does not depend on the inner one being correct.

### Ownership: an account sits above projects

For any of that to mean anything there must be an owner above `projects`, which today has none:

```
account
 ├── project
 │    ├── load_balancer
 │    │    ├── lb_node
 │    │    ├── domain ── alias
 │    │    └── api_key
 │    └── (zones, checks and satellites attach here in later changes)
 ├── role / membership
 └── (credits and billed items attach here in a later change)
```

Ownership is inherited rather than re-declared: a domain belongs to whatever account its load
balancer's project belongs to, and there is no second source of truth that could disagree.

Every table a query must filter or a grant may name does, however, carry its ancestors as real
columns — `account_id`, and `project_id` on the leaves. That sounds like the denormalization E2 of
the in-flight change rejected, and it would be, were it maintained by discipline. It is not: a
composite foreign key onto the parent's `(id, account_id)` makes a row that disagrees with its parent
**fail to insert**. The database enforces what E2 feared would need a trigger, and in exchange every
grant becomes a single-column predicate rather than a subquery — which is what makes the enforcement
mechanism above a fold rather than a compiler.

The account level itself is otherwise decision E2 of `add-accounts-and-public-edition`, adopted here
(see *Relationship to add-accounts-and-public-edition* below).

### Inheritance walks that graph

`inheritable` is what makes a grant reach downward. `[project, 12, inventory.view]` inheritable
matches every load balancer, node, domain and alias under project 12. The same triple without the
flag matches the project row itself and nothing inside it.

Evaluation resolves an object's ancestor chain — which is just following parent foreign keys the
rows already carry — and a grant matches when the action matches, the type matches, and the
selector names either the object itself or, when inheritable, one of its ancestors.

### Permissions are allow-only

A subject's authority is the union of their grants. Absence is denial. There are no deny rules.

The consequence is worth stating because it is a real constraint on how permissions get written:
to express *"admin of the project except the production zone"* you do **not** grant the project
and subtract. You grant the zones individually and leave the production one out. Adding a grant
can only ever add power, so evaluation is order-independent and a grant list can be read top to
bottom without holding a subtraction in your head.

**Rejected: allow plus deny, deny wins.** It expresses carve-outs directly, which is genuinely
convenient. It was rejected because deny interacting with inheritance has no obvious right answer —
does a deny at `project` beat an allow at `zone`, or does the more specific rule win? — and because
"why can Bob not do this" stops being answerable by looking at the object and needs a tool that
explains an absence. Every deny system we have used ends up growing that tool.

### Roles become rows, not constants

`ADMIN`, `CONFIG_ADMIN`, `KEY_ROTATION` and `READ_ONLY` are currently four Go constants and a
`map[Action]RoleSet` literal. They become **rows**: a role is a named bundle of grants, defined
per account, editable by anyone with permission to edit roles.

The existing four are re-expressed in the new language and seeded at migration time. This is what
lets the internal edition cross this change without behaving differently: `CONFIG_ADMIN` becomes a
role row whose grants are `[project, *, inventory.write]` inheritable and friends, an operator
signing in through Keycloak still lands in it by group mapping, and every scenario in the
`identity-and-roles` spec still passes. What changes is that the set is no longer closed.

`ADMIN` stays special, and is the only role that is:

- its grant is exactly `[*, *, *]`, inheritable;
- it cannot be edited or deleted, because a custom-roles system with no fixed point has no way back
  from a mistake;
- **it must always have at least one member.** Removing the last one is refused.

That last rule needs care rather than a check: two administrators each removing the other,
concurrently, will each observe one remaining administrator and both succeed, leaving zero. The
count and the removal must happen under `SELECT … FOR UPDATE` in one transaction.

### API keys become subjects that carry grants

A key stops being "a row with a load balancer foreign key" and becomes an anchored subject with a
grant list — the same grants a role holds, evaluated by the same code:

```
key k_1a2b  anchored at project 12
  [load_balancer, 7, challenge.publish]
  [lb_node, *, status.read]  inheritable
```

Migration is mechanical: every existing key gets one grant naming its current load balancer and
the `challenge.publish` action, and the comparison in `challenge.go` becomes
`Can(key, "challenge.publish", alias)` — the same decision, reached through the general mechanism.
`api_keys.load_balancer_id` is then dropped.

A key anchored at a project with grants covering several load balancers is the per-project key that
prompted this change, and it is now expressible without being a blanket widening: the anchor says
where it may reach, the grants say what it may do there.

### Permission is enforced in the query, and one function answers for every existing object

Grants are not checked after fetching a row. They are compiled into a condition that forms part of
the read, so an object the caller may not see never leaves the database. That has a consequence worth
stating on its own, because it removes a great deal of code: **reading one object is reading a
collection with one more condition.** So every read, update and delete of an existing object
authorizes through a single function that returns the object or nothing — and "nothing" covers both
"does not exist" and "you may not see it", because the query cannot tell them apart either. The
non-disclosure property is then structural rather than a rule somebody has to remember.

The same condition restricts the count reported alongside a page. A total that counted invisible rows
would leak their existence through a number, which is exactly what returning nothing was protecting.

Creation is the one operation with no object to check, and it is authorized on the **parent** —
`domain.create` on the load balancer, `load_balancer.create` on the project — fetched by that same
restricted read, so creating under a parent you cannot see fails identically to creating under one
that does not exist.

Creating a **project** is a deliberate exception: it requires the account's unrestricted role and is
not a grantable action. The account is the anchor of the grant model rather than an object inside it,
and allowing `[account, …, project.create]` would make it an ACL-addressable object — after which
every other account-level concern starts to look like it should be a grant too, and the line between
the tenant plane and account administration dissolves. One exception keeps that line, and it costs
nothing in practice: a public-edition customer is the unrestricted member of the account signup
created for them, so "only the admin may create projects" and "a user can create projects" are the
same sentence for an account of one.

### The management API splits in two, and the listeners collapse into one

The management API becomes a **versioned, publicly documented** API at `/api/v1/**`, used by the
console and by customer programs alike — the console is simply its first client — and an
**unversioned internal** API at `/api/admin/**` carrying staff operations, which have no external
callers and no stability obligation.

The three listeners of design D9 become one, separated by path prefix with per-group middleware.
D9's guarantee — "a stolen API key reaches no management route even if a routing rule in front of us
is wrong" — is now carried by grants rather than by network topology. Satellites keep a listener of
their own, because mutual TLS is a property of a listener rather than of a route.

### You may only grant what you hold

Once someone who is not an administrator can create a key or edit a role, "create a key with
`[*, *, *]`" is a self-promotion. Every grant written to a role or a key must therefore be within
the editor's own effective permissions.

Worked through: Bob holds `[project, 12, *]` inheritable and nothing else.

| Bob writes | Result |
| --- | --- |
| `[load_balancer, 7, challenge.publish]` where LB 7 is in project 12 | allowed — under his anchor |
| `[zone, 101, record.write]` where zone 101 is in project 12 | allowed |
| `[project, 13, inventory.view]` | refused — project 13 is not his |
| `[*, *, *]` | refused — he does not hold it |

The containment test this needs is the one genuinely novel algorithm in the change, and it is
deliberately **conservative**: a proposed grant must be covered by a *single* held grant, not by the
union of several. That will occasionally refuse something that is arguably permitted. Refusing too
much is the correct direction to be wrong in, and the limitation is specified rather than
incidental, so that nobody later "fixes" it without noticing it is a security-relevant relaxation.

### DNS record grants slice a zone by name and type

A zone cannot be usefully sliced by row identity — nobody wants to grant record #4471 — so for DNS
records the `id` slot holds a selector:

```
[dns_record, {zone: example.com, names: *.staging.example.com, types: {A, AAAA}}, write]
```

`*` stands for **one or more labels and never the empty string**, so a subtree grant does not
include the subtree's own apex:

| Name | Matched by `*.staging.example.com` |
| --- | --- |
| `web.staging.example.com` | yes |
| `db.web.staging.example.com` | yes — `*` spans several labels |
| `staging.example.com` | **no** — the apex is not matched; grant it explicitly if wanted |
| `www.example.com` | no |

The type constraint is what keeps a deployment key from becoming a mail-interception key: a grant
carrying `{A, AAAA}` cannot write `MX`, and cannot write `NS` to delegate a subtree away.

**Rejected: per-zone grants only** (the shape most DNS providers' API tokens actually take). Simpler,
and wrong for the case that motivates the feature: a CI system that deploys staging should not be
one compromised runner away from repointing production's mail.

### Domain names become unique per account, not globally

`domains_name_key unique (name)` is global today, which with more than one account is simply a bug:
an agency and their client, or two people whose test data uses `example.com`, collide for no reason.

Relaxing it to unique-per-account looks alarming — two accounts both claim `shop.example.com`, both
receive an alias, both may request challenges for it — and then turns out to be fine, because
**the CNAME is the proof of ownership.** Each claimant is given a *different* alias label. Whoever
actually controls `shop.example.com`'s DNS publishes `_acme-challenge.shop.example.com CNAME` to
exactly one of those labels. The other label answers nothing that any certificate authority will
ever be pointed at.

So both may *ask*, and only the real owner can *validate*. DNS delegation arbitrates, and no
dispute-resolution tooling needs to exist.

### Two mechanisms means two mechanisms, and one of them belongs in the database

The isolation argument above says a permission bug should over-permit inside one account rather than
leak across accounts, because `Scope` fences the query before any grant is consulted. That argument
is only as good as the fence, so it is worth being exact about what the fence catches.

`Scope` is an argument to every store method. A method written without it does not compile. That
genuinely removes the most common way tenant filters go missing — someone adds a query and forgets.

It does not remove two others. The first is a query that *has* a valid `Scope` and does not use it:
a handler for `GET /accounts/acme/load-balancers/{id}` builds the caller's `Scope` correctly and
passes the `id` straight from the URL, and whether that returns another account's load balancer
comes down to whether this one method's SQL included `AND account_id = $1`. The compiler sees the
argument being passed; it cannot see whether the `WHERE` clause used it.

The second is bigger. Several things this design's successors need must read across accounts by
definition, so they cannot take a `Scope` at all: the process that builds each satellite's zone feed,
the job that totals zone-time for billing, the path that ingests health-check reports, `adminctl`,
and every migration. None of those is behind the fence. The zone feed is the one to worry about —
a mistake there does not leak a row into an API response, it hands one customer's zone to a
satellite that then answers the internet with it.

So this change turns on Postgres **row-level security**: each table carrying `account_id` gets a
policy saying a row is visible only when its `account_id` equals a value set on the connection, and
the code that builds a `Scope` sets that value. A query that forgets its predicate now returns
nothing, because the database applied the predicate instead. The components that legitimately read
across accounts connect as a separate database role that is exempt — which is the second benefit:
the list of code that can cross tenants stops being "whatever bypassed the store package" and
becomes a short, named, greppable set.

This was deferred in the earlier proposal, and the reason it is affordable now is P2 of the design.
Once `account_id` is a real column on every row, held true by a composite foreign key, each policy is
a single column comparison. Had ownership stayed reachable only by joining up to `projects`, each
policy would have needed a subquery — which Postgres evaluates under the *caller's* policies, and
which can fail outright with `infinite recursion detected in policy`. The denormalization that made
`Scope` fast is what makes this cheap.

One table is deliberately left out. `challenges` and `challenge_interactions` carry no `account_id`;
they are reached only through an alias that was itself fetched under a `Scope`, and they sit on the
certificate-issuance critical path this change promises not to slow down. That exemption is written
into the design rather than left implicit, so it reads later as a decision and not as a table someone
missed.

### Not in scope

- **The staff/administrative plane.** Acting inside an account without being a member of it —
  support, suspension, billing operations — is a separate authorization system in a separate change.
  This change is only about authority held *inside* one account.
- **Zones, recordsets, satellites, checks and credits.** The `dns_record` selector is specified here
  because the grant grammar must accommodate it, but no zone or record is stored by this change.
- **Local accounts, signup, email confirmation, billing, editions, the marketing site.** All still
  belong to `public-edition`.

### Relationship to `add-accounts-and-public-edition`

That change is proposed and unarchived, and this one overlaps it deliberately. Splitting the
overlap explicitly, because leaving two live proposals disagreeing about roles is worse than either
of them:

**Adopted here, unchanged** — the account entity and its position above `projects` (E2), ownership
by inheritance rather than a repeated column, the single seeded account for the internal edition,
and the `Scope` helper that every store query must take (E3). These move into this change because
the permission model is meaningless without an owner to anchor to.

**Superseded** — its `identity-and-roles` delta, which specifies that the four role names stay
fixed, that roles are held account-wide, and that *"within an account there SHALL be no finer
scoping."* All three statements are now wrong. Its proposal's non-goal *"per-project or per-domain
roles inside an account"* is likewise reversed.

**Left alone** — everything about the public edition as a product: the authentication seam, plans
and entitlements, the billing adapter, edition selection, and the marketing site.
Those are untouched by this change and should remain in that one, which after this lands is a
smaller and more coherent proposal about *selling* the service rather than about *structuring* it.

`add-accounts-and-public-edition` therefore needs amending — not archiving — once this is accepted.

## Capabilities

### New Capabilities

- `accounts-and-tenancy`: the account entity, ownership of every inventory object by inheritance,
  account-scoped queries through a `Scope` that has no constructor from a bare identifier, and the
  single-account internal edition. Adopted from the in-flight change.
- `permissions`: the anchored triple, the containment graph and inheritance, allow-only evaluation,
  roles as per-account data, the `ADMIN` invariant, the subset rule, and the DNS record selector
  with its pattern semantics.

### Modified Capabilities

- `identity-and-roles`: roles stop being four fixed global constants and become per-account rows
  holding grants; group-to-role mapping resolves to a role row rather than to a constant; the
  `ADMIN` role gains its uneditable and last-member-standing invariants.
- `api-credentials`: a key stops being scoped by a load balancer foreign key and becomes an anchored
  subject carrying grants; keys may be anchored at a project; key creation is bounded by the
  creator's own permissions.
- `service-inventory`: every project belongs to an account; domain names are unique per account
  rather than globally; creating a project is a permission held inside an account rather than a
  global operator role.
- `audit-trail`: the vocabulary gains role and grant mutations; recorded actor roles become
  references to account-scoped role rows rather than names from a closed set.
- `admin-console`: the console gains a role editor and its "what may I do" answer becomes
  per-object rather than a flat list of permitted actions.
- `service-operations`: the three-listener split gives way to one listener separated by path prefix,
  plus a mutually authenticated listener for satellites; and the management API splits into a
  versioned, publicly documented API shared by the console and programmatic callers, and an
  unversioned internal administrative API.

## Impact

- **Identifiers.** Every primary and foreign key becomes a UUIDv7. Beyond not being enumerable — which
  strengthens the "absence and denial are indistinguishable" property, since the identifier cannot be
  guessed to begin with — it makes identifiers disjoint across tables, so a bug that compares
  `role_grants.object_id` without also comparing `object_type` finds nothing rather than finding the
  wrong object of the right number.
- **Schema.** New `accounts`, `account_members`, `roles`, `role_grants`, `api_key_grants`.
  `projects` gains `account_id NOT NULL`, backfilled to the single seeded account. `api_keys` gains
  an anchor and loses `load_balancer_id`. `domains_name_key` changes from `unique (name)` to
  `unique (account_id, name)` — reached through the load balancer's project, so it needs a
  functional or denormalized index and is the one place where ownership-by-inheritance costs
  something.
- **`internal/authz`.** `Can(roles, action)` becomes `Can(subject, action, object)`. The four role
  constants and the `permissions` matrix literal are replaced by table lookups. The containment
  test for the subset rule is new code with no existing analogue in the repository.
- **Every store query.** Mechanical but broad: each inventory query gains a `Scope` argument. Doing
  this as a signature change rather than a convention is the point — the compiler enumerates the
  call sites, and a missed one does not compile rather than silently returning another tenant's rows.
- **`internal/apikey`.** `Lookup` returns a subject with grants; `Verify` is unchanged; the
  middleware gains nothing, because authorization moves to the handler that knows the object.
- **`internal/httpapi/challenge.go`.** The load-balancer comparison becomes a permission check. This
  is the only change on the challenge hot path, and it must not add a query — grants are loaded with
  the key.
- **The console.** A role editor is new UI. Role-gated rendering changes from "do I hold
  `CONFIG_ADMIN`" to "may I do this to this object", which affects every guarded control.
- **Migration.** Seeding one account, seeding four roles expressed as grants, mapping every existing
  key to one grant, and backfilling `projects.account_id` — all reversible, all before there is any
  data belonging to anyone but us, which is the entire argument for doing it now.
- **Nothing on the DNS or challenge answering path.** `internal/dnsserver` and `internal/zoneview`
  are untouched.
