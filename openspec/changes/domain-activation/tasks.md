# Tasks

## 1. State

- [x] 1.1 Add exactly TWO nullable columns to `domains`: `last_checked_at` and `last_success_at`. No activation boolean, no failure counter — both would restate `last_success_at` and the first bug is the row where they disagree (A1)
- [x] 1.1a Derive everything from them: active is `last_success_at IS NOT NULL`; the cadence is NULL→minutes, set→daily; "broken since" is `now - last_success_at`
- [x] 1.2 Put the state on the DOMAIN, not the alias. A wildcard and its apex share one delegation today and both are activated by one observation; putting it on the alias makes them share a fate, which is right now and silently wrong the moment anything gives a domain its own alias (A1)
- [x] 1.3 Migration; every existing domain gets `last_success_at` backfilled to now, since they predate the rule and refusing them would be a regression dressed as a feature
- [x] 1.4 Regenerate `internal/store/schema.sql` via the golden test

## 2. The worker

- [x] 2.1 A sixth background goroutine, written to look like the five that already exist (A5)
- [x] 2.2 Fresh domains (`last_success_at IS NULL`): checked every few minutes, and NEVER given up on. Somebody who has just been shown the record is in the middle of publishing it, and minutes is the difference between "it worked" and "did I get it wrong?" (A2)
- [x] 2.3 Settled domains: re-checked about daily, to INFORM rather than to gate. Daily against thirty days of renewal slack is ample (A2, A3)
- [x] 2.4 Intervals come from configuration, not constants — `layered-configuration` makes that natural
- [x] 2.5 Idempotent, so two instances racing produce the same answer twice rather than a conflict (A5)
- [x] 2.6 Reuse `resolvercheck`, cache included. This must never run on a request path

## 3. The gate

- [x] 3.1 Refuse `/present` for a domain that has never been activated, before anything is published
- [x] 3.2 Answer **409**, distinguishable from the account refusals (`402` unfunded, `403` staff-suspended) and from a permission refusal. The request is well-formed and the credentials are right; the resource is not in a state that permits it (A4)
- [x] 3.3 The message names the delegation and what it should point at — this lands in the log of the machine whose operator has to fix it
- [x] 3.4 NEVER take an activated domain offline. Getting in requires proof; staying in is not revocable by a lookup (A3). This is the task most likely to be "improved" into a bug

## 4. The console

- [x] 4.1 Show a domain that is waiting for its delegation as waiting, not as broken — it is the normal state of a domain added five minutes ago
- [x] 4.2 Show when we last looked (`last_checked_at`) and, where the delegation has stopped resolving, since when (`last_success_at`) — without implying anything will be switched off (A3)
- [x] 4.3 Offer a check-now for somebody who has just published the record and does not want to wait for the poller

## 5. Proving it

- [x] 5.1 A domain is offline when added and answerable after a check sees the delegation
- [x] 5.2 An activated domain STAYS activated across a failed re-check, and across many (A3)
- [x] 5.3 A wildcard and its apex are both activated by the one observation they share (A1)
- [x] 5.4 The refusal is `409` and names the delegation
- [x] 5.5 Existing domains are activated by the migration and never refused (1.3)
- [x] 5.6 Acceptance: add a domain against the fake internet, watch it refuse, publish the delegation, watch it activate and answer
  - `backend/acceptance/delegation_test.go` is the customer's half: `onboardDomain` renders the
    `_acme-challenge` CNAME into `development/dns/zones/example.com.zone` through
    `development/scripts/lib.sh`'s own `render_zone`, restarts CoreDNS and waits for it to answer,
    then POSTs the load balancer's `verification` route — the console's check-now, which records
    what it sees — rather than sleeping for the poller. It restores the zone on cleanup so
    `TestGeneratedPlanMatchesTheServedZone` still holds.
  - `personal_test.go` and `team_test.go` moved their domains under `example.com`, the only zone
    the fake internet serves, and now assert the refusal FIRST: a funded, unsuspended account with
    a key minted on that very load balancer is refused 409, and the same call succeeds once the
    delegation exists. That is A4's distinction proven rather than asserted.
