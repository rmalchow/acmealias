## Why

### There is already an administrative surface, and its authorization is a placeholder

`internal/adminctl` is the command-line administration package: it creates and lists projects, load
balancers, nodes and domains, renders DNS plans and creates API keys, operating directly against the
datastore with no server process running. Its own documentation is candid about what authorizes it:

> *"It has no groups and no roles: the CLI runs with database credentials, so the authority behind a
> CLI change is 'had the connection string', and recording an empty role list says that honestly
> rather than implying a role nobody checked."*

And `internal/adminctl/actor.go` calls it *"the surface with the most authority, not the least."*

For a single organization administering its own fleet that is entirely defensible. The people who
hold the production connection string are the same people the console already trusts with
everything, so "possession of a secret" and "authorized" name the same set of people.

The moment inventory belongs to somebody else, they stop naming the same set. "Possession of a
secret" is not an authorization model, it produces no attributable subject, and it cannot be
time-boxed, scoped, reviewed or shown to the customer whose data it touched.

### Somebody has to be able to act inside an account without being a member of it

`tenancy-and-permissions` makes every management operation act in exactly one account, confined by a
`Scope` value that has no constructor from a caller-supplied identifier. That is the point of it:
there is no syntax, anywhere in the permission language, that names an object of another account.

But six ordinary jobs require exactly that:

| Job | What it needs | Why a tenant permission cannot express it |
| --- | --- | --- |
| **Support** — "my CNAME isn't working" | Read a customer's inventory, plan and verification state | The person reading is not a member and must not become one |
| **Enforcement** — abuse, suspension | Suspend an account, disable a key, remove a domain | Writes into a tenant *against* the tenant's wishes |
| **Billing operations** | Grant credits, adjust balances, resolve disputes | Concerns the account, not anything inside it |
| **Lifecycle** | Create an account for a sales-led customer, erase one on request | Acts on the account itself |
| **Platform** | The satellite fleet, the served zone, the alias label namespace | Not tenant data at all — it has no owning account |
| **Recovery** | Reassign account ownership | Deliberately breaks an invariant the tenant plane enforces |

The last row is the sharpest illustration. `tenancy-and-permissions` requires the unrestricted role
to always have at least one member, and requires that a subject may grant only what it holds. Put
those together and an account whose sole administrator leaves the company **cannot be repaired from
inside**: the guard that prevented the account from becoming administrator-less is the same guard
that prevents anyone remaining from becoming one. The fix has to come from outside the tenant model,
which means the administrative surface is by construction *the thing that can break the rules the
tenant plane enforces*. That is not a flaw in either design. It is why the constraints on this plane
— attribution, time-boxing, transparency — have to do the work that the subset rule does elsewhere.

### Why now

`satellite-sync`, `dns-records`, `health-checks` and `metering-and-credits` each introduce objects
with no owning account — satellites, the served zone, the price book — and each introduces an
operational job that is nobody's tenant permission: enrol a satellite, suspend an account, grant a
credit. Without this plane those land as ad-hoc environment variables and CLI subcommands, which is
how "possession of the connection string" became the authorization model the first time.

## What Changes

### Two planes, and the second one already exists

The instinct on reading "staff need to act inside tenants" is to widen the tenant permission model —
add a level above the account so a grant can anchor there and inherit downward. That is rejected
below. Instead there are **two authorization systems that never call each other**:

| | Tenant plane (`tenancy-and-permissions`) | Staff plane (this change) |
| --- | --- | --- |
| Subjects | Account members, API keys | Our own employees |
| Authenticated by | Whatever the edition uses | Our identity provider, always |
| Permission shape | `[type, id, action]` + inheritable | A flat named permission |
| Scoped to objects? | Yes — that is the whole point | No |
| Inheritance, wildcards, patterns | Yes | None |
| Subset rule on granting | Yes | None — membership is IdP configuration |
| Can name another account's object | **No, by construction** | Only through an access grant (below) |

The pleasant part is that the second system is already written. Today's `internal/authz` is exactly
this shape: `Mapping.RolesFor(groups)` derives a flat set from an identity-token group claim,
`Can(roles, action)` consults a `map[Action]RoleSet` literal, and an authenticated user mapping to
nothing is refused with the observed groups reported so the misconfiguration can be seen. Every
property that made that wrong for tenants — global, flat, object-blind, configured in the
environment rather than in data — is right here.

So `internal/authz` is **kept**, and only its vocabulary changes. `inventory.write` and `key.write`
give way to:

```
support.access.open          open a time-boxed access grant into one account
account.suspend              suspend or restore an account
account.credit.grant         adjust an account's credit balance
account.ownership.reassign   the recovery escape hatch
account.lifecycle            create, erase, export an account
platform.admin               satellites, the served zone, the price book
staff.audit.read             read the staff activity trail
```

`tenancy-and-permissions` moves the tenant-facing role machinery out of this package into the new
model; what is left behind, with its group mapping and per-request derivation intact, is the staff
plane.

### The only bridge is a third `Scope` producer

Staff must never call the tenant evaluator. If they did, a bug in wildcard matching or the
inheritance walk could produce cross-account access, and the entire argument for two planes
evaporates.

Instead the bridge is at the query layer. `tenancy-and-permissions` specifies a `Scope` — the value
every inventory query must carry, confining it to one account — with exactly two producers: the
authenticated tenant request path, and the single-account bootstrap. This change adds the third and
last:

```go
func ScopeForStaffAccess(a *StaffAccess) (Scope, error)
```

A staff member with an open access grant produces a `Scope` for that one account, and then runs
**the tenant's own queries** inside it. No permission triple is evaluated, no grant is consulted,
nothing in the tenant plane is asked anything. The boundary that keeps accounts apart is the same
boundary in both directions, and it is a type, not a check.

### Access into an account is time-boxed and reason-required

`StaffAccess` is a row:

```
(id, staff_subject, account_id, reason, opened_at, expires_at, closed_at)
```

Holding `support.access.open` does not grant access to anything. It grants the ability to *open* an
access grant, naming one account and stating a reason, which then expires. A support engineer
answering ticket #1183 opens access to account 42 for thirty minutes with `"ticket #1183"` recorded,
and every read they perform in that window is attributable to that grant and that reason.

The grant is re-evaluated on **every request**, never materialized into the session at sign-in.
There is direct precedent: `internal/authz/middleware.go` derives roles per request and its comment
explains why — *"Roles are derived per request and left in the Echo context, never stored on the
session row"* — because the mapping can change under a live session. An access grant can expire or
be revoked mid-session for the same reason.

**Friction is the feature.** Standing read access over every customer, controlled only by a log
nobody reads, is the arrangement that produces the incidents. A stated reason and an expiry make
casual browsing visible and deliberate browsing narratable.

### The customer sees that we looked

Staff activity appears in the **tenant's own audit trail**, attributed to `Support` with the reason,
never to the individual employee:

```
22 Aug 14:02   you       domain.create   shop.example.com
22 Aug 14:31   Support   account.read    ticket #1183
22 Aug 14:33   Support   apikey.revoke   k_1a2b   ticket #1183
```

This inverts the visibility rule of every existing audit row. Today an entry is something the tenant
did, which the tenant may read. These are entries *we* wrote, which the tenant is entitled to read —
and which must not disclose which of our employees wrote them.

That requires an audit entry to carry **two identities**: the real subject, for our own
accountability, and the presented one, for the tenant. Redacting the real subject when rendering the
tenant's view is explicitly rejected: one query path that forgets — a listing, an export, a filter,
a new endpoint written next year — leaks employee names to customers, and it leaks them into a file
the customer keeps. Separate columns fail safe; a render-time rule fails open.

### Staff reads are audited, and that is a new class of row

Today the audit trail records *"every creation, modification and deletion"*. Reads are deliberately
absent, because reads are the high-volume path and the retention policy was written for mutations.

Making staff reads attributable inverts that for one class of actor. A thirty-minute support session
browsing a console produces hundreds of read events against a table sized for changes. So staff read
activity is recorded **separately** from `audit_entries` — either its own table, or summarised per
access grant rather than per request — so that its volume and retention can be governed on its own
terms without distorting a trail that answers a different question.

### The CLI stays, narrowed and documented

`internal/adminctl` remains break-glass: unauthenticated by construction, full authority, audited
with `SourceCLI`, and available when the service itself is down — which is exactly when an
authenticated staff API is least likely to be reachable. That is a deliberate trade, and the residual
risk is stated plainly rather than mitigated away: **possession of the connection string is
unlimited tenant access.**

One thing must change with it. If staff activity is visible to the customer and CLI activity is not,
then the way to touch a customer's data unobserved is to use the emergency path — the incentive
gradient points exactly the wrong way. So `SourceCLI` entries that touch a tenant's inventory surface
in that tenant's trail too, attributed the same way as any other staff action.

### Not in scope

- **Any change to how tenants authorize themselves.** The tenant plane is `tenancy-and-permissions`
  and this change does not touch its evaluator, its grants or its roles.
- **A staff console.** This change specifies the plane, its permissions, access grants and their
  audit consequences. What the screens look like is a separate piece of work.
- **Approval workflows** — a second employee authorizing an access grant. The design leaves room and
  the audit trail would show it; requiring it is a policy decision, not a structural one.
- **Customer-initiated support sessions** — the customer opening access from their own console.
  Attractive for privacy, useless in the two cases that matter most (a customer locked out, and an
  abuse investigation where consent will not be given).

### What we considered and rejected

**An instance level above the account, staff grants anchored there.** Add a root to the containment
hierarchy so `[account, *, support.read]` anchored at the instance inherits into every account. One
evaluator, one audit vocabulary, and it would give the genuinely unowned state — the served zone, the
alias label namespace, the satellite fleet, the price book — somewhere to live. Rejected because it
makes cross-tenant reach a *writable value*: one grant row, one wildcard, one bug in the inheritance
walk away. `tenancy-and-permissions` argues that over-permission inside a tenant is a mistake while
cross-tenant is a breach, and that the two should not be reachable through the same mechanism. An
instance anchor makes them the same mechanism. The unowned platform state instead gets `platform.admin`
on this plane, which needs no hierarchy at all because it has no tenant to be scoped to.

**Just-in-time memberships into the tenant.** To act in account 42, a staff member grants themselves
a time-boxed membership in it. No new level, the tenant evaluator unchanged, and the expiry story is
identical. Rejected because the self-grant is precisely the escalation the subset rule forbids, so it
needs a carve-out in the one rule that must not have carve-outs — and because it leaves platform
state with no home.

**Standing staff read access, controlled only by the audit log.** Simplest: hold the permission, read
any account, every read logged. Rejected because retrospective control is control nobody exercises,
and because "who looked at my data, and why" is unanswerable when the answer is "anyone, at any time,
for no stated reason".

**Requiring a staff identity in the CLI.** Every CLI action would carry a real subject and the
attribution gap would close. Rejected because it makes the emergency path depend on the identity
provider being reachable — and the identity provider is very often part of what is broken when
somebody reaches for the emergency path.

## Capabilities

### New Capabilities

- `staff-authorization`: the staff plane — staff identity from our own identity provider, flat named
  permissions mapped from group claims, an evaluator with no object scoping, and the operations that
  have no tenant equivalent: suspension, credit adjustment, account lifecycle, ownership recovery and
  platform administration.
- `support-access`: time-boxed, reason-required access grants into one account; `ScopeForStaffAccess`
  as the sole bridge between the two planes; per-request re-evaluation; and the recording of staff
  reads as their own class of activity.

### Modified Capabilities

- `audit-trail`: entries gain a presented identity distinct from the real subject, so staff action is
  attributable to us internally and to `Support` externally; staff and command-line action inside a
  tenant's inventory becomes readable by that tenant.
- `service-inventory`: the command-line administration surface is narrowed to bootstrap and emergency
  use and documented as break-glass, rather than being an ordinary way to administer inventory.

## Impact

- **`internal/authz`.** Retained as the staff plane. `Mapping`, `RolesFor`, `Can` and the per-request
  derivation are unchanged in shape; the `Action` constants and the permission matrix are replaced.
  The four tenant role names leave with `tenancy-and-permissions`.
- **`internal/store/scope.go`.** Gains `ScopeForStaffAccess`, its third and final producer. The type's
  guarantee — no construction from a caller-supplied identifier — is unchanged.
- **New schema.** `staff_access` for the grants, and a staff activity table for reads. `audit_entries`
  gains presented-identity columns and a tenant-visibility flag.
- **`internal/adminctl`.** Documentation narrows it to bootstrap and emergency; entries it writes that
  touch tenant inventory become tenant-visible.
- **New routes.** A staff surface for opening and closing access, suspension, credits, lifecycle and
  platform administration — served on the existing management listener, gated by the staff plane.
- **Nothing on the DNS or challenge path.** `internal/dnsserver`, `internal/zoneview` and the challenge
  API are untouched.

## Dependencies

This change depends on `tenancy-and-permissions` and assumes from it:

- **The `account` entity** exists and owns every project, with ownership materialized as an
  `account_id` column whose agreement with its parent is enforced by composite foreign keys.
- **`Scope`** exists, is required by every inventory query, and has no exported constructor from a
  bare identifier — this change adds a producer to it and would be meaningless without it.
- **The tenant permission model** — anchored triples, inheritance, the subset rule and the
  unrestricted role's last-member guard — exists and is what this plane deliberately does not use.
- **The unrestricted role's last-member invariant** is what creates the recovery case this change
  answers.
