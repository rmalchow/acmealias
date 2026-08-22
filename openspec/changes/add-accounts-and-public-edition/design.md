## Context

The service today has exactly one kind of user: an operator of ours, who signed in through our
Keycloak, and who is trusted with everything the service knows. Design decision D10 of
`add-acme-alias-service` states the consequence plainly — roles are global, there is no
per-project scoping — and D4 describes an inventory (`projects → load_balancers → {nodes, domains,
aliases, api_keys}`) with no owner above `projects`.

This change adds a second kind of user: a stranger who signed up on a marketing page, whose
trust boundary is their own account and nothing else. The decisions below are about doing that
without forking the service and without weakening the internal edition.

Terms used throughout, defined once:

- **Edition** — which product a running deployment is. `internal` (our operators, OIDC, one
  organisation) or `public` (strangers, local accounts, billing). Selected by configuration at
  startup, not at build time.
- **Account** — the owner of inventory, and the boundary of every authorization decision. One
  per signup in the public edition; exactly one, ever, in the internal edition.
- **Membership** — a person's role *in one account*. Replaces today's global role, which becomes
  the special case "membership in the only account there is".
- **Entitlement** — what an account's plan permits: quotas, and whether it is suspended.

## Goals / Non-Goals

**Goals**

- One codebase and one binary for both editions, with the DNS-01 machinery shared literally
  rather than by copy.
- Tenant isolation that cannot be forgotten in a query, and whose failure mode in tests is a
  compile error rather than a silent disclosure.
- The internal edition's behaviour unchanged, observably: same roles, same screens, same
  answers, no account visible anywhere.
- The public edition's additions — signup, email confirmation, plans, payment — additive, so a
  decision not to commercialise costs nothing already spent.

**Non-Goals**

- Per-project or per-domain roles inside an account.
- Usage metering or usage-based pricing.
- Certificate issuance. We answer challenges; we do not hold private keys.
- Running both editions from one deployment or one database.
- Migrating internal inventory into multiple accounts. There is one, and everything is in it.

## Decisions

### E1. One binary, edition chosen by configuration

`ACME_EDITION` is read at startup and selects which implementations are wired into the
composition root in `cmd/acmeservice/main.go`:

| Seam | `internal` | `public` |
| --- | --- | --- |
| Authentication | `oidcauth` (Keycloak, today's flow) | `localauth` (email + password + confirmation) |
| Account resolution | the single seeded account | the caller's memberships |
| Entitlements | unlimited plan | the account's plan |
| Billing | no-op adapter | Stripe adapter |
| Console shell | today's three screens | plus signup, plan, billing |

The value is reported in the startup log line and on `/readyz`, so an operator looking at a
running process can see which policy it holds.

**Rejected: build tags.** Two binaries would make it impossible for public code to run
internally, which sounds like the stronger guarantee. It is the weaker one in practice: a
compile-time switch over *authorization* is the class of change that ships wrong, because the
wrong build is indistinguishable from the right one until someone reads data they should not.
It also doubles the test matrix for the shared 90% — the part that must be tested once, well.
Configuration is inspectable; a build tag is archaeology.

**Rejected: two deployments of two codebases.** See the proposal. The shared part is the
dangerous part.

### E2. The account sits above `projects`, and ownership is inherited

```
accounts
  └── projects           (account_id, NOT NULL)
        └── load_balancers
              ├── lb_nodes
              ├── domains ── aliases
              ├── api_keys
              └── challenge_interactions
```

Only `projects` gains a column. Everything below inherits its owner through the load balancer, exactly
as it already inherits its project. A domain does not carry an `account_id` because a domain
whose account disagreed with its load balancer's account would be a state with no meaning and no way
to resolve it — the kind of denormalisation that turns one bug into two rows that each look
right on their own.

The internal edition's migration inserts one account (slug `internal`, name taken from
`ACME_ORG_NAME` or defaulted) and backfills every existing project to it. `account_id` is then
`NOT NULL` for everyone.

**Rejected: `account_id` on every table.** Faster queries, no joins for scoping. Rejected
because it introduces the possibility of disagreement between a row and its parent, and the
only way to keep them in step is a trigger or discipline. A join through `load_balancers` is
cheap and cannot disagree with itself.

**Rejected: an account per project (i.e. renaming `projects` to `accounts`).** Superficially it
removes a level. It fails because a public customer with two projects — staging and production,
say — would need two logins, two subscriptions and two invoices for one company.

### E3. Scoping is a helper every query passes through, not a convention

The failure mode being designed against is "somebody wrote a new query and forgot the filter".
Conventions do not survive that; signatures do.

Inventory queries take a `Scope` and cannot be called without one:

```go
// internal/store/scope.go
type Scope struct{ accountID int64 }

// Everything reachable from an account, as a query the caller must build on.
func (s Scope) Projects(db *gorm.DB) *gorm.DB {
    return db.Where("projects.account_id = ?", s.accountID)
}

// Load balancers, domains, keys and interactions all reach the account through the load balancer.
func (s Scope) LoadBalancers(db *gorm.DB) *gorm.DB {
    return db.Joins("JOIN projects ON projects.id = load_balancers.project_id").
        Where("projects.account_id = ?", s.accountID)
}
```

A `Scope` is produced in exactly two places: the authenticated request path (from the caller's
membership) and the internal edition's bootstrap (the single account). It has no exported
constructor from a bare integer, so a handler cannot conjure one out of a path parameter — the
mistake that turns `GET /loadbalancers/41` into "any load balancer, if you guess the id".

**Rejected for now: Postgres row-level security as the enforcement.** Attractive: the database
refuses to return the row even if Go forgets. Three problems, none fatal but together decisive
for a first step. It needs the account on the *connection*, which fights a pooled `*sql.DB`
and requires `SET LOCAL` inside every transaction. GORM's `Preload` issues separate statements,
each of which must carry it. And the internal edition would run with a policy that is
effectively always-true, so the mechanism protecting the public edition would be the one nobody
exercises daily. Revisit as **defence in depth** once the Go-side scope is in place and the
public edition has real data; the two are not alternatives, and the second is much easier to
add when the first already draws the boundary in the same place.

### E4. Authorization takes a scope; the role table does not change

`authz.Allowed(roles)` becomes `authz.Allowed(roles, scope)`. The four roles, their names and
the permission matrix from D10 are untouched — what changes is that holding `CONFIG_ADMIN` is
now a fact about a person *in an account*, not about a person.

- **Internal edition.** Group claims map to roles exactly as today (`ACME_ADMIN_GROUP` and
  friends), and the resulting roles are granted on the single account. An operator sees no
  difference, and the group-to-role configuration keeps working unchanged.
- **Public edition.** Membership rows carry the role. The account creator is `ADMIN` of their
  own account; invited colleagues get one of the same four roles. Nothing new to learn, and the
  console's role-gating code is already written against these names.

The API-key path needs no change of concept: a key already belongs to a load balancer, which now
belongs to an account, so a key is account-scoped by construction. What it gains is an
entitlement check (E5) and a suspension check.

### E5. Entitlements are checked at create time, separately from billing

A plan is a row; an account points at one. Quotas are counts the service can check cheaply
before creating something:

| Quota | Why it exists |
| --- | --- |
| domains | the headline unit customers understand |
| load_balancers | bounds fan-out of the DNS plan |
| api_keys | bounds credential sprawl per customer |
| live challenge values | **abuse control**: one account cannot fill the zone view |
| publishes per month | catches a runaway client before it becomes an incident |

The first three are product; the last two are protection, and they are the reason entitlements
are a separate seam from billing rather than part of the Stripe adapter. A free tier with no
card attached still needs the last two on day one, and the internal edition wants the
protection with none of the product.

Suspension is a state on the account, not a plan of zero: a suspended account must keep
answering `/present` for a grace period rather than failing renewals the hour a card expires.
Certificates outlive invoices, and a service that breaks TLS over a billing hiccup is a service
nobody recommends.

### E6. Alias labels are a setting, and the public edition leaves it at its default

> **Superseded in mechanism by the change `generate-alias-labels`, decision A6.** The reasoning
> below is unchanged and is why the default is what it is; what changed is that label style is
> no longer a difference in *edition behaviour*. It is one setting, `ACME_ALIAS_USE_DOMAIN`,
> which defaults to `false` (generated, unguessable labels). The public edition simply leaves it
> at the default — and must. There is no edition-specific label code path to build here.

Before that change an alias label was derived from the domain: `demo.example.com` becomes
`demo-example-com`, and the customer publishes

```
_acme-challenge.demo.example.com.  CNAME  demo-example-com.acmeservice.cloudpowered.services.
```

That is excellent for a single-organisation deployment — the label is self-describing in logs,
in the DNS plan, and in the interaction history, which is exactly why
`ACME_ALIAS_USE_DOMAIN=true` remains available. Serving mutually untrusting customers it is two
problems at once:

1. **Collision.** Two accounts adding `demo.example.com` derive the same label. The label is the
   key the challenge store and zone view are built on, so this is not a cosmetic clash.
2. **Enumeration.** Our zone becomes a public list of our customers' domains. Anyone can walk
   it, and the alias name itself reveals the customer domain to anyone who sees the CNAME —
   including in the customer's own zone, where it is world-readable by design.

A public deployment therefore runs with the default: an opaque random token (16 lowercase base32
characters, 80 bits, e.g. `k7m3q9x2r4tv8w1p`), assigned by the service and choosable by nobody,
customer included. The domain it belongs to is recorded in our inventory, where it is readable
by its owner and by nobody else.

Nothing about this is edition-specific machinery. An internal deployment that wants unguessable
labels gets them the same way — by leaving the setting alone — and one that wants readable ones
sets `ACME_ALIAS_USE_DOMAIN=true`. Either way the labelling of aliases that already exist never
changes, so no deployment can break a published CNAME by flipping it.

**Rejected: tenant-prefixed derived labels** (`acct7-demo-example-com`). Solves the collision,
not the enumeration, and leaks the account count as a bonus.

### E7. Domain ownership proves itself, but unverified domains earn nothing

There is no separate ownership check to build, and that is a genuinely nice property of DNS-01
aliasing: the customer cannot make the alias work without publishing a CNAME in a zone they
control. Control *is* the mechanism.

But anyone can *add* `google.com` to their account. So the rule is that an unverified domain is
inert: it does not consume quota in a way that matters, it never appears in anyone else's plan,
and it holds no claim on a label that another account could not also hold. Verification is the
existing live check (`GET /loadbalancers/:id/verification`), which already answers exactly this
question.

This also makes "try now" honest: the moment the CNAME resolves, the console says so, and that
moment is the customer's proof and ours at the same time.

### E8. The marketing site is a separate deployment

The console is embedded in the Go binary (D9), which is right for an administration UI that must
be delivered with and versioned with the service. Every reason for that is inverted for a
marketing site: it changes for commercial reasons on a commercial cadence, it must serve people
who have no account and no session, and a copy change must never require redeploying the DNS
service.

Static site, its own pipeline, its own domain. "Try now" links into the public edition's signup
route. The only coupling is that link and a shared visual language.

## Risks / Trade-offs

- **The scoping change touches every inventory query.** Mitigated by making it a signature
  change: the compiler enumerates the work. The risk that remains is a query written *after*
  this change that takes a `Scope` and then ignores it; an integration test that runs the whole
  management API as two accounts and asserts each sees only its own rows is the backstop, and it
  belongs in this change rather than a later one.
- **Two editions means two paths that can rot.** The internal edition is exercised daily by us;
  the public one might not be, before launch. The acceptance harness must run both editions, or
  the unexercised path will be broken when it matters.
- **Suspension is a business decision encoded in code.** Grace periods, what a suspended account
  may still do, and when renewals actually stop are judgement calls with customer-visible
  consequences. They are configuration, not constants, and they need an owner outside
  engineering.
- **Local password authentication is a liability we do not have today.** Argon2id, rate
  limiting, credential-stuffing resistance, and a reset flow that cannot be used for account
  takeover are all table stakes and none of them is free. An alternative worth revisiting before
  building it: outsource public identity to a hosted IdP and keep `oidcauth` as the only
  authentication code we own.
- **Email becomes load-bearing.** Confirmation and reset mean a bounced or delayed message is a
  customer who cannot get in. Deliverability is an operational commitment, not a library.

## Migration Plan

The order matters, and the first two steps are the ones with a deadline:

1. **Accounts and scoping, internal edition only.** Migration adds `accounts`,
   `account_members`, and `projects.account_id`; a single account is seeded and backfilled.
   Queries take a `Scope`. Observable behaviour: none. This is the step that is cheap now and
   expensive later.
2. **Scoped authorization.** `authz.Allowed` takes the scope; group-to-role mapping grants roles
   on the single account. Observable behaviour: still none.
3. **The identity seam.** `oidcauth` moves behind an interface with no change to its behaviour;
   `localauth` is added but wired only in the public edition.
4. **Entitlements.** Plans, quotas, the create-time check, and the two protective quotas. The
   internal edition gets the unlimited plan.
5. **Billing.** Stripe adapter, webhooks, subscription state, suspension with a grace period.
6. **Public console and marketing site.** Signup, plan and billing screens; the static site and
   the "try now" flow.

Steps 1 and 2 are worth doing whatever happens commercially. Steps 3 onward should wait for a
decision that the public edition is actually being built.

## Open Questions

- **Hosted IdP for public identity?** Building `localauth` means owning password storage, reset
  flows and credential-stuffing defence. Delegating to a hosted provider keeps the number of
  authentication implementations we own at one. What does it cost, and does it constrain
  signup UX in ways marketing would object to?
- **What may a suspended account do?** Continue answering `/present` indefinitely (never break
  TLS), for a fixed grace period, or not at all? This decides whether a lapsed customer's site
  goes dark, and it is not an engineering call.
- **Free tier or trial only?** A free tier needs the protective quotas to be right on day one
  and invites abuse; a time-limited trial needs a card up front and loses "try now" as a
  frictionless promise.
- **Does the public edition share our zone?** One zone for both editions is simpler to operate
  and means one set of NS records; separate zones mean a public incident cannot touch internal
  aliases. Leaning separate, but it doubles a piece of DNS infrastructure.
- **Does a public customer ever see this level of the inventory?** Either the screens expose it
  the way the internal console does, or a public account gets one default load balancer it never
  names and adds domains straight to it. This is a decision about screens, not about vocabulary:
  the naming half is settled by `rename-lb-pair-to-load-balancer`, which gave the level a name a
  stranger with one server already owns, so exposing it no longer costs a glossary entry — but
  that lowers the price of showing it without deciding to show it.
