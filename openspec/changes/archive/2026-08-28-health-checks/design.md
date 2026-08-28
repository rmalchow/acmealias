# Design

Decisions are numbered `H1`, `H2`, … and referred to by number from `tasks.md`, from `proposal.md`,
and from `metering-and-credits`.

## Context

`challenge-history` records that a certificate authority came to a challenge name, looked, and was
satisfied. Then it stops, because everything after that happens on the customer's machine. The
system has no way to tell the difference between a customer whose renewal completed and a customer
whose renewal completed and whose certificate is still sitting in a directory nobody deployed.

This change closes that gap and does nothing else. A TLS handshake against each of a load balancer's
node addresses tells us what certificate that address actually serves: when it expires, who issued
it, and which names it covers. Comparing the names it covers against the account's inventory tells us
the thing a customer most wants told and can least easily see for themselves — that
`shop.example.com`, added last week, is covered by no certificate anywhere.

Terms used throughout, defined once:

- **Observation** — one completed (or failed) TLS handshake against one node address, and what was
  read from it.
- **Node address** — one address of one node of one load balancer. A dual-stack node is two node
  addresses.
- **Covered name** — a name appearing in the subject alternative name extension of an observed
  certificate.
- **Uncovered domain** — a domain in the account's inventory that appears in no observed certificate.

There is an existing checker that this is deliberately not. `internal/resolvercheck` verifies that a
customer published the `CNAME` we asked for, and its documentation is emphatic about its status:
*"Results are advisory only: DNS is somebody else's mutable state, so nothing here is ever persisted
as fact."* That is the right posture for a question about someone else's zone. An observation here is
the opposite: it is a fact about what a server presented to us at a moment in time, it is persisted,
and it is what a customer is shown. The two share the shape of an outbound probe and nothing else.
Sharing code between them would be a mistake; sharing a mental model would be a worse one.

Constraints that shaped everything below:

1. **Nothing acts on an observation.** It is displayed and it is retained. No answer changes because
   of it — see H1, which is the constraint that removes most of this document's former contents.
2. **The service never holds a certificate or a private key.** It looks at what is served.
3. **The billable unit is what the customer configured**, not how many times we looked, because how
   often we look is our decision to change.

## What this change used to be, in one paragraph

This was designed as health-checked DNS: a fleet of satellites probing customers' nodes, the centre
applying quorum, hysteresis and a fail-open rule to their reports, and the resulting recordset pushed
back out so that a failing node left the answers. `dns-records` and `satellite-sync` are archived
unimplemented and the service does not host customers' zones, so there is no recordset and nothing to
steer. Quorum, hysteresis, the never-serve-empty rule, per-family decision state, correlated-family
suppression and the late-report rules are all gone with it — every one of them existed because a
check result changed what the internet was told. Reachability probing went too, for a reason that is
not about the fleet at all: nothing would act on the result, and every operator being monitored
already has monitoring. That paragraph is the whole of the archaeology; everything below is work to
do.

## Goals / Non-Goals

**Goals:**

- Know what certificate each node address actually serves, and when it expires.
- Know which inventory domains are covered by no certificate at all.
- Keep enough history that "since when" is answerable.
- Emit one billable event shaped like what the customer asked for.

**Non-Goals:**

- Reachability or latency monitoring. Cut deliberately — see the proposal.
- Steering, failover, or any effect on what is served. There is nothing to steer.
- Alerting and on-call integration. The data supports it; who gets told is a separate question.
- Application-level assertions, synthetic transactions, response-body checks.
- Obtaining, renewing, installing or holding certificates or keys.

## Decisions

### H1. We observe, and nothing acts on what we see

An observation changes no answer, gates no request, and suspends nothing. It is written to history
and it is shown to a human.

This is worth stating as a decision rather than leaving as an absence, because the absence is what
makes the rest of this document short. A check result that changed what the internet was told would
need to be trusted, and everything that makes a check result trustworthy — several observers agreeing
before a state changes, several consecutive agreeing cycles before it changes again, a rule that
serves everything when the conclusion is not credible — is machinery whose entire justification is
that something downstream believes it. Nothing downstream believes this. One observer is therefore
enough, disagreement between two runs is just two facts about two moments, and a run that fails
wholesale costs a gap in a chart.

The one thing that survives from that machinery is its motivating rule, because it now applies
trivially: **the observation must not be able to cause a worse problem than the thing it is
watching.** A system that only reads and never decides satisfies it by construction, which is most of
why the reduced scope is worth having.

**Rejected: keeping a reachability signal "since we are connecting anyway".** The handshake does
tell us whether the address answered, and it is nearly free to record. It is still rejected as a
product surface: a status display that nobody acts on invites a customer to treat it as monitoring,
and a monitoring signal sampled every few hours is worse than the monitoring they already have. What
the handshake's failure *is* good for is explaining a missing observation (H3), which is a different
claim — "we could not look" rather than "your node is down".

### H2. One handshake per node address, and the certificate names itself

Connect to the node address, complete a TLS handshake, and record from the certificate offered: its
not-before and not-after, its issuer, its serial, and the names in its subject alternative name
extension.

The alternative is a matrix: for each node, for each domain, connect with that domain as the server
name. Four nodes and fifty domains is two hundred handshakes to discover what is almost always four
certificates. It is rejected on cost, which under constraint 3 is a cost we absorb rather than pass
on, and it is rejected for a second reason that matters more: taking the covered names *from the
certificate* rather than confirming them one at a time is what makes H4's comparison possible. The
matrix can tell you that `shop.example.com` was refused; only the union of observed names can tell
you that it is covered by nothing anywhere.

**Both address families are probed, separately.** A node with an IPv4 and an IPv6 address is two
node addresses, reached over different transit and through different firewalls. A certificate
deployed on one and not the other, or an IPv6 listener that was never configured with the renewed
certificate at all, is a real failure and one that is invisible to a dual-stack visitor and to the
customer's own browser. Splitting the observation is the only way to see it. The doubled probe volume
lands on us and not on the invoice (H6).

**Known limitation, accepted:** a server that offers different certificates for different server
names is observed only for whatever it presents by default. A per-name probe remains available later,
targeted at exactly the names H4 reports as uncovered, which is precisely where the distinction
matters. Building it now would be building the matrix for the case that has not yet been shown to
occur.

### H3. A failed handshake is an observation with an error, not a missing row

If the address refuses the connection, times out, or presents something that is not a certificate we
can read, that is recorded as an observation carrying the error.

The tempting alternative is to write nothing and let the absence speak. It does not speak clearly
enough. "We have no certificate for this node" and "we could not reach this node to look" are
different sentences with different audiences — the first is about the customer's deployment, the
second is about a network path that may well be ours — and a gap in a table is indistinguishable from
a scheduler that did not run. Recording the error also means the covered-names comparison in H4 can
be honest about its own coverage: a domain is *uncovered* only when we successfully read certificates
and none of them named it.

This is the sense in which the failure is still useful without being monitoring (H1): it qualifies
our own conclusions rather than making a claim about the customer's availability.

### H4. The interesting output is the comparison, not the row

Per-node expiry and issuer are the obvious output and the less valuable one. Most customers can find
out what certificate a node is serving. What they cannot easily see is the join:

```
inventory domains          example.com, www.example.com, shop.example.com
observed SAN union         example.com, www.example.com
                           ────────────────────────────
uncovered                  shop.example.com
```

Two derived answers come out of the observation history and both are reported:

- **Domains covered by no observed certificate.** The comparison above, over the account's inventory.
  This is the failure the challenge history structurally cannot see: validation succeeded, and the
  certificate was never deployed, or was deployed to two nodes out of four, or covers the names the
  customer had last month.
- **Approaching expiry**, reported with the lead time a renewal actually needs rather than at the
  moment it lapses. An expiry report that arrives on the expiry date is a post-mortem.

Both are computed from stored observations rather than at probe time, so a change to the inventory
re-answers them without waiting for the next cycle.

### H5. The cadence is hours, and history is kept longer than it is dense

Nothing depends on an observation inside a renewal window (H1), certificates do not change between
one hour and the next, and a renewal has about thirty days of slack. So the schedule is measured in
hours. There is no argument here about failover latency because there is no failover; the only thing
the interval buys is how stale the console can be, and a few hours of staleness against a thirty-day
window is not a number worth tuning.

That cadence makes retention easy in a way the old design's per-second reachability samples never
were. Observations accumulate at nodes × families × a handful per day, which is small enough to keep
at full resolution for a long window and downsample rather than drop beyond it. The value of the
history is a *trend measured in weeks* — this node has been renewing on time for six months; this one
stopped in March — so the retention rule is stated in weeks and the downsampling keeps one
observation per node address per day rather than discarding the period.

**Rejected: keeping only the latest observation per node address.** It is what the console screen
needs and it is one row per address. Rejected because it cannot answer "since when", which is the
first question asked about every problem this change exists to surface.

### H6. The invoice is a function of what the customer configured

`metering-and-credits` prices the events; this change decides their shape, and the shape is the
decision.

The billable event is **`node.observed`: a count over an interval, one row per account per period per
configured node**, emitted by the same periodic job that emits every other rate event. It is not one
row per handshake.

The customer chose *"observe this node"*. They did not choose the interval, and they did not choose
that a dual-stack node is two handshakes rather than one. Both of those are ours, and we will change
them: halving the interval for our own confidence must not double an invoice, and a node acquiring an
IPv6 address must not cost more to observe because we decided to look at it twice. Billing per
configured node per period puts the multiplication on our side of the line, which is the same side as
the engineering decisions that cause it.

The observations themselves are operational data and stay out of the credit ledger entirely — their
own table, their own retention (H5), no deduction row. `metering-and-credits` M11 states the general
rule they are an instance of.

## Risks / Trade-offs

- **A customer reads the certificate view as uptime monitoring** → the surface says what it is, and
  reachability is deliberately not reported (H1); the only failure shown is our own inability to look
  (H3).
- **A server presenting different certificates per server name is observed wrongly** → accepted, with
  per-name probing available later and aimed at the names H4 already flags as uncovered (H2).
- **The uncovered-domain report is wrong because we could not connect** → a failed handshake is a
  recorded error rather than an absence, and does not contribute to "covered by nothing" (H3).
- **Observation history grows** → it is hours-cadence data, retained by a stated policy and
  downsampled beyond a full-resolution window (H5).
- **Probe volume surprises us on the bill** → it cannot reach the invoice: the billable unit is the
  configured node, not the handshake (H6).
- **We are trusted with a private key by implication** → the service connects as a client and reads
  what is presented. It never obtains, installs or holds a certificate or a key, and that boundary is
  what keeps this out of the customer's trust chain.

## Migration Plan

1. Add `certificate_observations` to the model and a migration, with the materialized ownership
   columns and row-level security every tenant table has.
2. Add the observer: one handshake per node address, on an hours-scale schedule, recording success
   and failure alike (H2, H3).
3. Add the derived answers — uncovered domains and approaching expiry — computed from stored
   observations (H4).
4. Add retention and downsampling (H5).
5. Emit `node.observed` from the periodic rate job (H6).
6. Add the management API listing and the console view.

**Rollback**: nothing depends on an observation, so the observer can be disabled and the table left
in place with no effect on any other path. That is the whole benefit of H1, stated as an operational
property.

## Open Questions

- **What is the observation interval, in hours?** Six is the working assumption — four observations a
  day against a thirty-day renewal window. It wants a reason rather than a default, but no decision
  below it depends on the number.
- **How long is full-resolution history kept before downsampling, and how long after?** Weeks and
  months respectively (H5); the numbers want a look at real volume for a real account.
- **How much lead time does the expiry report use?** Long enough to cover a failed renewal and a
  retry, which argues for something like two weeks rather than two days.
- **Is a node with no observation ever reported as a problem?** A node configured for observation
  that has produced only failed handshakes for a week is either broken or gone, and we cannot tell
  which. Reporting it risks blaming a customer for our own transit; staying silent risks a customer
  believing they are covered.
- **Does the customer choose which nodes are observed, or is it every node in a load balancer?**
  Per node is the finer control and is also the billable unit (H6), which argues for making it
  explicit rather than implicit in the inventory.
