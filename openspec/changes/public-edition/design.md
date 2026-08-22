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
| Authentication | `oidcauth` | `localauth` | **this change** (U3, U4) |
| Account resolution | the single seeded account | the user's memberships and session selection | `tenancy-and-permissions` |
| Metering and credits | unmetered, unbilled | the credit ledger | `metering-and-credits`; the internal exemption is **U6** |
| Payment | none | a provider adapter funding credit purchases | **this change** (U5) |
| Console shell | today's screens | plus registration and purchase | **this change** (U8) |

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

### U3. Authentication is a seam, and the session is the thing both implementations produce

`internal/oidcauth` moves behind an interface with no change to its behaviour. `internal/localauth`
implements the same interface for the public edition. The interface's contract is that both produce
a server-side session, and that everything downstream of a session — its storage, its idle and
absolute expiry, its revocation, the cross-site request forgery token bound to it — is identical
whichever produced it.

That last clause is what makes the seam cheap. Session handling is already the part of
`internal/oidcauth` that is not OIDC-specific: sessions are rows with an opaque identifier in a
cookie, and nothing about that is a property of how the person proved who they were.

What a session *carries* once established is deliberately not specified here.
`tenancy-and-permissions` owns it: a subject, and a selected account which may be null. A newly
registered user has no memberships and therefore no selectable account, and that change already
specifies that state as normal rather than as an error.

**Rejected: making `localauth` a wrapper around a self-hosted identity provider.** Running Keycloak
for public customers would reuse `oidcauth` unchanged and own no password code. It was rejected as
the worst of both: we would own the operational burden of an identity provider *and* a registration
experience mediated by someone else's screens, which is the part of onboarding least worth ceding.
Delegating to a *hosted* provider remains genuinely open — see Open Questions.

### U4. What local authentication must own, stated as obligations rather than features

The public edition needs password authentication, and the honest framing is that each item below is
a liability accepted, not a capability gained. Today the service owns none of them.

- **Storage.** Passwords are stored only as a memory-hard hash — argon2id — and are never
  recoverable. This is not negotiable and not parameterizable downward.
- **Confirmation.** Registration grants nothing beyond signing in until the address has been
  confirmed by following a token sent to it. The token expires, is usable once, and is not
  guessable.
- **Reset.** Initiated by email, expiring, single-use, and it ends every existing session of that
  user on use — because the common reason for a reset is that the previous credential is believed
  compromised, and leaving its sessions alive defeats the purpose.
- **Non-disclosure.** Registering with an address that already has a user, and requesting a reset
  for an address that has none, must both produce responses indistinguishable from the opposite
  case. Otherwise the endpoints are a membership oracle.
- **Rate limiting.** Repeated authentication attempts against one user or from one source are
  limited. Note this is a *different* limiter from U6's: this one protects credentials, that one
  protects shared capacity, and conflating them produces a limiter that is wrong for both.

`account-invitations` establishes one useful interaction: an invitation link is itself proof of
control of the address it was sent to, so registration reached through one needs no separate
confirmation step. That change owns the rule; this one must not duplicate it.

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

**They exist, and they are inert.** `POST /api/v1/register` and the account-creation route are
present in the running binary and answer a single refusal — "registration is not open" — whenever
their configuration switch is off. They are not compiled out, not hidden behind a feature-flagged
router, not merely absent from the console's navigation.

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
2. **`localauth`.** Registration, confirmation, reset, attempt limiting. Wired only in the public
   edition, so the internal edition is unaffected whatever its state.
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

- **Should we own local authentication at all?** Delegating public identity to a hosted provider
  keeps the number of authentication implementations we own at one and deletes every obligation in
  U4. What it costs, and whether it constrains registration in ways the marketing site would object
  to, is not known. This is worth answering *before* building `localauth`, not after.
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
