## Context

This change is what remains of `add-accounts-and-public-edition` once seven later changes took over
its structural half. Its decisions were numbered E1–E8; this document renumbers as U1–U8 and, where
a decision survives essentially intact, says so rather than presenting inherited reasoning as new.

Disposition of the original decisions, so nothing is lost silently:

| Original | Disposition |
| --- | --- |
| **E1** one binary, edition chosen by configuration | Survives as **U1**, unchanged in substance; the seam table is updated because two of its rows now point at other changes |
| **E2** the account sits above `projects`, ownership inherited | Superseded by `tenancy-and-permissions` (its P2 keeps the hierarchy and materializes the ancestor columns behind composite foreign keys) |
| **E3** scoping is a helper every query passes through | Superseded by `tenancy-and-permissions` (P1 keeps the `Scope`; P13 goes further and gates reads inside the query) |
| **E4** authorization takes a scope; the role table does not change | Contradicted by `tenancy-and-permissions`: the role table does change, into rows holding permission grants |
| **E5** entitlements checked at create time, separately from billing | Split: the product half is superseded by `metering-and-credits`; the protective half survives as **U6**; the payment half becomes **U5** |
| **E6** alias labels are a setting | Already superseded before this collapse, by the archived `generate-alias-labels` |
| **E7** domain ownership proves itself | Ownership half superseded by `tenancy-and-permissions`; onboarding half survives as **U8** |
| **E8** the marketing site is a separate deployment | Survives as **U7**, unchanged |

Terms used throughout, defined once:

- **Edition** — which product a running deployment is. `internal` (our operators, an identity
  provider, one organization) or `public` (strangers, local authentication, payment). Selected by
  configuration at startup, never at build time.
- **User** — a person. Global, exists outside every account, created by registration.
- **Account** — the billing entity that holds credits and owns projects. Created by a user;
  specified by `tenancy-and-permissions`.
- **Credit** — the prepaid unit deducted for metered activity. The ledger belongs to
  `metering-and-credits`; only its purchase belongs here.

## Goals / Non-Goals

**Goals:**

- One artifact serving both editions, with the difference inspectable at runtime rather than
  inferable from a build.
- A second authentication implementation that produces sessions indistinguishable from the first.
- A path from a marketing page to a verified domain that a stranger completes without contacting
  anyone.
- Money converted to credits through a provider, with no interface that lets a balance be typed in.
- Protection of the shared service that does not depend on whether an account has paid.

**Non-Goals:**

- Tenancy, permissions, roles, query confinement — `tenancy-and-permissions`.
- The credit ledger, accrual, overdraft, suspension, restoration — `metering-and-credits`.
- Invitations and the flow that reaches registration through one — `account-invitations`.
- Certificate issuance. We answer challenges and serve DNS; we hold no private keys.
- Running both editions from one deployment, one database, or one zone.

## Decisions

### U1. One artifact, edition chosen by configuration — E1 of `add-accounts-and-public-edition`, unchanged

`ACME_EDITION` is read at startup and selects which implementations are wired into the composition
root in `cmd/acmeservice/main.go`. The reasoning is inherited verbatim and is not re-argued; what
follows is only the seam table, updated because two of its rows now name other changes:

| Seam | `internal` | `public` | Owned by |
| --- | --- | --- | --- |
| Authentication | `oidcauth`, staff tenant | `oidcauth`, customer tenant | **this change** (U3, U4) |
| Account resolution | the single seeded account | the user's memberships and session selection | `tenancy-and-permissions` |
| Metering and credits | unmetered, unbilled | the credit ledger | `metering-and-credits`; the internal exemption is **U6** |
| Payment | none | a provider adapter funding credit purchases | **this change** (U5) |
| Console shell | today's screens | plus account creation and purchase | **this change** (U8) |

The edition in force is reported in the startup log and on the readiness endpoint, so an operator
looking at a running process can see which policy it holds. An unrecognized value fails startup
naming the setting and the accepted values, rather than defaulting — because the safe default is
ambiguous here: defaulting to `internal` would run a public deployment with no payment and no
limits, and defaulting to `public` would put a billing surface in front of our own operators.

**Rejected: build tags producing two binaries.** Inherited rejection, still correct. A compile-time
flag that changes *authorization* is the class of change that ships wrong, because the wrong build
is indistinguishable from the right one until someone reads data they should not. It also doubles
the test matrix for the shared code that most needs testing once. Configuration is inspectable at
runtime; a build tag is archaeology.

**Rejected: two codebases.** Inherited rejection. The shared 90% is the DNS-01 correctness —
propagation ordering, wildcard rules, alias resolution, the answer-narrowly DNS server — and a fork
means two copies of it, diverging within a quarter, each with its own subtle bug.

### U2. Editions never share a datastore or a zone

A deployment of one edition does not share its database with another, and each edition is
authoritative for its own DNS zone.

The blast-radius argument is inherited and worth restating in one sentence, because it is the whole
justification: one database would hold our own fleet's inventory next to strangers' payment records,
under one retention policy, one backup and one compromise, and the internal edition would inherit
the public edition's compliance surface for nothing.

Separate zones are the part that costs something — it doubles a piece of DNS infrastructure and a
set of `NS` records. The original left this as an open question; this change settles it as
*separate*, on the grounds that a public incident that poisons a zone view must not be able to touch
internal aliases, and the cost is a second delegation rather than a second design.

### U3. Authentication is delegated to the identity provider in both editions, and the seam stays for one reason

`internal/oidcauth` is the only authentication implementation. Both editions authenticate against
the same self-hosted identity provider at `auth.edgedns.eu`, differing in which of its tenants — its
brand, its enrollment flow, its user directory — they point at. There is no `internal/localauth`.

The contract is unchanged in substance: authentication produces a server-side session, and everything
downstream of a session — its storage, its idle and absolute expiry, its revocation, the cross-site
request forgery token bound to it — is a property of the session layer rather than of how the person
proved who they were. Sessions are rows with an opaque identifier in a cookie, and nothing about that
is OIDC-specific.

**The seam is kept even with one implementation behind it**, which needs justifying rather than
assuming. It is kept because it is the boundary at which the identity provider's vocabulary stops:
downstream code sees a subject and a session, never a token, a claim or a flow. That boundary is what
makes the provider replaceable, and it costs one interface. It is not kept in the expectation of a
second implementation — if one is ever wanted, it will be for a reason nobody has yet.

What a session *carries* is `tenancy-and-permissions`' business: a subject, and a selected account
which may be null. Someone who has just authenticated for the first time has no memberships and
therefore no selectable account, and that change already specifies that state as normal.

**This reverses the earlier decision, and the reversal is recorded rather than tidied away.** The
previous version of this design rejected exactly this arrangement — "the worst of both: we would own
the operational burden of an identity provider *and* a registration experience mediated by someone
else's screens" — and it was wrong on both halves. The operational burden is not marginal, because
the identity provider is being run regardless for staff and for the internal edition; adding
customers to it is a tenant, not a system. And the registration experience is not meaningfully ceded:
the provider's screens can be themed, and the part of onboarding that actually matters — publishing a
`CNAME`, adding a domain, watching it verify — is entirely ours and happens after sign-in.

### U4. The obligations do not disappear; they move, and they must be configured deliberately

Delegating authentication removes a great deal of code and none of the responsibility. What was a
list of things to *build* becomes a list of things to *configure and verify*, and configuration that
nobody checked is the more dangerous of the two, because it looks finished.

Each obligation, and where it now lives:

- **Credential storage.** The provider's, not ours. The service holds no password, no hash, and no
  reset token — which is the single largest reduction in liability in this change. What remains ours
  is never accepting a credential: no field anywhere in the console takes a password.
- **Address confirmation.** An enrollment flow with an email-verification stage, so that a person who
  has not proved control of the address gets no session. This is a stage in the provider's flow that
  can be omitted, so it is verified rather than assumed.
- **Reset.** A recovery flow with email verification. The provider ends its own sessions; **our**
  session store does not know a reset happened, so an attacker's session in our console can outlive
  the reset that was meant to kill it. The answer for now is a **short idle expiry** on service
  sessions, which bounds that window to the idle timeout without any coupling to the provider. It is
  a bound rather than a fix, and it is chosen because it needs nothing from the provider, cannot
  break when the provider is upgraded, and fails in the safe direction. A back-channel logout or a
  token-revocation check would close the window properly and remains the eventual answer; both are
  deferred. What matters is that the window is now stated rather than unbounded.
- **Non-disclosure.** The provider's enrollment and recovery responses must not differ between an
  address that has a user and one that does not. Default flows generally get this right; it is on the
  verification list because it is a membership oracle when wrong.
- **Attempt limiting.** A reputation policy on failed attempts by username and by client address.
  Note this is a *different* limiter from U6's: this one protects credentials, that one protects
  shared capacity, and conflating them produces a limiter that is wrong for both. It is also the
  provider feature most often found misconfigured, so it is exercised as part of acceptance rather
  than trusted.
- **Operating it.** The provider becomes a hard dependency of every sign-in, including staff sign-in
  during an incident. That means backups of its database, a tested restore, a patching commitment
  (it is an internet-facing authentication service with a CVE history like any other), and an answer
  to "the identity provider is down and I need to reach the console". `internal/adminctl` is that
  answer — it authenticates with a database connection string and no provider at all, which is
  exactly why `staff-plane` S8 keeps it.

`account-invitations` establishes one useful interaction that survives delegation intact: an
invitation link is proof of control of the address it was sent to, so enrollment reached through one
needs no separate confirmation step. That change owns the rule; this one must not duplicate it. What
does change is the mechanism — the invitation hands off to the provider's enrollment flow rather than
to a registration form of ours.

**Rejected: a hosted identity provider.** Auth0, Entra, Cognito. Removes the operating obligation
above entirely and adds a per-user cost, a dependency on a company whose pricing changes, and
customer identity data held by a third party in a product whose selling point is not handing your
credentials to a third party. Since the provider is being self-hosted for staff regardless, the
marginal cost of one more tenant is close to zero and this trade is not worth making.

**Rejected: keeping `localauth` for the public edition anyway**, to avoid a hard dependency on the
provider at sign-in. It trades a dependency for an obligation — argon2id, tokens, reset semantics,
attempt limiting, and the breach consequences of holding password hashes for strangers — and the
dependency is the cheaper of the two, especially when it already exists for staff.

### U12. One identity provider instance, and separation inside it is policy rather than isolation

Staff and customers authenticate against the same authentik instance at `auth.edgedns.eu`. The
alternative — a second instance, or authentik's own multi-tenancy feature — is rejected: the feature
is experimental, and a second instance doubles the thing this design just decided was cheap precisely
because it already exists.

The consequence has to be stated before the mitigations, because it is the reason the mitigations
matter: **one instance is one user directory and one administrative plane.** Whatever is configured
below, an attacker who obtains authentik administrator access holds both planes. Separation inside a
single instance is a set of policies that can be misbound, not a boundary that cannot be crossed.

Given that, the strictest arrangement available:

- **Customers are `external` users; staff are `internal`.** authentik's user type distinction exists
  for exactly this shape — external users have no access to the administration interface or the
  application dashboard, which removes a whole surface without a policy having to be written.
- **Two applications, each bound to a group.** The console application admits the customer group; the
  staff application admits the staff group. A token for one is not a token for the other, and our
  service verifies which application issued the token rather than trusting a valid signature alone.
- **The staff application has no enrolment flow.** Not a disabled one — none. Staff accounts are
  created by an administrator. A customer's enrolment cannot reach a flow that does not exist.
- **Brands separate the hostnames**, so the customer enrolment and recovery flows are reachable at the
  customer-facing name and not from the staff one.
- **Administrator access is a small group with mandatory multi-factor authentication**, since that
  group is the ceiling on everything above.

**One property of the existing design helps and should not be overstated.** `staff-plane` S2 maps
group claims to staff permissions through configuration that is not editable at runtime, so inventing
a new group in authentik grants nothing until our service is reconfigured and restarted. That raises
the cost of one attack — group creation — and does nothing against an attacker who simply adds
themselves to a group already named in the mapping. It is a speed bump, not a boundary, and is
recorded that way.

**What our service must not do is trust the directory for tenancy.** `tenancy-and-permissions` P18 is
the load-bearing rule here: public account membership is rows written through `account-invitations`,
never derived from group claims. So even a compromised authentik cannot place someone inside a
customer's account — it can create a user, and that user belongs to nothing. Tenant composition is
ours, which is exactly why the two mechanisms were separated in the first place.

**Rejected: authentik's multi-tenancy feature.** Genuine isolation, and explicitly experimental.
Building the identity layer of a commercial service on an alpha feature trades a stated risk for an
unstated one.

**Rejected: a second authentik instance for customers.** Real isolation of directory and
administration, at the cost of a second deployment, second backups, second restore rehearsal and
second patching commitment — for one operator. Worth revisiting if customer identity ever becomes
attractive enough to attack independently, and the arrangement above is the reason it need not be
revisited now.

### U5. Payment funds credit purchases, and a balance is never typed in

`metering-and-credits` owns everything about credits except how they are bought. This change owns
only the conversion of money into balance.

The shape is deliberately narrow: a customer initiates a purchase of a quantity of credits, the
provider takes the payment, the provider notifies us, we verify the notification, and the balance
rises by the purchased quantity in the same transaction that records the purchase.

Two properties matter more than the mechanism:

**Purchase state is derived, never entered.** There is no interface — console, API or CLI — that
sets a balance directly. Every increase traces to a verified provider notification. A balance a
human can edit is not auditable, and the credit ledger's value as evidence for a charge (which
`metering-and-credits` specifies) collapses if its inputs can be conjured.

The one deliberate exception lives elsewhere and is named here so the rule is not read as absolute:
the staff plane's operator credit grant, specified by `staff-plane`, exists precisely for wrongful
suspensions and payments in flight. It is an *operator* action on a separate authorization plane,
audited and visible to the customer — not a field in the account's own billing screen.

**The webhook is unauthenticated by nature and must be verified by signature.** It is a public
endpoint that raises balances, which is as attractive a target as this service has. An
unverifiable notification changes nothing and is recorded as an attempt.

**Rejected: subscriptions.** The original's model — a monthly plan carrying quotas, with
subscription state driving suspension. Rejected because `metering-and-credits` replaced the
economic model entirely: charges are per publish, per delivered check, and per zone over time, none
of which a flat plan expresses. Retaining subscriptions alongside credits would mean two billing
concepts, two suspension rules, and an invoice nobody can explain.

**Rejected: charging a card at the moment of consumption.** It removes prepayment and the whole
overdraft mechanism with it. Rejected because it puts a third-party payment call on the path of
metered activity, so a provider outage becomes a service outage, and because prepayment is what
makes the balance a number the customer controls rather than a bill they discover.

### U9. Self-registration is a configuration switch, and the switch is enforced on the server

The first customers are people we know, invited by name. Self-service registration is what the
product needs when strangers arrive, and that is later. The question is what to do with the
registration routes in the meantime.

**The switch has two halves now that identity is delegated**, and separating them is what makes it
enforceable:

- **Enrolment** — becoming a *user* — is the identity provider's enrollment flow, and closing it
  means disabling that flow for the customer tenant. Invitation-driven enrolment continues to work,
  because it enters through a different flow.
- **Account creation** — becoming a *tenant* — stays ours, and it is the half that matters. A person
  who has somehow enrolled but cannot create an account has a session and nothing to do with it: no
  inventory, no zones, no spend. So even if the provider's enrollment flow is opened by accident, the
  service is not open for business.

**Our half exists and is inert.** The account-creation route is present in the running binary and
answers a single refusal — "registration is not open" — whenever its configuration switch is off. It
is not compiled out, not hidden behind a feature-flagged router, not merely absent from the console's
navigation.

The reasoning is about where the enforcement lives. A route removed from the user interface is not
removed from the internet: it is discoverable by anyone reading the JavaScript bundle, and it works.
So the decision is not *whether the route is visible* but *whether the server does the thing*, and
that must be a check in the handler, close enough to the work that no other entry point can reach
past it. One switch, one check, verified by a test that calls the endpoint directly rather than
through the console.

Keeping the routes present also keeps them honest: they are exercised by tests in both positions, so
turning registration on is a configuration change rather than the first time that code path has ever
run in anger.

The same switch governs whether the marketing site shows a "sign up" call to action at all, and the
console's registration links follow it. Those are presentation; the handler check is the enforcement.

**While it is off, accounts arrive by invitation.** `account-invitations` is the entry path: an
operator creates the account and invites an address, and the invited person completes registration
through the invitation's own flow — which is not the open registration route and is not governed by
this switch.

**Credits arrive by hand.** With payment integration deferred, the credit purchase flow of U5 is
likewise configuration-gated, and balances are established by the staff plane's operator credit grant
— the exception U5 already names, audited and visible to the customer. That keeps U5's central rule
intact: no interface in the account's own billing screen sets a balance, and every increase traces to
either a verified provider notification or a recorded staff action.

**Rejected: deleting the routes until they are needed.** It sounds cleaner and it is worse. The code
would be written under deadline pressure at the moment it first matters, with no test history, and
the "add signup" change would arrive as a large one rather than a switch.

**Rejected: gating in the console only.** The route would be live and undefended, discoverable by
anyone who reads the bundle. The interface is not a security boundary.

### U10. The marketing site states that DNSSEC is not supported

`dns-records` defers DNSSEC signing and refuses zones whose parent already publishes a delegation
signer. That refusal is the safe behaviour, and it means a customer can reach the end of onboarding
before discovering the limitation — which is exactly the moment not to discover it.

So the capability page says it plainly: **DNSSEC is not currently supported; a domain that is signed
today must have its delegation signer removed at the registrar before it can be hosted here.** Not
omitted, not deferred to a support conversation, not phrased as "coming soon" without a date.

The reasoning is commercial rather than technical. Every provider a buyer compares us against offers
DNSSEC at no charge, so the gap will be noticed. Being caught having omitted it costs more than the
sale it loses, and a stranger's trust in a DNS provider is most of what they are buying.

### U6. Protective limits are not credits, and apply in every edition

The original's quotas conflated two things under one name. The product half — how many domains your
plan permits — is gone with plans. The protective half survives, and separating it is the decision.

The question a protective limit answers is *can one account degrade the service for everyone else*.
That question is independent of payment, so its answer must be too:

- A prepaid account with a large balance is not thereby entitled to publish a million simultaneous
  challenge values. Money buys metered activity, not the right to exhaust a shared structure — the
  zone view is held in memory and served from by every satellite.
- An internal deployment bills nobody and still wants the protection.
- The first abusive account will not be a paying one, so a limit that only applies to paying
  accounts protects nothing at the moment it is needed.

So limits on simultaneously published challenge values, and on publication rate, apply to every
account in every edition, independently of balance. A refusal names the limit reached and when it
clears — because a limit that refuses without explanation is indistinguishable from a fault, and
generates a support ticket instead of a corrected client.

**Rejected: expressing protection as a very large credit price.** Elegant in that everything becomes
one mechanism: make the millionth simultaneous challenge cost a fortune. Rejected because it makes
protection purchasable, which is exactly what it must not be, and because credits are deducted
against a balance rather than evaluated against a concurrent state — the ledger cannot express "at
one time".

**Rejected: limits as a per-account configurable field.** Tempting for the large customer who
legitimately needs more. Rejected for now because a configurable protective limit is a protective
limit with an escalation path, and the escalation path is the thing that gets used under pressure by
the person least able to judge the risk. Raising a limit should be a deliberate operator act on the
staff plane, if it is ever needed.

### U7. The marketing site is a separate deployment — E8, unchanged

The console is embedded in the Go binary, which is right for an administration UI that must be
delivered and versioned with the service. Every reason for that is inverted for a marketing site: it
changes for commercial reasons on a commercial cadence, it serves people who have no account and no
session, and a copy change must never require redeploying a DNS service.

Static site, its own pipeline, its own domain. The only coupling is a link into the public edition's
registration route and a shared visual language.

The deployment's names, decided alongside the nameserver names in `dns-records` R9:

| Name | Serves |
| --- | --- |
| `www.edgedns.eu` | the marketing site — static, no session, its own pipeline |
| `console.edgedns.eu` | the authenticated console and the public API |
| `admin.edgedns.eu` | the staff surface of `staff-plane` |
| `auth.edgedns.eu` | the identity provider |

`admin` being a separate name rather than a path under `console` is worth stating as deliberate: the
staff routes are served on the same listener (`staff-plane` S9), and the separate hostname is what
lets network-level restriction be applied to the staff surface without touching the customer one.

One dependency this arrangement creates and which should be an operational decision rather than an
accident: `console`, `www`, `admin` and `auth` all live inside `edgedns.eu`, which is also a parent of
two of the fleet's nameservers. If that zone is served by the fleet itself, then a fleet-wide failure
takes down the console and the marketing site along with customers' DNS — including the pages a
customer would visit to find out what is happening.

### U11. The site carries the obligations, because several decisions elsewhere say "published"

U7 settles how the marketing site is *delivered* and says almost nothing about what it must contain.
That gap matters more than it looks, because three changes now describe commitments as published
without anything owning the publishing.

The site therefore carries, as requirements rather than as copy suggestions:

- **What the service does and does not do.** It answers ACME challenges and serves authoritative DNS;
  it does not issue or hold certificates. "ACME" leads people to expect a certificate authority, and
  the correction belongs before signup rather than in a support reply.
- **That DNSSEC is not supported**, and that a domain signed today must have its delegation signer
  removed at the registrar before it can be hosted (U10).
- **That availability is best-effort with no service level agreement.** Q6's position, and the terms
  are the only place it becomes a statement rather than an omission. A customer delegating their apex
  to us is handing over the availability of their whole domain, mail included; letting them infer a
  promise we have not made is the kind of silence that ends in a dispute.
- **An abuse contact and the response commitment.** A published address, and the twelve hours
  `staff-plane` S10 commits to, measured from delivery to that address. Without a published contact,
  reports reach our upstream provider instead of us, which is both slower and worse for us.
- **A privacy statement covering what is held and for how long.** Registration addresses, zone
  contents, staff access records, and the identity blocklist — which is a list of people retained
  after they have stopped being customers, and is the entry most obviously requiring disclosure.
- **The operator's identity.** Who is contracting, at what address. This is a legal requirement in
  the jurisdiction the service is operated from, and it is also the thing a stranger looks for before
  delegating a domain to an unfamiliar provider.

**These are requirements on the site, not on the service**, which is why they live beside U7 rather
than in the binary. They are stated here so that "published" elsewhere in these changes resolves to
something concrete, and so that the site cannot ship as three sentences and a signup button while
four other changes assume it says more.

**The wording is not drafted here.** Terms, privacy statement and imprint need the operating entity's
details and a template worth trusting, neither of which is a design decision. What is decided is that
the site does not go live without them.

### U8. Onboarding is the product's first-run story, and it is nearly all existing endpoints

A person who has just confirmed their address is led to a first verified domain without contacting
anyone. The console shows the exact record to publish, in the form it must be published in; lets
them check it; and reports what was found against what was expected.

The pleasing part, inherited from E7, is how little of this is new. `GET
/loadbalancers/:id/verification` already resolves the delegation and reports the result, and the DNS
plan already renders the record to publish. The onboarding path is a sequence of screens over
endpoints that exist, plus the honesty that the moment the `CNAME` resolves is simultaneously the
customer's proof that it worked and ours that they control the domain.

No certificate is issued at any point, because issuing certificates is not what this service does —
a distinction worth making explicitly on the marketing site, since "ACME" leads people to expect
otherwise.

## Risks / Trade-offs

- **Local password authentication is a liability we do not currently carry** → every obligation in
  U4 specified rather than assumed; the hosted-provider alternative left genuinely open rather than
  rhetorically open, because it deletes this risk entirely.
- **Email becomes load-bearing, and its failure locks people out** rather than degrading a feature →
  confirmation and reset both offer resend; delivery is an operational commitment with monitoring,
  not a library dependency. `account-invitations` shares the path, so a delivery failure has two
  customer-visible consequences.
- **The payment webhook is a public endpoint that raises balances** → signature verification is the
  only thing standing in front of it, so an unverifiable notification must change nothing and be
  recorded; and the balance must rise in the same transaction that records the purchase, so a
  partial failure cannot credit without evidence.
- **Two editions means two paths that can rot**, and the public one will not be exercised daily
  before launch → the acceptance harness runs both editions, including registration, confirmation, a
  domain reaching verification, a credit purchase and a limit refusal. This is the only defence, and
  it belongs in this change rather than a later one.
- **Protective limits set too low are indistinguishable from a broken service** → refusals name the
  limit and when it clears; limits are configuration with sane defaults, and U6 deliberately keeps
  raising them off the tenant surface.
- **A decision not to commercialize should cost nothing already spent** → this change now contains no
  structural work at all. Everything it depends on is being built for the internal edition anyway,
  which was the original proposal's stated intent and is only now actually true of it.

## Migration Plan

This change assumes `tenancy-and-permissions` has landed; it is steps 3 onward of the original
change's plan, whose steps 1 and 2 that change subsumes.

1. **The identity seam.** `oidcauth` moves behind an interface with no behavioural change.
   Observable behaviour in the internal edition: none. This step is safe to take on its own.
2. **The customer tenant.** A second tenant in the identity provider with its own enrolment and
   recovery flows, verified against the obligations in U4. Nothing in the internal edition points at
   it, so the internal edition is unaffected whatever its state.
3. **Edition selection.** `ACME_EDITION`, startup validation, reporting on the readiness endpoint.
4. **Protective limits.** U6, in both editions — the one step with an observable internal effect, and
   deliberately so.
5. **Credit purchase.** Provider adapter, checkout, webhook verification, balance increase. Depends
   on `metering-and-credits` for the ledger to increase.
6. **Public console and marketing site.** Registration screens, onboarding, purchase, balance and
   payment state; the static site and the "try now" link.

**Rollback**: steps 1–3 are reversible and internally invisible. Step 4 is a configuration change.
Steps 5 and 6 are additive and reachable only in the public edition, so reverting them cannot affect
an internal deployment.

## Open Questions

- **How does our session store learn that a credential was reset?** U4 names this as the one
  obligation delegation complicates. A back-channel logout, a token-revocation check, or simply a
  short idle expiry are all viable and they differ in how long a stolen session outlives the reset
  that was supposed to kill it.
- **When does the short idle expiry stop being enough?** U4 bounds the post-reset window with an
  idle timeout and defers the proper fix. The trigger for building back-channel logout is probably
  the first credential-compromise report, and the number itself — how short is short — is a usability
  trade nobody has made yet.
- **Does the staff plane require multi-factor authentication for everyone, or only administrators?**
  U12 requires it for authentik administrators. Extending it to every staff sign-in is a policy
  decision with no structural consequence.
- **When does self-registration open?** The switch of U9 exists; the trigger for turning it on — a
  number of invited customers, a working payment integration, or a date — is a commercial decision
  and is not made here.
- **Free tier, or trial only?** A free tier needs U6's limits to be right on day one and invites
  abuse; a time-limited trial needs a card up front and loses "try now" as a frictionless promise. A
  prepaid credit model offers a third option the original did not have — a small starting balance —
  which may be the answer, but the abuse profile of free credits is not obviously better.
- **How much of the inventory does a public customer see?** Either the console exposes projects and
  load balancers the way the internal one does, or a public account gets one default project and
  load balancer it never names and adds domains straight to it. This is a decision about screens; the
  vocabulary half was settled by `rename-lb-pair-to-load-balancer`.
- **Where does the unhoused billing rule go?** That an unverified domain is not charged for.
  Probably beside `metering-and-credits`' zone-time requirement, and possibly moot, since the billed
  unit there is the zone rather than the domain.
