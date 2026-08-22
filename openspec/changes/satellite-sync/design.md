## Context

`internal/dnsserver` answers authoritative DNS for the challenge zone, and `internal/zoneview` holds
the in-memory state it answers from. Neither touches the database on the query path — the package
documentation of `dnsserver` says *"it never recurses and never touches the database on the query
path — every answer comes from the in-memory zoneview"* — which means the answering half of the
service is already free of the storage half.

What binds them together today is one function: `zoneview.FromDB(db)`, which adapts
`store.ActiveByAlias` to the `Loader` type the view accepts. Replace that adapter and the same DNS
server runs anywhere.

This change is about what replaces it, and about the protocol that keeps the replacement current.

Terms used throughout, defined once:

- **Centre** — the deployment holding the database, the management API and the console. What exists
  today.
- **Satellite** — a process that runs `dnsserver` and `zoneview` and nothing else, holding a replica
  of the answerable state.
- **Sync stream** — the long-lived, satellite-initiated HTTP response over which the centre sends
  state.
- **Position** — a satellite's place in the centre's change sequence, used to resume after a
  disconnection.
- **Snapshot** — the complete answerable state, as opposed to a delta describing one change.
- **Report** — anything a satellite sends upward: check results, metering events, liveness.

Constraints that shaped everything below:

1. **A published challenge must be answerable within seconds**, because an ACME client is blocking on
   it. This rules out anything whose propagation delay is a poll interval.
2. **A satellite must keep answering when the centre is unreachable.** The control plane going away
   must not become a DNS outage.
3. **The control channel must work from networks we do not control**, including behind NAT and behind
   an egress proxy.
4. **The DNS answering code must be shared literally**, not reimplemented, because it is the part
   that is dangerous to get subtly wrong.

## Goals / Non-Goals

**Goals:**

- A satellite that is the existing DNS code plus a sync client, with no fork of the answering path.
- A protocol whose reconnection and catch-up behaviour is a property of the protocol rather than
  something we implement.
- Convergence for a satellite that was disconnected, restarted, or has just been enrolled.
- Reports that cannot be double-counted.

**Non-Goals:**

- Real DNS records, health checks, metering ledgers, DNSSEC — each is its own change.
- Customer-run satellites.
- Satellite identity and enrolment, which is genuinely unsettled and is left as an open question
  rather than answered badly.

## Decisions

### Y1. The satellite is the existing view with a different `Loader`

```go
// today, in the centre
view := zoneview.New(zoneview.FromDB(db))

// on a satellite
view := zoneview.New(satellite.FromStream(client))
```

Everything downstream of that line is unchanged: `Refresh` replaces the snapshot wholesale, `Run`
re-refreshes on an interval as a safety net, `Lookup` applies expiry at read time, and `dnsserver`
answers from the map.

This is not an accident to be exploited; the indirection was put there for it. `view.go` documents
the `Loader` type as existing *"so the view can be tested, and its failure behaviour exercised,
without a database"* — a source that is not a database is precisely the case it was designed to
admit.

**Rejected: a purpose-built satellite DNS responder.** A satellite has narrower needs than the centre
and a leaner responder could be written. Rejected because the responder is where the subtle
correctness lives — the NXDOMAIN-versus-empty-NOERROR distinction that `zoneview` exists to preserve,
the SOA in the authority section, the refusal of out-of-zone names, TCP fallback — and two
implementations of that means two behaviours, one of which is wrong in a way nobody notices until a
certificate authority does.

### Y2. Fail-safe on disconnection is already specified, and already tested

Constraint 2 is satisfied by behaviour that exists. `refresh.go` keeps the previous snapshot when a
load fails:

> *"On failure the previous snapshot is kept and served unchanged: a name whose value was known is
> never withdrawn because the database went away. The error is returned for the caller to log, and
> recorded for Status."*

Read with a sync stream in place of a database, that is the whole disconnection policy: a satellite
that loses the centre keeps answering what it last knew, and reports the failure through its status.

The bound on how stale that can become is not a timer we add, because there is already a better one.
`Lookup` filters values by `ExpiresAt` at read time, so challenge values decay on their own schedule
whether or not the centre is reachable. A disconnected satellite cannot answer a value past its
expiry, and a withdrawn-then-expired value cannot be resurrected by a stale snapshot.

That leaves exactly one gap to be explicit about: a value **withdrawn before its expiry** stays
answerable on a disconnected satellite until it expires. That is the correct trade — the alternative
is withdrawing names on connection loss, which turns every control-plane blip into an outage — and
the exposure is bounded by the challenge lifetime, which is already short.

### Y3. Server-Sent Events down, ordinary POST up

The asymmetry is real, so the transport is asymmetric:

| | Down (centre → satellite) | Up (satellite → centre) |
| --- | --- | --- |
| Volume | Small | Large |
| Urgency | Seconds matter | Minutes are fine |
| Ordering | Strict | Irrelevant |
| Mechanism | SSE stream | Batched POST |

SSE is chosen for one property above all: **`Last-Event-ID` is the resume protocol.** A client
reconnecting sends the last event identifier it processed in a header; the server continues from
there. Every streaming design needs that exchange, and here it is specified, implemented in every
HTTP stack, and requires no framing of our own.

Secondary properties that matter operationally: it is HTTP/1.1 text, so it survives any proxy that
speaks HTTP; it can be read with `curl` when diagnosing a satellite that is not converging; and it has
no keepalive protocol to get wrong, because a comment line is a heartbeat.

Reports go up as separate POSTs rather than over a duplex channel because they have a different
cadence, a different failure policy and a different retry story. Coupling them to the config channel
would mean a large report batch delaying a challenge publish.

**Rejected: WebSocket** — you reimplement framing, RPC shape, keepalive and resume; worse through
proxies. **Rejected: gRPC streaming** — requires HTTP/2 end to end, the most likely thing to break in
a network we do not control (constraint 3). **Rejected: polling with ETag** — propagation delay
becomes the poll interval, violating constraint 1.

### Y4. Reconciliation, not a delta stream

A satellite must converge from three starting states: never connected, disconnected for a while, and
disconnected for longer than the centre retains deltas. One mechanism covers all three.

The centre maintains a monotonic sequence number over changes to the answerable state. On connect:

| Satellite sends | Centre responds |
| --- | --- |
| no `Last-Event-ID` | `snapshot` event, then the live stream |
| `Last-Event-ID` within the retained window | the deltas since that position, then the live stream |
| `Last-Event-ID` older than the retained window | `snapshot` event, then the live stream |

The satellite applies a snapshot by replacing its map, which is one write-locked assignment — the
same operation `Refresh` already performs on every timed reload. So the fallback path is the cheap
one and the well-exercised one, and the delta path is an optimisation that can be disabled entirely
without breaking correctness.

A satellite that receives an event out of sequence, or cannot apply a delta, requests a snapshot
rather than guessing. Divergence must be self-correcting, because a satellite serving a state nobody
knows it holds is worse than one that briefly reloads everything.

**Rejected: deltas only, with the satellite responsible for spooling.** It removes the snapshot path
and its retention question. Rejected because it makes enrolment a special case, makes a long outage
unrecoverable without operator intervention, and gives divergence no way to heal.

### Y5. Reports are batched and idempotent by construction

Each batch carries a satellite-generated identifier, and the centre records the identifiers it has
accepted. Re-delivery is accepted and discarded.

This is not defensive engineering for an unlikely case. The common failure is ordinary: the satellite
posts, the centre commits, the response is lost to a timeout, and the satellite — correctly — retries.
Without an identifier the centre counts the batch twice, and the reports channel feeds both billing
and failover, where counting twice means a wrong invoice and a wrongly-ejected node.

The corollary matters for satellite design: because an undelivered batch has cost nobody anything, a
satellite may **drop** a batch it cannot deliver rather than spooling indefinitely. It does not need
durable local storage, which keeps a satellite a stateless process that can be replaced rather than
recovered.

### Y6. Satellites are platform objects on the staff plane

A satellite has no owning account and holds no tenant-specific authorization. It is enrolled,
observed and retired under `platform.admin` from `staff-plane`, and does not appear in any tenant's
console.

The security consequence to state plainly: a satellite holds every tenant's zone data. That is
tolerable because **DNS records are public data by construction** — the purpose of publishing a record
is that strangers can read it — and a satellite holds nothing else: no credentials, no billing data,
no personal data. The threat that matters is a compromised satellite serving *forged* answers, which
is a write problem, not a read problem, and which `dns-records` addresses by deciding where signing
happens.

### Y7. One image, satellite selected by configuration

The same container image runs as centre or satellite, chosen by a configuration value read at startup
and reported in the startup log line and on the readiness endpoint.

This follows the reasoning already applied to editions in `add-accounts-and-public-edition`: a
build-time switch produces two artefacts that are indistinguishable to an operator debugging a
running process, whereas configuration is inspectable at runtime. It also means the satellite is
built, tested and released by the existing pipeline rather than by a second one.

A satellite starts no database connection, no management listener and no console. Its readiness
depends on having a usable snapshot, not on reaching the centre — a satellite serving a valid stale
view is ready, and marking it otherwise would remove it from rotation exactly when the fleet is
already degraded.

### Y8. Staleness is observable, and it is the fleet's primary health signal

Each satellite reports the position it has applied and when it last applied one. The centre exposes,
per satellite, how far behind it is in events and in time.

This is the metric that matters operationally, and it is not "is the connection up". A satellite with
an open connection that has silently stopped applying events is the dangerous state, because it looks
healthy and answers wrongly. Position lag detects it; connection state does not.

## Risks / Trade-offs

- **A withdrawn challenge stays answerable on a disconnected satellite** → bounded by the value's own
  `expires_at`, which `Lookup` enforces locally (Y2); the alternative fails closed into an outage.
- **A satellite silently stops applying events** → position lag is reported and alerted on, rather than
  connection state (Y8).
- **Delta retention is exhausted during a long outage** → snapshot fallback is the well-exercised path,
  not an emergency one (Y4).
- **A report batch is counted twice** → batch identifiers, with re-delivery accepted and discarded
  (Y5).
- **A compromised satellite serves forged answers** → out of scope here and addressed by where signing
  happens in `dns-records`; noted so it is not mistaken for an oversight.
- **The sync endpoint becomes a way to read every tenant's data** → satellite authentication is its own
  concern and is an open question below; until it is answered this is the change's largest unresolved
  risk, and it is named rather than assumed away.
- **Snapshots are expensive as the fleet and the zone grow** → mitigated by delta resume in the common
  case; snapshot cost is one map build, and the view is already rebuilt wholesale on every timed
  refresh today.

## Migration Plan

1. Add a monotonic change sequence over the challenge state in the centre, and delta retention.
2. Add the sync endpoint: SSE, honouring `Last-Event-ID`, emitting snapshot or deltas.
3. Add the report intake endpoint with batch-identifier deduplication.
4. Add `internal/satellite`: connect, resume, apply, report.
5. Add the satellite run mode to `cmd/acmeservice`, reported at startup and on readiness.
6. Add satellite registration and observation to the staff surface under `platform.admin`.
7. Run a satellite alongside the centre answering the same zone, and compare answers under load before
   any delegation points at it.

**Rollback**: the centre continues to answer DNS itself throughout; satellites are additive until
delegation is changed to point at them, and reverting delegation is the rollback.

## Open Questions

- **How does a satellite prove it is ours?** Not settled. The options — an enrolment token exchanged
  for a credential, mutual TLS with a private certificate authority, a signed hardware or platform
  identity — differ substantially in how they handle a compromised satellite and how they handle
  bootstrapping a new one. This must be answered before the sync endpoint is exposed, and it is the
  one part of this change deliberately left open rather than decided in passing.
- **How long are deltas retained?** Long enough that a routine restart resumes rather than
  re-snapshots; short enough not to become a second store. A number is wanted once the event rate is
  known.
- **Does a satellite serve while it has never had a snapshot?** `zoneview.New` starts empty, and an
  empty view answers NXDOMAIN for names that exist. A satellite must therefore not accept queries
  before its first successful load — which the readiness rule in Y7 implies but which needs stating in
  the startup sequence.
- **Should the report channel and the sync stream share authentication?** They are separate endpoints
  with the same principal; whether they share a credential depends on the answer to the enrolment
  question above.
