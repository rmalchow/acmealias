# Design

## Context

This change used to be about **editions**. The service had two: `internal`, for our own operators
signing in through our own identity provider with roles derived from group claims, into one account
created at migration time; and `public`, for strangers who register themselves, pay, and must never
see each other. Its central decision, U1, was *one artifact, edition chosen by configuration* — a
setting called `ACME_EDITION` read at startup, selecting which implementations were wired into the
composition root.

**There is one edition now.** `personal-and-team-accounts` C10 removed the internal one: the
bootstrap scope that resolved "the one account", automatic account selection at sign-in, the
group-to-role mapping and its four `ACME_*_GROUP` settings, derived membership, and the constraint
that the internal edition's observable behaviour must not change. `ACME_EDITION` was never built and
is not going to be. Every user now has an implicit **personal account** — free, theirs, materialized
at first sign-in — and a **team account** is a thing you create by paying.

That removal takes the spine out of this change, and several other changes have taken the organs. So
this document is a rewrite rather than an edit, and it starts by saying where everything went.

### Disposition of the decisions this document used to carry

The numbering is `U1`–`U12`, inherited from the version of this change that collapsed
`add-accounts-and-public-edition`. **Surviving decisions keep their numbers**, because four of them
are referred to by name from outside this change — `authentik-and-self-signup` cites U12 five times,
`staff-plane` task 6.11 cites U11, and `backend/internal/config/config.go` cites U7 and U9 in
comments beside the settings they justify. Renumbering would buy tidiness and cost every one of those
references. The order below is therefore narrative, not numeric.

| Decision | Disposition |
| --- | --- |
| **U1** one artifact, edition chosen by `ACME_EDITION` | **Withdrawn.** There is one edition. `personal-and-team-accounts` C10 removed the other, and no setting selects between them. Nothing replaces this decision; the seam table it carried described seams that no longer have two sides |
| **U2** editions never share a datastore or a zone | **Reversed, and re-argued below.** The edition axis is gone, so there is nothing to separate along it. What survives is a smaller rule about environments, and a deliberate decision to keep our own inventory in an ordinary account inside the customer deployment |
| **U3** authentication is delegated to the identity provider | **Built.** `authentik-and-self-signup` K1 swapped Keycloak for authentik, gave each console its own OIDC client, and rewrote the acceptance suite against authentik's flow executor. The service holds no password, and the seam is a fact rather than a plan. What is left is a production instance, which is an impact of this change rather than a decision in it |
| **U4** the obligations of delegated authentication | **Survives, narrowed.** The login page's username oracle is already closed in the blueprint. Enrolment, recovery, non-disclosure on those two flows, attempt limiting and the post-reset session window are still unconfigured and still ours to verify |
| **U5** payment funds credits, and a balance is never typed in | **Survives, reshaped.** `personal-and-team-accounts` C8 built the checkout, the fixed amounts, the audit entry and the balance change — everything except the processor. This change puts a processor in the middle of a flow that already runs |
| **U6** protective limits are not credits | **Survives, and matters more.** The edition clause is gone; the payment clause is the whole of it now, and the free personal account makes it load-bearing rather than precautionary |
| **U7** the marketing site is a separate deployment | **Survives.** The hostname split it specified is half built: `ACME_STAFF_HOST` and two OIDC clients exist, the two ingress rules do not |
| **U8** onboarding is the first-run story | **Survives, much smaller.** `domain-setup-feedback` now owns checking a delegation and explaining what is wrong with it, and `domain-activation` owns what happens until it is right. What remains here is the empty-handed first five minutes |
| **U9** self-registration is a switch enforced on the server | **Survives, premise falsified.** The switch is built (`ACME_ALLOW_ACCOUNT_CREATION`, `admin_accounts.go`) — but it now gates creating a *team* account, and every enrolled person gets a personal one regardless. Re-argued below, because the sentence it rested on is no longer true |
| **U10** withdrawn: there is no DNSSEC statement to make | **Still withdrawn**, and U11 below is corrected to stop asking for one |
| **U10a** the site states what the service does not do | **Folded into U11**, which is the decision about what the site must contain |
| **U11** the site carries the published obligations | **Survives, and is the most under-owned thing in this change.** Three other changes describe commitments as "published" with nothing owning the publishing |
| **U12** one provider instance, separation inside it is policy | **Survives, partly built.** Two applications with separate clients, a group-restricted staff application, and a brand exist in development. External user types, an enrolment flow, mail, and administrator multi-factor do not, in development or anywhere |

Two capabilities this change used to claim have gone with their decisions. The whole of what U1
specified — one artifact, edition reported at startup and on the readiness endpoint, an edition
refusing to start without the settings it needs — is deleted rather than rehomed, because startup
validation of settings that exist is already `layered-configuration`'s. And U3's authentication
requirements are now `authentik-and-self-signup`'s and the main `identity-and-roles` spec's.

### Terms used throughout, defined once

- **Personal account** — the free account every user has by being a user. Implicit: nobody creates
  one, it is materialized at first sign-in by `store.EnsurePersonalAccount`. Five projects, no
  members, no roles, no balance, always usable. `personal-and-team-accounts` C1.
- **Team account** — an account created by paying, which holds other people and the roles they are
  given, and works only while its credit balance is above zero. C1 and C3 of the same change.
- **Credits** — the prepaid balance a team account is charged against. `personal-and-team-accounts`
  adds the number and the checkout that raises it; `metering-and-credits` adds the ledger that draws
  it down. This change adds the payment provider that stands behind the checkout.
- **Checkout** — `POST /api/v1/accounts/{accountId}/checkout`, and the create-an-account form that
  wraps it. It exists, it is exercised by the acceptance suite, and it credits the account without
  taking any money. C8 calls that a *real charge with no processor*, which is exactly the shape this
  change completes.
- **Enrolment** — becoming a *user* at the identity provider. authentik's word for self-registration,
  and a different act from creating an account.
- **Protective limit** — a bound on what one account can do to a resource shared with other accounts.
  Not a price and not an entitlement.

## Goals / Non-Goals

**Goals:**

- A real payment provider behind the checkout that already exists, with no change to what the
  checkout means: a fixed set of amounts, one audited charge, a balance that rises once.
- Limits that protect the shared service from an account that has paid nothing, which after
  `personal-and-team-accounts` is the default state of everybody.
- A stranger who has never spoken to us reaching a first working domain, and a marketing site that
  tells them what they are getting before they start.
- The commitments other changes describe as "published" actually published somewhere.
- The parts of the staff/customer separation that development exercises and production does not.

**Non-Goals:**

- Editions. There is one. Nothing here is conditional on which product a deployment is.
- What an account *is*, what limits it, what funds it, what happens at zero —
  `personal-and-team-accounts`.
- The credit ledger, prices, the tick, warnings by projected time to zero — `metering-and-credits`.
- Tenancy, permissions, roles, query confinement — `tenancy-and-permissions`.
- Invitations, including enrolment reached through one — `account-invitations`.
- Checking a delegation and explaining what is wrong with it — `domain-setup-feedback`. Whether a
  domain is served at all before its delegation has been seen — `domain-activation`.
- Certificate issuance, storage or distribution. We answer challenges and look at what is served.

## Decisions

### U9. Self-registration opens at the identity provider, not at our handler — and the reasoning that used to say otherwise is now wrong

This decision has to be re-argued from the start, because the sentence it rested on has become false.

**What it used to say.** Enrolling makes you a *user*; creating an account makes you a *tenant*; the
two are separate acts and the second is the one that matters. So the switch belongs on
account creation, in the handler rather than in the console's navigation, and *"a person who has
somehow enrolled but cannot create an account has a session and nothing to do with it: no inventory,
no zones, no spend."*

**Why that is no longer true.** `personal-and-team-accounts` C1 makes the personal account implicit.
`internal/oidcauth/flow.go` calls `store.EnsurePersonalAccount` on sign-in, unconditionally, and the
account it materializes is free, holds five projects, and is never suspended because it has no
balance to run out. So a person who enrols and signs in does *not* have a session and nothing to do
with it. They have somewhere to work, and they can add domains and publish challenge values there.

Meanwhile `ACME_ALLOW_ACCOUNT_CREATION` is built, defaults to false, and is enforced in the handler
exactly as U9 asked — but the route it guards, `POST /api/v1/accounts`, is the one that creates a
**team** account by paying. It is a switch on the commercial flow, not a switch on tenancy.

**So the gate on strangers moved, and it is worth being blunt about where it went.** The only thing
standing between an arbitrary person on the internet and a working free account is whether the
identity provider's enrolment flow exists. That is a blueprint entry
(`development/authentik/blueprints/acmeservice.yaml`), not a line of Go, and it is currently absent —
`authentik-and-self-signup` task group 5 is open and deferred to this change.

Three consequences follow, and they are the substance of this decision.

**First, the free personal account is the free tier, and self-registration is the decision to open
it.** This closes an open question the previous version of this document carried — *"free tier, or
trial only?"* — from the other end. Nobody decided to offer a free tier; it fell out of every user
having a personal account. It is a good answer, and it should be adopted deliberately rather than
inherited: five projects, metered at nothing, is a generous and bounded thing to give away, and it is
the same code path a paying customer uses, which is why it will keep working.

**Second, U6's protective limits stop being precautionary and become the floor.** When the only way
in was an invitation, an abusive account was an account we had invited. When enrolment is open, the
first abusive account arrives before anyone reads a log line, and it will not be a paying one. The
limits are what is left. This is why U6 keeps its number and gains a paragraph rather than losing
one.

**Third, the switch is not one switch.** It is three things that must be reasoned about together,
and the failure mode is believing any one of them closes the door:

| Gate | Where it lives | What it stops | State today |
| --- | --- | --- | --- |
| The enrolment flow | the identity provider's blueprint | strangers becoming users at all | absent, which is closed by accident rather than by decision |
| Personal-account materialization | `oidcauth/flow.go`, unconditional | nothing — it is not a gate | built |
| `ACME_ALLOW_ACCOUNT_CREATION` | `admin_accounts.go`, refuses with `account_creation_closed` | strangers buying a team account | built, default false |

The row in the middle is the one to stare at. There is deliberately no switch on personal-account
materialization, and adding one would be a mistake: a signed-in person with no account is the
empty-handed state `tenancy-and-permissions` P19 was built for and that
`personal-and-team-accounts` was pleased to delete, and reintroducing it to serve as a registration
gate would put the least-exercised path in the console back in front of every new arrival.

**What survives of the original reasoning, unchanged and still right:** enforcement lives in the
handler, not in the interface. A route removed from a menu is still on the internet, discoverable by
anyone who reads the JavaScript bundle, and it works. The test that proves it calls the endpoint
directly rather than clicking through the console. That is already how
`ACME_ALLOW_ACCOUNT_CREATION` is built, and it is the reason it was borrowed early.

**Rejected: gating personal-account creation on a setting too, for symmetry.** It would restore the
old sentence — enrol and you have nothing — at the cost of the empty-handed state, a second refusal
the console has to render, and a first-run experience that begins with a wall. And it would be
protecting the wrong thing: what an abusive free account can actually do is bounded by U6 and by
`domain-activation`, which refuses to serve a domain whose delegation has never been seen. A stranger
with a personal account and no domains they control has an empty room.

**Rejected: leaving the enrolment flow closed and inviting everybody.** That is the current posture
and it is correct for now — `SEQUENCE.md` says so, and `account-invitations` is the entry path while
it holds. What is wrong is leaving it closed *by absence*. A flow that has never existed is not
"built and inert", it is unbuilt, and the first time it runs will be the day it matters.
`authentik-and-self-signup` K6 already made this argument and then deferred the work to here.

### U6. Protective limits are not credits, and apply whether or not anybody has paid

The original quotas conflated two things under one name. The product half — how many domains your
plan permits — went with plans. The protective half is this decision, and separating them is its
whole content.

The question a protective limit answers is *can one account degrade the service for everyone else*.
That question is independent of payment, so its answer must be too:

- A team account with a large balance is not thereby entitled to publish a million simultaneous
  challenge values. Money buys metered activity, not the right to exhaust a shared structure — the
  challenge zone view is held in memory by every process that answers queries, and it is one
  structure for the whole service.
- A **personal** account is charged nothing at all and still has to be bounded. After
  `personal-and-team-accounts` this is the common case rather than the exception: most accounts in a
  running deployment will be free ones.
- The first abusive account will not be a paying one, so a limit that only applies to paying accounts
  protects nothing at the moment it is needed.

So limits on simultaneously published challenge values, and on publication rate, apply to every
account, independently of balance and of kind. A refusal names the limit reached and when it clears —
because a limit that refuses without explanation is indistinguishable from a fault, and generates a
support ticket instead of a corrected client.

**A protective refusal must be distinguishable from a funding refusal**, and after
`personal-and-team-accounts` C7 that is a concrete requirement rather than a nicety: an unfunded team
account is refused at `/present` with `402 Payment Required` and a message naming the balance. A
protective limit is not a payment problem and must not answer `402`; it answers with the limit and a
retry time. An operator who reads `402` goes to look at the balance, and on a personal account there
is no balance to look at.

**Rejected: expressing protection as a very large credit price.** Elegant, in that everything becomes
one mechanism: make the millionth simultaneous challenge cost a fortune. Rejected because it makes
protection purchasable, which is exactly what it must not be; because a personal account has no
balance for a price to be charged against; and because credits are deducted against a balance rather
than evaluated against a concurrent state — the ledger cannot express "at one time".

**Rejected: limits as a per-account configurable field.** Tempting for the large customer who
legitimately needs more. Rejected for now because a configurable protective limit is a protective
limit with an escalation path, and the escalation path is the thing that gets used under pressure by
the person least able to judge the risk. Raising a limit should be a deliberate operator act on the
staff plane, if it is ever needed.

### U4. Delegating authentication moved the obligations; it did not remove them

The service holds no password, no password hash and no reset token, and that is settled: authentik is
the only authenticator, and `authentik-and-self-signup` made it real in development. What remains is
that a list of things to *build* became a list of things to *configure and verify*, and configuration
nobody checked is the more dangerous of the two, because it looks finished.

One of them is already done, and it is worth naming because it is the model for the rest. authentik's
default identification stage asks for the identifier, submits it, and *then* asks for a password —
which is a membership oracle: the response to step one differs depending on whether the account
exists, so anybody can test an address list against the login page and learn which of them are
customers. The blueprint sets `password_stage` on that stage so both fields are submitted together
and a wrong identifier fails identically to a wrong password. That is a configuration change with a
security property, written down where somebody can see why.

The rest, each with where it now lives:

- **Address confirmation.** An enrolment flow with an email-verification stage, so a person who has
  not proved control of the address gets no session. It is a stage that can be omitted from a flow,
  so it is verified rather than assumed.
- **Recovery.** A recovery flow with email verification. The provider ends *its* sessions; **our**
  session store does not know a reset happened, so an attacker's session in our console can outlive
  the reset that was meant to kill it. The answer for now is a **short idle expiry** on service
  sessions, which bounds that window without any coupling to the provider. It is a bound rather than
  a fix, chosen because it needs nothing from the provider, cannot break when the provider is
  upgraded, and fails in the safe direction. Back-channel logout or a token-revocation check would
  close the window properly and remains the eventual answer. What matters is that the window is
  stated rather than unbounded.
- **Non-disclosure on enrolment and recovery.** Same oracle as the login page, two flows further on.
  Enrolling with an address that already has a user, and recovering an address that has none, must
  both look like the ordinary case. Default flows generally get this right; it is on the verification
  list because it is a membership oracle when wrong.
- **Attempt limiting.** A reputation policy on failed attempts by username and by client address.
  This is a *different* limiter from U6's: this one protects credentials, that one protects shared
  capacity, and conflating them produces a limiter that is wrong for both. It is also the provider
  feature most often found misconfigured, so it is exercised in acceptance rather than trusted.
- **Mail.** Every one of the flows above sends a message, and until one is configured they all fail
  silently at the last step. Development has a sink (`mailpit`); production has nothing.
- **Operating it.** The provider is a hard dependency of every sign-in, staff sign-in during an
  incident included. Backups, a tested restore, a patching commitment — it is an internet-facing
  authentication service with a CVE history like any other — and an answer to "the provider is down
  and I need to reach the console". `internal/adminctl` is that answer: it authenticates with a
  database connection string and no provider at all, which is exactly why `staff-plane` S8 keeps it.

`account-invitations` establishes one interaction that survives delegation intact: an invitation link
is proof of control of the address it was sent to, so enrolment reached through one needs no separate
confirmation step. That change owns the rule; this one must not restate it. What changes is the
mechanism — the invitation hands off to the provider's enrolment flow rather than to a registration
form of ours.

**Rejected: a hosted identity provider.** Auth0, Entra, Cognito. Removes the operating obligation
entirely and adds a per-user cost, a dependency on a company whose pricing changes, and customer
identity data held by a third party in a product whose selling point is not handing your credentials
to a third party. The provider is self-hosted for staff regardless, so the marginal cost of one more
application on it is close to zero and this trade is not worth making.

**Rejected: holding passwords ourselves.** An earlier version of this change specified
`internal/localauth` — email and password, argon2id, confirmation tokens, reset tokens, attempt
limiting. It was reversed and is not coming back. For a product whose entire pitch is *you do not
hand us credentials*, holding a table of strangers' password hashes was always against the grain, and
a database compromise now discloses email addresses and zone contents rather than credentials.

### U12. One provider instance; separation inside it is policy, and half of it is built

Staff and customers authenticate against one authentik instance. A second instance, or authentik's
own multi-tenancy feature, are both rejected below.

The consequence has to be stated before the mitigations, because it is why the mitigations matter:
**one instance is one user directory and one administrative plane.** Whatever is configured, somebody
who obtains authentik administrator access holds both planes. Separation inside a single instance is
a set of policies that can be misbound, not a boundary that cannot be crossed.

Given that, the strictest arrangement available — and what actually exists today:

| Property | Intent | State |
| --- | --- | --- |
| Two applications, each with its own OIDC client | a token for one is not a token for the other | **built** — `acmeservice-console` and `acmeservice-admin` in the blueprint, separate client ids and secrets |
| Two hostnames, cookies on separate origins | a script on the customer origin cannot drive the staff surface | **built** — `console.localhost` / `admin.localhost`, `ACME_STAFF_HOST` selects the console and the OIDC client |
| The staff application admits one group | staff access is a small list | **built** — `acmeservice-admin` is restricted to `acme-admin` |
| Our service verifies which application issued the token | a validly signed token is not enough | **to verify** — the issuer is validated per client, and the assertion that a console token is refused on the staff surface is not written |
| Two ingress rules | the hostname split restricts anything at all | **not built** — see U7, where the argument lives |
| Customers `external`, staff `internal` | external users reach neither the administration interface nor the application dashboard | **not built** — development has no such distinction to make and production does |
| The staff application has no enrolment flow | not a disabled one; none | **trivially true today**, because no enrolment flow exists at all. It stops being trivially true the moment U9 adds one, which is the point at which it needs asserting |
| Administrator access is a small group with mandatory multi-factor | that group is the ceiling on everything above | **not built** |

**One property of the existing design helps and should not be overstated.** `staff-plane` S2 maps
group claims to staff permissions through configuration that is not editable at runtime, so inventing
a new group in authentik grants nothing until our service is reconfigured and restarted. That raises
the cost of one attack — group creation — and does nothing against somebody who adds themselves to a
group already named in the mapping. A speed bump, not a boundary.

**What our service must not do is trust the directory for tenancy.** `tenancy-and-permissions` P18 is
the load-bearing rule, and `personal-and-team-accounts` C10 strengthened it by deleting the derived
membership branch that used to be its exception: membership of a team account is rows written through
`account-invitations`, never derived from a group claim. So even a compromised authentik cannot place
somebody inside a customer's account. It can create a user — and after U9 that user gets a personal
account of their own, which is empty and bounded, and belongs to nobody else.

**Rejected: authentik's multi-tenancy feature.** Genuine isolation, and explicitly experimental.
Building the identity layer of a commercial service on an alpha feature trades a stated risk for an
unstated one.

**Rejected: a second authentik instance for customers.** Real isolation of directory and
administration, at the cost of a second deployment, second backups, second restore rehearsal and
second patching commitment — for one operator. Worth revisiting if customer identity ever becomes
attractive enough to attack independently, and the arrangement above is the reason it need not be
revisited now.

### U5. A payment provider goes behind a checkout that already works

`personal-and-team-accounts` C8 built the whole funding mechanism minus the processor:
`POST /api/v1/accounts/{accountId}/checkout` takes one of a fixed set of amounts, credits the
account, writes an `account.checkout` audit entry, and returns — and the create-an-account route
wraps the same act, because a team account is created by paying. The acceptance suite walks both.
C8's stated reason for building it that way was that *"a fake that writes different state teaches
nothing"*, and this change is what that argument was making room for.

So the shape of this decision is narrow: keep everything C8 established and insert a provider between
the customer's click and the credit.

```
  today                    customer ──▶ POST /checkout ──▶ balance += amount, audit entry
  with a provider          customer ──▶ POST /checkout ──▶ provider's hosted payment page
                                                              │
                           provider notification ─────────────┘
                                     │ verified by signature
                                     ▼
                                balance += amount, audit entry   ← the same rows as today
```

Four properties, and the last two are the ones with teeth.

**The amount stays a fixed choice, not a number the caller supplies.** C8 made it so because there
was no processor to notice a body saying 1000000. With a processor there is one — and it stays fixed
anyway, because the price list is a price list.

**The balance rises in the same transaction that records the purchase.** Already true; it must stay
true when the trigger moves from a request to a notification, which is the part a naive webhook
handler gets wrong.

**Purchase state is derived, never entered.** There is no interface — console, API or CLI — that sets
a balance directly. Every increase traces to a verified provider notification. A balance a human can
edit is not auditable, and `metering-and-credits` treats the ledger as the evidence for a charge,
which collapses if its inputs can be conjured. The one deliberate exception lives elsewhere and is
named here so the rule is not read as absolute: `metering-and-credits`' *"An operator may grant
credits and thereby restore an account"*, which exists for wrongful suspensions and payments in
flight. That is an operator action on a separate authorization plane, audited and visible to the
customer — not a field in the account's own billing screen.

**The notification endpoint is unauthenticated by nature and must be verified by signature.** It is a
public endpoint that raises balances, which is as attractive a target as this service has. An
unverifiable notification changes nothing and is recorded as an attempt. A repeated delivery of one
completed payment credits exactly once, because providers retry and a retry is not a second purchase.

**Payment is configuration-gated, and the gate is what makes the transition safe.** While payment is
disabled the checkout behaves as C8 built it — it credits directly, which is right for development
and for the invited-customer period. While it is enabled the same route redirects to the provider and
credits nothing until the notification arrives. Two behaviours behind one setting is a thing to be
uneasy about, so it is worth saying why it is the right shape here: the alternative is a second route,
and a second route means the console has two flows, the acceptance suite exercises the one nobody
ships, and the day payment is switched on is the first day the customer-facing path has ever run.

**Rejected: subscriptions.** A monthly plan carrying quotas, with subscription state driving
suspension. `metering-and-credits` replaced the economic model entirely — charges are per publish and
per configured node observed, neither of which a flat plan expresses. Retaining subscriptions
alongside credits would mean two billing concepts, two suspension rules, and an invoice nobody can
explain.

**Rejected: charging a card at the moment of consumption.** It removes prepayment and the funding
suspension with it. Rejected because it puts a third-party payment call on the path of metered
activity, so a provider outage becomes a service outage, and because prepayment is what makes the
balance a number the customer controls rather than a bill they discover.

**Rejected: holding card details.** Never. The provider's hosted page takes the payment; what we
store is a provider reference and an amount. This is also what keeps U2's blast-radius arithmetic
small enough to reach the conclusion it reaches.

### U7. The marketing site is a separate deployment, and the hostname split is half finished

The console is embedded in the Go binary, which is right for an administration UI that must be
delivered and versioned with the service. Every reason for that is inverted for a marketing site: it
changes for commercial reasons on a commercial cadence, it serves people who have no account and no
session, and a copy change must never require redeploying a DNS service.

Static site, its own pipeline, its own domain. The only coupling is a link into the console's sign-in
route and a shared visual language.

| Name | Serves |
| --- | --- |
| `www.acmealias.com` | the marketing site — static, no session, its own pipeline |
| `console.acmealias.com` | the authenticated console and the public API |
| `admin.acmealias.com` | the staff surface of `staff-plane` |
| `auth.acmealias.com` | the identity provider |

One domain, not two. An earlier version split the nameservers across two domains in two top-level
domains at two registrars, so that one lapsed registration could not take every customer's DNS down
at once. That reasoning belonged to a product that hosted customers' zones; this one does not. What a
lost domain costs now is that certificate renewals stop working until somebody notices — with roughly
thirty days of slack — rather than every customer's website and mail going dark within a TTL. The
name is also no longer a compromise: `acmealias.com` says what the service does, which is hold the
alias a customer's `_acme-challenge` CNAME points at.

**`admin` is a separate name rather than a path under `console`, deliberately.** The staff routes are
served on the same listener (`staff-plane` S9), and a path would work for network restriction —
Traefik matches on path as readily as on host. What it would not do is separate the **browser
origin**, and origin is what the browser enforces. Same origin means one cookie jar, one
`localStorage`, and one blast radius: a cross-site scripting flaw in the customer console would run
with the staff surface's origin and could drive it in the browser of a staff member signed in to
both. U12's separate OIDC clients stop a customer's *token* from being a staff token; they do nothing
about a script already running on the staff origin. A hostname is the one boundary the browser
enforces for free.

**The hostname only restricts anything if the ingress refuses staff paths on the customer name, and
that is the part not built.** Both names resolve to the same listener, and the listener routes by
path — so `console.acmealias.com` will serve `/api/admin/**` perfectly happily unless something in
front says otherwise, and an address allowlist on `admin.acmealias.com` is then decorative. Two
rules, neither optional:

- `admin.acmealias.com` — address allowlist, forwards the staff tree and the staff console.
- `console.acmealias.com` — no allowlist, and **refuses `/api/admin/**` outright**.

This is the sort of arrangement that works on the day it is written and quietly stops working when
somebody simplifies the ingress, so it belongs in a test rather than only in a runbook: the staff
tree must answer 404 through the customer hostname regardless of who is asking.

One dependency this creates, which should be an operational decision rather than an accident:
`www`, `console`, `admin` and `auth` all live inside `acmealias.com`, which is also the domain whose
challenge zone this service is authoritative for. If this service serves that zone, an outage here
takes down the console and the marketing site along with challenge answering — including the pages a
customer would visit to find out what is happening. The apex and these four names are therefore
served by something that is not this service, so the status page survives the service being down.

### U11. The site carries the published obligations, because several other changes say "published"

U7 settles how the site is *delivered* and says almost nothing about what it must contain. That gap
matters more than it looks, because other changes describe commitments as published with nothing
owning the publishing. This decision is the owner.

The site carries, as requirements rather than as copy suggestions:

- **What the service does and does not do.** It answers ACME DNS-01 challenges and observes the
  certificates that result. It does **not** host your DNS, does **not** issue certificates, and does
  **not** hold private keys. Each of those is a thing a visitor may reasonably assume from the words
  "DNS" and "certificate", and each is cheaper to say on a capability page than to explain in a
  support reply after somebody has signed up expecting it. The pitch — *you publish one CNAME, once,
  and never hand anyone your DNS credentials* — only works if what is being declined is stated as
  plainly as what is offered. This absorbs what was U10a.
- **Nothing about DNSSEC.** An earlier version of this decision required the site to say DNSSEC was
  unsupported, and to tell a customer to remove their delegation signer at the registrar. That went
  with the DNS product: nobody delegates a zone here, the customer keeps their DNS where it is, and a
  `CNAME` in a signed zone is signed by its own operator and validates normally. U10 withdrew the
  statement and this list is corrected to stop asking for it — recorded rather than silently dropped,
  because "why does the site not mention DNSSEC" is a question somebody will ask.
- **That availability is best-effort with no service level agreement.** The terms are the only place
  this becomes a statement rather than an omission. The consequence of an outage here is that
  renewals stop, with weeks of slack — which is a good story, and it is a better one told than
  inferred.
- **An abuse contact and the response commitment.** A published address, and the twelve hours
  `staff-plane` S10 commits to, measured from delivery to that address. Without a published contact,
  reports reach our upstream provider instead of us, which is both slower and worse for us.
- **A privacy statement covering what is held and for how long.** Registration addresses, zone
  contents, challenge interaction records including a load balancer's client address, staff access
  records, and the identity blocklist — which is a list of people retained after they have stopped
  being customers, and is the entry most obviously requiring disclosure.
- **The operator's identity.** Who is contracting, at what address. A legal requirement in the
  jurisdiction the service is operated from, and also the thing a stranger looks for before trusting
  an unfamiliar provider with anything.

**These are requirements on the site, not on the service**, which is why they live beside U7 rather
than in the binary. They are stated here so that "published" elsewhere resolves to something
concrete, and so the site cannot ship as three sentences and a signup button while other changes
assume it says more. **The wording is not drafted here** — terms, privacy statement and imprint need
the operating entity's details and a template worth trusting, neither of which is a design decision.
What is decided is that the site does not go live without them.

### U8. Onboarding is the first five minutes, and most of it now belongs to somebody else

A person who has just enrolled is led to a first working domain without contacting anyone. That was
the whole of U8, and two later changes have taken most of it:

- `domain-setup-feedback` checks a domain's delegation without being asked, reports what was found
  against what was expected, and explains how to fix it.
- `domain-activation` decides whether the service does any work for a domain at all until its
  delegation has been seen, which is what makes the first `CNAME` meaningful rather than decorative.
- `personal-and-team-accounts` C11 and C12 settled what a personal account's navigation looks like
  and where `Upgrade` sits.

What is left is genuinely small, and it is the part nobody else can own: **the shape of an account
with nothing in it.** A person arriving for the first time has a personal account, no project, no
load balancer, no domain, and no idea that the first two exist. The first-run path is a sequence over
endpoints that already answer — the plan renders the record to publish, and
`GET /loadbalancers/:id/verification` resolves the delegation and reports the result — plus a
decision about how much of the inventory hierarchy a new arrival should have to understand before
they can add one domain.

That decision is not made here; it is an open question below, because it is a screen design and this
change has no basis to settle it. What is decided is that first run is a path with an end, that the
end is a domain whose delegation has been seen, and that no certificate is issued at any point,
because issuing certificates is not what this service does.

### U2. Our own inventory lives in an ordinary account, in the customer deployment — reversing this decision

The original rule was *editions never share a datastore or a zone*, on a blast-radius argument: one
database would hold our own fleet's inventory next to strangers' payment records, under one retention
policy, one backup and one compromise, and the internal edition would inherit the public edition's
compliance surface for nothing. A second argument said a public incident that poisons a zone view
must not reach internal aliases.

The edition axis is gone, so the rule as written has nothing to separate. The honest question is
whether it was saying something a single-edition service still wants, and the answer is that it was
saying two things, one of which is now wrong and one of which is worth keeping.

**The part that is wrong: keeping our own inventory out of the customer deployment.** It is tempting
for exactly the reason the original gave, and it should be rejected, for three reasons that compound.

The confinement machinery is the whole safety property of this product. `tenancy-and-permissions`
gates reads inside the query with row-level security, and the strongest continuous test of that is
having data in there that we would notice leaking. Task 11.4 of that change is the cautionary tale:
four cross-account reads had been silently emptied by row-level security, one of them stopping
certificate issuance altogether, and it took a full run from a destroyed database to find. A separate
deployment for ourselves means we spend every day in a system where confinement is not exercised by
anyone who would notice.

The blast-radius arithmetic has also changed underneath the argument. It was written when "strangers'
payment records" meant a table we would hold. U5 puts a hosted payment page in the middle, so what we
store is a provider reference and an amount — no card data, no credentials, and by U4 no password
hashes either. What one database now holds is domain names, alias labels, challenge values and email
addresses, for us and for customers alike. That is not a compliance surface worth a second deployment
for one operator.

And the cost of the alternative is not the database. It is a second migration chain to run, a second
backup to test, a second thing to patch, a second place to look during an incident, and a second
authentik application — for one person, for a handful of our own domains.

So: **our own domains live in an ordinary account, alongside customers', in one deployment, one
database, one zone.** If the confinement is not good enough for us, it is not good enough to sell,
and finding that out is the point.

**What survives, and it is not nothing:**

- **Environments never share a datastore or a zone.** Development, staging and production are
  separate deployments with separate databases and separate zones. This is the original
  blast-radius argument in the place where it still bites hardest, and it is not hypothetical:
  the development stack seeds users whose password is `devpass`, applies a blueprint on every start,
  and is reset from empty by a script anybody can run.
- **The service's own names are not served by the service** — U7's last paragraph, which is the
  surviving form of "a public incident must not take down the thing you look at to find out about
  it".

**Rejected: keeping a separate deployment for our own domains anyway**, as defence in depth. It
sounds free and is not: it buys isolation we have decided we do not need at the cost of never
exercising the isolation we have decided we do.

## Risks / Trade-offs

- **Opening enrolment opens a free tier to the entire internet, and it is not obviously bounded** →
  U6's limits are the floor, `domain-activation` refuses to serve a domain whose delegation has never
  been seen, and the personal-account project quota is configuration rather than a constant. The
  residual risk is that the numbers are guesses until somebody abuses them; the mitigation is that
  all three are settings.
- **The payment notification endpoint is a public endpoint that raises balances** → signature
  verification is the only thing standing in front of it, so an unverifiable notification must change
  nothing and be recorded, a repeated one must credit once, and the balance must rise in the same
  transaction that records the purchase, so a partial failure cannot credit without evidence.
- **The checkout behaves differently on either side of one setting** → both behaviours are exercised
  in acceptance, and the setting is the only difference between them. The failure this is protecting
  against is a customer-facing payment flow whose first real run is the day it is switched on.
- **Mail becomes load-bearing, and its failure locks people out** rather than degrading a feature →
  verification and recovery both offer resend; delivery is an operational commitment with monitoring.
  `account-invitations` and `metering-and-credits`' balance warnings share the path, so one delivery
  failure has three customer-visible consequences.
- **The staff/customer separation is policy inside one identity provider** → stated as such in U12
  rather than described as isolation, with the ingress rules of U7 tested rather than documented, and
  administrator access as the acknowledged ceiling.
- **The site can ship as a signup button** → U11's contents are requirements and the site's release
  is gated on them.
- **A decision not to commercialize should cost nothing already spent** → nothing structural is left
  in this change. Everything it depends on is built for the service as it stands.

## Migration Plan

Steps are independent except where stated, and each is separately reversible.

1. **The enrolment flow, in development first** (U9, U4). Add it to the blueprint with a verification
   stage, point authentik's mail at the stack's sink, walk it by hand once, and fix the refusal
   wording a self-registered person now sees. Nothing outside development is affected.
2. **Protective limits** (U6). The one step with an effect on every existing account, and deliberately
   so — a limit that only applies to new accounts is a limit nobody has tested.
3. **The production identity provider** (U4, U12). An instance at `auth.acmealias.com`, external user
   types, administrator multi-factor, backups and a tested restore. The service does not change.
4. **The ingress rules** (U7, U12). Two Traefik rules and one test asserting the staff tree answers
   404 through the customer hostname.
5. **Payment** (U5). Provider adapter, hosted checkout, notification verification, refunds. Behind the
   payment setting, so it lands disabled and is switched on separately.
6. **The marketing site and its obligations** (U7, U11). Its own pipeline; released when U11's
   contents exist and not before.
7. **First run** (U8). The last, because it is the one that wants the others to be real before its
   shape is worth arguing about.

**Rollback**: steps 1–4 are configuration and infrastructure, reversible without a release. Step 5 is
additive and gated by a setting whose default is off. Steps 6 and 7 are additive.

## Open Questions

- **How much of the inventory does a new arrival have to understand?** Either the console exposes
  projects and load balancers to a personal account the way it does to a team one, or a personal
  account gets one default project and load balancer it never names and adds domains straight to it.
  This is U8's remaining substance and it is a screen decision, not a model one.
- **When does the enrolment flow open in production?** The flow is built in development by step 1
  above; opening it to strangers is a commercial decision with a security consequence, and the
  trigger — a working payment integration, U6's limits measured against real traffic, or simply a
  date — is not made here.
- **What are the numbers?** How many simultaneous challenge values, at what rate, per account. U6
  decides that limits exist and are not purchasable; it does not decide the values, and nobody has
  data to decide them from yet.
- **How short is the short idle expiry?** U4 bounds the post-reset window with an idle timeout and
  defers back-channel logout. The number is a usability trade nobody has made, and the trigger for
  building the proper fix is probably the first credential-compromise report.
- **Does the staff plane require multi-factor authentication for everyone, or only administrators?**
  U12 requires it for authentik administrators. Extending it to every staff sign-in is a policy
  decision with no structural consequence.
- **Which payment provider, and does it settle in the currency the price list is in?** U5 specifies
  the shape and deliberately not the vendor. The one property that is not negotiable is a hosted
  payment page, because it is what keeps card data out of this deployment and U2's arithmetic
  correct.
