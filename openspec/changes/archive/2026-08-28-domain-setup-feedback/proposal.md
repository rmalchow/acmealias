# Telling a customer whether their delegation actually works

## What this changes, in one paragraph

The console can already check, live, whether a customer published the `CNAME` we
asked them for — and it does so only when somebody finds a button below the
domain table and presses it. Until then every domain reads "never validated",
which is indistinguishable from "broken". This makes the check run **per domain,
always, when the screen opens**, and turns its result into a plain statement per
record type with a way to find out what to do about it.

## Terms, before they are used

- The **delegation** is the one `CNAME` a customer publishes once:
  `_acme-challenge.their.domain` → `<alias>.our.zone`. It is the entire
  integration, and if it is wrong nothing else in the product works.
- A **CAA** record says which certificate authorities may issue for a name. It is
  optional. Its absence means *anyone may issue*, so absent is fine — which is
  the opposite of how the `CNAME` reads, and design D2 below is about that.
- **Checked** means we asked a resolver just now. Nothing here is stored: a check
  result is an observation, never state (the existing route is a `GET` for that
  reason).

## Why

### The check exists and nobody is shown its answer

`GET /loadbalancers/:id/verification` already resolves every record in the plan
through `internal/resolvercheck`, with a twenty-second cache so a burst of
lookups for one plan costs one query. `domainStatus` in the console already maps
those results onto individual domains.

The whole mechanism is built. What is missing is that it runs only on demand,
from a control that sits **below** the table whose rows it explains:

```
  Domains
    demo.example.com      [never validated]  "run verification below…"
    shop.example.com      [never validated]  "run verification below…"

  DNS plan
    [Run verification]    ← the button, down here
```

A customer who has just added a domain and mistyped the `CNAME` is told "never
validated", which reads like "nothing has happened yet" and is in fact "this will
never work". They find out weeks later, when a certificate does not renew.

### Every domain can need its own record, so the answer has to be per domain

One alias is issued per challenge name, and a wildcard and its apex share one —
`example.com` and `*.example.com` validate at the same `_acme-challenge.example.com`
and therefore need one `CNAME` between them. Two unrelated domains do not:
`demo.example.com` and `shop.example.com` each get their own alias and each need
their own record.

So "is the delegation set up" is a question with a different answer per row, and
a single verdict for the load balancer would be wrong for every domain but the
first one that failed.

### "Not configured" is the right sentence for a CNAME and the wrong one for a CAA

This is the part worth being careful about, and it is why the existing code
deliberately leaves CAA out of the per-domain status.

`resolvercheck` already models CAA with four outcomes, and only one of them is a
problem:

| Outcome | What it means | Is it bad? |
| --- | --- | --- |
| `correct` | a CAA set exists and authorises our issuer | no |
| `not-published` | no CAA record at all | **no** — anyone may issue |
| `blocking` | a CAA set exists and does NOT authorise our issuer | **yes** |
| `undetermined` | the lookup failed | unknown |

A chip reading "CAA not configured" for `not-published` would alarm a customer
about the single most common and entirely correct state. Worse, it would train
them to ignore the CAA chip — and `blocking` is the one condition here that
silently prevents issuance no matter how perfect the `CNAME` is.

## What Changes

- **The check runs per domain, automatically, when the load balancer screen
  opens.** No button to find.
- **Each domain shows one chip per record type**, saying what is true rather than
  a single verdict: whether the delegation is published, and what CAA says.
- **A help control opens an explanation** of what that state means and exactly
  what to publish to fix it, with the customer's own alias in the example.
- **Absent-resolver degrades to "not checked"** rather than an error on every
  page load.

## Capabilities

- `admin-console` — what a customer is told about their own delegation.
- `service-operations` — the verification read, now made per domain.

## Impact

- `internal/httpapi/admin_plan.go` gains a per-domain shape for its results.
- The console's load balancer screen runs the check on load and renders per-row
  state; `domain-status.ts` grows from three states to a per-record answer.
- No stored state, no migration, no new background work.

## What this does not do

- **It does not store results.** A check is an observation. Making it state
  invites a stale answer being shown as current, and `health-checks` is the
  change that owns observation over time.
- **It does not check on a schedule.** Nothing here runs unless somebody is
  looking at the screen.
