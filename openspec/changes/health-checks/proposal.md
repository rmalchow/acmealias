> **Reduced to certificate observation.**
>
> This proposal argued that checks exist *primarily to steer DNS answers*, and that the status
> display and the expiry log come free as byproducts. It was explicit that designing it the other
> way round — a monitoring product that might later steer DNS — would produce something with no
> quorum, no fail-open rule, and per-satellite results that disagree.
>
> The product no longer serves customers' DNS, so nothing steers. That removes the reason for
> quorum, hysteresis, the never-serve-empty rule, per-family failover state and the correlated-family
> suppression — all of which existed because check results changed what the internet was told. The
> warning above no longer applies either: it was about *later* steering with a mechanism not built
> to be trusted, and there is no later.
>
> What remains is the half the original called a byproduct, and it is the half that closes a loop
> nothing else can see. The service answers a DNS-01 challenge; `challenge-history` records that a
> certificate authority came and validated; and then nothing in the system knows whether a
> certificate was ever installed, or whether it covers the names it should. One TLS handshake per
> node per address family answers that.
>
> Reachability probing is NOT kept. Its only remaining purpose would be a status display, nothing
> acts on it, and everyone being monitored already has monitoring. Certificate expiry and coverage
> are the things a customer genuinely cannot see easily, which is what makes them worth building.

## Why

### A nameserver that knows a host is down and answers with it anyway is doing half a job

Once `dns-records` makes the service authoritative for a customer's zone, `www.example.com` resolves
to whatever we say it resolves to. If that is `203.0.113.10` and `203.0.113.11`, and the first of
those has been unreachable for ten minutes, we are actively directing half the customer's traffic
into a hole.

We are also uniquely well placed not to. We already know the customer's nodes — `lb_nodes` holds
their addresses, and `internal/inventory` already generates a DNS plan from them. We are about to
have a fleet of processes distributed near their users, whose entire job is answering DNS about
those nodes. Having those processes also *ask* whether the nodes are answering is a small addition
that turns a static record into a health-checked one.

### The status display and the certificate log are byproducts, not a second product

The original ask was for a per-host status display: reachability, plus certificate expiry. It is
worth being clear about the ordering, because it determines the design.

These checks exist **primarily to steer DNS answers.** A node that fails checks leaves the recordset.
That makes check results load-bearing: they change what the internet is told, so they need quorum,
hysteresis and a fail-open rule. The status display, the expiry log and the metrics history then come
free, because the data is already being collected and reported for a reason that justifies its own
cost.

Designing it the other way round — a monitoring product that might later steer DNS — would produce
something with no quorum, no fail-open rule, and per-satellite results that disagree, and then
steering answers with it would be unsafe.

### The certificate check closes a loop nothing else can see

The service answers DNS-01 challenges. `challenge-history` records that a certificate authority came
and looked, and that validation succeeded. Then it stops, because what happens next happens on the
customer's machine.

There is a failure mode entirely invisible from here: validation passed, a certificate was issued,
and it was never deployed. Or it was deployed to two of four nodes. Or it covers `example.com` but
not the `shop.example.com` the customer added last week. The challenge history shows success in every
one of those cases, and the customer finds out when a browser tells them.

Connecting to a node and reading the certificate it actually serves closes that loop: we answered a
challenge → a certificate authority issued → the certificate is installed, on this node, covering
these names, expiring on this date. That makes certificate observation the natural completion of the
challenge service rather than a monitoring feature bolted onto it.

### What already exists, and why it is not this

`internal/resolvercheck` resolves a DNS plan against a configured resolver and reports what is
published. It is deliberately, explicitly advisory:

> *"Results are advisory only: DNS is somebody else's mutable state, so nothing here is ever persisted
> as fact."*

That is right for what it does — checking whether a customer published the `CNAME` we asked for is a
question about someone else's zone, and the answer is a snapshot with no authority. The checks in
this change are the opposite in every respect: persisted as history, metered as billable events, and
load-bearing for the answers we serve. Same shape of outbound probe, entirely different status, and
they should not be conflated in the code or in anyone's head.

## What Changes

### Satellites check; the centre decides

A satellite runs the probes and reports what it saw. It does **not** act on what it saw.

The alternative — each satellite pulling a node it cannot reach, immediately and locally — is
tempting: failover in milliseconds, and it keeps working while disconnected. It is rejected because
it produces disagreement that is visible to the customer as a name resolving differently depending
on which nameserver was asked, and because a satellite with a bad transit path to one node would
silently blackhole a perfectly healthy host for everyone who happened to ask that satellite.

So results go up over the report channel `satellite-sync` specifies, the centre applies **quorum** —
some number of satellites must agree before a node's state changes — and the resulting recordset goes
back down the stream to every satellite. Every nameserver then gives the same answer, and one bad
network path cannot remove a healthy node.

### Three rules that stop the check system causing outages

**Never return an empty answer.** If every node in a recordset fails quorum, serve them all. This
looks like ignoring the checks and it is the most important rule here. "All of the customer's nodes
are down simultaneously" is a rare event; "our checking is broken, or a shared network path to them
is" is a much more common explanation for the same observation. Between serving addresses that might
be dead and serving nothing at all, the first at least has a chance of working, and the second is
guaranteed not to. **The check system must never be able to cause a worse outage than the thing it is
watching.**

**Hysteresis.** A node is marked down only after some number of consecutive failing cycles, and comes
back only after more than one consecutive passing cycle. Without it, a node on a lossy path
oscillates — and each oscillation is a recordset change pushed to every satellite in the fleet, so
flapping is expensive as well as wrong.

**TTL and check interval are chosen together.** It is tempting to optimise the reporting and decision
path for speed, and it is mostly pointless: a resolver that cached `www.example.com` under a
300-second TTL will not ask again for five minutes, whatever we decided in the meantime. The lever
for fast failover is a **low TTL on health-checked recordsets**, and a low TTL is what makes a fast
decision loop worth having. Choosing one without the other gets you either a fast pipeline nobody
observes or a low TTL with nothing useful to say.

### Certificates are checked per node, per distinct certificate

The naive shape is a matrix: for each node, for each domain, connect with that domain as the server
name and read the certificate. Four nodes and fifty domains is two hundred handshakes per cycle, to
discover what is almost always four certificates.

Instead: connect once per node, read the certificate offered, and take the names it covers from its
subject alternative names. Fifty domains on one certificate is one probe. And it yields something the
matrix approach does not give directly — **which domains are covered by no certificate at all** — by
comparing the inventory against the union of observed names.

Record the issuer as well as the expiry, because that is what closes the loop described above.

### Per-node history

Reachability, latency, certificate expiry and issuer are kept as history, per node, so the status
display can show a trend rather than an instant, and so "when did this start" is answerable.

This is the part with the least interesting design and the most operationally important question,
which is retention: a per-node time series across a fleet of checkers accumulates quickly, and how
long it is kept at what resolution is a real decision rather than a default.

### Not in scope

- **Alerting and notification.** The data supports it; who gets told and how is separate.
- **Application-level checks** beyond reachability and a TLS handshake — no HTTP status assertions, no
  response body matching, no synthetic transactions.
- **Latency-based or geographic steering.** Checks answer "is this node usable", not "which node is
  best for this resolver".
- **Certificate issuance or renewal.** We observe certificates; we do not obtain or install them.
- **The pricing of a check.** `metering-and-credits` decides what an event costs; this change emits
  the events.

### What we considered and rejected

**Satellites react locally.** Failover in milliseconds and it survives disconnection. Rejected for
split brain — the same name resolving differently per nameserver, which is very hard to debug from
the outside — and because one satellite's bad path becomes a global blackhole for a healthy node.

**Observational only, with failover as a later change.** Much simpler: no quorum, no consistency
question, no fail-open rule, and the check data could earn trust before anything depended on it.
Rejected because the resulting design would be shaped wrong for steering — per-satellite results with
no agreement protocol — and retrofitting quorum into a system whose consumers assume a single opinion
is harder than building it once.

**Probing the node-by-domain matrix for certificates.** Exhaustive, and it does catch a real failure
the per-node approach can miss: a server that offers different certificates for different server
names. Rejected on cost — two hundred handshakes to observe four certificates — with the note that a
per-name probe remains available later for names the certificate does not appear to cover, which is
the case where the distinction actually matters.

**Deciding failover on a single satellite's report.** Simplest possible rule, and lowest latency.
Rejected because it makes every satellite's transit path a single point of failure for the whole
fleet's answer.

## Capabilities

### New Capabilities

- `health-monitoring`: what is checked and how often, that satellites probe and report while the
  centre decides, quorum, hysteresis, and the retention of per-node history.
- `failover-answers`: recordsets whose contents follow check results — how a node leaves and rejoins,
  the rule that an answer is never empty, and the relationship between the check interval and the
  time to live.
- `certificate-observation`: observing the certificate a node actually serves — expiry, issuer and
  covered names — and identifying inventory domains that no observed certificate covers.

### Modified Capabilities

- `admin-console`: the console gains a per-node status view showing reachability, certificate expiry
  and history.

## Impact

- **New schema.** Check definitions, check results as history, per-node certificate observations, and
  the state machine backing hysteresis and quorum.
- **`satellite-sync`'s report channel.** Carries check results, batched and idempotent as specified
  there — which matters more here than anywhere, since a double-counted report is a wrongly-ejected
  node.
- **`satellite-sync`'s distribution stream.** Carries recordset changes originating from check
  decisions rather than from a customer's edit.
- **`dns-records`.** A recordset may be health-checked, which makes its served contents a function of
  its stored contents and the current node states.
- **New `internal/healthcheck`.** Probe execution on satellites; quorum, hysteresis and decision on the
  centre. Deliberately separate from `internal/resolvercheck`, which stays advisory.
- **`metering-and-credits`.** Delivered check reports become billable events.
- **The console.** A per-node status view is new.

## Dependencies

This change depends on `tenancy-and-permissions`, `satellite-sync` and `dns-records`, and assumes
from them:

- **Ownership through a project** to an account, so check results, history and certificate
  observations are confined like any other inventory.
- **Permission grants that take an object**, so reading node status can be granted without granting
  the inventory, and configuring checks can be granted separately from reading them.
- **The report channel**, batched and idempotent, since check results arrive that way and a
  re-delivered batch must not eject a node twice.
- **The distribution stream**, since a failover decision reaches satellites as a recordset change.
- **Recordsets** as the unit of storage and distribution, since a health-checked recordset is one
  whose values are filtered by node state.
