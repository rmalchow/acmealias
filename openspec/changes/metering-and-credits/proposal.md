## Why

### The balance already exists and only ever goes up

`personal-and-team-accounts` gives a team account a credit balance, a checkout that raises it, and a
rule that says the account works while `credits > 0` and stops at zero. All of that is built before
this change and all of it is exercised — an account can be funded, can be topped up, and can be
refused at `/present` when it is not.

What is missing is the other direction. Nothing spends. A funded account stays funded forever,
because no activity costs anything, and the whole apparatus of suspension sits there waiting for a
number that never falls.

This change is that half: the record of what happened, the price of what happened, and the deduction
that moves the balance. It is deliberately last of the billing work, because credits granted by hand
to customers we know personally are a perfectly good substitute for a meter, right up until they are
not — and the trigger for building this is a count of customers, not a date.

### Two things cost money, and they are not the same shape

A **challenge publish** is a discrete authenticated action. It happens at an instant, it is initiated
by the customer through their own API key, and it is countable.

A **node configured for certificate observation** is a continuing arrangement. Nothing happens at a
moment; something is true across an interval. `health-checks` observes it a few times a day, and how
often is our decision.

One mechanism has to express both, or there will be a meter for one and a subscription for the
other, and then two suspension rules and an invoice nobody can explain. The event shape that covers
both is a quantity that is either *a count of occurrences* or *a count sustained over an interval* —
same table, same rating, same ledger.

### Billing for something a stranger can trigger is a vulnerability, not a pricing mistake

This is the decision worth explaining at length, because the obvious design is dangerous.

The intuitive way to charge for the challenge service is per DNS query answered — it tracks the cost
of serving most directly. It is unsafe, and the reason is structural rather than a matter of degree.

The challenge name is **public by construction.** The customer publishes, in their own zone, a
`CNAME` from `_acme-challenge.shop.example.com` to our alias label, say `k4m2p9.acmealias.com`. That
record has to be world-readable, because a certificate authority has to follow it. So anyone can
discover where a customer's challenges are answered, and anyone can then query that name in a loop.

If queries are billable, that loop spends the customer's credits. When the balance runs out,
certificate renewal fails, and the next expiry takes the customer's site down. That is a remote denial
of service executed with `dig` and a shell loop, against a victim who did nothing wrong.

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
- **Prices change without touching producers.** The publish path emits events forever; the price book
  moves independently of it.

One caution has to come with that separation, because it is the way it usually goes wrong: separating
the *concerns* must not mean deferring the *timing*. The funding check reads a balance on the
challenge path, so rating stays synchronous — the event is written and rated in the same transaction —
while the price lives in a price book rather than in code. And every deduction records the price that
was applied, or a later price change silently rewrites history for anyone who recomputes.

## What Changes

### An immutable event ledger, in one normalized shape

Everything meterable becomes an event: an account, an event type, a quantity, and either a count or a
count over an interval.

```
account 42   challenge.publish    N = 1                     at 14:02:11
account 42   node.observed        N/t = 4 nodes × 1h        for 14:00–15:00
account 42   dns.query            N = 18422                 for 14:00–15:00   (metered, not billed)
```

Events are append-only and never edited. Producers are the challenge API (publishes), the DNS server
(query counts, aggregated), and a scheduled tick job (the rate events). Every event carries the
identity of the period or request that produced it, so re-recording is a no-op — here a double count
is a wrong invoice.

### A price book turns events into deductions

A separate table maps event type to credits. Rating happens in the same transaction as the event, and
the deduction row records the price it used. Changing a price affects future events only. An event
type absent from the price book, or priced at zero, records the event and writes no deduction at all —
a deduction row is evidence that money moved, and a zero-credit deduction is evidence of nothing.

### Two units, two mechanics

**Publishes** are deducted synchronously at `/present`, in the transaction that already carries the
publish, immediately after the funding check `personal-and-team-accounts` C7 put there. Nothing about
this is eventually consistent.

**Observation** is charged as a rate: one event per account per period per node *configured* for
observation, emitted by a small scheduled job. Not per handshake. The customer chose "observe this
node"; they did not choose the interval, and they did not choose that a dual-stack node costs two
handshakes rather than one. Both are ours to change, and `health-checks` says plainly that we intend
to. An invoice must not move because we decided to look more often.

Ticks are idempotent and self-healing: keyed on `(account, type, period)` with a unique constraint,
and each run charges *every unbilled period since the last one* rather than "the last hour". A
six-hour outage writes six rows on the next run; a job that runs twice writes one. The precedent is
already in the schema — `project_daily_stats` has `primary key (project_id, day)` for exactly this
reason. The point is that **revenue stops depending on cron reliability.**

### The floor is zero, and the overshoot is forgiven

`personal-and-team-accounts` C4 decided that an account stops at zero rather than coasting on an
overdraft, and gave the reasons: an overdraft needs a third state that is hard to explain, and its
kindness is largely illusory when refusing `/present` stops renewals rather than taking a site down.
This change inherits that and adds one small rule of its own.

If a tick's deduction would take a balance below zero, **the excess is discarded** — not carried as
debt, not chased, not reserved at dispatch. The bounded loss is one period's charge for one account,
which is less than the reservation bookkeeping that avoids it would cost to build, test and explain.

An earlier version of this proposal argued at length for an overdraft. What survives from that
argument is its conclusion, and it is the part worth keeping: **the mitigation is not a different
overdraft, it is a different warning.**

### Warn on time to zero, not on balance

"You have 100 credits left" is meaningless: it means fifty days for one customer and five hours for
another, and neither can tell which from the number. "You have about six hours" is actionable for
both, requires no per-tier tuning, and is the same arithmetic already being done to know when an
account will be suspended.

The warning is delivered by email as well as in the console, because a warning that requires the
service to be usable arrives too late by definition. What still slips through — a payment in flight,
a disputed charge, a wrongful accrual — is covered by the staff plane's credit-granting permission.

### The levels we sell are not something the system knows about

The service is sold at two levels: challenge answering on its own, and challenge answering plus
certificate observation. That is how the product is described to a buyer, and the obvious next step
is to make it a field — a `tier` on the account, a gate on enabling observation, a monthly fee per
level.

This change does not do that, deliberately.

A tier is a **second thing that can stop an account**, next to the one that already exists. It brings
gates to write and test, upgrade and downgrade paths that each have a half-finished middle, a support
burden of explaining which level someone is on, and a second answer to "why can't I do this" that has
to be kept consistent with the first.

And it answers nothing the ledger does not. A customer on the higher level is a customer who
configured a node for observation. A customer on the lower one is one who did not. The level is a
**description of what someone configured**, not a permission they were granted. Configuring more
produces more events; more events cost more credits; credits are what run out. One mechanism stops an
account, and it is the balance.

The honest cost of this: there is no recurring base charge, so a customer who configures nothing pays
nothing however long they stay, and there is no way to sell "unlimited observation for a flat monthly
fee". Both are fine at this size, and both are reversible in the easy direction — an entitlement can
be added later, whereas one that customers have already bought cannot be taken away.

### Not in scope

- **The balance, the checkout and the behaviour at zero.** All three arrive with
  `personal-and-team-accounts` (C3, C4, C8). This change spends against them.
- **Payment.** Turning real money into credits is a separate integration behind that checkout.
- **Invoicing, tax, currency.** The ledger is in credits.
- **Setting prices.** The price book exists; what goes in it is commercial.
- **Committed spend, discounts, negotiated pricing.** A price book per account is where they would
  go, and nothing here forecloses it.
- **Product tiers as an entitlement.** Not deferred — decided against, for the reasons above.

### What we considered and rejected

**Per-query billing for the challenge path.** Rejected above at length: the challenge name is public,
so the billable action is attacker-triggerable.

**Billing per certificate observation.** The most direct relationship between what we do and what we
charge. Rejected because the observation count is a function of decisions the customer did not make —
the interval, and the number of address families — and an invoice that moves when we change our minds
is not one anybody can budget against.

**Reserving credits in advance and settling later.** It keeps the balance from ever being optimistic.
Rejected because the exposure it protects is one tick period for one account, which is cheaper to
write off than to account for.

**Carrying the overshoot below zero as a debt.** Exact, and it turns fractions of a credit owed by
people who are by definition not paying into a collections problem. Rejected on the same arithmetic.

**Lazily computed balances** — balance as a function of purchases minus elapsed usage. No ticks, no
rollups, exact at any instant. Rejected because the balance stops being a column and becomes a
computation that every reader must perform identically, including the funding check on the publish
path.

**A per-account tier column.** Rejected above: it is a second mechanism that can stop an account, and
it answers nothing the balance does not.

## Capabilities

### New Capabilities

- `usage-metering`: the immutable event ledger — its normalized shape as a count or a count over an
  interval, its producers, its idempotency, metering that is deliberately not billed, and the rule
  that the billable observation quantity is derived from configuration rather than from handshakes.
- `credit-ledger`: the price book, synchronous rating, deductions that record the price applied,
  per-account rate ticks that are idempotent and self-healing, the floor at zero, and warning on
  projected time remaining.

`credit-ledger` also carries the one behavioural rule this change adds to an unfunded account:
certificate observation stops being scheduled for it, and resumes when it is funded again. Every
other refusal at zero belongs to `personal-and-team-accounts` and is unchanged here.

### Modified Capabilities

None. The account, its balance, its funding check and its suspension already exist; this change adds
tables and a deduction rather than altering an existing capability's behaviour.

## Impact

- **New schema.** An append-only event ledger, a price book, and a deduction ledger. The balance
  column already exists; this change writes against it.
- **`/present`.** Gains a synchronous deduction in the same transaction as the publish, immediately
  after the funding check that is already there. This is the only change on the challenge critical
  path and it must not add a query beyond the balance read.
- **`health-checks`.** Observation scheduling stops for an account that is not funded, and a node
  configured for observation becomes a rate event.
- **`personal-and-team-accounts`.** Its balance now falls as well as rises; its suspension is now
  reachable by usage rather than only by time.
- **`staff-plane`.** The credit-granting permission is exercised here.
- **The console.** Balance history, projected time to zero, recent events and their prices.
- **`usage-reporting`.** Unchanged. Its per-project attempt aggregates answer an operational question
  and remain useful alongside the billing ledger; they are not the same data and neither replaces the
  other.

## Dependencies

This change depends on `tenancy-and-permissions`, `personal-and-team-accounts`, `staff-plane` and
`health-checks`, and assumes from them:

- **The `account`** as the entity a balance belongs to, and the balance itself — credits are held
  above projects, so a customer with staging and production has one balance.
- **The funding check and its `402`**, already on the challenge path, so this change adds a deduction
  rather than a gate.
- **Permission grants that take an object**, so reading a balance can be granted separately from
  spending against it.
- **The staff plane's credit-granting permission**, which is the escape hatch for a wrongful or
  premature suspension.
- **Nodes configured for certificate observation** as the thing a rate event counts.
