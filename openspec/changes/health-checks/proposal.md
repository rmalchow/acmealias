## Why

### The challenge history opens a question it cannot close

The service answers DNS-01 challenges. `challenge-history` records that a certificate authority came
to a challenge name, looked, and was satisfied. Then it stops, because everything that happens next
happens on the customer's machine.

There is a failure mode that is entirely invisible from here. Validation passed, a certificate was
issued, and it was never deployed. Or it was deployed to two nodes out of four. Or it covers
`example.com` but not the `shop.example.com` the customer added last week. The challenge history
shows success in every one of those cases, because in every one of those cases the challenge
succeeded. The customer finds out when a browser tells them.

Connecting to a node and reading the certificate it actually serves closes the loop: we answered a
challenge → a certificate authority issued → the certificate is installed, on this address, covering
these names, expiring on this date. That makes certificate observation the completion of the
challenge service rather than a monitoring feature bolted onto it — it is the only thing in the
system that can tell the difference between a renewal that worked and a renewal that finished.

### The valuable answer is the one the customer cannot get themselves

Any customer can find out what certificate their own load balancer is serving. Two things are much
harder for them to see, and both come out of the same handshake.

The first is the **join against their inventory**: which of the domains they have configured here are
covered by no certificate anywhere. That is a comparison between two lists, one of which only we
hold, and it is the failure that produces an outage weeks later with no warning in between.

The second is **per address family**. A node with an IPv4 and an IPv6 address is reached over
different transit and through different firewalls, and a certificate renewed on one listener and not
the other is invisible to a dual-stack visitor — including the customer, checking with their own
browser, from their own desk. Only a client that deliberately probes each family separately sees it.

### Reachability monitoring is not part of this, and that is a decision

The original ask was for a per-host status display: reachability first, certificate expiry second.
This change keeps the second and drops the first.

The reason is not cost. It is that nothing would act on the result. Reachability data that steers DNS
answers earns its keep by changing what the internet is told; reachability data that does not is a
status page, and every operator running a load balancer already has monitoring that samples far more
often than we would and can page somebody. Adding a second, slower, less trustworthy opinion invites
a customer to rely on it, which is worse than not offering it.

Certificate expiry and coverage are the opposite: genuinely hard for the customer to see, changing on
a timescale where an hours-apart sample is ample, and joined against data only we hold.

### What already exists, and why it is not this

`internal/resolvercheck` resolves a DNS plan against a configured resolver and reports what is
published. It is deliberately, explicitly advisory:

> *"Results are advisory only: DNS is somebody else's mutable state, so nothing here is ever persisted
> as fact."*

That is right for what it does — asking whether a customer published the `CNAME` we asked for is a
question about someone else's zone, and the answer is a snapshot with no authority. An observation
here is a different kind of claim: a server presented this certificate to us at this moment, which is
a fact about an exchange we took part in, and it is persisted, retained and shown to the customer as
history. Same shape of outbound probe, different status, and they should not be conflated in the code
or in anyone's head.

## What Changes

### One handshake per node address, hours apart

Connect to each address of each node, complete a TLS handshake, and record what was offered: expiry,
issuer, serial, and the names in the certificate's subject alternative name extension.

Not a matrix. The naive shape is one connection per (node, domain) pair with the domain as the server
name — four nodes and fifty domains is two hundred handshakes per cycle, to discover what is almost
always four certificates. Reading the covered names *from* the certificate is cheaper and gives a
better answer, because the union of observed names is what makes the uncovered-domain comparison
possible at all.

Both address families are probed separately, for the reason above. The schedule is measured in hours:
nothing depends on the result inside a renewal window, and a certificate does not change between one
hour and the next.

### A failed handshake is recorded, not omitted

"There is no certificate here" and "we could not look" are different answers, and only one of them is
about the customer. A refused connection, a timeout or an unreadable response is stored as an
observation carrying its error, so that a gap in the history is never ambiguous between a broken node
and a scheduler that did not run — and so that a domain is reported as uncovered only when we
successfully read certificates and none of them named it.

### The reports are derived from the history

Two things are reported and both are computed from stored observations rather than at probe time, so
that a change to the inventory re-answers them immediately:

- **Domains covered by no observed certificate**, from the account's inventory against the union of
  observed names.
- **Approaching expiry**, with the lead time a renewal needs rather than the moment it lapses.

### Not in scope

- **Reachability and latency monitoring.** Cut deliberately, for the reason above.
- **Failover, steering, or any effect on what is served.** The service does not host customers' zones,
  so there is nothing an observation could steer.
- **Alerting and notification.** The data supports it; who gets told and how is separate.
- **Application-level checks** beyond a TLS handshake — no HTTP status assertions, no response body
  matching, no synthetic transactions.
- **Certificate issuance, renewal or installation.** We observe certificates; we never obtain, install
  or hold one, and never hold a private key.
- **The pricing of an observation.** `metering-and-credits` decides what an event costs; this change
  decides its shape.

### What we considered and rejected

**Keeping reachability because the handshake reveals it anyway.** Nearly free to record, and it is
what was originally asked for. Rejected as a product surface for the reason above: nobody acts on it,
and offering it invites a customer to treat an hours-apart sample as monitoring. The failure of a
handshake is kept for a narrower purpose — explaining why we have no observation, rather than
claiming a node is down.

**Probing the node-by-domain matrix.** Exhaustive, and it does catch one real failure the per-node
approach misses: a server offering different certificates for different server names. Rejected on
cost — two hundred handshakes to observe four certificates — with the note that a per-name probe
remains available later, aimed at exactly the names no observed certificate covers, which is the case
where the distinction actually matters.

**Storing only the latest observation per node address.** One row per address, and it is all the
console screen strictly needs. Rejected because it cannot answer "since when", which is the first
question asked about every problem this change exists to surface.

**Billing per handshake.** The most direct relationship between cost and charge. Rejected because the
customer configured *a node to be observed*; they did not choose the interval, and they did not choose
that a dual-stack node costs two handshakes. Both are ours to change, and changing them must not move
an invoice.

## Capabilities

### New Capabilities

- `certificate-observation`: observing the certificate a node actually serves — expiry, issuer and
  covered names — per node address, recording failed handshakes as observations, identifying
  inventory domains that no observed certificate covers, reporting approaching expiry, and the
  retention of that history.

### Modified Capabilities

- `admin-console`: the console gains a per-load-balancer certificate view showing, per node and
  address family, the observed certificate and its history, the domains covered by nothing, and the
  expiries approaching.

## Impact

- **New schema.** `certificate_observations`, carrying the materialized ownership columns and forced
  row-level security every tenant table has.
- **New `internal/certobserve`.** Handshake execution and the derived comparisons. Deliberately
  separate from `internal/resolvercheck`, which stays advisory.
- **`metering-and-credits`.** A configured node becomes a billable rate event, `node.observed`. The
  observations themselves stay out of the credit ledger.
- **The console.** A per-load-balancer certificate view is new.

## Dependencies

This change depends on `tenancy-and-permissions`, and assumes from it:

- **Ownership through a project** to an account, so observations are confined like any other
  inventory row.
- **The materialized ownership columns and the ACL predicate**, so reading certificate status can be
  granted without granting the inventory it describes.

It has no dependency on any DNS-hosting change. `dns-records` and `satellite-sync` are archived
unimplemented, and nothing here needs them: the observer is an outbound TLS client that runs where
the service runs.
