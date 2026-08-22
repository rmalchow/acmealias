## Context

Four earlier changes each introduced something that costs money to provide: challenge publishes
(existing), zones served continuously (`dns-records`), probes executed on a schedule
(`health-checks`), and the fleet that does both (`satellite-sync`). None of them decided what any of
it costs, deliberately, because the mechanics of charging for a discrete authenticated action, a
scheduled edge-observed action and a continuous passive state are genuinely different.

This change is the ledger, the meter and the lifecycle. It is not payment: turning money into credits
is an integration, and what this specifies is where the credits live and how they are spent.

Terms used throughout, defined once:

- **Event** — an immutable record that something meterable happened: an account, a type, a quantity,
  and the period it covers. Never edited.
- **Rating** — turning an event into a number of credits, using the price book.
- **Price book** — the mapping from event type to credits. Data, not code.
- **Deduction** — a credit ledger row reducing a balance, recording the price it applied.
- **Tick** — one scheduled accrual of continuous cost for one account for one period.
- **Soft suspension** — balance at or below zero: existing service continues, new actions are refused.
- **Terminal state** — the overdraft is exhausted: the zone is withdrawn from distribution.

Constraints that shaped everything below:

1. **No billable action may be triggerable by someone other than the account holder.**
2. **The balance the suspension check reads must be current**, not eventually consistent.
3. **Revenue must not depend on a scheduled job having run on time.**
4. **Recovery after payment must be fast**, because a prepaid model assumes people come back.

## Goals / Non-Goals

**Goals:**

- One event shape covering counts and continuous rates, immutable and disputable.
- Pricing that moves independently of the code that produces events.
- A balance accurate enough to drive suspension.
- A lifecycle that never breaks a paying customer and recovers in seconds when they pay.

**Non-Goals:**

- Payment processing, invoicing, tax, currency.
- Deciding actual prices.
- Committed spend, tiers, discounts.
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
what suspension reads, and a balance that lags by a batch interval would suspend late or suspend an
account that has already paid.

What the separation buys is everything in the proposal — metering without billing, disputes answerable
from the events, prices changing without touching producers — and it costs nothing, because
"synchronous" and "separate" are orthogonal. Deferred or batch rating remains available later without
a schema change.

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
check.reported        count   4          14:00–14:01
zone.active           rate    3          14:00–15:00      (3 zones × 1 hour)
dns.query             count   18422      14:00–15:00      metered, priced at zero
```

Two unit kinds cover everything: things that happen, and things that persist. Zone-time is not a
special billing mechanism, it is an event whose unit is a rate — which means the tick job (M5) is just
another event producer, and there is one ledger rather than a ledger plus a subscription system.

`dns.query` is present deliberately and priced at zero. Per-query *billing* is rejected (M3), but
per-query *metering* is safe and informative, and the only way to know whether it could ever be priced
is to have counted it. Collecting it costs a counter on a path that already has metrics.

### M3. The challenge path is billed per publish, and this is a security decision

Constraint 1 is the whole argument, and it is worth restating in the design because it will look like
a pricing preference to a future reader.

The alias label a customer's `CNAME` points at is public. It has to be: a certificate authority
follows that `CNAME`, so `_acme-challenge.shop.example.com → k4m2p9.acme.example.net` is world-
readable in the customer's own zone. Anyone can therefore find where a given customer's challenges are
answered.

If a query were billable, a stranger could spend the customer's balance from a shell loop, and the
consequence is not a large invoice — it is a **failed renewal and an outage at the next certificate
expiry**. An attacker with no access to anything takes down a site by making the victim's prepaid
balance run out.

So the billable moment is the publish: authenticated with the account's own key, initiated by the
account, and bounded by its own behaviour. Answering queries is free, which is also honest — a lookup
in the in-memory view costs microseconds.

The deduction happens in the same transaction as the publish, at the centre, so `POST /present`
refuses with `402` against a balance that is current rather than one that lags a report cycle.

### M4. Checks are billed on delivery, and overspend is dropped

A scheduled check is a promise. The **report** is the delivery, and delivery is what is billable.

If the deduction would take the balance below zero, the excess is **discarded** — not carried as debt,
not chased, not reserved in advance.

This is only safe because suspension is soft (M6). With a hard stop, a stale balance could suspend an
account wrongly, which is why reservation schemes exist. With soft suspension, the worst case is one
report cycle of unbilled checks, which is cheaper to write off than to account for.

Two consequences worth naming:

- **No reservation bookkeeping.** No held credits, no settlement, no release path for a check that
  never ran.
- **An undelivered report costs the customer nothing**, so a satellite may drop a batch it cannot
  deliver rather than spooling it durably. That is what keeps a satellite a stateless process that can
  be replaced rather than recovered — a property `satellite-sync` relies on.

Batch identifiers make re-delivery a no-op. `satellite-sync` already requires them; here they are
financially load-bearing, because a double-counted batch is a wrong invoice.

### M5. Zone-time ticks per account, idempotently, catching up on every run

Zone-time is the only charge that drains a balance while the customer does nothing, which is what
makes it the one that needs an overdraft.

**Per account, not per zone.** One row per account per period, carrying the zone count and the rate:

```
account 42   zone.active   quantity=3   unit=rate   period 14:00–15:00
```

A thousand zones is one row rather than a thousand. The derivation stays on the row, so the charge is
still auditable — which was the whole reason for writing real rows rather than computing a balance
lazily.

**Idempotent and self-healing.** The tick is keyed `UNIQUE (account_id, period)`, and each run charges
**every unbilled period since the account's last one**, not "the previous hour":

```sql
-- for each account, for each period in (last_billed, now):
INSERT INTO events (account_id, type, quantity, unit, period_start, period_end)
VALUES (...) ON CONFLICT (account_id, period_start) DO NOTHING;
```

A job down for six hours writes six rows on its next run. A job that runs twice writes one. A deploy
that skips a tick costs nothing. Constraint 3 satisfied: revenue does not depend on cron, which
matters much more once the meter also drives suspension — a missed tick would otherwise mean an
account that should be suspended is not.

The precedent is in the schema already: `project_daily_stats` is `primary key (project_id, day)`,
a period-keyed rollup for the same reason.

**Rejected: lazily computed balances.** `balance = purchases − Σ(zones × elapsed)` is exact at every
instant with no job at all. Rejected because the balance stops being a column and becomes a
computation every reader must perform identically — including the suspension check on the publish
path — and because rate changes mid-period turn a subtraction into a piecewise integral.

**Rejected: per-zone ticks.** Most granular, and rejected on volume for information one row already
carries.

### M6. Soft suspension, then terminal — and the rule that keeps the overdraft honest

| Balance | Publishes | Checks | Zone serving | Zone-time accrual |
| --- | --- | --- | --- | --- |
| `> 0` | ✓ | ✓ | ✓ | accruing |
| `≤ 0` | `402` | paused | ✓ | accruing into overdraft |
| `≤ -overdraft` | `402` | paused | withdrawn | stopped |

**The overdraft keeps what you have alive; it does not buy you anything new.** Publishes and checks
stop at zero; only zone-time may go negative. Without that rule the overdraft is free credit and the
rational move is to live at zero and spend it every month.

Accrual continues through soft suspension because we are still serving the zones and still incurring
the cost. It also means the overdraft divided by the daily burn *is* the grace period, which makes the
overdraft a single tunable dial rather than a number requiring a separate timer.

**Rejected: freezing the meter during suspension.** More generous and simpler to explain. Rejected
because "suspended" would then be strictly cheaper than "active" while DNS still resolves, rewarding
lapsing, and because the terminal state would need its own timer with no balance signal to drive it.

### M7. The terminal state withdraws the zone: REFUSED, never NXDOMAIN

Constraint 4 makes this a technical decision rather than a policy one. "Stop serving" has several DNS
meanings and they differ by orders of magnitude in how fast a payment restores service.

| Response | Cached by resolvers? | Recovery after payment |
| --- | --- | --- |
| `NXDOMAIN` | Yes — negatively, per RFC 2308, for the `SOA` minimum (typically 300–3600s). RFC 8020 additionally permits concluding nothing exists *below* the name | Up to an hour after payment |
| `SERVFAIL` | Briefly, in some implementations | Seconds to minutes |
| `REFUSED` | No — treated as a server-side condition; resolvers try other nameservers | Seconds |

So the zone is **dropped** from the satellite rather than retained and denied. The satellite is no
longer authoritative for the name, `REFUSED` is the correct response, and nothing is cached to expire.
On payment, the zone is redistributed over the sync stream and resolution resumes as fast as the
stream delivers.

`NXDOMAIN` is the worst available option precisely because it is the most emphatic one: an
authoritative denial at an apex can poison a compliant resolver's view of every subdomain, so the
customer who pays late stays broken longer than the outage they paid to end.

The existing requirement that a query outside the served zones is answered `REFUSED` already produces
this behaviour once the zone is withdrawn; what this change does is make explicit that withdrawal —
not denial — is how the terminal state is implemented.

### M8. Warn on projected time to zero

The overdraft is flat per account, so the grace *period* varies inversely with size: two zones might
coast for weeks, five hundred for hours.

The response is not a per-tier overdraft, it is a better warning. Balance is not a useful number to
show — the same figure means fifty days for one customer and five hours for another. **Projected time
to zero** is comparable across every account, requires no tuning, and is the same arithmetic already
performed to know when the terminal state arrives.

Notification must not depend on the service being usable, so it is email as well as a console banner,
and it fires well before zero rather than at it. What still slips through — a payment in flight, a
disputed charge, a wrongful accrual — is handled by the staff plane's credit-granting permission,
which exists for exactly this.

### M9. The balance read is on the publish critical path and must stay cheap

`POST /present` gains a balance check and a deduction in its existing transaction. The challenge path
is on a certificate issuance critical path, and `tenancy-and-permissions` already required that
authorization add no query to it.

The balance is therefore a column on the account, maintained by the deduction, not an aggregate
computed per request. This is the concrete reason M5 rejects lazily computed balances: a lazy balance
would put a scan on the hot path of every publish.

## Risks / Trade-offs

- **An attacker drains a customer's balance** → the only attacker-triggerable action, the DNS query,
  is not billable (M3); every billable action is authenticated as the account.
- **A double-delivered report double-charges** → batch identifiers make re-delivery a no-op (M4).
- **A missed tick loses revenue or delays a suspension** → catch-up over all unbilled periods, keyed
  idempotently (M5).
- **A price change rewrites history** → the applied price is recorded on the deduction (M1).
- **A large account hits the terminal state within hours of lapsing** → accepted consequence of a flat
  overdraft; mitigated by warning on projected time to zero (M8) and by staff credit grants.
- **A customer pays and stays broken** → `REFUSED` rather than `NXDOMAIN`, so nothing is negatively
  cached (M7); restoration travels the sync stream in seconds.
- **Soft suspension lets a non-payer coast** → bounded by the overdraft, which continues to accrue
  during suspension (M6), so the grace period is the dial.
- **The publish path gets slower** → the balance is a column, and the deduction shares the existing
  transaction (M9).
- **Metered-but-unpriced events accumulate cost with no revenue** → `dns.query` is aggregated per
  period per account, not per query (M2).

## Migration Plan

1. Add the event ledger, price book, credit ledger and account balance.
2. Seed the price book, with the query event priced at zero.
3. Add synchronous rating: event and deduction in one transaction, recording the applied price.
4. Add the publish deduction and the `402` refusal to `POST /present`.
5. Add check-report deduction on delivery, clamped at zero with the excess dropped.
6. Add the zone-time tick job, keyed on `(account_id, period)`, catching up over unbilled periods.
7. Add the suspension state machine and the pausing of scheduled checks.
8. Add the terminal state: withdraw the zone from distribution, restore on top-up.
9. Add balance, projected time to zero, and event history to the console; add warning notifications.
10. Add suspension state and credit granting to the staff surface.

**Rollback**: with an empty price book every event rates to zero, so no balance moves and no account
is suspended. The ledger can therefore run in metering-only mode in production before any charge is
made, which is also the recommended way to deploy it.

## Open Questions

- **What is the tick period?** Hourly is the working assumption — fine enough for the grace period to
  be meaningful, coarse enough that one row per account per period is small.
- **What is the flat overdraft, in credits?** Commercial, but it is the single dial governing how long
  a lapsed customer is carried, so it should be chosen with the zone-time rate rather than separately.
- **Does the terminal state ever delete data, or only stop serving?** Withdrawing from distribution is
  reversible in seconds; deleting a zone is not. Deletion after a long terminal period is a separate
  decision with legal dimensions.
- **How long are events retained?** They are the evidence for disputes, so longer than deductions
  might suggest; and `dns.query` events are the highest-volume and least valuable of them.
- **Should the price book be per account?** Committed spend and negotiated pricing would need it, and
  the table is already shaped for it, but nothing requires it yet.
