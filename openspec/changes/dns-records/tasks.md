## 1. Zone and recordset storage

- [ ] 1.1 Add `zones` and `recordsets` to the model and a migration, with materialized `account_id` and the composite foreign keys that make ownership unforgeable
- [ ] 1.2 Store recordset values as a list with one time to live per recordset (design R1)
- [ ] 1.3 Add `UNIQUE (account_id, name)` on zones and `UNIQUE (zone_id, name, type)` on recordsets
- [ ] 1.4 Add a change sequence over zones and recordsets for the distribution stream
- [ ] 1.5 Regenerate `internal/store/schema.sql` via the golden test

## 2. Validation

- [ ] 2.1 Refuse a `CNAME` at a name holding any other type, and any other type at a name holding a `CNAME`
- [ ] 2.2 Refuse a `CNAME` at the zone apex
- [ ] 2.3 Refuse a recordset whose name falls outside its zone
- [ ] 2.4 Validate values per record type; refuse malformed values
- [ ] 2.5 Table-driven tests for every rule, asserting nothing invalid can be stored

## 3. Apex records

- [ ] 3.1 Generate `SOA` and apex `NS` from the fleet's identity when a zone is created
- [ ] 3.2 Advance the `SOA` serial on every change to the zone
- [ ] 3.3 Refuse management operations that would create, change or delete apex `SOA` or `NS`
- [ ] 3.4 Permit `NS` below the apex, subject to a grant covering the `NS` type
- [ ] 3.5 Read the fleet's nameserver names from configuration (design R9); refuse startup unless there are at least two names spanning at least two parent domains in two top-level domains
- [ ] 3.6 Keep every nameserver and zone name in documentation and fixtures illustrative, so no concrete name is depended on before it is chosen

## 3a. Delegation signer pre-flight (design R8)

- [ ] 3a.1 Query the parent for `DS` at the zone name when a zone is added, using the same resolver path as delegation verification
- [ ] 3a.2 Refuse the zone while a `DS` exists, with a message naming the cause and the remedy
- [ ] 3a.3 Refuse the zone when the parent cannot be queried, rather than assuming unsigned
- [ ] 3a.4 Add a periodic re-check over served zones and raise an alert when a `DS` appears
- [ ] 3a.5 Tests: signed parent refused, unreachable parent refused, unsigned parent accepted, `DS` appearing later alerts

## 4. Permission enforcement

- [ ] 4.1 Evaluate the `dns_record` selector on every recordset read and write, matching zone, name and type
- [ ] 4.2 Authorize deletion as a change
- [ ] 4.3 Test that a subtree grant excludes its own apex and that a parent grant includes it (design R3)
- [ ] 4.4 Test that a type-limited grant refuses `MX` and `NS`

## 5. Generalise the answering path

- [ ] 5.1 Generalise `internal/zoneview` from label-to-TXT to recordsets keyed by name and type, keeping snapshot replacement, locking, fail-safe and read-time expiry
- [ ] 5.2 Generalise `internal/dnsserver` to serve several zones and all record types
- [ ] 5.3 Preserve the distinction between a non-existent name and an existing name with no records of the queried type
- [ ] 5.4 Keep the refusal to recurse and the handling of out-of-zone names
- [ ] 5.5 Represent challenge aliases as recordsets of the challenge zone (design R7)
- [ ] 5.6 Run the existing challenge acceptance suite unchanged and confirm it passes

## 6. Backend interface

- [ ] 6.1 Add `internal/dnsbackend` with `Apply` and `Snapshot` over whole recordsets
- [ ] 6.2 Implement the in-process backend over the generalised view
- [ ] 6.3 Implement the RFC 2136 dynamic update backend with TSIG authentication
- [ ] 6.4 Report a refused update as a failure rather than recording the change as distributed
- [ ] 6.5 Conformance test asserting both implementations serve equivalent answers for the same input

## 7. Distribution

- [ ] 7.1 Extend the sync stream with recordset events carrying a signature field that is always empty
- [ ] 7.2 Carry the complete new state of a recordset in each event, never a partial mutation
- [ ] 7.3 Include recordsets in the snapshot the centre sends on reconnect
- [ ] 7.4 Test that a satellite converges on zone contents after a disconnection and after a restart

## 8. Management API and console

- [ ] 8.1 Add zone routes: create, list, read, delete
- [ ] 8.2 Add recordset routes operating on whole recordsets
- [ ] 8.3 Report permitted actions per zone and per recordset alongside listings
- [ ] 8.4 Add console screens for zones and recordset editing
- [ ] 8.5 Show, per zone, the delegation the customer must publish at their registrar

## 9. Documentation

- [ ] 9.1 Document zone delegation and what a customer must set at their registrar
- [ ] 9.2 Document the record grant model with worked examples of subtree and type limits
- [ ] 9.3 Document the backend options and when dynamic update is preferable to in-process
- [ ] 9.4 Record that signing is deferred, that the pipeline carries a signature field, and why signing must be central
