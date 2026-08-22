## 1. Change sequence in the centre

- [ ] 1.1 Add a monotonic sequence over changes to the answerable state, advanced on publish, withdraw, alias creation and alias deletion
- [ ] 1.2 Add delta retention with a configured window, and a way to report whether a position is still resumable
- [ ] 1.3 Add a snapshot builder producing the same shape `store.ActiveByAlias` returns
- [ ] 1.4 Test that a snapshot and the deltas since a position converge on the same state

## 2. Sync endpoint

- [ ] 2.1 Add the Server-Sent Events endpoint, emitting `snapshot`, `publish`, `withdraw` and alias lifecycle events with positions as event identifiers
- [ ] 2.2 Honour `Last-Event-ID`: resume from a retained position, otherwise send a snapshot
- [ ] 2.3 Emit periodic comment heartbeats so a dead connection is detected without a keepalive protocol
- [ ] 2.4 Test the three connect cases: no position, retained position, position too old

## 3. Report intake

- [ ] 3.1 Add the batched report endpoint, separate from the stream
- [ ] 3.2 Record accepted batch identifiers and discard re-delivered batches
- [ ] 3.3 Test that a batch delivered twice is applied once
- [ ] 3.4 Test that a large batch does not delay distribution on the same satellite's stream

## 4. Satellite client

- [ ] 4.1 Add `internal/satellite` with connect, resume, apply and reconnect-with-backoff
- [ ] 4.2 Implement `satellite.FromStream` satisfying `zoneview.Loader`
- [ ] 4.3 Request a snapshot on an out-of-sequence event or an unapplicable change (design Y4)
- [ ] 4.4 Batch and post reports; discard an undeliverable batch rather than spooling
- [ ] 4.5 Track and report applied position and the time it was applied

## 5. Run mode

- [ ] 5.1 Add the satellite run mode to `cmd/acmeservice`, selected by configuration, reported at startup and on readiness
- [ ] 5.2 Start no datastore connection, no management listener and no console in satellite mode
- [ ] 5.3 Withhold readiness and refuse queries until the first successful load (design Y7)
- [ ] 5.4 Test that a satellite holding a replica reports ready while disconnected

## 6. Fleet administration

- [ ] 6.1 Add satellite registration, listing and retirement to the staff surface under `platform.admin`
- [ ] 6.2 Expose per-satellite lag in events and in time
- [ ] 6.3 Add an alert-worthy metric for a satellite whose connection is open but whose position is not advancing
- [ ] 6.4 Confirm no account-facing route lists or names a satellite

## 7. Verification

- [ ] 7.1 Run a satellite alongside the centre for the same zone and compare answers under load
- [ ] 7.2 Test that a withdrawn value stops being answered on a disconnected satellite when it expires
- [ ] 7.3 Test challenge propagation latency from publish to answerable on a satellite
- [ ] 7.4 Acceptance test covering a satellite restart, a long disconnection, and a fresh enrolment

## 8. Documentation

- [ ] 8.1 Document the satellite deployment, its configuration and what it requires from the network
- [ ] 8.2 Document the fail-safe policy on disconnection and the bound on staleness
- [ ] 8.3 Record satellite identity and enrolment as unresolved, and that the sync endpoint must not be exposed until it is answered
