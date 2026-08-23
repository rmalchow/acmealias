## Why

### One change was doing two jobs

`add-accounts-and-public-edition` argued for two things at once, and said so in its own opening:
*"introduce the small number of seams that let one codebase serve both, and — the part with a
deadline on it — get the data model ready before there is public data in it."*

Those two halves have come apart, decisively and in the right direction. The half with the
deadline — an owner above `projects`, authorization that takes an object, query confinement that
cannot be forgotten — has been superseded in far more detail by seven changes that did not exist
when it was written. `tenancy-and-permissions` alone replaced its four fixed roles with a
permission model, its plans with a different economic model, and its account-scoped queries with a
mechanism that gates reads inside the query rather than after it.

What remains is the other half: **the public edition as a product.** Signing strangers up,
authenticating them without an identity provider we run, taking their money, protecting the shared
service from them, and telling them what it does — none of which any other change has touched, and
none of which the data-model work depends on.

This change is that residue, made coherent on its own terms. It is deliberately a *smaller and
more commercial* proposal than the one it replaces, and that is the point: a decision not to sell
this can now be taken without unpicking anything, because nothing structural is left inside it.

### What a "public edition" is

The service answers ACME DNS-01 challenges — and, once `dns-records` lands, serves authoritative
DNS — for domains whose zones we do not administer. A customer publishes one `CNAME` record once,
and their load balancer can thereafter prove control of that domain to a certificate authority by
asking us, rather than by holding write credentials for their own DNS.

Two audiences can be sold that, and they share nothing else:

- **The internal edition** — our own operators, signing in through our own identity provider,
  administering our own fleet. Everyone who can sign in is trusted. This is what exists today.
- **The public edition** — the same machinery offered to strangers. Someone finds a marketing
  page, clicks "try now", registers with an email address, confirms it, adds a domain, publishes
  the `CNAME` we show them, buys credits, and is running within a few minutes. They must never
  see, or be able to reach, another customer's anything.

The isolation half of that last sentence is `tenancy-and-permissions`' problem and is solved
there. This change is everything else a stranger needs before and around it.

### Why the remainder is worth a change of its own

Because every item in it is a genuine commitment we do not currently carry, and each one is
individually decidable:

- **Identity for strangers.** Somebody has to hold customer credentials, verify addresses and
  survive credential-stuffing. This proposal originally answered "we do, in a new package"; it now
  answers "the identity provider does, in a second tenant" — see below, because the reversal is the
  largest single change to this proposal.
- **Email becomes load-bearing.** A verification message that bounces is a customer who cannot get
  in. Deliverability is an operational commitment, not a library dependency, and it is now split
  across two senders: the identity provider sends verification and recovery, we send invitations and
  balance warnings.
- **Taking money** means a payment provider in the request path of something, webhook
  verification, and an auditable trail from a card charge to a credit balance.
- **Strangers on shared infrastructure** need limits that exist regardless of what they have paid,
  because the first abusive account will not be a paying one.

Bundled into a data-model change, none of those got a decision of its own. Separated, each does.

## What Changes

### Editions are selected by configuration, not by build

One artifact. `ACME_EDITION=internal|public` is read at startup and selects which implementations
are wired into the composition root, and the value in force is reported in the startup log and on
the readiness endpoint, so an operator looking at a running process can see which policy it holds.
This is decision E1 of `add-accounts-and-public-edition`, adopted unchanged.

Two editions never share a datastore or a DNS zone. Two deployments, two databases, two zones —
never one deployment serving both audiences.

### Customers authenticate through the same identity provider we already run

This proposal originally specified a second authentication implementation — `internal/localauth`,
with email and password, argon2id hashing, confirmation tokens, reset tokens and attempt limiting —
and explicitly rejected running an identity provider for public customers as "the worst of both".
**That is reversed**, and it is worth saying why plainly, because it deletes the largest piece of
new security-sensitive code in the change.

The rejection assumed the identity provider would be *additional*. It is not: one is being run
anyway, at `auth.acmealias.de`, for staff and for the internal edition. Adding customers to it is a
second tenant, not a second system. The second half of the rejection — that registration through
someone else's screens cedes the part of onboarding worth owning — was also wrong, because the
screens can be themed, and the part of onboarding that actually matters happens *after* sign-in:
publishing a `CNAME`, adding a domain, watching it verify.

What this buys is not convenience, it is liability. The service ends up holding **no password, no
password hash and no reset token for anybody.** A database compromise discloses email addresses and
zone contents, not credentials. For a product whose entire pitch is "you do not hand us credentials",
holding a table of strangers' password hashes was always slightly against the grain.

What it costs is worth being exact about, because "delegate it" is not the same as "it is handled":

- **Every sign-in now depends on the provider being up**, staff sign-in during an incident included.
  The answer is `internal/adminctl`, which authenticates with a database connection string and no
  provider at all — which is precisely why `staff-plane` keeps it.
- **The obligations become configuration rather than code**, and configuration nobody verified is
  more dangerous than code nobody wrote, because it looks finished. Address verification, recovery
  verification, non-disclosure on enrolment and recovery, and failed-attempt limiting are each a flow
  or policy that can be silently absent. They are on the acceptance list rather than assumed.
- **One thing genuinely gets harder.** When a customer resets a compromised credential, the provider
  ends *its* sessions — but the service's own session store knows nothing about it, so an attacker's
  session in our console can outlive the reset that was meant to kill it. That needs a back-channel
  logout, a revocation check, or a deliberately short idle expiry. It is the one place delegation
  adds a problem instead of removing one, and it is called out because the failure is silent.
- **We now operate an internet-facing authentication service**: backups, a tested restore, and a
  patching commitment.

The seam itself survives with one implementation behind it, for a reason worth stating: it is the
boundary at which the provider's vocabulary stops. Nothing downstream of a session sees a token, a
claim or a flow, which is what keeps the provider replaceable.

A vocabulary note that matters more than it looks: **enrolling creates a *user*, not an account.**

A vocabulary note that matters more than it looks: In the model these changes now share, a user is a person and exists outside every
account; an account is the billing entity that holds credits and owns projects. A newly registered
user belongs to no account at all, and `tenancy-and-permissions` specifies that state as a normal
one. The original spec text called registration "creating an account", which is now ambiguous
between two different entities, and this change renames it.

### Payment funds credits, not subscriptions

The original proposed flat monthly plans with quotas, and a Stripe adapter turning
checkout and webhooks into subscription state. `metering-and-credits` replaced that model
wholesale: credits are **prepaid**, actions are deducted, active zones accrue over time, and an
overdraft carries an account briefly before its zones are withdrawn.

So what this change keeps is only the part that model does not have: **the payment provider
integration that turns money into credits.** A customer buys a quantity of credits; the provider
confirms the payment; the balance rises. Purchase state is derived from what the provider reports
and can never be typed in by hand, because a balance a human can edit is not an auditable one.

Everything downstream of the balance — the ledger, accrual, the overdraft, warning by projected
time remaining, suspension, restoration — belongs to `metering-and-credits` and is not restated
here.

### Abuse limits are not the same thing as credits, and survive separately

The original's quotas did two unrelated jobs under one name. The product half (how many domains
your plan permits) is gone, replaced by credits. The **protective** half survives and is separated
out, because it answers a different question: not *have you paid for this*, but *can one account
degrade the service for everyone else*.

Concurrency and rate limits therefore apply in every edition, to every account, independently of
its balance. A prepaid account with a large balance is not entitled to publish a million challenge
values at once, and an internal deployment that bills nobody still wants the protection.

### A stranger reaches a verified domain without help

The onboarding story survives as written, and it is the clearest thing the original had: a person
who has just confirmed their address is led to a first verified domain. The console shows the
exact record to publish, in the form it must be published in; lets them check it; and reports what
was found against what was expected, plainly enough to act on.

`GET /loadbalancers/:id/verification` already answers exactly that question, so "try now" has a
satisfying shape built almost entirely from endpoints that exist. No certificate is issued, because
issuing certificates is not what this service does.

### The marketing site is not in the binary

The console is compiled into the Go binary, which is right for an administration UI delivered and
versioned with the service. Every reason for that is inverted for a marketing site: it changes on a
commercial cadence for commercial reasons, it must serve people who have no account and no session,
and a copy change must never require redeploying a DNS service. Static site, its own pipeline, its
own domain; the only coupling is a link into the public edition's registration route.

### The first customers arrive by invitation, and the signup route is switched off at the server

Everything above describes a stranger finding a marketing page and being productive within minutes.
That is what the public edition is for, and it is not what the first months look like. The first
customers are people we know, invited by name, given credits by hand while payment integration does
not yet exist.

That does not make self-service registration something to build later. It makes it something to
build now and **switch off**, and the interesting part is where the switch lives.

The tempting version is to hide the "sign up" button. That is not a switch, it is a decoration: the
route is still on the internet, still reachable by anyone who reads the JavaScript bundle, and still
creates accounts when called. So the check goes in the **handler** — one configuration value, one
refusal, close enough to the work that no other entry point can get past it, and covered by a test
that calls the endpoint directly rather than clicking through the console.

Keeping the code present and inert has a second benefit beyond honesty about the boundary: the path
is exercised by tests in both positions, so opening registration one day is a configuration change
rather than the first time that code has ever run against a real request.

The same applies to buying credits. The purchase endpoints exist and refuse while payment is
disabled, and the only way a balance rises in the meantime is the audited staff credit grant that
`staff-plane` already specifies for wrongful suspensions and payments in flight. The rule this
proposal cares most about survives intact: nothing inside a customer's own account can set a balance.

Meanwhile accounts arrive through `account-invitations`, which is a different route with a different
flow and is not governed by either switch — an invited person registers by accepting an invitation,
which is exactly what an invite-only period needs.

### One sentence the marketing site has to contain

Not about DNSSEC — that concern left with the DNS product, because nobody delegates a zone here. The
sentence that matters now is the boundary:

**The service answers ACME DNS-01 challenges and observes the certificates that result. It does not
host your DNS, does not issue certificates, and does not hold private keys.**

Every one of those is a thing a visitor may reasonably assume from the words "DNS" and "certificate"
on a page, and every one is cheaper to say there than to explain in a support reply after somebody
has signed up expecting it. The pitch — *you publish one CNAME, once, and never hand anyone your DNS
credentials* — only works if what is being declined is stated as plainly as what is offered.

### Not in scope

- **Anything about tenancy, permissions, roles or query confinement.** All of it belongs to
  `tenancy-and-permissions`.
- **The credit ledger itself** — accrual, overdraft, suspension, restoration, warnings. Those are
  `metering-and-credits`.
- **Invitations.** `account-invitations` owns the whole flow, including registration reached
  *through* an invitation link.
- **Certificate issuance, storage or distribution.** Unchanged: we answer challenges.
- **Social sign-in, SSO for public customers, SCIM.** The identity seam makes them additive.
- **Choosing when to open self-registration.** The switch is specified here; the decision to turn it
  on is commercial.

## What this change no longer contains

Everything below was in `add-accounts-and-public-edition` and has been taken over, in most cases in
considerably more depth. A reader of the old change needs to be told where its content went:

| Removed from this change | Now specified by |
| --- | --- |
| The whole `accounts-and-tenancy` capability — the account entity, ownership by inheritance, account-scoped queries, the single-account internal edition | `tenancy-and-permissions` |
| Roles held per account, the four fixed role names, and *"within an account there SHALL be no finer scoping"* | `tenancy-and-permissions`, which **contradicts** all three: roles become per-account records holding permission grants, and the set is no longer closed |
| The non-goal *"per-project or per-domain roles inside an account"* | Reversed by `tenancy-and-permissions`; per-project API keys and per-name DNS record grants are the reason it exists |
| *"A person may hold memberships in several accounts"* and the acting-account rule | `tenancy-and-permissions`, whose P19 makes the acting account a session selection that is additionally named on every request |
| `service-inventory` MODIFIED *"The inventory is projects, load balancers and domains"* | `tenancy-and-permissions` modifies the same requirement; keeping it here would be a direct delta collision |
| `service-inventory` MODIFIED *"An alias is one label in the served zone per challenge name"* | `tenancy-and-permissions`, likewise |
| *"A domain earns nothing until it is verified"* | Its ownership half is `tenancy-and-permissions`' *"A domain name is unique within an account and not across the service"*, which states the same rule and the same CNAME-as-proof argument. Its billing half has no home; see *Impact* |
| Plans, product quotas, subscription state | `metering-and-credits`, which replaces flat plans with prepaid credits |
| *"Suspension does not break certificate renewal without warning"* | `metering-and-credits`, which specifies a soft-suspension lifecycle in detail: existing service continues, new publishes are refused, zone-time accrues into an overdraft, and only exhausting the overdraft withdraws zones |
| `admin-console` *"The console makes the acting account unambiguous"* | `tenancy-and-permissions`' session selection, which gates functionality on a selected account rather than merely displaying one |
| Decision E6, alias labels as an edition behaviour | Already superseded before this collapse, by the archived change `generate-alias-labels`: label style is one setting, not an edition difference |

## Capabilities

### New Capabilities

- `credit-purchase`: buying credits through a payment provider — checkout, verified provider
  notifications, the derivation of balance increases from confirmed payments, and the absence of
  any interface for setting a balance by hand.
- `abuse-limits`: concurrency and rate limits that protect the shared service from any single
  account in any edition, independently of payment.

### Modified Capabilities

- `identity-and-roles`: authentication is delegated to an external identity provider in both
  editions, which hold no credential of their own; editions differ only in the tenant they
  authenticate against; the obligations that come with delegation — verified enrolment, verified
  recovery, non-disclosure, attempt limiting, and a reset that ends the service's own sessions — are
  stated as requirements on the deployment.
- `service-operations`: one artifact serves both editions with the edition selected at startup and
  reported at runtime; editions never share a datastore or a zone; public-facing content is
  released separately from the service; the acceptance environment exercises both editions.
- `admin-console`: the console gains the account-creation screen for a signed-in person who belongs
  to no account, the onboarding path to a first verified domain, and the account's credit balance and
  payment state. It gains no credential screens.

## Impact

- **`internal/oidcauth`.** Moves behind an interface with no behavioural change. The interface is
  the whole of the work; the existing flow is not rewritten.
- **`internal/localauth`.** Not built. The reversal above deletes it; what remains in its place is
  identity-provider configuration and the acceptance tests that prove the configuration is real.
- **The identity provider.** Becomes a hard dependency of every sign-in and an operational
  responsibility: a customer tenant separate from the staff one, backups, a tested restore, and a
  patching commitment.
- **Email delivery.** New operational dependency, and the first one whose failure locks a customer
  out rather than degrading a feature. `account-invitations` depends on the same delivery path.
- **Payment provider.** New external dependency and a webhook endpoint whose verification is
  security-critical. The endpoint is unauthenticated by nature and must be verified by signature.
- **`internal/config`.** `ACME_EDITION` and the settings each edition requires, validated at
  startup so a public deployment cannot start without the things a public deployment needs.
- **The console.** Account creation and purchase screens, and the onboarding path. No credential
  screens: enrolment, sign-in and recovery are handed off.
- **The acceptance harness.** Must run both editions, or the edition that is not used daily will be
  broken exactly when it matters.
- **Marketing site.** A separate deployment with its own pipeline and release cadence.
- **One requirement is left unhoused deliberately.** The billing half of *"A domain earns nothing
  until it is verified"* — that an unverified domain is not charged for — is not stated anywhere.
  It plausibly belongs beside `metering-and-credits`' *"Active zones are charged over time"*, but
  that change is already written and this one does not edit it. Note that under a credit model the
  billed unit is the zone rather than the domain, so the gap may be empty in practice; it is
  flagged rather than closed.
- **Nothing on the DNS or challenge answering path.** `internal/dnsserver` and `internal/zoneview`
  are untouched.
