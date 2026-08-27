# Build order

The product is **ACME DNS-01 challenge handling, plus certificate observation.** A customer
publishes one `CNAME` into our challenge zone, once, and keeps their DNS wherever it already is.
We answer the one question a certificate authority asks about that name, and afterwards we look at
what they actually serve.

That is smaller than this repository once planned for, deliberately. What was removed and why is at
the bottom.

## The order

1. **`tenancy-and-permissions`** — the floor, and the only change that is substantially cheaper now
   than later, because it rewrites the shape of every table while the only data is ours.
   *Implemented: 81 of 81 tasks. The acceptance suite runs green on the branch, from a destroyed
   database through a fresh migration chain — which is what finally showed that four cross-account
   reads had been silently emptied by row-level security, one of them stopping certificate issuance
   altogether. See task 11.4.*
2. **`account-invitations`** — the entry path. While self-registration is closed, an invitation is
   the only way a person becomes a user.
3. **First customers, on challenge answering alone.** Worth pausing at rather than passing through:
   a renewal has about thirty days of slack, so a service interruption of a few hours costs nobody
   anything. Nothing else in this list is that forgiving.
4. **`health-checks`** — certificate observation. One TLS handshake per node per address family,
   hours apart, answering the question the challenge history opens and cannot close: validation
   succeeded, but was a certificate ever installed, and does it cover the right names.
5. **`metering-and-credits`** — needs something to meter, and the billable shapes only settle once
   observation exists. Deferred this far because credits are granted by hand while the customers
   are people we know.
6. **`public-edition`** — self-registration, payment, abuse limits, the marketing site.

`staff-plane` sits outside the numbering. Its suspension button and blocklists are needed **before
the first customer we do not know personally**, wherever that falls; its support-access machinery
can follow.

`authentik-and-self-signup` also sits outside the numbering, and belongs **between 1 and 2**. It
replaces Keycloak with authentik in the development stack — the provider `public-edition` U12 already
committed production to — and turns on self-registration so that path is built and inert rather than
unbuilt, which is the posture the triggers below already assume for it. It is placed here for one
reason: `account-invitations` is about turning an authenticated stranger into a member of an account,
and it is worth having the stranger arrive through the software that will actually produce them
before building the machinery that adopts them. It is deliberately **not** a prerequisite for
answering challenges for the first customers, who are people we know and enrol by hand.

## Triggers, not dates

- **`metering-and-credits`** waits until manual credit grants stop being reasonable — a count of
  customers, not a date.
- **`public-edition`'s registration switch** and payment integration wait until strangers arrive.
  Both are built and inert by then, so opening them is configuration.
- **`staff-plane`'s suspension path** must exist before the first stranger. Discovering it is
  missing during a live phishing incident is the wrong time to write it.

## What was removed, and what that bought

`dns-records` and `satellite-sync` are archived unimplemented. `health-checks` was cut down to its
certificate half.

The service was going to host customers' zones: real records, a replicated fleet to answer them,
health checks steering those answers. That is gone.

- **The step change in risk went with it.** The old order had a phase after which "a mistake takes a
  customer's domain off the internet". Nothing here does that. The worst failure now is that
  renewals stop until someone notices, with weeks of slack.
- **DNSSEC stopped being a problem in any form** — no delegation, no delegation signer, no
  `SERVFAIL` for a customer who followed the instructions.
- **The nameservers stopped needing to be diverse.** One domain, `acmealias.com`, rather than two at
  two registrars. The name says what the service does, which the old one did not.
- **Quorum, hysteresis, fail-open and failover answers went** — every one existed because check
  results changed what the internet was told.
- **`zone.active` went**, and with it the only charge that drained a balance while a customer did
  nothing.

Roughly 140 tasks of designed work removed, and the single largest source of irreversible risk for
one person running this alone.

## What this sequence is protecting against

One person can hold one change in their head at a time. The value of the order is that each change
is implemented while its reasoning is still recent — and that the change with the largest
irreversible surface, the data model, is decided first.
