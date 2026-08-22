## Context

`tenancy-and-permissions` gives every management operation exactly one account to act in, confined by
a `Scope` value that cannot be built from a caller-supplied identifier. That is deliberate and it is
absolute: there is no expression in the tenant permission language that names an object of another
account.

This change is about the people who nevertheless have to. Support engineers diagnosing a customer's
CNAME, abuse handlers suspending an account, billing operators granting a credit, platform engineers
enrolling a satellite, and whoever has to repair an account whose only administrator has left.

Today all of that is done by `internal/adminctl` against the datastore, authorized by possession of
the connection string. That works while every row belongs to us and stops working the day one does
not.

Terms used throughout, defined once:

- **Tenant plane** — the authorization system specified by `tenancy-and-permissions`: object-scoped
  permission triples, held by account members and API keys.
- **Staff plane** — the system specified here: flat named permissions, held by our own employees,
  derived from identity-provider group claims.
- **Access grant** — a `staff_access` row admitting one staff subject into one account for a bounded
  time with a stated reason. Not a membership, and not a permission.
- **Presented identity** — who an audit entry says acted, as shown to the tenant (`Support`), as
  opposed to the real subject recorded for our own accountability.
- **Break-glass** — the command-line path, authorized by possession of database credentials, kept
  for the case where the service itself is unavailable.

Constraints that shaped everything below:

1. **A bug in the tenant evaluator must never produce cross-tenant access.** This is the reason there
   are two planes rather than one generalised one.
2. **The emergency path must survive the service being broken.** Whatever authorizes normal staff work
   cannot be a prerequisite for repairing an outage.
3. **A tenant must be able to see that we looked**, without learning which of us did.
4. **Staff read volume must not distort the audit trail**, which is retained for mutations.

## Goals / Non-Goals

**Goals:**

- An authorization plane for our own employees with no path through the tenant evaluator.
- Exactly one bridge between the planes, at the query layer, typed rather than checked.
- Access into a tenant that is bounded in time, carries a stated reason, and is visible to the tenant.
- A break-glass path that remains usable when everything else is not, with its residual risk stated
  rather than obscured.

**Non-Goals:**

- Changing how tenants authorize themselves.
- A staff console. This specifies the plane, not its screens.
- Approval workflows, customer-initiated support sessions, or staff access expressed as tenant
  memberships.

## Decisions

### S1. Two planes, and the staff plane is today's `internal/authz` with a new vocabulary

The staff plane needs: subjects authenticated against our own identity provider, group claims mapped
to a flat set of named permissions by configuration, no object scoping, no inheritance, no patterns,
no subset rule. That is a precise description of `internal/authz` as it stands.

So the package is kept rather than deleted, and only its `Action` constants and the `permissions`
matrix change. `Mapping.RolesFor(groups)` keeps working; the refusal of an authenticated user who
maps to no permission, with the observed groups reported so a "Full group path" misconfiguration can
be diagnosed, keeps working; per-request derivation keeps working.

The cost of "two authorization systems to maintain" is therefore much lower than it appears: system
two is the one that already exists and is already tested, and `tenancy-and-permissions` is building
system one regardless. What would be genuinely expensive — two object-scoped evaluators with two
inheritance walks and two subset rules — is not what this is.

**Rejected: one generalised evaluator with an instance level above the account.** See the proposal.
The short form: it converts "cross-tenant access has no representation" into "cross-tenant access is
a row you can write", and the two failure modes stop being separable.

### S2. Staff permissions are flat, and that is a property to preserve, not a limitation to fix

There is no `[account, 42, support.read]` on this plane. A staff permission is a bare name.

The temptation to add object scoping here will recur — "why can this support engineer open access to
any account rather than only European ones?" — and it should be resisted, because the moment staff
permissions take objects, they need an inheritance model, and then a subset rule, and then they are
the tenant evaluator with a second implementation. Scoping *which accounts* a staff member may reach
belongs in the access grant (S3), which is a row with an expiry and a reason, not in the permission.

### S3. Access grants are the scoping mechanism, and they expire

```sql
CREATE TABLE staff_access (
  id            uuid   PRIMARY KEY DEFAULT uuidv7(),
  staff_subject text NOT NULL,
  account_id    uuid   NOT NULL REFERENCES accounts (id),
  reason        text NOT NULL,
  opened_at     timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  closed_at     timestamptz NULL
);
```

`support.access.open` permits *opening* one of these. It does not itself permit reading anything.
An open, unexpired, unclosed row for `(subject, account)` is what admits a request, and it admits it
to that account only.

`reason` is `NOT NULL` and non-empty. A reason nobody can be made to write is a column of empty
strings; a reason that is structurally required is the thing that makes an access review possible a
year later.

**Evaluated per request, never cached on the session.** The precedent is explicit in
`internal/authz/middleware.go`:

> *"Roles are derived per request and left in the Echo context, never stored on the session row: the
> session records the groups as seen at sign-in, and the mapping is read from the running
> configuration, so neither is cached here."*

An access grant expires on a wall clock and can be closed early. Materializing it at sign-in would
make a thirty-minute grant last as long as the session, which is the one property it exists to deny.

### S4. `ScopeForStaffAccess` is the only bridge, and it is a type, not a check

```go
// internal/store/scope.go
func ScopeFor(m *Membership) Scope                        // tenant request path
func ScopeForSingleAccount() Scope                        // internal edition bootstrap
func ScopeForStaffAccess(a *StaffAccess) (Scope, error)   // this change — the third and last
```

`ScopeForStaffAccess` returns an error unless the grant is open, unexpired and unclosed. Given one,
it returns a `Scope` for that account, and the staff handler then calls **the tenant's own store
queries** with it.

The consequence worth being explicit about: staff code never calls `Can(subject, action, object)`.
It cannot accidentally benefit from a wildcard that matches too much or an inheritance walk that goes
one level too far, because it never enters that code. The isolation boundary is a value that must be
produced, and there are exactly three ways to produce one.

This also means staff read paths need no separate query layer. A support engineer looking at account
42's load balancers runs `store.ListLoadBalancers(ctx, db, scope)` — the same function, the same
filters, the same confinement.

### S5. Audit entries carry two identities, in two columns

`audit_entries` gains a presented identity alongside the real one, and a flag for whether the entry
is visible to the tenant:

| Column | Holds | Read by |
| --- | --- | --- |
| `actor_subject`, `actor_name` | the real employee | us |
| `presented_actor`, `presented_reason` | `Support`, `"ticket #1183"` | the tenant |
| `tenant_visible` | whether the tenant's trail includes it | both |

The tenant's audit view selects the presented pair and never the real one.

**Rejected: one identity, redacted when rendering the tenant's view.** Half the columns, no
duplication, and the real subject is always available. Rejected because it makes disclosure the
default and correctness a property of every read path — a listing, a filter, a CSV export, an
endpoint written next year by someone who has not read this document. A missing `tenant_visible`
predicate returns too many rows, which is noticeable; a missing redaction returns the right rows with
an employee's name in them, which is not noticeable until it is in a customer's inbox. Two columns
fail safe.

### S6. Staff reads are recorded separately from the audit trail

The `audit-trail` capability records *"every creation, modification and deletion"* and deliberately
not reads. That is a volume decision as much as a semantic one.

Staff reads must be attributable, and they are the high-volume path: a support engineer clicking
through a console for thirty minutes generates hundreds of them. Writing those into `audit_entries`
would grow a table whose retention was sized for changes by an order of magnitude, and would bury the
mutations it exists to make findable.

So staff read activity is its own store, and it may be recorded **per access grant** — what was
opened, by whom, for what reason, over which window, with a count and a summary of what was reached —
rather than one row per HTTP request. The tenant-visible statement *"Support accessed your account
on 22 August, ticket #1183"* is a statement about a grant, not about a request; recording it at grant
granularity answers the question that is actually asked, at a thousandth of the volume.

Staff *writes* remain in `audit_entries` with everything else, because they are changes and belong
next to the changes they are indistinguishable from in effect.

### S7. Recovery is the escape hatch, and it is deliberately auditable rather than deliberately constrained

`account.ownership.reassign` grants a member of an account the unrestricted role, or creates a
membership where none can be created from inside. It bypasses the subset rule and the last-member
guard, because it exists precisely for the states those rules produce.

There is no way to constrain it into safety — a rule that could stop it being misused could also stop
it being used, and then the account stays broken. What makes it safe instead is that it is narrow
(one action, one account, one membership), rarely held, always attributable to a real employee, and
always visible to the tenant. It is the clearest case in the system where the audit trail is not a
record of the control but *is* the control.

### S8. The break-glass path keeps its authority and loses its invisibility

`internal/adminctl` stays as it is: no authentication, full authority, `SourceCLI` in the audit
trail, usable with nothing running but a database. Constraint 2 requires it — an authenticated staff
API cannot be a prerequisite for repairing the thing that serves the authenticated staff API.

The residual risk is stated and not mitigated: **the connection string is unlimited tenant access.**

One change comes with it. `SourceCLI` entries touching a tenant's inventory become `tenant_visible`,
attributed like any other staff action. Without that, the emergency path is the transparency bypass:
the way to touch a customer's data unobserved is to use the tool that skips the observation. Making
CLI action visible costs nothing operationally and removes an incentive that points the wrong way.

Documentation narrows its stated purpose to bootstrap and emergency. The subcommands remain, because
removing them would not remove the authority — the connection string is the authority — and would
only remove the audited way to exercise it.

### S9. Staff routes are served on the existing management listener

`service-operations` specifies three HTTP surfaces on separate listeners. Staff routes go on the
management listener rather than a fourth, because they are management operations differing only in
who is authorized, and because a listener is an operational object — a port, a firewall rule, a
health check, a certificate — that should not multiply without cause.

They are distinguished by path prefix and by the middleware that gates them, which is the staff
plane's guard rather than the tenant's.

## Risks / Trade-offs

- **Standing platform permissions become a de facto superuser** → `platform.admin` grants no access
  to any tenant's inventory; reaching a tenant always requires an access grant, which always requires
  a reason and always expires.
- **Reason strings degenerate into `"support"`** → the field is required, recorded, tenant-visible and
  reviewable; a low-quality reason is visible as a low-quality reason, which is a management problem
  rather than a structural one. Requiring a ticket-shaped reason is available later without a schema
  change.
- **An access grant outlives its need** → expiry is mandatory and re-evaluated per request (S3);
  closing early is available; the tenant sees the window.
- **Staff read recording grows without bound anyway** → recorded per access grant rather than per
  request (S6), with its own retention independent of `audit_entries`.
- **A tenant-visible entry leaks an employee's identity** → separate columns rather than render-time
  redaction (S5); the tenant's view has no access to the real subject columns at the query level.
- **The break-glass path is used routinely because it is easier** → its subcommands stay, but its
  entries are tenant-visible and attributable to a host and OS user, and normal work has an
  authenticated surface that is more convenient than a database connection.
- **Recovery is abused to take over an account** → narrow, rarely held, attributable, tenant-visible
  (S7). This is the residual risk of having an escape hatch at all, and the alternative — no escape
  hatch — is worse.

## Migration Plan

1. Replace the `Action` constants and permission matrix in `internal/authz` with the staff vocabulary;
   repoint the `ACME_*_GROUP` settings at staff permissions.
2. Create `staff_access` and the staff activity store.
3. Add `presented_actor`, `presented_reason` and `tenant_visible` to `audit_entries`; backfill
   existing rows as `tenant_visible = true` with a null presented identity, since every existing row
   was written by the tenant itself.
4. Add `ScopeForStaffAccess` and the guard middleware for staff routes.
5. Add the staff routes: open and close access, suspend, credit, lifecycle, ownership reassignment,
   platform administration.
6. Filter the tenant audit view to `tenant_visible`, selecting the presented identity.
7. Mark `SourceCLI` entries touching tenant inventory as `tenant_visible`; update `docs/` to describe
   the CLI as break-glass.

**Rollback**: the schema additions are additive and nullable; removing the staff routes and reverting
the `internal/authz` vocabulary restores the prior behaviour, since no tenant-plane code depends on
this change.

## Open Questions

- **Should opening access require a second employee's approval?** The structure supports it and the
  audit trail would show it. Left as policy: it is a change to who may call one route, not to what the
  route does.
- **How long should a default access grant last?** Thirty minutes is the working assumption. Short
  enough to require intent, long enough to finish a diagnosis; worth revisiting once there is data on
  how often grants are re-opened.
- **Does a tenant get notified, or only shown?** The entries are readable in their trail. Pushing a
  notification when support opens access is a stronger transparency posture and a possible source of
  alarm; deferred.
- **Where does staff read recording live physically** — its own table, or an events store shared with
  `metering-and-credits`? Both want append-only, high-volume, independently retained storage. Worth
  deciding once that change exists rather than inventing two.
