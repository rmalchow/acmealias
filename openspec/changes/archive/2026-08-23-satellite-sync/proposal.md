> **Withdrawn — the fleet it distributes to has no reason to exist.**
>
> This change opens: *"one process cannot be an authoritative nameserver for the internet"* — needing
> DNS in several places, near the resolvers asking, surviving an upgrade of the thing that holds the
> data. Every one of those pressures came from serving customers' real zones.
>
> The product answers ACME DNS-01 challenges and observes certificates. A challenge answer sits on a
> renewal path with roughly thirty days of slack, so an hour when nothing answers is invisible to
> everyone; and certificate observation is infrequent enough that one prober is enough. Neither
> justifies a replicated fleet, an event stream, enrolment credentials, or a staleness story.
>
> The parts worth remembering if a fleet is ever wanted: the connection direction (satellites dial
> out, so we never hold a customer's DNS provider credentials), and enrolment as a single-use token
> exchanged for a per-satellite credential, which is the cheapest arrangement that can revoke ONE
> machine.
>
> Archived unimplemented. Nothing should be built from it.

## Why

### One process cannot be an authoritative nameserver for the internet

Today the service is one binary. It holds the database, it serves the management API and the console,
and it answers DNS on port 53 from an in-memory view of that database. For a challenge-only service
with one deployment behind one address, that is exactly right.

Answering real DNS for other people's domains is a different job. It needs to be in several places at
once, because a nameserver that is unreachable is a domain that does not resolve; it needs to be near
the resolvers asking, because every millisecond is on the critical path of every page load; and it
must keep answering while the thing that holds the data is being upgraded, migrated or is simply
broken.

So the answering has to be separable from the deciding. This change introduces the **satellite**: a
process that serves DNS and nothing else, holding a replica of what the centre has decided, kept
current by a connection it opens itself.

### The connection direction is a product decision, not a plumbing detail

There are two ways to keep a remote nameserver current, and they have completely different security
properties.

The industry-standard way is for the control plane to **push** into whatever DNS the customer already
runs — Route 53, Cloudflare, PowerDNS — using an API credential the customer hands over. That
credential can rewrite the customer's entire zone. Every provider that works this way is storing, for
every customer, a secret that is equivalent to full control of that customer's domain, mail included.
It is a standing obligation and a very attractive target.

Satellites invert it. The satellite opens an **outbound** connection to us and pulls what it is
entitled to. Consequences worth being explicit about:

- **We never hold a customer's DNS provider credentials**, because there is no provider to
  authenticate to. There is no secret in our database whose compromise would let anyone rewrite a
  customer's zone somewhere else.
- **Satellites need no inbound firewall rule and no public address for the control channel.** They can
  sit behind NAT, in a private network, in a location whose ingress we do not control. Only port 53
  needs to be reachable, and that is the job they exist to do.
- **Connectivity failures are the satellite's problem to retry**, not ours to detect. A satellite that
  cannot reach us reconnects; a push architecture has to discover that the far end went away and then
  reconcile.

### Most of the satellite is already written

This is the part that makes the change smaller than it sounds. `internal/zoneview` is already a
snapshot of the answerable state, replaced wholesale and read under a shared lock:

> *"View is a snapshot of the zone, replaced wholesale by each successful refresh and read under a
> shared lock in between."*

It is already fed through an indirection that exists precisely so the source can be something other
than the database — `internal/zoneview/view.go` defines `type Loader func(ctx) (map[string][]store.ChallengeValue, error)` and says:

> *"store.ActiveByAlias is the implementation; the indirection exists so the view can be tested, and
> its failure behaviour exercised, without a database."*

Its behaviour when the source is unavailable is already decided, documented and tested, in
`internal/zoneview/refresh.go`:

> *"On failure the previous snapshot is kept and served unchanged: a name whose value was known is
> never withdrawn because the database went away."*

That is exactly the policy a disconnected satellite needs. Serving a slightly stale answer is
enormously better than serving none, and the alternative — withdrawing names because the control
channel dropped — would turn a control-plane blip into a customer-visible outage.

Even expiry is already handled locally rather than centrally. `View.Lookup` filters expired values at
read time:

> *"Expiry is applied here, at read time, so a value stops being answered the instant it expires no
> matter when the sweep or the next refresh runs."*

So a disconnected satellite cannot resurrect a challenge value that has passed its `expires_at`, and
it needs no instruction from us to stop answering it.

A satellite is therefore `internal/dnsserver` plus `internal/zoneview` plus a `Loader` that reads
from a sync stream instead of from `store.ActiveByAlias`. The DNS answering code, which is the part
that took longest to get right, is shared literally rather than reimplemented.

## What Changes

### Satellites are ours, and they are platform objects

We run the fleet. A satellite is an instance-level object with no owning account, managed on the
staff plane under `platform.admin`, and invisible to tenants — a customer sees working DNS, not a
list of machines.

That has one consequence to state plainly: **a satellite holds every tenant's zone data.** It is less
alarming than it first reads, because DNS records are public data by construction — anyone may query
them, and the whole purpose of publishing a record is that strangers can read it. A satellite holds
no billing data, no credentials and no personal data. The real risk is not disclosure but forgery,
and that is addressed by where signing happens, which `dns-records` takes up.

### The transport is Server-Sent Events down and ordinary POST up

The two directions have nothing in common. Configuration flows down: small, urgent, strictly ordered,
and a challenge publish must be answerable within seconds because an ACME client is waiting on it.
Reports flow up: batched, high-volume, tolerant of delay.

So they get different mechanisms rather than one bidirectional pipe:

```
satellite ──GET /satellite/sync (SSE)──────────> centre
          <── event: snapshot   id: 1042
          <── event: publish    id: 1043
          <── event: withdraw   id: 1044

   [connection drops]

          ──GET /satellite/sync
            Last-Event-ID: 1044 ─────────────> centre
          <── deltas since 1044, or a fresh snapshot

satellite ──POST /satellite/reports (batched)──> centre
```

The decisive property is that **`Last-Event-ID` is the reconciliation primitive.** It is part of the
Server-Sent Events protocol: a client reconnecting sends the identifier of the last event it
processed, and the server resumes from there. That is precisely the "I am at version N, what did I
miss" exchange that any replication protocol needs, and it arrives without being designed, framed or
debugged.

### What is designed is reconciliation, not a delta stream

A stream of changes is not enough. A satellite that was disconnected for an hour, restarted, or has
just been enrolled must converge on the correct state, and "apply every delta since the beginning of
time" is not convergence.

So the centre keeps a monotonic sequence over changes and answers a reconnect one of two ways: the
deltas since the satellite's last event, or — when the satellite is too far behind, or has no
position at all — a **full snapshot** followed by the stream. Snapshot is the cheap path here, not
the expensive one, precisely because `zoneview` replaces its map wholesale: applying a snapshot is
one assignment under a write lock, and it is the code path that already runs on every refresh today.

### Reports are batched and idempotent

Satellites send back what they observed: health check results (`health-checks`), metering events
(`metering-and-credits`), and their own liveness. These go up as ordinary POSTs on their own cadence.

Every batch carries an identifier, and re-delivering a batch is a no-op. This matters because the
failure mode is not hypothetical: a satellite posts a batch, the response is lost, the satellite
retries. Without idempotency the centre counts it twice — and in a system where reports drive both
billing and failover decisions, counting twice is a wrong invoice and a wrongly-ejected node.

### Not in scope

- **Real DNS records.** This change moves the *existing* challenge zone view to satellites and defines
  the protocol. Zones and recordsets arrive in `dns-records`.
- **Health checks and failover.** Satellites will run checks and the reports channel is specified
  here, but what is checked, how quorum is decided and how answers change is `health-checks`.
- **Metering.** The reports channel carries events; the ledger and the price book are
  `metering-and-credits`.
- **DNSSEC.** Deferred; `dns-records` shapes the pipeline for it.
- **Customer-run satellites.** Decided against rather than deferred, and the reason is in the
  section above: a satellite holds every other customer's zone data and answers the internet on their
  behalf. Both of those are fine on hardware we control and are a different proposition on hardware
  we do not.
- **Partitioned replicas.** Every satellite holds every zone. Splitting the replica per satellite is
  an optimization for a size we are nowhere near, and it would turn one stream and one position into
  one of each per satellite.

### How a satellite proves it is ours

A satellite opens a connection to us and asks for every customer's zone data. Something has to
establish that the thing on the other end is a machine we deployed, and this proposal settles it
rather than leaving it to implementation.

**Staff create the satellite first, and get a token.** A member of staff registers the satellite in
the console and is shown a long random string once — an *enrolment token* — which is stored only as a
hash and expires within hours. That token goes into the new machine's configuration alongside our
address.

**The token is spent on first start.** The satellite calls one endpoint, hands over the token, and
receives a credential that belongs to that machine alone. The centre marks the token permanently
used in the same breath. A second machine presenting the same token is refused; so is the same
machine after a reinstall, which is deliberate — re-enrolling is staff issuing a new token, and that
leaves a record of who did it and when.

What this buys, concretely, is the ability to **kill one satellite**. A machine is stolen, or a
provider account is compromised, or a satellite is simply decommissioned badly: revoke its
credential, and its stream closes and will not re-open, while every other satellite carries on. It
also means no long-lived secret lives in configuration management, because the only secret that ever
sits in a config file is a token that stops working within hours.

The two obvious alternatives were weighed and set aside. A **shared secret** in every satellite's
configuration is one value authenticating the whole fleet — unrotatable without redeploying
everything, unrevokable per machine, and catastrophic in a single leaked config file. **Mutual TLS
with our own certificate authority** is genuinely stronger and is where this goes at a larger size,
but running a CA is a job with a key to protect, renewals to schedule and revocation to publish, and
it is not a job worth taking on to authenticate a handful of machines.

The sync stream and the report channel share the one credential. They are the same machine; issuing
it two secrets would double the rotation work and protect against nothing, since a satellite that can
be impersonated on either channel is already lost.

### What we considered and rejected

**WebSocket.** One duplex connection carrying both directions, which sounds like the obvious modern
answer. Rejected because everything it appears to give you, you then write yourself: message framing,
a request/response convention, ping/pong keepalive, and — the one that matters — a resume-from-sequence
protocol, which is exactly what `Last-Event-ID` already is. It also fares worse through corporate
egress proxies, and satellites may well live behind one.

**gRPC server-streaming.** A typed, versioned contract, generated clients, real stream semantics, and
a good Go story. Rejected because it requires HTTP/2 end to end, which is the single thing most likely
to break at an egress proxy or a middlebox between a satellite and us — and a control channel that
fails in an environment we do not control is worse than a slightly less elegant one that does not.
SSE is HTTP/1.1 text that survives anything that speaks HTTP, and can be diagnosed with `curl`.

**Polling with an ETag.** Genuinely the simplest thing: no persistent connections, no keepalive, no
resume, scales without connection management. Rejected on latency alone. Challenge propagation delay
becomes the poll interval, and an ACME client that publishes a TXT expects it answerable in seconds,
not on the next minute boundary.

**Pushing into the customer's existing DNS provider.** The industry-standard approach. Rejected as
described above: it obliges us to hold a credential per customer that is equivalent to full control
of their domain.

## Capabilities

### New Capabilities

- `satellite-fleet`: the satellite as an instance-level platform object — what one is, that it is
  managed on the staff plane, that it holds no tenant-specific authorization, and how its liveness and
  staleness are observable.
- `zone-distribution`: the synchronisation protocol — an outbound-initiated event stream carrying
  snapshots and deltas, position-based reconciliation on reconnect, the batched idempotent report
  channel, and what a satellite serves when the centre is unreachable.

### Modified Capabilities

- `challenge-dns-server`: the requirement that DNS reads survive datastore unavailability generalises
  to surviving loss of the source of the view, whether that source is a datastore or a sync stream,
  since a satellite has no datastore at all.

## Impact

- **`internal/zoneview`.** Unchanged in shape. A second `Loader` implementation reads from the sync
  client instead of `store.ActiveByAlias`; `Refresh`, `Run`, the fail-safe policy and read-time expiry
  are untouched.
- **`internal/dnsserver`.** Unchanged. It already never touches the database on the query path — *"every
  answer comes from the in-memory zoneview"* — which is what makes it deployable with no database
  behind it.
- **New `internal/satellite`.** The sync client: connect, resume from position, apply snapshot or
  deltas, batch and post reports.
- **New sync endpoints** on the centre: the event stream and the report intake, on their own listener
  or path prefix, authenticated as satellites rather than as staff or tenants.
- **A monotonic change sequence** over the challenge state, so a position can be resumed from.
- **Deployment.** A second run mode for the same image — satellite rather than centre — selected by
  configuration and reported at startup and on the readiness endpoint.
- **Nothing on the management API or the console.** Tenants do not see satellites.

## Dependencies

This change depends on `tenancy-and-permissions` and `staff-plane`, and assumes from them:

- **The `account` entity** exists, so distributed state can be attributed to an owner for the
  metering and check reports the channel carries.
- **The staff plane** exists with a `platform.admin` permission, which is what authorizes managing the
  fleet. Satellites are platform objects with no owning account, and without that plane they would
  have no authorization model at all.
- **`Scope`** exists and confines tenant queries, which is what lets the centre build a satellite's
  view across accounts without that being a tenant operation.
