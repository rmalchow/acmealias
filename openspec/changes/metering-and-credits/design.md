# Design

Decisions are numbered `M1`, `M2`, … and referred to by number from `tasks.md` and from code
comments.

## Context

Two things this service does cost money to provide, and they have different economic shapes. A
challenge publish is a discrete authenticated action, initiated by the customer, countable at the
moment it happens. A node configured for certificate observation (`health-checks`) is a continuing
arrangement: nothing happens at a moment, something is true across an interval.

This change is the **ledger and the meter**. It is not the balance, and it is not payment.
`personal-and-team-accounts` already adds the credits column, the checkout that raises it (C8), the
rule that funded means `credits > 0` and nothing else (C3), and what a suspended account may still do
(C4). By the time this change lands, an account can be topped up and can run out. What it cannot yet
do is *spend* — every activity is free, so the balance only ever goes up. This change is the other
half: the events, the prices, and the deductions that draw the balance down.

That split is why this change waits. Credits are granted by hand while the customers are people we
know, and a balance that never falls is a perfectly usable placeholder for as long as that is true.

Terms used throughout, defined once:

- **Event** — an immutable record that something meterable happened: an account, a type, a quantity,
  and the period it covers. Never edited.
- **Rating** — turning an event into a number of credits, using the price book.
- **Price book** — the mapping from event type to credits. Data, not code.
- **Deduction** — a credit ledger row reducing a balance, recording the price it applied.
- **Rate event** — an event whose quantity is a count sustained over an interval rather than a count
  of occurrences.
- **Tick** — one scheduled emission of rate events for one account for one period.

Constraints that shaped everything below:

1. **No billable action may be triggerable by someone other than the account holder.**
2. **The balance the funding check reads must be current**, not eventually consistent —
   `personal-and-team-accounts` C7 puts that check on the challenge path.
3. **Revenue must not depend on a scheduled job having run on time.**
4. **The invoice follows what the customer configured**, not what our infrastructure did about it.

## What this change used to be, in two paragraphs

It was written for a service that hosted customers' zones. There was a third economic shape,
`zone.active` — a charge for zones existing, accruing hourly whether or not the customer did
anything — and it was the interesting one, because it was the only charge that could drain a balance
while the customer slept. Most of the machinery here was built for it: the tick job, its idempotent
period key, the backfill-across-an-outage rule, an overdraft to carry a lapsed customer, and a
terminal state that withdrew a zone from the fleet and answered `REFUSED` rather than `NXDOMAIN` so
that payment restored service in seconds rather than after a negative-cache TTL. There are no
customer zones, so all of that is gone. The tick job survives because M4 still needs a rate event;
the terminal state does not survive at all, because there is nothing left to withdraw — a suspended
account is refused at `/present` and its already-published challenge values expire on their normal
schedule.

Checks were the other shape and they have shrunk. `health-checks` was a satellite fleet probing
customers' nodes every thirty seconds under quorum, which produced tens of thousands of probe results
per customer per day and a genuine question about how to bill something so multiplied. It is now one
TLS handshake per node address, a few times a day, in-process. The *rule* that came out of that
question is unchanged and is still the right one (M4) — bill the configured node, not the
observation — but it now protects against a much smaller number.

## Goals / Non-Goals

**Goals:**

- One event shape covering counts and continuing arrangements, immutable and disputable.
- Pricing that moves independently of the code that produces events.
- A balance accurate enough for `personal-and-team-accounts`' funding check to read on the challenge
  path.
- Warning that arrives before zero and is useful to accounts of every size.

**Non-Goals:**

- The balance, the checkout, and the behaviour at zero. All three arrive with
  `personal-and-team-accounts`; this change spends against them.
- Payment processing, invoicing, tax, currency.
- Deciding actual prices.
- Committed spend, discounts, negotiated pricing.
- Product tiers as an entitlement. Deliberately absent, and M10 explains why.
- Metering as an observability product — these events exist to be charged for, and `usage-reporting`
  continues to answer operational questions separately.

## Decisions

### M1. Events and prices are separate tables, and rating is still synchronous

The separation is about *where the price lives*, not about *when it is applied*.

```
events      (id, account_id, type, quantity, unit, period_start, period_end, source_ref)
price_book  (event_type, credits_per_unit, effective_from)
deductions  (id, account_id, event_id, credits, price_applied, at)
```

An event is written and rated in the **same transaction**. Constraint 2 requires it: the balance is
what the funding check reads, and a balance that lags a batch interval would refuse an account that
has already paid, or serve one that ran out an hour ago.

What the separation buys is metering without billing, disputes answerable from the events, and prices
that change without touching the code that produces events. It costs nothing, because "synchronous"
and "separate" are orthogonal — deferred or batch rating remains available later with no schema
change.

`price_applied` on the deduction is not redundant with the price book. Without it, a price change
retroactively alters what any recomputation concludes about a past charge, which is precisely the
class of bug that destroys trust in a ledger.

**Rejected: one table holding "the customer owes 3 credits".** Fewer joins, one write. Rejected
because it fuses a fact that never changes with a decision that changes often, and makes "why was I
charged this" unanswerable once the code has moved on.

### M2. The unit is a count or a count over an interval

```
type                  unit    quantity   period
challenge.publish     count   1          instant
node.observed         rate    4          14:00–15:00      (4 configured nodes × 1 hour)
dns.query             count   18422      14:00–15:00      metered, priced at zero
```

Two unit kinds cover everything: things that happen, and things that persist. An arrangement that
persists is not a second billing mechanism, it is an event whose unit is a rate — which means the
tick job (M5) is just another event producer, and there is one ledger rather than a ledger plus a
subscription system.

`dns.query` is present deliberately and priced at zero. Per-query *billing* is rejected (M3), but
per-query *metering* is safe and informative, and the only way to know whether it could ever be
priced is to have counted it. Collecting it costs a counter on a path that already has metrics.

### M3. The challenge path is billed per publish, and this is a security decision

Constraint 1 is the whole argument, and it is worth restating in the design because it will look like
a pricing preference to a future reader.

The alias label a customer's `CNAME` points at is public. It has to be: a certificate authority
follows that `CNAME`, so `_acme-challenge.shop.example.com → k4m2p9.acmealias.com` is world-readable
in the customer's own zone. Anyone can therefore find where a given customer's challenges are
answered.

If a query were billable, a stranger could spend the customer's balance from a shell loop, and the
consequence is not a large invoice — it is a **failed renewal and an outage at the next certificate
expiry**. An attacker with no access to anything takes down a site by making the victim's prepaid
balance run out.

So the billable moment is the publish: authenticated with the account's own key, initiated by the
account, and bounded by its own behaviour. Answering queries is free, which is also honest — a lookup
in the in-memory view costs microseconds.

The deduction happens in the same transaction as the publish, at the point
`personal-and-team-accounts` C7 already established the account, so `/present` refuses against a
balance that is current rather than one that lags.

Per-query billing is also unpredictable for honest use, which is the weaker argument and worth
recording anyway: a single ACME validation is genuinely multi-query, because certificate authorities
validate from several vantage points and retry.

### M4. The billable observation event is derived from configuration, and zero-priced events write no deduction

`health-checks` observes a certificate by completing a TLS handshake against each node address, a few
times a day. Those handshakes are not what is billed.

**What is billed is a rate event: `node.observed`, one row per account per period per node configured
for observation**, emitted by the tick job (M5) as a count over an interval.

The reason is constraint 4. The customer configured *a node to be observed*. They did not choose the
interval, and they did not choose that a dual-stack node is two handshakes rather than one — both are
ours, and `health-checks` H6 says plainly that we intend to change them. Billing per handshake would
mean an invoice rising because we decided to look more often for our own confidence, or doubling
because a node acquired an IPv6 address. Billing per configured node per period keeps the
multiplication on our side of the line, which is the same side as the engineering decisions that
cause it.

This rule was written when checking was a satellite fleet producing ~69,000 probe results per
customer per day, and it mattered enormously. At an hours-scale in-process handshake it protects a
much smaller number, and it is kept anyway, because the principle is what makes an invoice
predictable and the principle did not change with the volume.

**A zero-priced event writes an event and no deduction.** A deduction of zero credits is not a fact
anyone will ever dispute; it is a row recording that nothing happened to a balance. The `deductions`
table stays what M1 wants it to be — the evidence for money that moved. This is what keeps
`dns.query` from doubling the highest-volume table in the system.

**Observations live outside the ledger.** They are operational data, kept in their own table with
their own retention (`health-checks` H5). The ledger keeps `events` and `deductions` indefinitely,
because those are the evidence for a charge and they stay small enough for a person to read in a
dispute.

### M5. The tick emits rate events per account, idempotently, catching up on every run

One row per account per period per configured node, carrying the count and the rate:

```
account 42   node.observed   quantity=4   unit=rate   period 14:00–15:00
```

**Idempotent and self-healing.** The tick is keyed `UNIQUE (account_id, type, period_start)`, and each
run emits **every unbilled period since the account's last one**, not "the previous hour":

```sql
-- for each account, for each period in (last_billed, now):
INSERT INTO events (account_id, type, quantity, unit, period_start, period_end)
VALUES (...) ON CONFLICT (account_id, type, period_start) DO NOTHING;
```

A job down for six hours writes six rows on its next run. A job that runs twice writes one. A deploy
that skips a tick costs nothing. Constraint 3 satisfied: revenue does not depend on cron.

The precedent is in the schema already: `project_daily_stats` is `primary key (project_id, day)`, a
period-keyed rollup for the same reason.

**Backfill charges for the arrangement, not for the observations.** A period the tick job missed is
still charged, because the node was configured for observation throughout it and the observer was
running. That the job which counts it was not running is our operational problem and not a discount —
and the tick can determine it from configuration state it holds rather than from anything the
observer reported, which is the whole point of M4. Stated as one rule: **the meter follows what the
customer configured, not what our infrastructure managed to observe.**

The condition on that is the obvious one: a node is billed as observed for a period only if it was
configured for observation in that period.

**Rejected: lazily computed balances.** `balance = purchases − Σ(nodes × elapsed)` is exact at every
instant with no job at all. Rejected because the balance stops being a column and becomes a
computation every reader must perform identically — including the funding check on the publish path
(M9) — and because a rate change mid-period turns a subtraction into a piecewise integral.

**Rejected: emitting one row per node.** Most granular and most obviously auditable. Rejected on
volume for information that one row per account per period already carries, since it carries both the
count and the rate that produced it.

### M6. This change spends the balance; it does not own it

Worth stating explicitly, because two earlier drafts of this document specified a balance and a
checkout that now arrive before it.

`personal-and-team-accounts` owns:

- the credits column on the account, held above projects, so a customer with staging and production
  has one balance (C1, C3);
- `POST /api/v1/accounts/{accountId}/checkout`, the real charge with a fixed set of amounts and no
  processor behind it (C8);
- what happens at zero — refusal at `/present` with `402`, account selection restricted to the owner,
  and the console reduced to the balance and the way to fix it (C4, C5);
- the separation between a funding suspension and a staff suspension, so that neither can lift the
  other (C6).

This change owns the events, the price book, the deductions, and the tick. It adds nothing to the
account row except the deductions that move the number already there.

The practical consequence for implementation order: the funding check and its `402` already exist and
are already tested by the time this lands. What is new is that the balance now falls.

### M7. There is no overdraft. An account stops at zero, and the overshoot is discarded

This document previously argued for a flat per-account overdraft under the rule *"the overdraft keeps
what you have alive; it does not buy you anything new"* — a lapsed account kept serving the zones it
had and was refused only new ones.

**That was reversed, deliberately, by `personal-and-team-accounts` C4**, and the reasoning is worth
having here rather than only there. An overdraft needs a third state: "funded", "overdrawn but
serving", and "stopped" are three behaviours, three predicates and three explanations in a console,
and the middle one is the hardest to describe to somebody who has not paid. And its kindness is
largely an illusion at this timescale — refusing `/present` does not take a customer's site down, it
stops renewals, and the certificate on the wire keeps working until it expires. The customer has
weeks either way. An overdraft buys a softer version of a consequence that is already soft, at the
price of a state nobody can explain.

Removing zone-time is what made that reversal cheap. The overdraft existed because a charge could
drain a balance while a customer did nothing; nothing does that any more. `node.observed` accrues
only for nodes an account configured, and an account that has configured none accrues nothing at all,
however long it sits there.

So the balance floors at zero rather than going negative. If a tick's deduction would take a balance
below zero, **the excess is discarded** — not carried as debt, not chased, not reserved in advance.
The bounded loss is one period's charge for one account. Reservation bookkeeping — holding credits at
dispatch and settling later — costs more to build and to explain than the amount it protects.

**What the overdraft was really protecting against survives, and it is not the refusal — it is the
surprise. The mitigation is not a different overdraft, it is a different warning** (M8).

**Rejected: carrying the overshoot as a debt.** It is exact, and it is a collections process for
fractions of a credit against customers who by definition are not paying.

### M8. Warn on projected time to zero

Balance is not a useful number to show. The same figure means fifty days for one customer and five
hours for another, and neither can tell which they are from the number alone.

**Projected time to zero** is comparable across every account, requires no per-tier tuning, and is
the same arithmetic already performed to know when a funding suspension will arrive. It is what the
console shows beside the balance, and it is what the threshold for warning is expressed in.

Notification must not depend on the service being usable by the account being notified, so it is
email as well as a console banner, and it fires well before zero rather than at it. What still slips
through — a payment in flight, a disputed charge, a wrongful accrual — is handled by the staff plane's
credit-granting permission, which exists for exactly this.

### M9. The balance read is on the publish critical path and must stay cheap

`/present` gains a deduction inside the transaction that already carries the publish.
`tenancy-and-permissions` required that authorization add no query to that path, and
`personal-and-team-accounts` C7 already put a funding check on it; this change must not add a second
read.

The balance is therefore a column on the account, maintained by the deduction, not an aggregate
computed per request. This is the concrete reason M5 rejects lazily computed balances: a lazy balance
would put a scan on the hot path of every publish.

### M10. There is no tier column: what a customer bought is what they configured

The product is sold at two levels — challenge answering alone, and challenge answering plus
certificate observation. That is the sales conversation, and it is tempting to make it a column:
`accounts.tier`, a gate on enabling observation, a monthly base charge per level.

**Rejected, and this is a decision rather than a deferral.** A tier is a second mechanism that can
stop an account, sitting beside the one that already exists. Every gate has to be written, tested,
supported and explained; every upgrade and downgrade is a state transition with a partial state in the
middle; and none of it answers a question the ledger does not already answer. A customer on the higher
level is a customer who configured a node for observation. A customer on the lower one is one who did
not. The level is an *observation about their configuration*, not a permission granted to them.

So the levels are real on the marketing page and absent from the schema. What a customer configures
produces events; events cost credits; credits run out. **One mechanism can stop an account, and it is
the balance** — which is exactly the shape `personal-and-team-accounts` C3 and C6 already give it.

Two things this forfeits, named so the trade is explicit. There is no recurring base charge, so
revenue is entirely usage-shaped — a customer who configures nothing pays nothing, however long they
have been a customer. And there is no way to sell a capability without selling its usage, so
"unlimited observation for a fixed monthly fee" is not expressible. Both are acceptable at this size
and both are reversible: adding an entitlement later is additive, whereas removing one that customers
have bought is not.

## Risks / Trade-offs

- **An attacker drains a customer's balance** → the only attacker-triggerable action, the DNS query,
  is not billable (M3); every billable action is authenticated as the account.
- **A missed tick loses revenue or delays a suspension** → catch-up over all unbilled periods, keyed
  idempotently (M5).
- **A price change rewrites history** → the applied price is recorded on the deduction (M1).
- **A customer runs out with no warning** → projected time to zero, delivered by email as well as in
  the console, ahead of zero (M8).
- **A period's charge is silently forgiven at the floor** → accepted and bounded at one period per
  account (M7); the alternative is a collections process worth less than it costs.
- **The publish path gets slower** → the balance is a column, and the deduction shares the existing
  transaction (M9).
- **Metered-but-unpriced events accumulate cost with no revenue** → `dns.query` is aggregated per
  period per account, not per query (M2), and writes no deduction (M4).
- **Observation volume reaches the invoice** → it cannot: the billable unit is the configured node
  (M4).

## Migration Plan

1. Add the event ledger, the price book and the deduction ledger. The balance column already exists.
2. Seed the price book, with `dns.query` priced at zero.
3. Add synchronous rating: event and deduction in one transaction, recording the applied price.
4. Add the publish deduction to `/present`, inside the existing transaction and behind the funding
   check that is already there.
5. Add the tick job emitting `node.observed`, keyed idempotently, catching up over unbilled periods.
6. Stop observation scheduling for an account that is not funded.
7. Add the query counter, aggregated per account per period and priced at zero.
8. Add balance history, projected time to zero and event listing to the console; add the warning
   notifications.
9. Add credit granting to the staff surface.

**Rollback**: with an empty price book every event rates to zero, so no balance moves and nobody is
suspended by this change. The ledger can therefore run in metering-only mode in production before any
charge is made, which is also the recommended way to deploy it — and the reason task 10.1 says to.

## Open Questions

- **What is the tick period?** Hourly is the working assumption — fine enough for the projection in
  M8 to be meaningful, coarse enough that one row per account per period is small. It is now a much
  looser constraint than it was, since nothing accrues for an account that configured nothing.
- **How long are events retained?** They are the evidence for disputes, so longer than the deductions
  might suggest. `dns.query` is the highest-volume and least valuable type and is the case to decide.
- **Is `node.observed` billed for a period in which every handshake failed?** M5 says the
  configuration governs, which bills it. The opposite reading — that a period in which we could see
  nothing was not a period of service — is defensible, and would need a rule for how much failure is
  too much. Note that `health-checks` H3 already records those failures, so the data to decide it
  either way exists.
- **Should the price book be per account?** Committed spend and negotiated pricing would need it, and
  the table is already shaped for it, but nothing requires it yet.
- **Is a personal account metered at all?** `personal-and-team-accounts` C1 gives it no balance, so
  it cannot be charged. Recording its events anyway costs little and is the only way to know what a
  free tier actually costs us.
