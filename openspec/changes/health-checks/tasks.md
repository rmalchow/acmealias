## 1. Model and schema

- [ ] 1.1 Add check definitions, per-node current state, probe results history and certificate observations, with materialized `account_id` and composite foreign keys
- [ ] 1.2 Keep results history and decision state in separate tables so retention on one cannot endanger the other (design H7)
- [ ] 1.3 Add a health-checked flag and a per-recordset time to live to `recordsets`
- [ ] 1.4 Regenerate `internal/store/schema.sql` via the golden test

## 2. Probe execution on satellites

- [ ] 2.1 Add `internal/healthcheck` probe execution, deliberately separate from `internal/resolvercheck`
- [ ] 2.2 Implement reachability probing per node address
- [ ] 2.3 Implement certificate observation: one handshake per node, reading expiry, issuer and covered names (design H6)
- [ ] 2.4 Emit results on the existing report channel, batched with a batch identifier
- [ ] 2.5 Ensure a satellite never changes what it serves from its own probe result

## 3. Central decision

- [ ] 3.1 Evaluate quorum over distinct satellites, counting a re-delivered cycle once (design H8)
- [ ] 3.2 Implement hysteresis with K failing cycles to remove and M passing cycles to restore, M > 1
- [ ] 3.3 Refuse to change state when fewer satellites are assigned than quorum requires, and report the condition
- [ ] 3.4 Assign satellites to nodes so that quorum is reachable
- [ ] 3.5 Test that a persistent minority reporter changes nothing and is itself surfaced

## 4. Failover answers

- [ ] 4.1 Compute served values as stored values filtered by node state, without altering the stored recordset (design H9)
- [ ] 4.2 Implement the never-empty rule: serve everything when every value would be excluded (design H3)
- [ ] 4.3 Raise an alarm for the all-unavailable state without changing what is served
- [ ] 4.4 Apply a lower default time to live to health-checked recordsets
- [ ] 4.5 Distribute a failover decision over the existing stream as a served-values change
- [ ] 4.6 Test that a returning node is served again with no change to stored values

## 5. Certificate reporting

- [ ] 5.1 Compare inventory domains against observed covered names and report those covered by none
- [ ] 5.2 Report approaching expiry against a configurable threshold, and expired certificates distinctly
- [ ] 5.3 Retain certificate observations as history with a retention longer than reachability samples
- [ ] 5.4 Assert no private key and no certificate material beyond the observation is stored

## 6. Retention

- [ ] 6.1 Implement retention for probe history: full resolution for a short window, downsampled beyond
- [ ] 6.2 Confirm decision state is unaffected by history removal
- [ ] 6.3 Measure the growth rate per node per satellite per cycle and record it in the operator documentation

## 7. Metering

- [ ] 7.1 Record a delivered probe result as a metering event; record nothing for an undelivered batch
- [ ] 7.2 Confirm event cardinality is per node and per certificate, not per node-domain pair

## 8. Management API and console

- [ ] 8.1 Add routes for check configuration, node status and history, gated by permission
- [ ] 8.2 Add the per-node status view with reachability, certificate, expiry, issuer and history
- [ ] 8.3 Surface uncovered domains and approaching expiries
- [ ] 8.4 Show which values of a health-checked recordset are served and which are excluded, and the all-unavailable state

## 9. Documentation

- [ ] 9.1 Document that the time to live, not the probe interval, bounds observed failover time (design H4)
- [ ] 9.2 Document the never-empty rule and why the check system fails open
- [ ] 9.3 Document the difference between these checks and the advisory checks in `internal/resolvercheck`
- [ ] 9.4 Document the certificate observation limitation for servers offering different certificates per server name
