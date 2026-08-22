## 1. Ledgers

- [ ] 1.1 Add the event ledger: account, kind, quantity, unit, period, source reference; append-only, with no update or delete path
- [ ] 1.2 Add the price list as data, keyed by event kind with an effective date
- [ ] 1.3 Add the credit ledger of purchases and deductions, each deduction recording the price it applied
- [ ] 1.4 Add the account balance as a column maintained by deductions, not computed per read (design M9)
- [ ] 1.5 Add the database privilege restriction and a test asserting no event can be modified or deleted
- [ ] 1.6 Regenerate `internal/store/schema.sql` via the golden test

## 2. Rating

- [ ] 2.1 Rate an event and apply its deduction in the same transaction as the event is recorded
- [ ] 2.2 Record the applied price on every deduction
- [ ] 2.3 Charge nothing for an event kind absent from the price list
- [ ] 2.4 Test that a price change alters later events and leaves past deductions unchanged

## 3. Publishes

- [ ] 3.1 Deduct for a challenge publication in the same transaction as the publish
- [ ] 3.2 Refuse a publish with `402` when the balance is not above zero
- [ ] 3.3 Charge nothing for withdrawal and nothing for answering a query
- [ ] 3.4 Benchmark `POST /present` and confirm no query is added beyond the balance read

## 4. Checks

- [ ] 4.1 Record events for delivered check results only
- [ ] 4.2 Discard overspend rather than recording an amount owed (design M4)
- [ ] 4.3 Deduplicate by batch identifier so a re-delivered batch charges once
- [ ] 4.4 Pause check scheduling for an account whose balance is not above zero
- [ ] 4.5 Test that an undelivered batch produces no event and no charge

## 5. Zone-time accrual

- [ ] 5.1 Add the accrual job writing one event per account per period, carrying the zone count and rate
- [ ] 5.2 Key the accrual `UNIQUE (account_id, period)` and insert on conflict do nothing
- [ ] 5.3 Charge every unbilled period since the account's last charged period, not only the previous one
- [ ] 5.4 Test catch-up after a multi-period outage and idempotency on a repeated run
- [ ] 5.5 Continue accrual while an account is soft-suspended

## 6. Query metering

- [ ] 6.1 Count answered queries per account per period on the satellites
- [ ] 6.2 Report them as aggregated events, priced at zero
- [ ] 6.3 Confirm no per-query row is written anywhere

## 7. Suspension lifecycle

- [ ] 7.1 Implement soft suspension: DNS serves, publishes refused, checks paused, accrual continues
- [ ] 7.2 Implement the overdraft limit and permit only zone-time charges to cross zero (design M6)
- [ ] 7.3 Implement the terminal state: withdraw the zone from distribution
- [ ] 7.4 Confirm a withdrawn zone yields `REFUSED` and never `NXDOMAIN` (design M7)
- [ ] 7.5 Implement restoration: redistribute zones, resume publishing and checks on a positive balance
- [ ] 7.6 Test that restoration resolves without waiting for a cached denial to expire

## 8. Warning and staff control

- [ ] 8.1 Compute projected time to zero from the current rate of consumption
- [ ] 8.2 Warn in advance of zero, by email as well as in the console
- [ ] 8.3 Expose suspension state and credit granting on the staff surface
- [ ] 8.4 Record a staff credit grant and make it visible to the account

## 9. Console

- [ ] 9.1 Show balance, projected time to zero, and suspension state
- [ ] 9.2 Show recent events with the price applied to each
- [ ] 9.3 Show a banner when suspended, naming the cause and what is still working

## 10. Deployment and documentation

- [ ] 10.1 Deploy with an empty price list so every event rates to zero, and observe real volumes before charging
- [ ] 10.2 Document why the challenge path is charged per publish and never per query
- [ ] 10.3 Document the lifecycle, the overdraft rule, and why the terminal state refuses rather than denies
- [ ] 10.4 Document the relationship between this ledger and the existing per-project usage reporting

## 8. Derived check billing, retention and backfill

- [ ] 8.1 Record probe results in their own table, partitioned by month, dropped at ninety days (design M11); keep them out of `events` and `deductions`
- [ ] 8.2 Write no deduction for an event type priced at zero
- [ ] 8.3 Emit `node.checked` from the tick job as a count over an interval, one row per account per period per configured node
- [ ] 8.4 Assert in a test that the billable quantity is unchanged when satellite count, interval or address-family count changes
- [ ] 8.5 Backfill `zone.active` and `node.checked` for periods the tick job missed, idempotently by period key (design M12)
- [ ] 8.6 Produce no billable event for a probe result delivered beyond the permitted delay
- [ ] 8.7 Test that a four-hour backlog delivered at once moves no balance and suspends nobody

## 9. No tier column

- [ ] 9.1 Confirm by test that no capability is gated by anything other than the balance and the permission model (design M10) — creating a zone, enabling checks and publishing a challenge each require credits and a grant, and nothing else
