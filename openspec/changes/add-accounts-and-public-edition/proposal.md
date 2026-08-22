## Why

### Two products, one service

The service we have answers ACME DNS-01 challenges for domains whose DNS we do not administer.
A customer publishes one `CNAME` record once, and from then on their load balancer can prove
control of that domain to a certificate authority by asking us, instead of by holding write
credentials for the customer's DNS zone.

That mechanism is worth selling twice, to two audiences that share nothing else:

- **The internal edition** — what exists today. Our own operators, signing in through our own
  identity provider, administering our own fleet. Everyone who can sign in is trusted; the four
  roles are global (a `CONFIG_ADMIN` is a `CONFIG_ADMIN` everywhere) and that is exactly right,
  because there is one organisation and it is us.

- **The public edition** — the same DNS-01 machinery offered to strangers. Someone finds a
  marketing page, clicks "try now", signs up with an email address, confirms it, adds a domain,
  publishes the CNAME we show them, and pays us monthly. They must never see, or be able to
  reach, another customer's anything.

The question this change answers is *how* to have both without maintaining the hard part twice.

### What "the hard part" actually is

Almost everything in this service is identical for both audiences. The alias naming rules
(`internal/dnsname`), the wildcard validation, the in-memory zone view the DNS server answers
from, the challenge store, the propagation-safe publish/withdraw ordering, the generated DNS
plan and its live verification, the interaction history that tells you whether the CA ever came
to look — none of that changes because the person paying is a stranger rather than a colleague.
It is also the part that took the longest to get right and is the most dangerous to get wrong.

What differs is a thin ring around it: who you are, whose rows you may touch, how much you are
allowed to have, and who pays.

So this change is **not** "build a SaaS". It is: introduce the small number of seams that let
one codebase serve both, and — the part with a deadline on it — get the data model ready
*before* there is public data in it.

### The one change that gets more expensive every week

Today authorization is a global role check. `authz.Allowed(roles)` asks what a person may do;
it never asks *to what*. The spec says so in as many words: "Roles are global and fixed. There
is no per-project scoping." Every query in `internal/store` reads its whole table, because
every signed-in human is entitled to the whole table.

For the internal edition that is correct and should stay correct. For a public one it is a
data breach waiting for its first customer. Consider two accounts that both add
`demo.example.com` — a perfectly ordinary collision, because `example.com` is reserved for
documentation and half the internet's test data uses it. Today the service would:

1. derive the same alias label, `demo-example-com`, for both, and collide on it;
2. list both customers' projects to whichever of them opened the console;
3. show one customer's load balancer node addresses (`203.0.113.10`, `203.0.113.11`) to the other in
   a generated DNS plan.

None of those is a bug in a specific query. They are the absence of a concept: **there is no
owner above a project.** Adding one later, to a live public service, means auditing every query
ever written and hoping none was missed — where "missed" means cross-tenant disclosure.

Adding it now, while there is one operator organisation and a handful of rows, is a migration
and a compile error. That asymmetry is the whole argument for doing this first.

### Why now

The internal edition is close to done and its shape is stable. Nothing about the public edition
is committed to commercially — but the two decisions that constrain everything else (an owner
above `projects`, and authorization that takes a scope) are cheap today and expensive after the
first paying stranger. This change lands those two, defines the seams for the rest, and leaves
signup, billing and marketing genuinely optional.

## What Changes

### An account owns everything

A new entity, the **account**, sits above `projects`. Every project belongs to exactly one
account; load balancers, nodes, domains, aliases, API keys, challenges and interaction history all
inherit their owner through `project → load_balancer`. Nothing else in the hierarchy changes.

The internal edition runs with exactly one account, seeded at migration time, invisible in the
console. Its behaviour is unchanged: one organisation, four global roles, everyone sees
everything — because "everything" is now "everything in the single account", which is the same
set.

The public edition creates one account per signup. A person may belong to several (an agency
managing two clients) and holds a role *per account*.

### Authorization takes a scope

`authz.Allowed(roles)` becomes `authz.Allowed(roles, scope)`, where the scope is the account
being touched. Every management route resolves the account of the object it is about and passes
it. The internal edition supplies the single account and maps IdP groups to roles on it exactly
as today; no operator sees a difference.

Query scoping is enforced in **one** place — a helper every store query passes through — rather
than by remembering to add a `WHERE account_id = ?` two hundred times. Getting this wrong once
is a disclosure, so it must not be a thing anyone has to remember.

### Identity becomes a seam

The existing OIDC sign-in (`internal/oidcauth`) becomes one implementation of an interface. A
second, `internal/localauth`, adds what a public edition needs: email and password with argon2id
hashing, an email-confirmation token, password reset, and later optional social sign-in. Both
produce the same thing — a server-side session carrying a subject, a display name and a set of
account memberships. The session machinery, the CSRF protection and the console's sign-in flow
are unchanged.

### Entitlements, then billing

A **plan** on each account carries quotas: domains, load balancers, API keys, live challenge values,
publishes per month. A single entitlements check runs at create time. The internal edition gets
an unlimited plan.

Billing is a separate seam behind that: a Stripe adapter turning checkout and webhooks into
subscription state, and subscription state into plan and suspension. Quotas are useful — as
abuse control — before anyone is charged, which is why they are not the same seam.

### A marketing site that is not in the binary

Today the compiled Angular console is embedded in the Go binary, which is right for an
administration UI delivered with the service. A marketing site is the opposite: it changes
weekly for reasons that have nothing to do with the service, and it must be reachable by people
who have no account. It is a separate static deployment.

"Try now" then has a satisfying shape using only endpoints that already exist: sign up, add a
domain, we show the CNAME, `GET /loadbalancers/:id/verification` polls until it is live, and the
customer sees "your CNAME is correct — this domain is ready" within a minute of arriving. No
certificate is issued, because issuing certificates is not what this service does.

### Editions are selected by configuration

One binary. `ACME_EDITION=internal|public` at startup wires the identity, entitlement and
billing implementations. Two deployments, two databases, two zones — never one deployment
serving both.

### Not in scope

- Per-project or per-domain roles inside an account. Roles stay account-scoped; a member of an
  account can act on everything in it. Finer granularity is a later change if customers ask.
- Migrating existing internal data to a different owner. There is one account and everything is
  in it.
- Certificate issuance, storage or distribution. Unchanged: we answer challenges.
- Social sign-in, SSO for public customers, SCIM. The identity seam makes them additive.
- Usage-based pricing. Plans are flat with quotas; metering is a later change and depends on
  the challenge history that `add-audit-and-usage-reporting` specifies.

### What we considered and rejected

**Two codebases (a fork).** The obvious way to keep a public product from destabilising an
internal one. Rejected: the shared 90% is the DNS-01 correctness — propagation ordering,
wildcard rules, alias resolution, the answer-narrowly DNS server — and a fork means two copies
of it, diverging within a quarter, each with its own subtle bug. The 10% that differs is
precisely the part that is easy to isolate behind interfaces.

**Build tags, producing two binaries.** Tempting because it makes it impossible for public code
to run internally. Rejected: a compile-time flag that changes *authorization* is exactly the
kind of thing that ships wrong, the test matrix doubles for the code that most needs testing
once, and an operator debugging production cannot tell from the binary which policy it holds.
Configuration is inspectable at runtime; a build tag is archaeology.

**One deployment serving both audiences, separated by account.** The purest multi-tenancy
reading. Rejected on blast radius and obligation: one database would hold our own fleet's
inventory next to strangers' billing records, with one retention policy, one backup, one
compromise. The internal edition would inherit the public edition's compliance surface for
nothing.

**Row-level security in Postgres instead of a scoping helper in Go.** Genuinely attractive —
the database enforces isolation even if application code forgets. Not rejected outright, but
deferred to a decision inside the design: it interacts awkwardly with a connection pool and
with GORM's `Preload`, and it must not become the *only* barrier, because the internal edition
would then run with a policy nobody exercises.

**Keeping global roles and adding an account filter in the console.** The cheapest-looking
option. Rejected because a filter in the browser is not an authorization boundary; the API
would still answer with everything to anyone who asked it directly.

## Capabilities

### New Capabilities

- **accounts-and-tenancy** — the account entity, ownership of every inventory object,
  account-scoped queries and the single-account internal edition.
- **subscription-and-entitlements** — plans, quotas enforced at create time, subscription state
  and suspension, and the billing adapter seam.

### Modified Capabilities

- **identity-and-roles** — authentication becomes a seam with two implementations; roles become
  account-scoped memberships rather than global facts; local accounts with email confirmation
  and password reset are added for the public edition.
- **service-inventory** — every project belongs to an account; alias labels stop being derived
  from the domain name in the public edition; inventory queries are scoped.
- **admin-console** — the console learns which account it is acting in, and the public edition's
  console shows signup, plan and billing state.
- **service-operations** — two editions, two deployments, two databases, one image; edition is
  configuration and is reported at startup and on the health endpoints.

## Impact

- **Schema.** New `accounts`, `account_members`, `plans`/`subscriptions` tables; `account_id`
  added to `projects` and backfilled to the single internal account. No other table changes
  shape — ownership is inherited, not copied.
- **Every store query.** Mechanical but broad: each inventory query gains a scope argument. The
  compiler finds them all, which is the point of doing it as a signature change rather than as a
  convention.
- **`internal/authz`.** The permission check gains a scope; the role table and the four role
  names are unchanged.
- **The console.** Route shapes gain an account for the public edition; the breadcrumb block
  introduced by the console rework gains a level. The internal edition renders neither.
- **Operations.** A second deployment, a second zone, a second database, and a marketing site
  with its own release cadence.
- **Nothing on the DNS or challenge path.** `internal/dnsserver`, `internal/zoneview` and the
  challenge API keep their shape; the challenge API's authorization is already per-key and
  per-load-balancer, which is already account-scoped once a load balancer has an owner.
