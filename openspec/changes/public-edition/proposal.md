# Opening the service to strangers: enrolment, payment, limits, and the site that says what we do

## What this changes, in one paragraph

Everything in this repository up to here builds a service for customers we already know. They are
enrolled by hand at the identity provider, given credits by hand, and told what the service does in
person. This change is the set of decisions that stop being avoidable when the next customer is
somebody we have never spoken to: an enrolment flow they can walk on their own, a payment provider
behind the checkout that currently takes no money, limits that hold whether or not anybody has paid,
and a public site that states what the service does, what it does not do, who operates it, and where
to send an abuse report.

## Terms, before they are used

- A **personal account** is the free account every user has by being a user. Nobody creates one — it
  is materialized at first sign-in. Five projects, no colleagues, no roles, no balance, always
  usable. `personal-and-team-accounts` C1.
- A **team account** is created by paying, holds other people and the roles they are given, and works
  only while its credit balance is above zero.
- **Enrolment** is becoming a *user* at the identity provider — authentik's word for what a person
  does when they sign themselves up. It is a different act from creating an account, and after
  `personal-and-team-accounts` the difference matters less than it used to, which is one of the
  findings below.
- **Checkout** is `POST /api/v1/accounts/{accountId}/checkout`. It exists today, it offers a fixed
  set of amounts, it credits the account and writes an audit entry, and there is no payment processor
  behind it.
- A **protective limit** bounds what one account can do to a resource shared with every other
  account. It is not a price and not an entitlement, and it is the distinction this change spends the
  most words on.

## Why

### This change used to be about editions, and there is only one edition now

The version of this proposal being replaced had a service with two products in one binary. The
**internal** edition served our own operators: one account created at migration time, membership and
roles derived from identity-provider group claims, four fixed role names configured by environment
variable. The **public** edition served strangers: self-registration, payment, isolation. Its central
decision was that one artifact served both, with a setting called `ACME_EDITION` read at startup
selecting which implementations were wired in.

`personal-and-team-accounts` removed the internal edition. Not as tidying — as a precondition, and it
says so: *"every user has their own personal account" and "everybody shares the one account created
at migration time" cannot both be true, so there is no order in which this is optional.* With it went
`ScopeForBootstrap`, automatic account selection at sign-in, the group-to-role mapping and its
`ACME_*_GROUP` settings, derived membership, and the constraint that the internal edition's behaviour
must not change. `ACME_EDITION` was never built, and there is nothing left for it to select between.

That takes the spine out of this change. Several other changes have taken the organs, and rather than
leave a document that is mostly archaeology, this is a rewrite that says in one table where each
piece went and then argues only about what is left.

### What went where

| What this change used to own | Where it is now |
| --- | --- |
| One artifact, edition selected by `ACME_EDITION`, reported at startup and on the readiness endpoint | **Withdrawn.** There is one edition (`personal-and-team-accounts` C10). Validating settings at startup is `layered-configuration`'s |
| Accounts, ownership, query confinement, the acting account | `tenancy-and-permissions` |
| Two kinds of account, the credit **balance**, the checkout that raises it, suspension at zero, the owner's way back in | `personal-and-team-accounts` C1, C3, C4, C8 |
| The credit **ledger** that draws the balance down — prices, events, the tick, warnings, the operator credit grant | `metering-and-credits` |
| Authentication delegated to an identity provider; the service holding no password, hash or reset token | `authentik-and-self-signup` K1, which built it. Keycloak is gone, both consoles have their own OIDC client, and the acceptance suite signs in through authentik's flow executor |
| Checking a domain's delegation and explaining what is wrong with it | `domain-setup-feedback` |
| Whether the service works for a domain before its delegation has been seen | `domain-activation` |
| Invitations, and enrolment reached through one | `account-invitations` |
| Editions never share a datastore or a zone | **Reversed** — see below, because this is the one that deserved a real argument rather than a deletion |

### What is actually left, and why each piece is worth a decision

**Payment.** The checkout is built and takes no money. `personal-and-team-accounts` C8 built it that
way deliberately — *"the whole funding mechanism minus the processor"*, so that the balance, the
funded predicate, the suspension it drives, the console flow and the audit entry all get exercised
against real state while nobody can lose money to a mistake — and named this change as the owner of
the integration that replaces the middle step. That is a narrow, well-specified job: a hosted payment
page, a notification endpoint verified by signature, a balance that rises once per completed payment
however many times the provider retries, and refunds handled as recorded adjustments.

**Protective limits.** These have become considerably more important than when they were written, for
a reason worth stating plainly: after `personal-and-team-accounts`, **most accounts in a running
deployment are free ones.** A personal account is charged nothing, so nothing about a balance bounds
it. The first abusive account will not be a paying one, and a limit that applies only to paying
accounts protects nothing at the moment it is needed.

**Self-registration, which is a bigger decision than it was.** `authentik-and-self-signup` deferred
its enrolment flow here on the grounds that self-registration is a separate concern from signing in
and is not needed until strangers arrive. That is right. What has also happened is that the
*reasoning* behind this change's registration switch has been falsified, and the rewrite has to deal
with it rather than inherit it — see the next section, because it is the single most consequential
thing this document has to say.

**The published obligations.** `staff-plane` commits to a twelve-hour abuse response and says
elsewhere that it is published. `metering-and-credits` and `personal-and-team-accounts` both describe
a customer who needs to understand what they are buying. Nothing owns the publishing. This change
does, and it treats the contents as requirements rather than as copy suggestions, because a site that
ships as a signup button and three sentences leaves four other changes referring to commitments that
do not exist anywhere a customer can read them.

**Onboarding, now much smaller.** `domain-setup-feedback` and `domain-activation` between them own
almost all of what used to be the onboarding story. What is left is the first five minutes of an
account with nothing in it, which nobody else can own and which is mostly a screen decision.

**The staff/customer separation, partly built.** Two applications with separate OIDC clients, two
hostnames, a group-restricted staff application and a brand exist in the development stack. External
user types, an enrolment flow that the staff application must not have, administrator multi-factor,
and the two ingress rules the hostname split actually depends on do not exist anywhere.

### The finding that changes this proposal: the registration switch no longer gates what it was built to gate

This is worth its own section, because it is the sort of thing that stays wrong quietly.

The switch exists and works. `ACME_ALLOW_ACCOUNT_CREATION` defaults to false and is enforced in the
handler — `backend/internal/httpapi/admin_accounts.go` refuses `POST /api/v1/accounts` with
`account_creation_closed` — exactly as this change asked, with the enforcement in the server rather
than in the console's navigation, because a route hidden from a menu is still on the internet. The
comment beside the setting cites this change's U9 by number.

What has changed is what that route means. When U9 was written, creating an account meant *becoming a
tenant*, and the argument rested on a sentence: a person who has somehow enrolled but cannot create
an account has a session and nothing to do with it — no inventory, no zones, no spend.

**That sentence is now false.** `personal-and-team-accounts` made the personal account implicit, and
`internal/oidcauth/flow.go` materializes it at sign-in unconditionally. Anyone who can enrol at the
identity provider gets a free account with five projects and can publish challenge values in it.
`POST /api/v1/accounts` now creates a **team** account by paying, so the switch guards the commercial
flow, not the front door.

The front door is the identity provider's enrolment flow — which does not exist, so the service is
closed by accident rather than by decision. Three consequences follow, and the design works each of
them out:

- **The free personal account is the free tier.** Nobody decided to offer one; it fell out of the
  account model. It is a good answer and should be adopted deliberately, which also closes an open
  question this change used to carry — *free tier, or trial only?* — from the other end.
- **Protective limits become the floor rather than a precaution**, because they are the only thing
  bounding an account that pays nothing.
- **There is no one switch.** There are three things — the enrolment flow, personal-account
  materialization, and the account-creation setting — and believing any single one of them closes the
  door is the failure mode. The design's U9 sets them out in a table and argues against adding a
  fourth.

### The one decision that deserved an argument rather than a deletion

The removed decision list above has a line in it saying "reversed", and it is the rule that two
editions never share a datastore or a DNS zone.

The edition axis is gone, so the rule as written has nothing to separate. But it was saying two
things, and only one of them was about editions. The other was: *do not keep our own inventory in the
same database as customers'.* A single-edition service can still want that, so it is argued rather
than dropped.

It is rejected, and the reasoning is that the argument has inverted. Confinement — reads gated inside
the query by row-level security — is the entire safety property of this product, and the strongest
continuous test of it is having data in there we would notice leaking. `tenancy-and-permissions` task
11.4 is the cautionary tale: four cross-account reads had been silently emptied, one of them stopping
certificate issuance altogether, and it took a full run from a destroyed database to find. Keeping
our own domains somewhere else means nobody who would notice ever spends a day inside the property we
are selling.

The blast-radius arithmetic has changed too. The original argument imagined one database holding our
fleet's inventory next to *strangers' payment records*. With a hosted payment page there are no
payment records here — a provider reference and an amount — and with delegated authentication there
are no password hashes. What one database holds is domain names, alias labels, challenge values and
email addresses, ours and customers' alike. That is not a compliance surface worth a second
deployment, a second migration chain, a second backup to test and a second thing to patch, for one
operator and a handful of our own domains.

So our own domains live in an ordinary account beside everybody else's. What survives of the rule is
smaller and still real: **environments** never share a datastore or a zone — the development stack
seeds users whose password is `devpass` — and the service's own hostnames are served by something
that is not this service, so the page a customer visits to find out what is happening survives the
thing being down.

## What Changes

- **A payment provider behind the existing checkout.** Hosted payment page, a notification endpoint
  verified by signature, one credit per completed payment however often it is delivered, refunds and
  reversals as recorded adjustments, and no interface anywhere that sets a balance by hand. Gated by
  configuration, so it lands disabled and is switched on separately.
- **Protective limits** on simultaneously published challenge values and on publication rate, per
  account, independent of balance and of account kind, with a refusal that names the limit and when
  it clears and is distinguishable from the `402` an unfunded team account gets.
- **A self-service enrolment flow** at the identity provider, with address verification,
  non-disclosure, and attempt limiting — and mail configured, because every one of those flows sends
  a message and until then they all fail silently at the last step.
- **The rest of the staff/customer separation**: external user types for customers, no enrolment flow
  on the staff application, administrator multi-factor, and the two ingress rules without which the
  hostname split restricts nothing.
- **A marketing site**, static, its own pipeline, its own domain, linking into the console's sign-in
  route — carrying the published obligations, and not released without them.
- **A first-run path** for an account with nothing in it, over endpoints that already answer.

## Capabilities

### New Capabilities

- `credit-purchase` — buying credits through a payment provider: checkout, verified provider
  notifications, balance increases derived from confirmed payments, and the absence of any interface
  that sets a balance by hand.
- `abuse-limits` — bounds on what one account can do to a shared resource, applying whether or not
  the account is charged for anything.

### Modified Capabilities

- `identity-and-roles` — a person may become a user by enrolling themselves; the obligations that come
  with delegating that (verified enrolment, verified recovery, non-disclosure, attempt limiting, a
  bounded post-reset session window) are stated as requirements on the deployment; and the staff and
  customer surfaces are separated within the one identity provider.
- `service-operations` — the public site is delivered separately from the service and states what the
  service does, does not do, commits to and holds; deployments of different environments share
  neither datastore nor zone.
- `admin-console` — first run for an empty account, and an account's own view of what it has bought.
- `acceptance-harness` — the commercial path is exercised before release: enrolment, a purchase
  through the provider, and a protective limit refusing an operation.

## Impact

- **The identity provider.** An enrolment flow, a recovery flow, mail, a reputation policy, and
  customer users typed `external`. Then a production instance at `auth.acmealias.com` that does not
  exist: backups, a tested restore, and a patching commitment for an internet-facing authentication
  service. `internal/adminctl` remains the way in when it is down.
- **Mail delivery.** A new operational dependency, and the first one whose failure locks a customer
  out rather than degrading a feature. Shared with `account-invitations` and with
  `metering-and-credits`' balance warnings, so one delivery failure has three customer-visible
  consequences.
- **Payment provider.** A new external dependency and a public notification endpoint whose signature
  verification is security-critical, because it is an unauthenticated endpoint that raises balances.
- **`internal/config`.** The payment settings and the protective limits, as configuration rather than
  constants for the same reason the project quota is. `ACME_ALLOW_ACCOUNT_CREATION` already exists;
  its doc comment needs correcting, because it explains itself in terms of "creating a tenant" and
  what it now gates is buying a team account.
- **The challenge API.** The protective limit check, and its refusal kept distinguishable from
  `personal-and-team-accounts` C7's `402`.
- **The console.** First run, and a billing view showing what has been bought. No credential screens:
  enrolment, sign-in and recovery are handed off.
- **The ingress.** Two Traefik rules and a test that the staff tree answers 404 through the customer
  hostname.
- **Marketing site.** A separate deployment with its own pipeline and release cadence, gated on U11's
  contents.
- **Nothing on the DNS or challenge answering path** beyond the limit check.
  `internal/dnsserver` and `internal/zoneview` are otherwise untouched.

## What this does not solve, and should be read as a decision

- **The numbers are not chosen.** How many simultaneous challenge values, at what rate. This change
  decides that limits exist, are per account, and are not purchasable; it does not decide the values,
  because nobody has traffic to decide them from. They are configuration.
- **Neither is the payment provider.** The shape is specified — hosted page, signed notification,
  idempotent credit — and the vendor is not. The one non-negotiable property is that the payment page
  is hosted by them, because that is what keeps card data out of this deployment.
- **When enrolment opens is a commercial decision.** The flow is built here so that opening it is
  configuration rather than the first run of untested code. Choosing the day is not made here.
- **Back-channel logout is deferred.** A credential reset at the provider ends the provider's
  sessions and not ours. The bound is a short idle expiry, which is a bound rather than a fix, and it
  is stated rather than implied.
