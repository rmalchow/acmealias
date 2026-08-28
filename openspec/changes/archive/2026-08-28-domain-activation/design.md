# Design

Decisions are numbered `A1`, `A2`, … and referred to by number from `tasks.md`
and from code comments.

## A1. Two timestamps, and everything else is derived

`domains` gains exactly two nullable columns:

```
  last_checked_at   when we last looked
  last_success_at   when we last SAW the delegation   (NULL = never)
```

There is no activation boolean and no consecutive-failure counter, because both
would be restatements of `last_success_at` that can disagree with it:

| Question | Answer |
| --- | --- |
| Is this domain active? | `last_success_at IS NOT NULL` |
| How often should it be checked? | `NULL` → minutes; set → daily (A2) |
| How long has it been broken? | `now - last_success_at` |
| Did we look recently? | `last_checked_at` |

A boolean beside a timestamp is a second source of truth for one fact, and the
first bug is the row where they disagree. A failure counter is worse: it resets
on any success, so it answers "how many times in a row" when the question a human
actually asks is "since when" — which the timestamp answers exactly.

The state is on the DOMAIN rather than on the alias, even though the delegation
is an alias-shaped fact. The reason is the wildcard rule: `example.com` and
`*.example.com` share one challenge name and therefore one alias and one `CNAME`.
Putting activation on the alias would be tidier and would make the two domains
share a fate — which is correct today and would silently become wrong the moment
anything gives a domain its own alias. Per domain, both rows are activated by the
same observation, and nothing depends on them being the same row.

## A2. Two cadences, because a fresh domain and a settled one are asking different questions

The cadence falls out of `last_success_at` rather than being tracked separately.

**Fresh — `last_success_at IS NULL`.** Checked every few minutes. Somebody who
has just been shown the record to publish is, at that moment, in the middle of
publishing it, and minutes is the difference between "it worked" and "did I get
it wrong?". It never gives up: a customer may add a domain on Friday and publish
the `CNAME` on Monday, and a poller that stops after an hour turns that into a
support ticket.

The cost is one lookup per unseen domain per interval — and unseen domains are,
by construction, the ones nobody is paying us for yet, so the abuse controls
bound it.

**Settled — `last_success_at` set.** Re-checked about daily. Nothing depends on
the answer (A3), so the cadence is chosen for how quickly we would like to *tell*
somebody their delegation broke, not for how quickly we must react. Daily against
thirty days of renewal slack is ample.

Both use the existing `resolvercheck`, including its cache. Neither runs on any
request path.

## A3. Activation is one-way. A failed re-check never takes a domain offline

This is the decision I expected to go the other way, and the abuse analysis
inverts it.

Consider what a customer gains by publishing the delegation, activating, and then
removing it. Their domain is active, so we will accept `/present` — and the
challenge value we publish is unreachable, because the certificate authority
follows the `CNAME` that no longer exists. **Validation fails. They have stopped
themselves.** There is nothing to defend against, because the delegation is not a
permission we check, it is the mechanism by which the whole thing functions.

What remains is small and already covered: they can call `/present` and have us
hold a TXT value in our zone, which is free storage of a few dozen bytes,
rate-limited by per-domain billing on a team account and by five projects on a
personal one.

Against that, deactivating has a real victim. A customer migrating DNS providers,
rebuilding a zone, or changing a TTL produces a delegation that blips. Deactivate
on that and their renewals stop — and with thirty days of slack they will not
find out for weeks, which is precisely the silent failure this product is
supposed to make survivable.

So: **getting in requires proof; staying in is not revocable by a lookup.**
Re-checks exist to inform, and the consecutive-failure counter exists so the
console can say "we have not seen this for three days" — not so anything can act
on it.

**Rejected: deactivating after N consecutive failures.** It buys nothing (the
paragraph above) and costs a class of outage that is invisible for weeks.

**Rejected: never re-checking at all.** Then a customer whose delegation broke is
told nothing until a certificate expires, and we hold information we are not
passing on.

## A4. The refusal is its own answer, distinguishable from every other

The challenge path now has two reasons to refuse before it does any work — the
account is not usable (`personal-and-team-accounts` C4) and the domain is not
activated. They are different problems with different fixes and must not share a
status:

| Refusal | Status | What fixes it |
| --- | --- | --- |
| Account unfunded | `402` | pay |
| Account suspended by staff | `403` | talk to us |
| Domain never activated | `409` | publish the delegation |
| Key not permitted | `403` | a grant |

`409 Conflict` for activation because the request is well-formed and the
credentials are right — the resource is simply not in a state that permits it.
The message names the delegation and what it should point at, since this lands in
the log of a machine whose operator is the person who has to fix it.

## A5. The worker is a sixth goroutine, and it looks like the other five

The service already runs zone-view refresh, query-count flushing, the sweeper,
reporting snapshots and a challenge sampler as goroutines started at boot. This
is the same shape and should be written to look like it — including reading its
interval from configuration rather than a constant, which the layered
configuration now makes natural.

It must be safe to run several instances of the service. Activation is idempotent
— a check that sees the delegation sets a state that is already set — so two
workers racing produce the same answer twice rather than a conflict.
