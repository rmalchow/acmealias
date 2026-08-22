## Context

`dns-records` makes the service authoritative for customer zones, so what a recordset holds is what
the internet is told. `satellite-sync` puts a fleet of processes near the resolvers doing the asking,
each already holding a replica and already talking to the centre in both directions.

Those two together make health-checked DNS a small addition rather than a new subsystem: the
probes have somewhere to run, the results have a channel home, and the decisions have a way back out.

There is an existing checker that this is deliberately not. `internal/resolvercheck` verifies that a
customer published the `CNAME` we asked for, and its documentation is emphatic about its status:
*"Results are advisory only: DNS is somebody else's mutable state, so nothing here is ever persisted
as fact."* The checks designed here are persisted, metered, and change what is served. Sharing code
between them would be a mistake; sharing a mental model would be a worse one.

Terms used throughout, defined once:

- **Probe** — one attempt by one satellite to determine one fact about one target: is this address
  reachable, what certificate does this node serve.
- **Cycle** — one scheduled round of probes for a target.
- **Quorum** — the number of satellites that must agree before the centre changes a node's state.
- **Hysteresis** — requiring several consecutive agreeing cycles before changing state, so that a
  marginal target does not oscillate.
- **Health-checked recordset** — a recordset whose served values are its stored values filtered by
  current node state.
- **Fail-open** — serving everything when the check system's conclusion is not credible.

Constraints that shaped everything below:

1. **The check system must never cause a worse outage than the thing it watches.**
2. **Every nameserver must give the same answer for the same name.**
3. **A re-delivered report must not change a decision twice** — inherited from `satellite-sync`, and
   sharper here, because a duplicate report can push a node over a quorum threshold.
4. **Check results are billable**, so their volume is a cost to the customer and their cardinality is
   a design constraint.

## Goals / Non-Goals

**Goals:**

- Health-checked DNS answers that are consistent across the fleet.
- A decision path that fails towards serving rather than towards silence.
- Certificate observation that closes the loop the challenge history cannot.
- Per-node history that answers "when did this start".

**Non-Goals:**

- Alerting, notification, or on-call integration.
- Application-level assertions, synthetic transactions, or response-body checks.
- Latency-based or geographic traffic steering.
- Obtaining, renewing or installing certificates.

## Decisions

### H1. Satellites probe, the centre decides

A satellite executes probes and reports results. It never changes what it serves on the basis of its
own observation.

The temptation is real and should be named: local reaction gives failover in milliseconds and works
while the satellite is disconnected. It is rejected for two reasons that both come from constraint 2.

First, split brain. Two satellites with different network paths reach different conclusions, and the
same name resolves differently depending on which nameserver a resolver happened to ask:

```
dig www.example.com @ns1   →  203.0.113.10, 203.0.113.11
dig www.example.com @ns2   →  203.0.113.10, 203.0.113.11, 203.0.113.12
```

That is close to undebuggable from the customer's side, because the answer changes when they retry.

Second, blast radius in the wrong direction. A satellite with a bad transit path to one node would
remove a perfectly healthy host from the answers it gives — and it gives them to everyone who resolves
through it.

Centralising the decision costs one report interval plus one push before an answer changes. H4
explains why that cost is close to irrelevant.

### H2. Quorum, and what it protects against

A node's state changes only when at least *N* satellites agree, from a probing set of at least *N*
distinct satellites assigned to that node.

```
sat-eu   node3 DOWN ┐
sat-us   node3 DOWN ├──> centre: 2 of 3 agree ──> node3 leaves the recordset
sat-ap   node3 OK   ┘                             ──> pushed to every satellite
```

What quorum buys is protection against the checker being wrong rather than the node being down: a
transit failure between one satellite and one node, a satellite whose egress is degraded, a satellite
whose clock or configuration is off. Those produce a minority opinion, and a minority opinion changes
nothing.

Scheduling follows from this: each node must be assigned to enough satellites that quorum is
reachable, and those satellites should not share a network path if the point is to distinguish node
failure from path failure.

### H3. Never serve an empty answer

If every value in a health-checked recordset fails quorum, the recordset is served **in full**, as
though no checks existed.

This is the single most important rule here and it will look wrong to a reader who thinks of checks
as authoritative. The reasoning is about likelihood: "all four of the customer's nodes became
unreachable simultaneously" is a rare event, while "our probing is broken", "a shared upstream path
failed", or "a configuration change made every probe fail" are common ones, and they produce exactly
the same observation.

Between serving addresses that may be dead and serving nothing, the first can still work — the
failure may be in the path from our satellites and not in the path from the user — and the second
cannot. Constraint 1 in one sentence: the monitoring must not be able to take down something it is
only supposed to be watching.

A recordset serving all values while all of them fail checks is an alarm-worthy state, and is
reported as such. It is not, however, a state in which we stop answering.

### H4. The time to live dominates, so choose it with the interval

The apparent cost of central decision-making is latency: report interval, then decision, then push.
It is worth putting next to the number that actually governs failover.

A resolver that cached `www.example.com` with a 300-second TTL will not ask again for up to five
minutes. Against that, a 60-second decision loop is noise. Conversely, a 10-second decision loop
behind a 300-second TTL is effort spent on something no client will observe.

So the two are chosen together:

| TTL on health-checked recordsets | Useful check interval | Actual failover time |
| --- | --- | --- |
| 300s | 60s | up to ~6 minutes |
| 60s | 30s | up to ~1.5 minutes |
| 30s | 15s | up to ~45 seconds |

The lever for fast failover is the TTL, and a low TTL is what makes a fast decision loop worth
paying for — in query volume to satellites, in probe volume, and in metered check events. Health-checked
recordsets therefore get their own TTL default, lower than the ordinary one, rather than inheriting
whatever a zone uses.

### H5. Hysteresis, asymmetric on purpose

A node is marked down after *K* consecutive failing cycles and comes back after *M* consecutive
passing cycles, with *M* > 1.

Without it a node on a lossy path oscillates, and here oscillation is not merely untidy: each state
change is a recordset change distributed to every satellite in the fleet, so a flapping node
generates fleet-wide write amplification.

The asymmetry is deliberate. Removing a node is the safer error — the remaining nodes absorb the
traffic — while returning a node that is not actually healthy sends real traffic into a failure. So
recovery is required to be more convincingly demonstrated than failure.

### H6. Certificates are observed per node, per distinct certificate

Connect to the node, complete a TLS handshake, read the certificate offered, and record its expiry,
its issuer, and the names in its subject alternative name extension.

The matrix alternative — one handshake per (node, domain) pair with the domain as the server name — is
two hundred probes for four nodes and fifty domains, to observe what is usually four certificates. It
is rejected on cost, which under constraint 4 is the customer's cost as well as ours.

Taking covered names from the certificate rather than probing per name yields something better than
the matrix gives directly: comparing the union of observed names against the account's inventory
identifies **domains covered by no certificate at all**, which is the failure a customer most wants
told about and cannot easily see themselves.

The issuer is recorded because it closes the loop described in the proposal. The service answers a
DNS-01 challenge; `challenge-history` records that a certificate authority came and validated; and
then nothing else in the system can tell whether a certificate was ever installed. Observing the
served certificate completes the chain, and the failure it catches — validation succeeded, deployment
did not — is invisible everywhere else.

**Known limitation, accepted:** a server offering different certificates for different server names
is observed only for the name probed. A per-name probe remains available later, targeted at names that
no observed certificate covers, which is exactly where the distinction matters.

### H7. Check results are history; the decision state is separate

Two different things with two different lifetimes:

- **Results** are an append-only time series: which satellite probed which target, when, with what
  outcome and latency. Kept for trend, for "when did this start", and for the status display.
- **State** is the current conclusion per node — up or down, how many consecutive cycles have agreed —
  which is small, mutable, and read on every decision.

Keeping them apart means retention on the history can be aggressive without endangering the decision
path, and the decision path does not scan a growing table.

Retention is a real decision rather than a default: satellites × nodes × cycles accumulates fast. The
working shape is full resolution for a short window, downsampled beyond it, with certificate
observations kept longer than reachability samples because their natural cadence is days and their
value is the trend towards an expiry date.

### H8. Reports are idempotent, and here that is a correctness requirement

`satellite-sync` specifies batch identifiers and discarding re-deliveries. In this change that stops
being hygiene: a re-delivered batch counted twice can supply the majority that ejects a node, and can
advance a hysteresis counter past its threshold.

So the centre records results keyed such that re-delivery is a no-op, and quorum counts **distinct
satellites**, not reports — a satellite reporting the same cycle twice contributes one opinion.

### H9. Health-checked recordsets are filtered at serve time from state, not rewritten

A health-checked recordset stores its full set of values. What is served is those values filtered by
current node state.

The alternative — rewriting the stored recordset when a node leaves — would lose the information that
the node belongs to the set, so restoring it would require remembering it somewhere else anyway. It
would also make a customer's edit and a failover decision write to the same field, with the ordering
problems that implies.

So the stored recordset is the customer's intent, node state is the fleet's observation, and the
served answer is a function of both, computed centrally and distributed as the served result.

## Risks / Trade-offs

- **The check system causes an outage it was meant to prevent** → fail open when everything fails
  quorum (H3); the state is alarmed rather than acted upon.
- **A node flaps and amplifies writes across the fleet** → asymmetric hysteresis (H5).
- **A duplicate report ejects a node** → quorum counts distinct satellites, and re-delivery is a no-op
  (H8).
- **Failover is slower than the customer expects** → TTL is the governing number and is chosen with the
  interval (H4); documented so the expectation is set correctly rather than tuned in the wrong place.
- **A satellite with a degraded path reports everything down** → it holds a minority opinion (H2), and
  a satellite whose reports disagree persistently with its peers is itself an alarm.
- **Check history grows without bound** → separated from decision state, downsampled, retained by
  policy (H7).
- **A per-server-name certificate mismatch is missed** → accepted, with per-name probing available for
  names no observed certificate covers (H6).
- **Metered check volume surprises a customer** → the billable unit and its price are
  `metering-and-credits`; what this change controls is that cardinality is per node and per
  certificate, not per node-domain pair.

## Migration Plan

1. Add check definitions, results history, node state, and certificate observations to the model and a
   migration.
2. Add `internal/healthcheck` probe execution to the satellite; emit results on the existing report
   channel.
3. Add central quorum and hysteresis evaluation over reported results.
4. Add health-checked recordsets to `dns-records`, served as stored values filtered by node state
   (H9), with their own lower TTL default.
5. Implement the fail-open rule and alarm on the all-down state (H3).
6. Add certificate observation and the uncovered-domain comparison (H6).
7. Add the console status view and history.
8. Emit delivered check reports as metering events for `metering-and-credits`.

**Rollback**: recordsets are not health-checked unless configured to be, so the failover path is
inert until enabled per recordset; disabling it restores unfiltered serving immediately.

## Open Questions

- **What are the default quorum size, K and M?** Quorum of 2 from 3 assigned satellites, K=3, M=5 is
  the working assumption. All three want validation against real probe data rather than argument.
- **How are satellites assigned to nodes?** Enough for quorum, and ideally not sharing a network path —
  but expressing "does not share a path" is itself unsolved, and geography is a poor proxy for it.
- **Does the customer choose the check interval?** It is a cost lever and a failover-speed lever at
  once, which argues for exposing it; it is also a way to generate metered events, which argues for
  bounding it.
- **How long is certificate history kept?** Longer than reachability history, since its value is a
  trend measured in weeks — but a number is wanted.
- **Should an all-down recordset be reported to the customer as an incident?** It is our strongest
  signal that something is badly wrong, and it is exactly when a customer most wants to be told; but
  it is also the signal most likely to be caused by our own checking being broken.
