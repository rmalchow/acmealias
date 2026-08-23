# Tasks

> Quorum, hysteresis, fail-open, failover answers, per-family decision state, correlated-family
> suppression and the late-report rules were all removed with the DNS product. Every one existed
> because check results changed what the internet was told; nothing steers now.
>
> What is left is one TLS handshake per node per address family, and what is done with what it sees.

## 1. Model and schema

- [ ] 1.1 Add `certificate_observations` to the model and a migration: node, address family, observed at, not-before, not-after, issuer, serial, the subject alternative names, and the error when the handshake failed
- [ ] 1.2 Carry the materialized `account_id`, `project_id` and `load_balancer_id` of design P3, so an observation is confined and filterable like every other row
- [ ] 1.3 Give the table the forced row-level security policy every tenant table has
- [ ] 1.4 Index by `(load_balancer_id, observed_at desc)` for the per-load-balancer view, and by `not_after` for the expiry sweep
- [ ] 1.5 Regenerate `internal/store/schema.sql` via the golden test

## 2. Observation

- [ ] 2.1 Connect to each node address, complete a TLS handshake, read the certificate offered, and record it. One handshake per (node, address family), not per (node, domain) — the matrix is two hundred probes for four nodes and fifty domains, to observe what is usually four certificates
- [ ] 2.2 Probe both address families separately: they are different paths through different transit, and a certificate served on one and not the other is a real and otherwise invisible failure
- [ ] 2.3 Record a failed handshake as an observation with its error, not as an absence. "We could not look" and "there is nothing there" are different answers and only one of them is about the customer
- [ ] 2.4 Run on a schedule measured in hours, not seconds. Nothing depends on the result within a renewal window, and a certificate does not change between one hour and the next
- [ ] 2.5 Never obtain, install or hold a certificate or a private key — the service observes, and that boundary is what keeps it out of the customer's trust chain

## 3. What the observations answer

- [ ] 3.1 Compare the union of observed subject alternative names against the account's inventory, and identify domains covered by no observed certificate at all
- [ ] 3.2 Report approaching expiry before it occurs, with the lead time a renewal needs rather than the moment it lapses
- [ ] 3.3 Surface the case the challenge history cannot see: validation succeeded, and the certificate was never deployed — or was deployed to some nodes and not others

## 4. Retention

- [ ] 4.1 Keep observations as history with a stated retention, downsampled beyond a full-resolution window; their value is a trend measured in weeks
- [ ] 4.2 Keep them out of the credit ledger: they are operational data, and what is billed is the node being observed rather than each observation

## 5. Metering

- [ ] 5.1 Emit `node.observed` as a count over an interval from the same periodic job that emits every other rate event, so the invoice is a function of what the customer configured rather than of how often we look

## 6. Management API and console

- [ ] 6.1 Report observations per load balancer, newest first, with the covered-names comparison alongside
- [ ] 6.2 Show expiry, issuer and covered names per node and address family, and name the domains covered by nothing
- [ ] 6.3 Confine every read by `Scope` and gate it by the ACL predicate, like every other listing

## 7. Documentation

- [ ] 7.1 Document what is observed, how often, and what it explicitly does not do: no reachability monitoring, no certificate issuance, no private keys
