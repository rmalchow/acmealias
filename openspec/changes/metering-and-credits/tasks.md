# Tasks

> Scope: the ledger that draws a balance down. The balance itself, the checkout that raises it and
> the behaviour at zero arrive earlier with `personal-and-team-accounts` (C3, C4, C8) and are not
> rebuilt here. There is no zone-time and no overdraft — see design M6 and M7.

## 1. Ledgers

- [ ] 1.1 Add the event ledger: account, kind, quantity, unit, period, source reference; append-only, with no update or delete path
- [ ] 1.2 Add the price list as data, keyed by event kind with an effective date
- [ ] 1.3 Add the deduction ledger, each deduction recording the price it applied, against the account balance that already exists
- [ ] 1.4 Confirm by test that this change adds no second balance column and no second definition of "usable" (design M6)
- [ ] 1.5 Add the database privilege restriction and a test asserting no event can be modified or deleted
- [ ] 1.6 Regenerate `internal/store/schema.sql` via the golden test

## 2. Rating

- [ ] 2.1 Rate an event and apply its deduction in the same transaction as the event is recorded (design M1)
- [ ] 2.2 Record the applied price on every deduction
- [ ] 2.3 Charge nothing for an event kind absent from the price list
- [ ] 2.4 Write no deduction at all for an event kind priced at zero (design M4)
- [ ] 2.5 Test that a price change alters later events and leaves past deductions unchanged

## 3. Publishes

- [ ] 3.1 Deduct for a challenge publication in the same transaction as the publish, after the funding check `personal-and-team-accounts` C7 already placed there
- [ ] 3.2 Charge nothing for withdrawal and nothing for answering a query (design M3)
- [ ] 3.3 Benchmark `/present` and confirm no query is added beyond the balance read (design M9)

## 4. Observation accrual

- [ ] 4.1 Add the tick job writing one event per account per period per node configured for certificate observation, carrying the count and the rate (design M5)
- [ ] 4.2 Key the accrual `UNIQUE (account_id, type, period_start)` and insert on conflict do nothing
- [ ] 4.3 Charge every unbilled period since the account's last charged period, not only the previous one
- [ ] 4.4 Test catch-up after a multi-period outage and idempotency on a repeated run
- [ ] 4.5 Bill a node for a period only if it was configured for observation in that period, irrespective of whether any handshake succeeded
- [ ] 4.6 Assert in a test that the billable quantity is unchanged when the observation interval or the number of address families changes (design M4)
- [ ] 4.7 Test that an account with nothing configured accrues nothing, however many periods pass
- [ ] 4.8 Keep certificate observations out of `events` and `deductions`; they stay in their own table under `health-checks` retention

## 5. Query metering

- [ ] 5.1 Count answered queries per account per period on the DNS server
- [ ] 5.2 Report them as aggregated events, priced at zero
- [ ] 5.3 Confirm no per-query row is written anywhere

## 6. The floor at zero

- [ ] 6.1 Clamp a deduction at zero and discard the excess; record no debt (design M7)
- [ ] 6.2 Test that nothing is charged later for a clamped amount
- [ ] 6.3 Stop scheduling certificate observation for an account whose balance is not above zero
- [ ] 6.4 Resume observation without further action when credits are added
- [ ] 6.5 Confirm by test that no path in this change permits a balance below zero

## 7. Warning and staff control

- [ ] 7.1 Compute projected time to zero from the current rate of consumption (design M8)
- [ ] 7.2 Warn in advance of zero, by email as well as in the console
- [ ] 7.3 Expose credit granting on the staff surface
- [ ] 7.4 Record a staff credit grant and make it visible to the account

## 8. Console

- [ ] 8.1 Show projected time to zero beside the balance the account screen already shows
- [ ] 8.2 Show recent events with the price applied to each
- [ ] 8.3 Show what is accruing and at what rate, so a projection can be checked against its cause

## 9. No tier column

- [ ] 9.1 Confirm by test that no capability is gated by anything other than the balance and the permission model (design M10) — configuring observation and publishing a challenge each require credits and a grant, and nothing else

## 10. Deployment and documentation

- [ ] 10.1 Deploy with an empty price list so every event rates to zero, and observe real volumes before charging
- [ ] 10.2 Document why the challenge path is charged per publish and never per query
- [ ] 10.3 Document why observation is billed per configured node rather than per observation, and that the interval is ours to change
- [ ] 10.4 Document the relationship between this ledger and the existing per-project usage reporting
