# Design — creating an apex and its wildcard in one action

## Context

`inventory.CreateDomain` in `backend/internal/inventory/domain.go` is the single place a domain
row is born. Every entry point goes through it: the management API handler
`admin.createDomain` in `backend/internal/httpapi/admin_inventory.go`, and the command line via
`adminctl.DomainCreate` in `backend/internal/adminctl/inventory.go`. Its current signature is:

```go
func CreateDomain(ctx context.Context, db *gorm.DB, loadBalancerID int64, name string, useDomainLabel bool) (*DomainCreation, error)
```

and it does, in order:

1. loads the load balancer (existence check),
2. `dnsname.ValidateDomain(name)` — normalization plus the wildcard rules,
3. refuses a name already in the inventory, naming the owning load balancer,
4. derives the challenge name with `dnsname.ChallengeName`, finds the alias for it, and refuses
   the creation if that alias belongs to a *different* load balancer,
5. collects coverage warnings against the load balancer's existing domains,
6. creates the alias if the challenge name has none, then the domain row.

Steps 1–5 are all *decisions*, made before anything is written; step 6 is the only write, and it
is already wrapped in a transaction. That separation is what makes this change small: a
two-row creation is steps 1–5 run twice and step 6 run once, over both rows.

Two facts from `backend/internal/dnsname/` are load-bearing throughout and are stated once here:

- **`ChallengeName` strips a leading `*.`.** `example.com` and `*.example.com` both yield
  `_acme-challenge.example.com`, so a pair shares one alias, one label, one customer-side CNAME.
- **`ValidateDomain` treats a wildcard as its own name with its own rules.** In particular a
  wildcard whose remainder is a public suffix is refused: `example.co.uk` is a perfectly good
  domain, and `*.co.uk` is not.

## Goals

- One operator action creates the apex and its wildcard, as two rows against one alias.
- The behaviour lives in the service, so the console, the CLI and any other API client get it
  without each re-implementing it.
- A partial result is impossible: two rows, or none.
- The pair remains a choice, never a default.

## Non-goals

- Deleting the pair in one action. Removing `example.com` while `*.example.com` remains is
  legitimate, and nothing has asked for a paired delete. `DeleteDomain` is untouched.
- Repairing existing inventories. No backfill offers the missing sibling for domains added
  before this change; an operator who wants one adds it as they do today.
- Any change to what a wildcard covers, to the challenge path, or to the certificate a CA issues.

## Decisions

### B1. The pairing is server-side, in one transaction, both-or-neither

A boolean field on the domain-create request — `includeWildcard` on the API, `--include-wildcard`
on the CLI — makes `inventory.CreateDomain` derive the sibling name, validate both, and write
both domain rows and the shared alias inside the transaction the caller already holds. Two audit
events for the two domains (plus the alias event when the alias is new) are emitted from that same
transaction, so the audit trail can never show one row of a pair.

Concretely, adding `example.com` with the flag set on a load balancer that has neither name
writes three rows — `aliases`(1) + `domains`(2) — and commits once.

**Rejected: the console issuing two `POST /domains` calls.** This is the version that needs no
backend change at all, and it fails in the specific way that costs an operator the most time. The
first call succeeds and creates `example.com`. The second is refused — the load balancer's alias
for `_acme-challenge.example.com` turns out to be owned by another load balancer, or the name was
created by a colleague thirty seconds ago, or the request simply timed out. The operator is now
looking at an error message beside a form that *already worked*, with no way to tell from the
screen which half landed, and the recovery is to close the dialog, re-read the domain list, and
work out what state the inventory is in. Every one of those failures is invisible to a client that
has already committed to a two-call plan.

It is also the wrong layer. `adminctl domain create` would need the same two-call sequence with
the same failure mode, and so would any script a customer's own automation runs against the
management API. Behaviour that must be identical at three entry points belongs at the point they
share, which is `inventory.CreateDomain`.

**Rejected: a distinct `POST /loadbalancers/{id}/domain-pairs` endpoint.** A second route for
what is the same creation with one more input, needing its own authorization, its own audit
wiring, its own console client method and its own tests. The flag on the existing route is the
same capability with none of that.

### B2. The control mirrors when the submitted name is already a wildcard

An operator who has typed `*.example.com` is exactly as likely to want the pair as one who typed
`example.com` — they are the same customer with the same requirement, approached from the other
end. So the flag means "create the sibling", and which sibling that is depends on what was
submitted:

| submitted | flag | rows created |
| --- | --- | --- |
| `example.com` | off | `example.com` |
| `example.com` | on | `example.com`, `*.example.com` |
| `*.example.com` | off | `*.example.com` |
| `*.example.com` | on | `*.example.com`, `example.com` |
| `shop.example.com` | on | `shop.example.com`, `*.shop.example.com` |

The console's checkbox label is computed from the typed name, so it reads "also add the wildcard
`*.example.com`" in the first case and "also add the apex `example.com`" in the second. The
operator is never shown an option whose effect they have to work out.

The bottom row is deliberate and is not a special case: `*.shop.example.com` is a valid name that
covers `a.shop.example.com`, shares the alias `_acme-challenge.shop.example.com` with its base,
and is occasionally exactly what a customer wants. The rule is "the other form of this name", not
"the registrable domain's wildcard".

**Rejected: making the option meaningless for a wildcard input** — greying the checkbox out, or
accepting the flag and ignoring it. Ignoring it silently produces a request that reports success
having done less than it was asked; disabling it leaves the operator who typed the wildcard first
with no way to reach the pair, for no reason they can see. The third variant, treating
`includeWildcard` on `*.example.com` as "add `*.example.com`" again, is a duplicate-name conflict
against a row the same request just created. All three are bug reports; the mirror is the only
reading that is not.

### B3. Both names are validated before anything is created, and one failure refuses everything

The derived name gets the full `dnsname.ValidateDomain` treatment plus every pre-write check
`CreateDomain` already performs — the duplicate-name check and the alias-ownership check —
before either row is written.

This is not defensive tidiness; the wildcard rules are genuinely not inherited from the apex. The
case that proves it:

```
POST /api/v1/loadbalancers/7/domains
{"name": "co.uk", "includeWildcard": true}
```

`co.uk` passes `ValidateDomain` as a plain name — it is syntactically a domain, and nothing
forbids a non-wildcard name at a public suffix. Its sibling `*.co.uk` does not pass: a wildcard
whose remainder is a public suffix is refused outright, because it would cover every registrable
domain beneath it. One name valid, the other not, from one form field.

(`example.co.uk` with the flag is fine, for the avoidance of doubt: its sibling is
`*.example.co.uk`, one label below the public suffix. The rule bites on the name *at* the suffix,
which an operator can type.)

Length is the second case, less exotic than it sounds: a name of 252 or 253 octets validates and
its wildcard form, two octets longer, does not.

Either way the whole request is refused, with the error naming the derived name and the rule it
broke, and `co.uk` is **not** created. The alternative is the failure mode B1 exists to prevent,
reintroduced inside the service: a request that half-succeeds and reports an error about a name
the operator did not type.

The same holds for the conflict checks. If `*.example.com` already exists on another load
balancer, adding `example.com` with the flag is refused entirely, naming that load balancer,
rather than creating the apex and then discovering the wildcard is unavailable.

### B4. The option defaults to off

The field is absent-means-false in the API, the CLI flag defaults to `false`, and the console's
checkbox renders unticked every time the dialog opens. There is no setting that changes the
default and no memory of the operator's last choice.

A ticked-by-default box would mean every domain added to this service silently acquires:

- **a recommendation to publish a wildcard address record**, which answers for *every*
  unregistered name in the customer's zone. That is a real change to their DNS, not a
  formality: it can shadow a mail host, catch a subdomain served by a different provider
  entirely, and turn every typo into a resolving name pointed at our load balancer. It is
  published by the customer, in their zone, on our written instruction.
- **a certificate whose compromise covers every hostname under the domain.** A stolen
  `*.example.com` key is a valid certificate for names the customer has not created yet.

Neither is unreasonable to *choose*. Both are unreasonable to acquire by not noticing a checkbox.
Some organisations forbid wildcard certificates outright as policy, which is precisely what CAA's
`issuewild` tag exists to express — and which this service already emits in its DNS plan when an
issuer is configured. Shipping a default that a customer's own published CAA policy would refuse
is not a default worth having.

The ergonomic argument for pre-ticking survives an unchecked box intact: the operator still gets
the pair in one action, one click earlier in the same dialog. Consent does not survive a checked
one — a box that was already ticked is not evidence anyone decided anything.

`README.md` argues that a wildcard is worth pushing for above two or three hostnames, and
`docs/load-balancer-setup.md` repeats it. That is an argument for making the option **prominent**
— clearly labelled, adjacent to the name field, with the coverage stated next to it — and it is
an argument the documents are the right place to make. It is not an argument for making the
choice on the customer's behalf.

## Risks

- **An operator ticks the box out of habit on a domain that should not have a wildcard.** The
  mitigation is that the label names the exact name being added, and the created rows are visible
  in the domain list immediately; the row can be deleted, and deleting one of a pair leaves the
  alias intact because a second domain still references it (`service-inventory`, "Shared alias
  survives partial removal").
- **The two-row creation grows the DNS plan the customer must publish**, by one address record
  per node address. This is the intended effect and the reason the row exists, but the plan the
  operator emails is longer than the one they emailed yesterday, and the change should say so in
  `docs/load-balancer-setup.md` rather than let it be discovered.
- **`CreateDomain`'s signature changes**, so every caller must be touched in the same commit.
  There are two, both in this repository.
