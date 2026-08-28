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
2. **`layered-configuration`** — configuration in files that overlay, with `${VAR:default}`
   placeholders. It comes before the account rework for one concrete reason: that change needs the
   project quota to be configuration rather than a constant (C13), and this is where a setting like
   that acquires somewhere to live. It changes no behaviour — every `ACME_*` variable keeps its
   name, its meaning and the highest precedence — which is what makes it safe to do first.
3. **`personal-and-team-accounts`** — what an account IS, and what makes one work. Every user gets
   an implicit personal account; a team account is created by paying and works only while funded.
   It also removes the internal edition, which is why it comes before everything rather than
   alongside it: "every user has their own account" and "everybody shares the one account created
   at migration time" cannot both be true, so there is no order in which this is optional.
4. **`transactional-email`** — one sender identity, one footer, one subject grammar across the
   identity provider's mail and the service's own. It sits immediately before invitations because
   that is the first thing that sends mail to a stranger, and because the part of it that fails
   fails SILENTLY: a message that does not authenticate as the domain in its own `From` is filed as
   spam rather than bounced, and a domain that starts in spam folders stays there. The DNS records
   are the long-lead item. There is no version of this that is cheaper later.
5. **`account-invitations`** — the entry path into a TEAM account. While self-registration is
   closed, an invitation is the only way a person joins one. It now depends on the change above,
   because team accounts do not exist without it.
6. **`domain-setup-feedback`** — telling a customer whether their delegation actually works. It
   sits immediately before the first customers rather than after them, because the very first thing
   any customer does is publish one `CNAME`, and this is the only thing that tells them whether they
   did it right. Without it the failure is silent for weeks and then arrives as an expired
   certificate. It depends on nothing above it — the check is already built and merely hidden — so
   its position here is about when it is NEEDED, not what it needs.
7. **`domain-activation`** — a domain is offline until we have seen its delegation, then stays
   online. This is the third of the three controls that make the abuse model manageable, alongside
   five projects on a personal account and per-domain billing on a team one; it is the one that
   makes the other two mean anything, because without it a domain nobody controls is
   indistinguishable from one somebody does. It pairs with the change above — that one tells a
   customer what is wrong, this one decides whether we do any work for them at all.
8. **First customers, on challenge answering alone.** Worth pausing at rather than passing through:
   a renewal has about thirty days of slack, so a service interruption of a few hours costs nobody
   anything. Nothing else in this list is that forgiving.
9. **`health-checks`** — certificate observation. One TLS handshake per node per address family,
   hours apart, answering the question the challenge history opens and cannot close: validation
   succeeded, but was a certificate ever installed, and does it cover the right names.
10. **`metering-and-credits`** — needs something to meter, and the billable shapes only settle once
   observation exists. Deferred this far because credits are granted by hand while the customers
   are people we know.
11. **`public-edition`** — what stops being avoidable when the next customer is somebody we have
    never spoken to: a self-service enrolment flow at the identity provider, a real payment provider
    behind the checkout that currently takes no money, protective limits that hold whether or not
    anybody has paid, the production identity provider and the rest of the staff/customer separation,
    and a marketing site carrying the commitments other changes describe as "published". It no longer
    has anything to do with editions — `personal-and-team-accounts` removed the second one — and its
    tenancy, balance, ledger and delegation-checking halves have all gone to the changes above it.

`staff-plane` sits outside the numbering. Its suspension button and blocklists are needed **before
the first customer we do not know personally**, wherever that falls; its support-access machinery
can follow.

`authentik-and-self-signup` is **done in the half that matters and deferred in the half that does
not**. The identity provider is swapped, both consoles have their own OIDC client, and the
acceptance suite signs in through authentik's flow executor — all of which had to land before
anything else, because a suite that cannot sign in cannot prove that the next change broke nothing.
Self-registration is a separate concern and waits for `public-edition`. The change originally read: It
replaces Keycloak with authentik in the development stack — the provider `public-edition` U12 already
committed production to — and turns on self-registration so that path is built and inert rather than
unbuilt, which is the posture the triggers below already assume for it. It is placed here for one
reason: `account-invitations` is about turning an authenticated stranger into a member of an account,
and it is worth having the stranger arrive through the software that will actually produce them
before building the machinery that adopts them. It is deliberately **not** a prerequisite for
answering challenges for the first customers, who are people we know and enrol by hand.

## Triggers, not dates

- **`metering-and-credits`** waits until manual credit grants stop being reasonable — a count of
  customers, not a date. Note that the BALANCE arrives earlier, with
  `personal-and-team-accounts`: that change adds the number, the checkout that raises it and what
  happens at zero. What waits is the ledger that draws it down.
- **`public-edition`'s enrolment flow** and payment integration wait until strangers arrive. Both are
  built and inert by then, so opening them is configuration. One correction to what this used to say:
  the switch that gates strangers is **not** `ACME_ALLOW_ACCOUNT_CREATION`. That is built, and after
  `personal-and-team-accounts` it gates buying a TEAM account — every person who can enrol at the
  identity provider gets a free personal account regardless. The front door is the provider's
  enrolment flow, which does not exist yet, so the service is closed by accident rather than by
  decision. `public-edition` U9 works that out.
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
