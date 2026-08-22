## Why

### The two rows an operator almost always wants

This service answers ACME DNS-01 challenges for domains whose DNS we do not administer. The
**inventory** is the record of what we serve: a project owns **load balancers**, and a load
balancer owns **domains** — one row per domain name — and **nodes**, the machines behind that
load balancer with their IPv4 and IPv6 addresses.

The commonest onboarding a customer asks for is a wildcard certificate: `*.example.com` covering
every host they will ever serve, plus `example.com` itself, because a wildcard does *not* cover
its own base name. ACME is explicit about that, and so is `docs/load-balancer-setup.md`:

> `*.example.com` does **not** cover `example.com` itself. Add the apex as a second domain if it
> is served.

So the operator opens the load balancer, clicks **Add domain**, types `example.com`, submits,
clicks **Add domain** again, types `*.example.com`, submits again. Two dialogs, two round trips,
for what the operator experienced as one decision: *this customer gets a wildcard*.

This change gives the add-domain action an **include wildcard** option that creates both rows in
one action.

### Why this is not merely a convenience

Missing the second row is not a cosmetic slip; it produces a certificate that works and a site
that does not, or the reverse, and the symptom appears days later.

Consider a load balancer with two nodes, `lb-a` at `198.51.100.10` and `lb-b` at `198.51.100.11`.
The operator adds `example.com` alone and forgets the wildcard. The **DNS plan** — the set of
records the customer must publish in their own zone, which the console renders and the operator
emails — comes out as:

```
example.com.                       300 IN A     198.51.100.10
example.com.                       300 IN A     198.51.100.11
_acme-challenge.example.com.       300 IN CNAME k7m3q9x2r4tv8w1p.acmeservice.cloudpowered.services.
```

The customer publishes it. Traefik on the load balancer is configured with
`tls.domains: [{main: example.com, sans: ["*.example.com"]}]` and asks for a wildcard. The
challenge succeeds — the challenge name for `*.example.com` is `_acme-challenge.example.com`, the
same name the apex already delegated — and a valid wildcard certificate is issued. Then
`shop.example.com` does not resolve at all, because nothing in the plan ever told the customer to
publish an address record for it. The certificate is fine. The site is down. The plan that was
supposed to be the complete instruction was complete for the rows that existed.

Adding `*.example.com` as a second row produces the two lines that were missing:

```
*.example.com.                     300 IN A     198.51.100.10
*.example.com.                     300 IN A     198.51.100.11
```

and no second CNAME, because the CNAME is emitted once per **alias** and the two domains share
one.

### What an alias is, and why the pair is nearly free

An **alias** is a single label inside the zone we serve — `k7m3q9x2r4tv8w1p` above — that the
customer's `_acme-challenge` CNAME points at. Aliases are keyed by **challenge name**, and
`dnsname.ChallengeName` in `backend/internal/dnsname/challenge.go` strips a leading `*.` before
prefixing `_acme-challenge.`:

| domain | challenge name |
| --- | --- |
| `example.com` | `_acme-challenge.example.com` |
| `*.example.com` | `_acme-challenge.example.com` |
| `shop.example.com` | `_acme-challenge.shop.example.com` |

So the apex and its wildcard already share one alias row, one label, and one customer-side CNAME.
The `service-inventory` spec says so today under "Shared alias for wildcard and apex", and
`inventory.CreateDomain` implements it: it looks the alias up by challenge name and reuses it when
it exists, creating a second alias only when the challenge name is new.

That is the shape of what this change adds. The second row costs one `domains` row, its address
records, and nothing else — no second alias, no second label, no second thing for the customer to
publish.

## What Changes

- **The domain-create request gains an `includeWildcard` flag.** `POST
  /api/v1/loadbalancers/{id}/domains` accepts `{"name": "example.com", "includeWildcard": true}`
  and creates two domain rows, both pointing at the one alias for
  `_acme-challenge.example.com`. The handler already decodes this body with
  `DisallowUnknownFields`, so the field must be added there deliberately — an older client
  sending it today is refused, which is the correct behaviour until the field exists.
- **Both rows commit together or neither does**, inside the transaction the handler already
  opens, along with both audit entries.
- **The flag mirrors when the submitted name is already a wildcard.** `{"name":
  "*.example.com", "includeWildcard": true}` creates `*.example.com` and its apex
  `example.com` — the same pair, reached from the other side.
- **The derived name is validated before anything is written**, with the same
  `dnsname.ValidateDomain` rules as the submitted one, and a failure refuses the whole request.
- **The flag defaults to off.** Absent field, absent flag, `false` — one row, exactly today's
  behaviour.
- **The console's Add domain dialog gains a checkbox**, unticked, whose label follows what the
  operator has typed: "also add the wildcard `*.example.com`" for an apex, "also add the apex
  `example.com`" for a wildcard.
- **The CLI gains `--include-wildcard`** on `adminctl domain create`, so the API and the command
  line offer the same capability.
- **The documents say the pair is one action**: `docs/load-balancer-setup.md` (which currently
  tells the reader to "add the apex as a second domain"), `docs/console-guide.md` and `README.md`.

## What does not change

Everything about *what* a domain and an alias are. A domain is still one name on one load
balancer; an alias is still one label per challenge name owned by one load balancer; the wildcard
validation rules are unchanged; the "already covered by an existing wildcard" warning is
unchanged. This change adds only a way for one request to create two rows that an operator can
already create with two requests.

## Rejected: making the wildcard implicit

The obvious simplification is to have no option at all — every `example.com` in the inventory
automatically covers `*.example.com`, with no second row, no checkbox and no decision to make.
It deserves writing up properly because it looks strictly simpler and is in fact wrong, for
reasons that are not visible from the DNS-01 side.

**At the challenge level it is already implicit, so implicitness buys nothing.** A CA validating
`*.example.com` looks up `_acme-challenge.example.com`, which is the apex's own challenge name.
The challenge path — `POST /present` in the challenge API, the authorization check against the
alias, the TXT record served from our zone — never consults the domain list to decide whether a
wildcard is permitted; it checks that the *challenge name* has an alias owned by the load
balancer whose key is presenting. A customer with only `example.com` in the inventory can already
obtain a wildcard certificate today. Making wildcards implicit would formalise a permission that
is not enforced anywhere in the first place.

**What the explicit row actually carries is two things the implicit version cannot express.**

1. *The address records, which are genuinely not implied.* The DNS plan emits address records per
   domain name and one CNAME per alias — see `backend/internal/inventory/plan.go`, where the
   address loop iterates `domains` and the CNAME loop iterates aliases. `*.example.com` needs its
   own A and AAAA records; the apex's do not cover it, because DNS wildcards are records in the
   customer's zone, not an inheritance rule. An implicit wildcard leaves the plan with a
   choice between two bad options: recommend a wildcard A record to *everyone*, including the
   customers who wanted one hostname — which catches every unregistered name in their zone and
   can shadow a mail host or a subdomain served somewhere else entirely — or omit it, and hand
   the customer a wildcard certificate resolving nowhere.

2. *The customer's intent.* A row is a record that someone decided this. Implicit coverage has
   nowhere to put that decision, and nothing to show an auditor asking why we recommended a
   wildcard record for a zone whose owner never asked for one.

**And it makes per-domain reporting ambiguous.** The console's interaction history reports per
alias and the usage report per domain; the never-queried filter — the one that identifies a
customer who never published their `_acme-challenge` CNAME — is read as "this domain is not
working". With an implicit wildcard behind every apex there is no way to answer the obvious
follow-up: is the apex failing, is the wildcard failing, or is the wildcard simply not in use
because the customer never wanted one? Two rows answer it by existing.

The explicit option keeps the one-click ergonomics the implicit version was reaching for, and
keeps the row that carries the address records and the intent.

## Impact

- Affected specs: `service-inventory` (two modified requirements), `admin-console` (one modified
  requirement)
- Affected code: `backend/internal/inventory/domain.go`,
  `backend/internal/httpapi/admin_inventory.go`, `backend/internal/adminctl/inventory.go`,
  `backend/cmd/acmeservice/cli.go`,
  `frontend/src/app/features/domains/domain-editor.component.ts`,
  `frontend/src/app/features/models.ts`
- Affected documents: `README.md`, `docs/load-balancer-setup.md`, `docs/console-guide.md`
- **Not affected**: the challenge API, the DNS server, the alias model, the wildcard validation
  rules, and every domain that already exists. No migration.
