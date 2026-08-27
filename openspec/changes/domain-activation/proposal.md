# A domain is offline until we have seen its delegation

## What this changes, in one paragraph

Adding a domain today makes it immediately answerable: the service will publish
challenge values for it whether or not the customer ever published the `CNAME`
that makes those values reachable. This makes a new domain **offline** until a
check has seen its delegation, checks fresh domains repeatedly until it does, and
re-checks settled ones occasionally. It is admission control, not user feedback —
`domain-setup-feedback` owns telling a customer what is wrong, and this owns
whether we do any work for them at all.

## Terms, before they are used

- The **delegation** is the customer's one `CNAME`:
  `_acme-challenge.their.domain` → `<alias>.our.zone`.
- **Offline** means the service will not publish challenge values for that
  domain. The domain exists, holds its alias and is visible; it simply does not
  work yet.
- **Activation** is the first time a check sees the delegation. It happens once.

## Why

### Adding a domain currently costs the adder nothing

A domain can be added for a name the customer has no control over. Nothing checks,
because nothing needs to: a challenge published for a domain whose delegation
does not exist is simply never reached, so the *product* is unharmed.

What is harmed is us. Every added domain is a row, an alias in our zone, and a
callable `/present`. The three controls that make the abuse model manageable —
five projects on a personal account, per-domain billing on a team account, and
this — are cheap individually and only work together. This is the one that makes
the other two meaningful, because without it a domain nobody controls is
indistinguishable from one somebody does.

### It cannot be checked when the domain is added, and the reason is the alias

The obvious design is to refuse a domain whose delegation is absent. It is
impossible:

```
  customer adds domain ──▶ we assign the alias ──▶ customer publishes the CNAME
         │                                                   │
         └───────── the target does not exist until here ────┘
```

The `CNAME` points at an alias we have not issued yet. So the check cannot gate
the write; it has to gate the *work*, afterwards, which is what "offline until
seen" means.

### The check must not run on the challenge path

`/present` is called by lego in the middle of an issuance. It is the most
latency-sensitive and least interruptible route in the product, and a live
resolver call there would make certificate issuance depend on a third party's
availability — a much worse failure than the abuse it prevents.

So the check runs out of band and its result is stored. That is a deliberate
departure from `domain-setup-feedback` D1, which says a check is an observation
and never state. Both are right, because they are different uses of one lookup:

| | Console feedback | Admission control |
| --- | --- | --- |
| Freshness | live — "is it right *now*" | may be minutes old |
| Stored | no | **yes**, or the hot path pays |
| Cost of being stale | it lies to a customer | it briefly allows or refuses |

## What Changes

- A domain gains **two timestamps** — when we last looked, and when we last saw its delegation. Everything else derives from them, including whether it is active.
- A **fresh domain is checked repeatedly**, frequently at first and less often
  as it stays unseen, until its delegation appears.
- **Activation happens once** and is not reversed by a later failed check
  (design A3 — this is the decision that surprised me most).
- **Settled domains are re-checked occasionally**, to inform rather than to gate.
- The **challenge API refuses** to publish for a domain that has never been
  activated, distinguishably from every other refusal.

## Capabilities

- `service-inventory` — a domain's activation state.
- `acme-challenge-api` — refusing an unactivated domain.
- `admin-console` — showing that a domain is waiting.

## Impact

- One new background worker, alongside the five that already run.
- A migration adding activation state to `domains`.
- `internal/httpapi/challenge.go` gains one more check on a path that now has
  several — account usability, and this.

## What this does not do

- **It does not deactivate.** See design A3.
- **It does not replace the console's live check.** A customer looking at a
  domain still gets a fresh answer from `domain-setup-feedback`; this is what the
  service acts on.
- **It does not check anything but the delegation.** CAA is a permission, not a
  prerequisite, and a domain with no CAA record is perfectly serviceable.
