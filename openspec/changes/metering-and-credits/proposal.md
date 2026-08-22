## Why

### Three things now cost money, and none of them can be charged the same way

`dns-records` makes the service hold zones and answer for them continuously. `health-checks` makes a
fleet of satellites probe customers' nodes on a schedule. The challenge service publishes values on
demand. Those are three genuinely different economic shapes:

| | Publishes | Checks | Zones |
| --- | --- | --- | --- |
| Triggered by | The customer, explicitly | A schedule | Nothing — existence |
| Observed at | The centre | A satellite | Neither; it is a state |
| Countable as | A count | A count | A count over time |
| Can drain a balance while idle | No | Yes | **Yes** |

A single billing mechanism that handles all three does not exist, and pretending otherwise produces
either a meter that cannot express continuous cost or a subscription that cannot express usage.

### Billing for something a stranger can trigger is a vulnerability, not a pricing mistake

This is the decision worth explaining at length, because the obvious design is dangerous.

The intuitive way to charge for the challenge service is per DNS query answered — it tracks the cost
of serving most directly. It is unsafe, and the reason is structural rather than a matter of degree.

The challenge name is **public by construction.** The customer publishes, in their own zone, a
`CNAME` from `_acme-challenge.shop.example.com` to our alias label, say
`k4m2p9.acme.example.net`. That record has to be world-readable, because a certificate authority has
to follow it. So anyone can discover where a customer's challenges are answered, and anyone can then
query that name in a loop.

If queries are billable, that loop spends the customer's credits. When the balance runs out,
certificate renewal fails, and the next expiry takes the customer's site down. That is a remote
denial of service executed with `dig` and a shell loop, against a victim who did nothing wrong.

Rate limiting does not fix it, because the principle is the problem: **you cannot bill for an action
an attacker can trigger.** Per-query billing is also unpredictable for honest use — a single ACME
validation is genuinely multi-query, as certificate authorities validate from several vantage points
and retry.

So the challenge path is billed **per publish**: an authenticated act, initiated by the customer
through their own API key, countable, attributable, and bounded by their own behaviour. Queries are
served free, which also happens to reflect the real cost, since answering from an in-memory map is
microseconds.

### Metering and pricing are different problems and should not be one table

The instinct is to write a row that says "the customer owes 3 credits". That conflates two things
that change at completely different rates: *what happened*, which is a fact and never changes, and
*what it costs*, which is a commercial decision that changes whenever someone decides it should.

Keeping them apart buys three things that are hard to retrofit:

- **You can meter without billing.** Per-query billing is rejected above — but per-query *metering* is
  harmless and genuinely useful. Counting queries tells you the real distribution before anyone
  decides whether it is ever priceable, and it costs nothing to collect alongside everything else.
- **Disputes become a query.** "Why was I charged this" is answered by showing the events, rather than
  by reconstructing what the code did three months ago.
- **Prices change without touching producers.** Satellites emit events forever; the price book moves
  independently of the fleet.

One caution has to come with that separation, because it is the way it usually goes wrong: separating
the *concerns* must not mean deferring the *timing*. Suspension depends on a current balance, so
rating stays synchronous — the event is written and rated in the same transaction — while the price
lives in a price book rather than in code. And every deduction records the price that was applied, or
a later price change silently rewrites history for anyone who recomputes.

## What Changes

### An immutable event ledger, in one normalized shape

Everything meterable becomes an event: an account, an event type, a quantity, and either a count or a
count over an interval.

```
account 42   challenge.publish    N = 1        at 14:02:11
account 42   check.reported       N = 4        for cycle 14:00–14:01
account 42   zone.active          N/t = 3 zones × 1h   for 14:00–15:00
account 42   dns.query            N = 18422    for 14:00–15:00     (metered, not billed)
```

Events are append-only and never edited. Producers are the centre (publishes), satellites (check
reports, query counts), and a scheduled job (zone-time). Every event carries the identity of the batch
or period that produced it, so re-delivery is a no-op — `satellite-sync` already requires batch
identifiers, and here a double count is a wrong invoice.

### A price book turns events into deductions

A separate table maps event type to credits. Rating happens in the same transaction as the event, and
the deduction row records the price it used. Changing a price affects future events only.

### Three units, three mechanics

**Publishes** are deducted synchronously at `POST /present`, centrally, against an accurate balance.
Nothing about this is eventually consistent.

**Checks** are deducted when the report is **delivered**. Before delivery a scheduled check is only a
promise, and a promise is not billable. If a deduction would take the balance below zero, the excess
is **dropped** rather than carried as debt — no reservations, no collections, and a bounded loss of
at most one report cycle.

That is safe precisely because suspension is soft (below). It has a useful corollary for satellite
design: an undelivered report has cost the customer nothing, so a satellite may discard a batch it
cannot deliver rather than spooling it forever, which is what keeps a satellite a replaceable
stateless process.

**Zone-time** accrues continuously. A scheduled job writes real ledger rows so the balance stays a
plain column, but it ticks **per account, not per zone**: one row per account per period, carrying
the zone count and the rate that produced it. A thousand zones is one row rather than a thousand, and
the derivation stays visible on the row.

Ticks are idempotent and self-healing: keyed on `(account_id, period)` with a unique constraint, and
each run charges *every unbilled period since the last one* rather than "the last hour". A six-hour
outage writes six rows on the next run; a job that runs twice writes one. The precedent is already in
the schema — `project_daily_stats` has `primary key (project_id, day)` for exactly this reason. The
point is that **revenue stops depending on cron reliability**, which matters more once the meter also
drives suspension.

### The lifecycle: soft suspension, then a terminal state

```
balance > 0            everything works

balance ≤ 0            SOFT SUSPENSION
                       DNS keeps resolving          ✓
                       new publishes refused (402)  ✗
                       checks paused                ✗
                       zone-time still accruing → into the overdraft

balance ≤ -overdraft   TERMINAL
                       the zone is dropped; satellites answer REFUSED
```

The governing rule, worth stating as a rule because it is what stops the overdraft becoming free
credit: **the overdraft keeps what you have alive; it does not buy you anything new.** Publishes and
checks stop at zero. Only zone-time may go negative. You can coast on an overdraft; you cannot run a
workload on one.

### Why REFUSED and never NXDOMAIN

When the terminal state is reached, the zone is **dropped** — the satellite is no longer authoritative
for it — rather than kept and answered with `NXDOMAIN`. Same commercial effect, wildly different
recovery, and the difference is entirely about resolver caching:

- **`NXDOMAIN` is an authoritative denial.** Resolvers cache it per RFC 2308 for the `SOA` minimum,
  typically 300–3600 seconds. So a customer who pays stays broken for the negative TTL. Worse, RFC
  8020 permits a resolver to conclude that *nothing exists below* a name that returned `NXDOMAIN`, so
  a denial at the apex can poison every subdomain in a compliant resolver's cache. It is the one
  option where recovery is slower than the outage.
- **`REFUSED` means we are not authoritative for that name.** Resolvers treat it as a server-side
  condition, try the other nameservers, and do **not** negatively cache the name. Top up, the zone is
  redistributed over the sync stream, and resolution resumes in seconds because there is nothing
  cached to expire.

For a prepaid product whose entire commercial model assumes people top up and come back, an hour of
unnecessary post-payment outage is the difference between a support ticket and a churn event.

### Warn on time-to-zero, not on balance

The overdraft is a flat number per account. That has an honest consequence: the grace *period*
inversely tracks account size. Two zones might coast for fifty days on a hundred credits; five hundred
zones burn the same hundred in hours.

The mitigation is not a different overdraft, it is a different **warning**. "You have 100 credits
left" is meaningless when it means fifty days for one customer and five hours for another. "You have
about six hours" is actionable for both, requires no per-tier tuning, and is the same arithmetic
already being done to know when the terminal state arrives. What still slips through is covered by the
staff plane's credit-granting permission.

### Not in scope

- **Payment.** Taking money and turning it into credits is a separate integration. This change is the
  ledger those credits land in.
- **Invoicing, tax, currency.** The ledger is in credits.
- **Setting prices.** The price book exists; what goes in it is commercial.
- **Usage-based tiers, committed spend, discounts.** A price book per account is where they would go,
  and nothing here forecloses it.

### What we considered and rejected

**Per-query billing for the challenge path.** Rejected above at length: the challenge name is public,
so the billable action is attacker-triggerable.

**Reserving check credits at dispatch and settling on report.** It keeps the balance from ever being
optimistic, which matters if a stale balance can trigger a hard stop. Rejected because suspension is
soft, so a stale balance costs at most one report cycle of unbilled checks — which is cheaper than
reservation bookkeeping, and is written off rather than chased.

**Ticking zone-time per zone.** One row per zone per period is the most granular and most obviously
auditable. Rejected on volume: a thousand zones ticked hourly is twenty-four thousand rows a day for
information fully captured by one row per account carrying the count.

**Lazily computed balances** — balance as a function of purchases minus elapsed zone-time. No ticks,
no rollups, exact at any instant. Rejected because the balance stops being a column and becomes a
computation that every reader must perform identically, including the suspension check on the hot
path.

**A hard stop at zero.** Unambiguous and impossible to freeload. Rejected because it takes a paying
customer's site down over a lapsed card, and because the support cost exceeds the revenue recovered.

**Freezing the meter during suspension.** Generous and simple. Rejected because it makes "suspended"
strictly cheaper than "active" while DNS still resolves, which rewards lapsing.

## Capabilities

### New Capabilities

- `usage-metering`: the immutable event ledger — its normalized shape as a count or a count over an
  interval, its producers, its idempotency, and metering that is deliberately not billed.
- `credit-ledger`: the price book, synchronous rating, deductions that record the price applied, the
  balance, per-account zone-time ticks that are idempotent and self-healing, and the overdraft.
- `account-suspension`: the lifecycle from full service through soft suspension to the terminal state,
  what each permits, and how service is restored.

### Modified Capabilities

- `challenge-dns-server`: a zone that has been withdrawn is answered as a name outside the served
  zones — `REFUSED` — rather than being retained and denied with `NXDOMAIN`.

## Impact

- **New schema.** An append-only event ledger, a price book, a credit ledger of purchases and
  deductions, per-account balance, and the zone-time tick keyed on `(account_id, period)`.
- **`POST /present`.** Gains a synchronous deduction in the same transaction as the publish, and a
  `402` when the balance is not positive. This is the only change on the challenge critical path and
  it must not add a query beyond the balance read.
- **`satellite-sync`'s report intake.** Check reports become metering events on delivery; batch
  identifiers, already required there, become financially load-bearing.
- **`health-checks`.** Scheduled checks are paused for a suspended account.
- **`dns-records`.** A zone is withdrawn from distribution at the terminal state and redistributed on
  restoration.
- **`staff-plane`.** The credit-granting permission is exercised here; suspension state is visible and
  adjustable on the staff surface.
- **The console.** Balance, projected time to zero, recent events and their prices.
- **`usage-reporting`.** Unchanged. Its per-project attempt aggregates answer an operational question
  and remain useful alongside the billing ledger; they are not the same data and neither replaces the
  other.

## Dependencies

This change depends on `tenancy-and-permissions`, `staff-plane`, `satellite-sync`, `dns-records` and
`health-checks`, and assumes from them:

- **The `account`** as the entity a balance belongs to — credits are held above projects, so a
  customer with staging and production has one balance and one overdraft.
- **Permission grants that take an object**, so reading a balance can be granted separately from
  spending against it.
- **The staff plane's credit-granting permission**, which is the escape hatch for a wrongful or
  premature suspension and is what makes a flat overdraft tolerable.
- **Batched, idempotent reports**, since a re-delivered batch would otherwise be double-charged.
- **Zones and recordsets**, since zone-time is metered per active zone and the terminal state
  withdraws one from distribution.
- **Delivered check reports** as the billable moment for checks.
