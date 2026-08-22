## Context

`acmeservice` is a new, empty repository. Everything described here is greenfield, so this
document is mostly a record of choices made before any code exists, and of the alternatives
that were rejected.

The proposal explains the motivation; the one-paragraph recap is: our Traefik load balancers
need to publish **DNS-01 challenge records** (a TXT record at `_acme-challenge.<domain>` proving
to Let's Encrypt that we control `<domain>`) for domains whose DNS we do not administer. Rather
than collecting DNS credentials for every customer, each customer publishes a single permanent
**CNAME** — a DNS alias record meaning "look over there instead" — pointing
`_acme-challenge.shop.example.com` at `shop-example-com.acmeservice.cloudpowered.services`. We own
that target zone. Let's Encrypt follows the CNAME and asks us.

So the service must (a) accept challenge values from Traefik, (b) answer DNS queries for them,
(c) know which load balancer is allowed to write which names, and (d) let a human manage
all of that and find out what went wrong.

### Constraints we are designing inside

- **We must speak real DNS on port 53.** There is no way around this: Let's Encrypt's
  validation servers are ordinary DNS clients on the public internet. This is the single most
  awkward requirement in the whole design, because most of our deployment tooling assumes HTTP
  services behind a proxy, and UDP load balancing is a different story from HTTP routing.
- **Availability during issuance matters, but only in bursts.** The service is idle almost
  always, then must be correct for a ~2 minute window per certificate renewal. A failure is not
  a user-visible outage; it is a failed renewal that retries. That tolerance buys us a lot — it
  is why a single instance is acceptable for the first version, and why we can afford an ORM on
  the write path.
- **Its own TLS certificates cannot come from itself.** `acme-api.cloudpowered.services` (the
  challenge API) and `acme-console.cloudpowered.services` both need HTTPS, and issuing those
  through this service would be circular. Since `cloudpowered.services` is a zone we already
  administer, those certificates are issued the ordinary way.
- **Blast radius.** Anyone who can call `/present` for a name, or who controls the zone, can
  obtain certificates for that name. Authorization must be positive (an explicit inventory of
  names), never merely authenticated.
- **The people managing this are not all the same people.** Permissions must be derivable from
  IdP group membership, and every privileged action must be attributable to a named human.

## Goals / Non-Goals

**Goals:**

- A single Go binary that serves the `httpreq` HTTP API, the authoritative DNS zone, the admin
  REST API and the compiled Angular console, with no external moving parts other than
  PostgreSQL and the IdP.
- A published challenge value must be answerable in DNS by the time the `/present` call returns
  — no propagation window of our own making.
- Writing a challenge for a name requires that name to be present in the inventory, attached to
  the load balancer whose key is presenting it. A stolen key cannot mint certificates for anything
  else, and — if the key is address-scoped — cannot be used away from that load balancer's nodes.
- Wildcard certificates work, including the case where a wildcard and its apex are validated at
  the same challenge name simultaneously.
- The DNS records a customer must publish are generated, not hand-written, and the service can
  tell you whether they were published correctly.
- Survive restart and redeploy in the middle of a certificate issuance.
- An acceptance environment where the whole chain — real Traefik, real ACME protocol, real DNS
  resolution, real certificate verification — runs against fakes only where the real thing is
  unusable in a test. Designed here, in D14; built by the change `add-acceptance-environment`.

**Non-Goals:**

- A general-purpose authoritative nameserver. It serves exactly one zone, answers TXT, SOA and
  NS, and nothing else. No zone transfers, no dynamic update protocol, no subzone delegation.
- DNSSEC signing of our zone. Worth doing eventually; not required for ACME validation. Until it
  is signed, the delegation must carry **no DS record** — see Migration Plan step 2 for why that
  is a load-bearing detail rather than an omission.
- Writing into customers' zones. We generate the list of records and verify them; a human at
  the other end creates them.
- Supporting ACME clients other than Traefik/lego, or challenge types other than DNS-01.
- Running more than one instance. The design avoids decisions that would make it hard later
  (D6), but does not implement it.
- Customer-facing self-service. The console is for us.

## Decisions

### D1. Go and Angular, with a deliberately short dependency list

Go 1.23+, module path `gitlab-mcww-admin.versioncontrol.io/tools/acmeservice`, built as a
binary in a minimal container image. Angular (current LTS) for the console, in a workspace under
`frontend/` with a single project named `admin`. The repository is laid out as three top-level
directories: `backend/`, `frontend/` and `development/`.

Go because `github.com/miekg/dns` is the only mature choice for writing an authoritative DNS
server in a high-level language, and because lego — the ACME library inside Traefik whose API we
are implementing — is itself Go, so its exact request and response shapes are readable source
rather than guesswork.

| Concern | Choice | Why not the alternative |
| --- | --- | --- |
| DNS server | `github.com/miekg/dns` | No real competitor in Go. |
| HTTP server | `github.com/labstack/echo/v4` | See D9. |
| ORM / data access | `gorm.io/gorm` + `gorm.io/driver/postgres` | See D8. |
| Migrations | `github.com/golang-migrate/migrate/v4` with `source/iofs` | See D8. |
| OIDC | `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2` | Discovery, JWKS rotation and ID-token verification are exactly the things not to hand-roll. |
| Logging | stdlib `log/slog`, JSON handler | Structured logging is in the standard library now; no reason for zap or zerolog. |
| Metrics | `github.com/prometheus/client_golang` | Matches existing scrapers. |
| Public suffix data | `golang.org/x/net/publicsuffix` | Needed to refuse `*.co.uk`-shaped wildcards (D4). |
| Config | env vars parsed by hand | A config library — and a file overlay with precedence rules — is more code than the settings we have. Compose and the deployment already know how to set environment variables; the dev stack keeps its values in `development/.env`. |
| Integration test infra | a real PostgreSQL named by `TEST_DATABASE_URL`, skipped when unset | Rejected `testcontainers-go`: it pulls a large dependency tree and manages container lifecycles from inside the test process, when the dev compose stack already has a PostgreSQL running. Tests that need a database `t.Skip` when the variable is unset — which is the case in CI, deliberately (D13). |
| Acceptance test infra | Docker Compose, driven from a Go test | See D14. Built by `add-acceptance-environment`. |

Two dependencies from the first draft of this design are **gone**: `pressly/goose` (replaced by
golang-migrate, D8) and `golang.org/x/crypto/bcrypt`. Bcrypt disappears because there are no
longer any human passwords to hash — the console authenticates against the IdP — and because API
key secrets are high-entropy random values for which a slow KDF buys nothing (D3). `pgx` does not
disappear, it recedes: GORM's PostgreSQL driver uses `pgx/v5` underneath, so it remains the wire
implementation without being the API we write against.

Package layout:

```
backend/                        the Go module
  cmd/acmeservice/main.go       wiring, signal handling, subcommand dispatch
  internal/config/              parsing and validation of settings
  internal/model/               GORM models — the shared vocabulary of the whole service
  internal/store/               repositories, transactions, migrations (embedded SQL)
  internal/dnsname/             normalization, wildcard rules, challenge-name derivation
  internal/inventory/           projects, load balancers, domains, aliases, the DNS plan
  internal/apikey/              generation, hashing, verification, CIDR scoping
  internal/authz/               roles, group mapping, permission checks
  internal/oidcauth/            OIDC flow, sessions
  internal/httpapi/             echo servers: challenge API, admin API, internal listener
  internal/webui/               embed.FS of the built console + SPA handler
  internal/webui/dist/          ng build output (gitignored) — see below
  internal/zoneview/            the in-memory view of aliases and their active values
  internal/dnsserver/           miekg/dns handler over that view
  internal/resolvercheck/       live verification of customer-side DNS records
  internal/adminctl/            CLI subcommands (bootstrap and emergencies)
  internal/store/migrations/    numbered .up.sql/.down.sql, embedded
  acceptance/                   the acceptance test, behind an `acceptance` build tag
                                (add-acceptance-environment)

frontend/                       the Angular workspace; project `admin`

development/                    everything needed to run this locally
  compose.yaml                  postgres, identity provider, dev traefik overlay
  scripts/                      up, down, seed, reset, realm import
  traefik/                      dev-only file-provider routing rules
  realm/                        identity provider realm export: client, group mapper, users
  acceptance/                   the end-to-end environment (D14) and its fixtures
                                (add-acceptance-environment)
```

**One consequence of moving the module out of the repository root**: `//go:embed` cannot
reference paths outside its own package directory, so the console cannot be embedded from
`frontend/dist/`. Angular's `outputPath` is therefore set to `../backend/internal/webui/dist`,
which is where `ng build admin` writes and where the embed directive reads. That directory is
gitignored, with a checked-in placeholder so `go build` succeeds on a fresh clone before the
frontend has ever been built.

### D2. The `httpreq` contract, taken literally

lego's `httpreq` provider is configured with an endpoint URL and appends the path itself. With
`HTTPREQ_ENDPOINT=https://acme-api.cloudpowered.services/` it POSTs to `…/present` and
`…/cleanup`. In its default mode the body is:

```json
{"fqdn": "shop-example-com.acmeservice.cloudpowered.services.", "value": "gfj9XqRg85nMk2p...Q"}
```

Note the **trailing dot** on `fqdn` — DNS's way of writing a fully-qualified name — and note whose
name this is: **ours**, the alias, not the customer's `_acme-challenge.shop.example.com`.

**This corrects a claim an earlier draft of this document got backwards**, and the correction is
recorded rather than quietly applied because the wrong version is the intuitive one. That draft
said lego posts the customer's FQDN and "has no idea an alias exists". It does know. In lego,
`providers/dns/httpreq/httpreq.go` sends `info.EffectiveFQDN`, and in
`challenge/dns01/dns_challenge.go` `EffectiveFQDN = lookupCNAME(fqdn)` whenever CNAME support is
on — the default, and the thing this design depends on. lego performs the CNAME resolution itself
and hands us the target. Verified in lego v5.2.2 and confirmed against a live issuance, whose
actual payload was:

```json
{"fqdn":"demo-example-com.dev.acmeservice.internal.","value":"..."}
```

The consequence for the endpoint is that it must resolve an alias two ways, and **both are
equally normal**:

| `fqdn` received | Resolved |
| --- | --- |
| a name within the served zone (`<label>.<zone>`) | by **alias label** |
| any other name | by **challenge name**, as before |

Authorization, normalization and responses are identical on both paths — the alias must belong to
the load balancer the presented key is scoped to — so accepting two forms widens no privilege.

**The reason for two forms is client heterogeneity, not failure.** We do not control which ACME
client a load balancer runs, and both conventions are legitimate:

- lego with CNAME support on — the default — sends the resolved target.
- lego with `LEGO_DISABLE_CNAME_SUPPORT=true` sends `_acme-challenge.<domain>` every single time,
  for a perfectly healthy domain.
- Other ACME clients differ: some resolve the alias themselves, some deliberately leave the
  indirection to the DNS provider, which is exactly what a provider API is for.
- lego *also* falls back to the un-aliased form when the CNAME does not resolve — but that is one
  cause among several, not the meaning of the form.

That last point is why the design deliberately draws **no inference** from which form arrives. An
earlier revision of this section proposed logging the un-aliased form at WARN as a
"delegation not published" signal. That was rejected: for any load balancer whose client has CNAME support
off, the warning would fire on every renewal of every correctly configured domain, and a warning
that is usually noise trains people to ignore the log — including on the occasion it is real.

What we do instead costs nothing and asserts nothing: the submitted form is recorded as a
structured log field and as a two-valued metric label, which answers "which convention does this
load balancer's client use" — a genuinely useful question when debugging one load balancer — without claiming to
know why. The authoritative answer to "does the customer's CNAME actually exist" is the DNS-plan
verification of D5, which resolves the record directly and exists precisely for that purpose.

lego also has `HTTPREQ_MODE=RAW`, which sends `{"domain", "token", "keyAuth"}` and expects the
server to compute the TXT value itself. **We implement only the default mode.** RAW would require
us to reimplement the key-authorization digest, adding a second code path and a cryptographic
detail to get wrong, for no benefit. A RAW body fails validation and returns `400` with a message
naming the expected fields, so the misconfiguration is diagnosable rather than mysterious.

Any non-`200` response makes lego fail the challenge and retry later, which is the behaviour we
want for storage failures — hence the requirement that a failed database write returns `500`
rather than optimistically returning `200`.

Two Traefik-side subtleties worth recording, because both produce symptoms that look like
nothing at all:

- Before handing off to the CA, lego performs its own **propagation check** — and it does not do
  it the way you would guess. It resolves the zone's **authoritative nameservers and queries them
  directly on port 53**, ignoring `dnsChallenge.resolvers` for this step; that setting governs
  other lookups, not this one. Two hard consequences follow, both observed rather than assumed:
  **our server must return a non-empty NS RRset at the zone apex**, or every issuance stalls with
  "could not determine authoritative nameservers" — which is why the apex NS records of D7 are
  load-bearing rather than housekeeping — and a deployment serving DNS on a **non-standard port**
  cannot satisfy the check at all, since lego will go to 53 regardless, so such a deployment needs
  glue pointing at something that does listen there. It also means the check only works once the
  NS delegation for `acmeservice.cloudpowered.services` is live; before delegation, every issuance
  hangs here rather than failing fast.
- lego's CNAME following has moved twice, and the whole design depends on it being on. Two
  environment variables will turn up in search results and both need a sentence:
  `LEGO_EXPERIMENTAL_CNAME_SUPPORT=true` was the opt-in on *older* builds and is historical — it is
  neither needed nor read here, and setting it does nothing. `LEGO_DISABLE_CNAME_SUPPORT=true`
  turns the behaviour off and **must never be set**; doing so breaks every aliased domain at once,
  because lego would then look for the challenge record in the customer's own zone, where it does
  not exist.

**The requirement is on lego, not on Traefik**, and stating it the other way round is a mistake we
already made once. What we need is behavioural: *the lego version bundled in the Traefik build must
follow CNAMEs by default* — that is, it must be a lego that has `LEGO_DISABLE_CNAME_SUPPORT` (an
opt-*out*, implying the behaviour is on) rather than one that only has the older
`LEGO_EXPERIMENTAL_CNAME_SUPPORT` opt-in. That has been true since lego v4.9.0.

As guidance, **Traefik 2.11 or newer** satisfies it. But the mapping from Traefik version to lego
version is **not monotonic**, which is the whole reason to state the requirement in terms of lego:

```
traefik v2.11  →  github.com/go-acme/lego/v5 v5.2.2
traefik v3.0   →  github.com/go-acme/lego/v4 v4.17.4     ← older lego than 2.11 ships
traefik v3.6   →  github.com/go-acme/lego/v5 v5.3.1
```

Traefik 2.11 bundles a *newer* ACME client than Traefik 3.0 does. So "upgrade Traefik" does not
reliably mean "newer ACME client", and an operator reasoning from the Traefik version alone will
sooner or later reason wrongly. Check what a given tag actually bundles before pinning anything:

```
curl -s https://raw.githubusercontent.com/traefik/traefik/<tag>/go.mod | grep go-acme/lego
```

That check is one line and it is cheap. An earlier draft of this document declared a floor of
Traefik 3.0 without running it, on the assumption that a higher major version meant a newer
embedded client. It does not, the floor was wrong by two minor versions in the wrong direction, and
the correction is recorded here rather than silently applied so the next person pinning a version
runs the check first.

### D3. API keys, transported over HTTP Basic, optionally pinned to source addresses

**Format and storage.** A key is a load balancer: a public identifier and a secret.

```
identifier  k_7f3a91c4                      stored in clear, safe to log and display
secret      32 random bytes, base64url       shown once, never stored
stored      sha256(secret)                   plus a 6-character display prefix
```

Verification is a lookup by identifier followed by a constant-time comparison of the SHA-256
digest. **Not bcrypt**, which the earlier draft used for human passwords: bcrypt's cost exists to
defend low-entropy secrets against offline guessing, and a 256-bit random secret has nothing to
guess. A fast hash also removes the timing-oracle problem the earlier design had to work around —
though we still fail closed on unknown identifiers, comparing against a fixed dummy digest so the
two paths cost the same and an attacker cannot enumerate valid identifiers by response time.

**Why Basic auth is the transport.** lego's `httpreq` provider exposes exactly two credential
knobs, `HTTPREQ_USERNAME` and `HTTPREQ_PASSWORD`, and sends them as an `Authorization: Basic`
header. It offers no way to set an arbitrary header. So the key rides as
`Authorization: Basic base64(identifier:secret)`. This is a transport choice, not a second
authentication scheme — there are no accounts with passwords behind it. For our own tooling and
for `curl`, the same key is also accepted as `Authorization: Bearer <identifier>.<secret>` and as
`X-API-Key: <identifier>.<secret>`; all three paths converge on one verification function.

*Rejected: putting the key in the endpoint path* (`HTTPREQ_ENDPOINT=https://…/k/<secret>/`), which
also works around the header limitation. Rejected because secrets in URLs end up in access logs,
proxy logs and metrics labels, and because it makes rotation a Traefik static-config change
rather than an environment variable change.

**Scope.** A key belongs to exactly one load balancer. The names it may write are precisely the
challenge names derived from that load balancer's domains — a positive allowlist recomputed from the
inventory on every request, not a copy stored on the key. Revoking a domain therefore revokes it
everywhere immediately.

**Source-address restriction.** A key may carry zero or more CIDR blocks. Zero means
unrestricted. One or more means the request's client address must fall inside one of them, and a
request from anywhere else is rejected — with `401` and a log line naming the reason, never a
distinct status code, because a distinct code would tell an attacker holding a stolen key that
the key itself is valid and only the address is wrong.

The subtlety is **which address**. The service sits behind our own Traefik, so the TCP peer is
always the proxy; the real client address arrives in `X-Forwarded-For`, which is trivially
forgeable by anyone who can reach the service directly. Echo's `IPExtractor` handles this
correctly when told which peers to trust:

```go
e.IPExtractor = echo.ExtractIPFromXFFHeader(
    echo.TrustLoopback(false), echo.TrustLinkLocal(false), echo.TrustPrivateNet(false),
    echo.TrustIPRange(cfg.TrustedProxyCIDRs...),
)
```

and we **fail closed** around it: if any key in the database has a CIDR restriction while
`ACME_TRUSTED_PROXY_CIDRS` is empty, the service refuses to start, naming the problem. An
address restriction that silently degrades to "trust whatever the caller claims" is worse than no
restriction at all, because it reads as protection on the screen.

**Lifecycle.** Keys carry an optional `expires_at`, a `last_used_at` updated at most once a
minute (so verification does not become a write on every request), a `disabled_at`, and a note.
Rotation is "create a second key for the load balancer, deploy it, delete the first" — the model supports
several live keys per load balancer precisely so rotation needs no downtime.

### D4. The inventory: projects → load balancers → domains, and aliases keyed by challenge name

The hierarchy is a strict tree. A **project** is a customer engagement or one of our own
products. An **load balancer** is two Traefik nodes fronting one environment for that project, and it
is the unit that holds credentials and node addresses. A **domain** is one name that load balancer answers
for, either a concrete host (`shop.example.com`), a zone apex (`example.com`), or a wildcard
(`*.example.com`).

```sql
create table projects (
  id          bigserial primary key,
  slug        text not null unique,          -- 'acme-shop'
  name        text not null,
  created_at  timestamptz not null default now()
);

create table load_balancers (
  id          bigserial primary key,
  project_id  bigint not null references projects(id) on delete restrict,
  slug        text not null,                 -- 'eu-prod', unique within the project
  name        text not null,
  created_at  timestamptz not null default now(),
  unique (project_id, slug)
);

create table lb_nodes (
  id          bigserial primary key,
  load_balancer_id  bigint not null references load_balancers(id) on delete cascade,
  label       text not null,                 -- 'a' / 'b'
  ipv4        inet,
  ipv6        inet,
  unique (load_balancer_id, label)
);
```

`lb_nodes` is a child table rather than two columns on `load_balancers` because "pair" is the common
case, not a constraint we want in the schema — a three-node cluster should not require a
migration. The name stays `load_balancers` because that is what everyone calls them.

**Domains and aliases.** The naive schema puts the challenge name and the alias label on the
domain row. Wildcards break that, and the way they break it is the most important detail in this
document.

For a certificate covering `*.example.com`, the ACME challenge name is `_acme-challenge.example.com`
— the `*.` label is **stripped**, not included. That is identical to the challenge name for the
apex `example.com`. So two different domain rows produce one challenge name, and therefore must
share one alias and one customer-side CNAME. A `unique` constraint on a per-domain challenge name
would make the load balancer `example.com` + `*.example.com` — the single most common wildcard setup —
impossible to represent.

Aliases are therefore their own entity, keyed by challenge name, and domains point at them:

```sql
create table aliases (
  id              bigserial primary key,
  load_balancer_id      bigint not null references load_balancers(id) on delete restrict,
  challenge_fqdn  text not null unique,      -- '_acme-challenge.example.com', normalized
  label           text not null unique,      -- 'example-com'
  created_at      timestamptz not null default now()
);

create table domains (
  id          bigserial primary key,
  load_balancer_id  bigint not null references load_balancers(id) on delete restrict,
  alias_id    bigint not null references aliases(id) on delete restrict,
  name        text not null unique,          -- 'example.com' | '*.example.com' | 'shop.example.com'
  created_at  timestamptz not null default now()
);
```

Adding a domain derives `base = strip_leading_wildcard(name)`, then
`challenge_fqdn = "_acme-challenge." + base`, then finds or creates the alias for that name. The
operator is offered a generated label (`example-com` from `example.com`, disambiguated with a
suffix on collision) and may override it, because a human-readable label is what makes a zone
file or a log line legible months later. The earlier draft rejected *mechanical* derivation
outright; the refinement is that a suggestion the operator can accept with one click gets the
readability without the typing.

**An alias belongs to exactly one load balancer.** Adding `*.example.com` to `eu-staging` while
`example.com` already lives on `eu-prod` is **refused**, with an error naming the conflicting lb.
It would in fact work at the DNS layer — both load balancers' values would coexist as multiple TXT records,
which is the same mechanism wildcards already need — but it would make "which key may write this
name" ambiguous and give two load balancers a legitimate reason to publish to a name only one of them owns.
One owner per challenge name keeps the authorization rule to a single join. *Rejected: allowing
shared aliases with a union authorization rule*, for that reason.

**Wildcard validation**, all enforced at creation:

| Input | Verdict |
| --- | --- |
| `*.example.com` | accepted |
| `example.com` | accepted; shares an alias with `*.example.com` if both exist |
| `*.*.example.com` | rejected — ACME permits one wildcard label, leftmost only |
| `foo.*.example.com` | rejected — wildcard must be leftmost |
| `*` | rejected |
| `*.com`, `*.co.uk` | rejected — the wildcard sits at a public suffix |
| `shop.example.com` when `*.example.com` exists | accepted, with a warning that the wildcard already covers it |

The public-suffix rule uses `golang.org/x/net/publicsuffix`: a wildcard whose base is itself a
public suffix would ask a CA for a certificate covering an entire TLD, which no CA will issue and
which is a sign of a typo. The last row is a warning rather than an error because there are real
reasons to hold a specific certificate alongside a wildcard, but it is nearly always a mistake.

Two facts about wildcards the console states explicitly next to the input field, because both
surprise people: `*.example.com` does **not** cover `example.com` itself, and it does **not**
cover `a.b.example.com` — a wildcard matches exactly one label.

**Normalization** is the boring part that causes real bugs, so it lives in one package,
`internal/dnsname`: lowercase, strip the trailing dot, reject empty and over-length names, and
validate each label. Every entry point — the challenge API, the admin API, the CLI — goes through
the same functions, and DNS queries match `label` case-insensitively.

### D5. The DNS plan, generated per load balancer and verified live

For a given load balancer, everything the outside world must publish is derivable, and the console and
CLI render it as a table and as a zone-file snippet:

```
; DNS records for project acme-shop / load balancer eu-prod
; nodes: 203.0.113.10 / 2001:db8:1::10, 203.0.113.11 / 2001:db8:1::11

shop.example.com.                  300  IN  A      203.0.113.10
shop.example.com.                  300  IN  A      203.0.113.11
shop.example.com.                  300  IN  AAAA   2001:db8:1::10
shop.example.com.                  300  IN  AAAA   2001:db8:1::11
example.com.                       300  IN  A      203.0.113.10
example.com.                       300  IN  A      203.0.113.11
example.com.                       300  IN  AAAA   2001:db8:1::10
example.com.                       300  IN  AAAA   2001:db8:1::11
*.example.com.                     300  IN  A      203.0.113.10
*.example.com.                     300  IN  A      203.0.113.11
*.example.com.                     300  IN  AAAA   2001:db8:1::10
*.example.com.                     300  IN  AAAA   2001:db8:1::11
_acme-challenge.shop.example.com.  300  IN  CNAME  shop-example-com.acmeservice.cloudpowered.services.
_acme-challenge.example.com.       300  IN  CNAME  example-com.acmeservice.cloudpowered.services.
example.com.                       300  IN  CAA    0 issue "letsencrypt.org"
example.com.                       300  IN  CAA    0 issuewild "letsencrypt.org"
```

**The plan is dual-stack.** `lb_nodes` carries an `ipv4` and an `ipv6` column, either of which may
be null, and the generator emits an A record for every node with an address and an AAAA record for
every node with a v6 address — so a load balancer whose nodes are v4-only produces exactly the v4 table
above, and a dual-stack load balancer produces both. This matters more than it looks: a domain given only A
records on a dual-stack load balancer is reachable over v6 at whatever the customer's old AAAA record still
points at, which is a failure mode that looks like "the site works for most people".

Note the CNAME count: three domains, two challenge names, because the wildcard and the apex share
one. The CNAME set is unaffected by dual stack — a challenge name is a name, not an address. That collapse is the whole reason wildcards are worth the trouble at onboarding time, and it
is visible right there in the generated list.

**CAA records are generated too, and they are the one part of the plan that is optional.** A CAA
record ("Certification Authority Authorization") names which certificate authorities may issue for
a domain; a domain with *no* CAA record permits every CA, which is the default state of most of the
internet and works fine. Publishing one bounds the blast radius of this whole system: even if our
authorization is defeated — a stolen key, a mistaken inventory row, a compromised console session —
an attacker still cannot obtain a certificate from a CA the customer has not authorized. So we
generate the records and ask; we never require them.

Four details decide whether the generated rows are useful or actively harmful:

- **The issuer is configuration, not a constant.** `ACME_CAA_ISSUER` holds one or more CA
  identifiers (e.g. `letsencrypt.org`), and if it is unset the plan emits **no** CAA rows at all
  rather than guessing. A plan that confidently tells a customer to authorize a CA we do not
  actually use would lock us out of issuance, which is a worse outcome than saying nothing.
- **One set per registrable domain, not per host.** CAA is inherited down the tree: a resolver
  looking for authorization walks up from the certificate's name and stops at the nearest ancestor
  that has a CAA record set. So the record belongs at `example.com`, and emitting
  `shop.example.com CAA` beside it is noise that also *shadows* the parent for that one host —
  someone later adding a CA at `example.com` would silently not affect `shop.example.com`. The
  registrable domain is found with the same `publicsuffix` logic the wildcard validation already
  uses (D4), and the plan emits one CAA set per distinct registrable domain it covers.
- **Both `issue` and `issuewild`.** When an `issuewild` record is present it governs wildcard
  issuance *instead of* `issue`. A plan listing only `issue` would therefore leave wildcard
  issuance — the case this service exists to make easy — unconstrained, while looking constrained.
- **Missing and wrong are opposite problems.** See the verification table below.

Address records are included because "the required DNS entries for a set of LBs" is not only the
challenge delegation — a domain that CNAMEs its challenge name to us but points its A records at
the wrong place produces a valid certificate on a server nobody reaches. The two failure modes
show up in the same table.

**Verification** is a read-only DNS check per row, run on demand from the console and available
as a CLI subcommand, against a configurable resolver (default: the system resolver, with a public
resolver as an alternative so a cached internal view cannot mask a missing public record):

| Check | Result |
| --- | --- |
| `_acme-challenge.<base>` CNAMEs to the expected label in our zone | `ok` |
| the name does not resolve | `missing` |
| it CNAMEs somewhere else, or holds TXT/other records directly | `wrong`, with what was found |
| A and AAAA records match the load balancer's node addresses | `ok` |
| address records resolve elsewhere | `mismatch`, with what was found |
| no CAA record exists at or above the registrable domain | `not-published` — **not a failure**; any CA may issue and everything works |
| a CAA record set exists and includes our issuer | `ok` |
| a CAA record set exists and does **not** include our issuer | `blocking`, with what was found — issuance will fail outright |
| the lookup itself fails (SERVFAIL, timeout) | `unknown`, retryable |

The CAA load balancer is the one place in this table where the intuitive reading is backwards, so it is
worth stating twice: **absent is fine, present-but-wrong is fatal.** A missing CAA record is the
default state of the internet and blocks nothing; a CAA record listing only some other CA blocks
every issuance for that domain, immediately and permanently, with an error from the CA rather than
from us. Reporting a missing record as a failure would paint most of the inventory red and train
everyone to ignore the column; reporting a wrong one as a minor note would hide the single clearest
blocker the check can find.

Results are shown, cached briefly, and never stored as fact — DNS is somebody else's mutable
state, and a green check from yesterday means nothing. This check turns the single most common
onboarding failure ("they said they added it") into a yes/no answer in a few seconds.

### D6. Challenge records live in Postgres, served from an in-memory view

```sql
create table challenges (
  id             bigserial primary key,
  alias_id       bigint not null references aliases(id) on delete cascade,
  value          text not null,
  published_by   text,                          -- the key *identifier*, e.g. 'k_7f3a91c4'
  created_at     timestamptz not null default now(),
  expires_at     timestamptz not null,
  unique (alias_id, value)
);
create index on challenges (expires_at);
```

`published_by` holds the key identifier as text rather than a foreign key to `api_keys`. A
foreign key would force a choice between `on delete cascade` (deleting a key silently withdraws
the challenges it published, which can fail an issuance in flight) and `on delete set null`
(which loses the attribution the spec requires the record to retain). The identifier is a
non-secret, stable, human-readable string; storing it directly satisfies both and removes a join
from the "live challenges" screen.

**Why Postgres at all, when a challenge lives for ninety seconds.** Because a deploy or a crash
in the middle of an issuance would otherwise silently break renewals, at a moment nobody is
watching. And because inventory, credentials and role mappings need durable storage regardless.

**Why also an in-memory view.** DNS answers must be fast and must not fail because the database
is briefly unavailable. The DNS handler reads from a snapshot guarded by an `RWMutex`, refreshed
on every successful write and on a timer (default 30s) as a safety net against rows changing
behind our back.

The snapshot holds **every alias label**, not only the labels that currently have values:

```go
type snapshot struct{ aliases map[string][]value }   // label → active values, possibly empty
type value  struct{ txt string; expires time.Time }
```

An entry that is present with an empty (or wholly expired) slice is a *known label with no
records* and must answer `NOERROR` with an empty answer; a label absent from the map does not
exist and must answer `NXDOMAIN`. A view keyed only by challenge values cannot tell those two
apart, and would answer `NXDOMAIN` after every successful cleanup — which is both wrong per D7
and the state the acceptance suite asserts against directly. The refresh therefore reads
`aliases` left-joined to non-expired `challenges`, and creating or deleting an alias refreshes
the snapshot the same way a `/present` does.

The write path is:

```
POST /present → tx → upsert challenge row → commit → update in-memory map → 200 OK
```

The in-memory update happens **before** the response is written, which is what makes the
"answerable by the time `/present` returns" guarantee true.

**Multiple values at one name are load-bearing, not incidental.** `unique (alias_id, value)` makes
a repeated present an upsert that refreshes `expires_at`, so idempotency is enforced by the
database rather than by a read-then-write race. More importantly, a certificate covering both
`example.com` and `*.example.com` produces two authorizations validated at one challenge name, and
the CA expects to find **both** TXT values there at the same time. Two rules follow, and violating
either breaks wildcard issuance in a way that looks like a random flake:

- `/present` never replaces the existing values for a name; it adds to them.
- `/cleanup` deletes only the row matching `(alias, value)` exactly. Deleting every value for the
  name — the obvious implementation — would withdraw a sibling authorization that is still in
  flight.

Expiry is belt and braces: the in-memory view filters on `expires_at` at read time, so a record
stops being answered the instant it expires, and a sweep every few minutes deletes the rows.
Default lifetime one hour — far longer than an issuance needs, short enough that a value left
behind by a crashed client does not linger.

*Rejected: `LISTEN`/`NOTIFY` to push writes to other replicas.* Correct, and the right answer if
we ever run more than one instance, but machinery in service of a scenario the first version does
not have. The timed refresh is the placeholder that makes adding it later a small change.

### D7. The DNS server answers narrowly and refuses everything else

One `dns.Handler` registered for the zone, served on `udp` and `tcp` listeners.

| Query | Answer |
| --- | --- |
| TXT for a known label with active values | those values, TTL 30s, AA set |
| TXT for a known label with no active values | `NOERROR`, empty answer, SOA in authority |
| any other type for a known label | `NOERROR`, empty answer, SOA in authority |
| SOA or NS at the zone apex | from configuration, AA set |
| any name in the zone that is not a known label | `NXDOMAIN`, SOA in authority |
| any name outside the zone | `REFUSED` |
| unparseable message | `FORMERR` |

Three points worth spelling out, because getting them wrong produces failures that look like
nothing at all:

- **`NOERROR` with an empty answer is not the same as `NXDOMAIN`.** The first says "this name
  exists but has no records of that type"; the second says "this name does not exist". Resolvers
  cache them differently, and the negative caching duration comes from the **SOA minimum** field,
  not from a TTL on the answer — which is why the SOA must be in the authority section of every
  negative response, and why its minimum is set low (30s).
- **`REFUSED` for out-of-zone queries**, not recursion and not `NXDOMAIN`. We are authoritative
  for one zone and nothing else; answering anything about other zones would make us an open
  resolver, which is both wrong and an amplification vector.
- **TCP is a first-class transport here, not a fallback.** The obvious reading is that TCP/53
  matters only when a response is too big for UDP: a response carrying several TXT values — the
  wildcard-plus-apex case above, routinely — can exceed the 512-byte classic UDP limit, so we
  honour the client's advertised EDNS(0) buffer size and, if the answer still does not fit, set
  the TC (truncated) flag so the client retries over TCP. All true, but it understates the case.
  **Observed in a real issuance: the CA queried our TXT record over TCP directly**, with no UDP
  attempt and nothing to truncate — pebble's validation lookup arrived on TCP. Validating
  resolvers are free to do this and some do. So TCP/53 is a hard requirement of its own: a
  deployment that publishes only UDP/53 will fail validation outright, not merely fail large
  answers. The acceptance environment tests both a multi-value truncation and a plain TCP query.

*Rejected: a wildcard record answering any name under the zone.* It would remove the alias lookup
from the DNS path, but it makes the zone leak — every name would exist — and decouples DNS from
the inventory that is the entire security model.

### D8. GORM for queries, golang-migrate for schema, and a test that keeps them honest

**GORM** (`gorm.io/gorm` with `gorm.io/driver/postgres`, which uses `pgx/v5` underneath) is the
data-access layer. The earlier draft used raw `pgx` on the grounds that the query set was tiny.
That stopped being true: the inventory alone is projects, load balancers, nodes, domains, aliases, keys,
CIDR restrictions, sessions and audit entries, most of which are read as object
graphs by the console — "this project with its load balancers with their domains" — which is precisely the
shape hand-written scanning code gets tedious and error-prone at. GORM's preloading, its
`clause.OnConflict` upserts and its transaction helper carry that weight.

The usual objection is performance, and it does not apply here: the DNS hot path never touches
the database (D6), and the write path handles a few hundred requests a day. We are spending
microseconds we have to save code we would otherwise maintain.

**golang-migrate** owns the schema. Numbered `NNNNNN_name.up.sql` / `.down.sql` files under
`internal/store/migrations/`, compiled into the binary with `embed.FS` and read through the
`source/iofs` driver:

```go
src, _ := iofs.New(migrationsFS, "migrations")
m, _ := migrate.NewWithSourceInstance("iofs", src, cfg.DatabaseURL)
err := m.Up()
```

The earlier draft rejected golang-migrate for needing "its files at runtime or a separate binary".
That objection was simply wrong — `source/iofs` has existed since v4.15 and gives exactly the
embedded-schema property goose was chosen for. golang-migrate is preferred now because plain
numbered SQL files are the format every operator can read and apply by hand in an emergency, and
because it takes a PostgreSQL advisory lock for the duration of a migration, so two instances
starting at once cannot both migrate.

**The rule that matters: `AutoMigrate` is never called.** GORM's schema generation and
golang-migrate's files would be two sources of truth for one schema, and the failure mode — a
column that exists on staging because a process once ran with a different binary — is miserable
to debug. GORM is a query layer here and nothing else.

Keeping the two in sync is then a testing problem, solved by two checks:

1. **Schema golden test.** Apply all migrations to a fresh PostgreSQL (the one named by
   `TEST_DATABASE_URL`), dump the schema, and compare against a checked-in
   `internal/store/schema.sql`. A migration that changes the schema without updating the golden
   file fails. The dump is produced by querying `information_schema` rather than by shelling out
   to `pg_dump`, so the test needs no client binary in the image.
2. **Model drift test.** With the migrated schema in place, walk every registered GORM model
   through `db.Migrator()` and assert that each mapped field has a corresponding column of a
   compatible type, and that no model references a table that does not exist. This catches the
   common mistake — adding a field to a struct and forgetting the migration — at test time
   rather than at the first query.

Both need a live database, so both are in the skipped bucket in CI (D13). They are a **local**
guard, run against the dev stack before pushing a migration — not a gate that will catch the
mistake for you afterwards. The compensating control is the acceptance job, which does run in CI
and which starts a real PostgreSQL, applies every migration to it, and then exercises the schema
through the whole service: a migration that fails to apply, or a model that no longer matches its
table, fails the acceptance run at the first query even though the targeted check never executed.
That is slower and gives a worse error message than the golden test would, and it is the honest
trade the team is making.

One check golang-migrate does not perform on its own: if the database has been migrated by a
*newer* binary, `m.Up()` is a silent no-op and the old binary then queries a schema it does not
understand. So startup compares `m.Version()` against the highest embedded migration and exits
with a diagnostic when the database is ahead.

**The operational consequence, stated plainly because whoever performs an upgrade needs to know it
before they need it:** rolling back across a schema change is not a tag change. Deploying image
`v5` runs its migrations; re-deploying `v4` afterwards will find a database ahead of it and
**refuse to start**, repeatedly, with the diagnostic above. That is the correct behaviour — the
alternative is an old binary quietly issuing queries against a schema that has moved, which
corrupts data instead of refusing to — but it means a rollback across a migration is a
restore-from-backup operation, not a redeploy. So: take a database backup immediately before
deploying any release that carries a migration, and treat "does this release migrate?" as the
question that decides whether rollback is thirty seconds or thirty minutes. Rollbacks *not*
crossing a migration are an ordinary image swap. Down migrations exist and are tested, but running
one against production data is a deliberate, supervised act, never part of an automated rollback.

Migrations run on startup by default, before any listener opens, and can be disabled with
`ACME_MIGRATE_ON_START=false` for deployments that prefer to run `acmeservice migrate up` as a
separate job. Down migrations exist and are tested, but are not run automatically, ever.

### D9. Echo, three listeners, and the console served from the binary

**Echo** (`labstack/echo/v4`) replaces the stdlib routing of the earlier draft. That draft was
right for what it described — two routes and a health endpoint do not need a framework. What
exists now is three surfaces with genuinely different middleware stacks: the challenge API
(API-key auth, strict body limits), the admin API (about thirty routes, OIDC session, CSRF, role
checks, JSON error mapping, audit logging), and a single-page application with history fallback.
Route groups, a middleware chain, binding and validation, and `HTTPErrorHandler` are the parts of
Echo we actually use; the correct trusted-proxy `IPExtractor` that D3 depends on is another
(getting `X-Forwarded-For` parsing right by hand is a classic source of quiet security bugs).

Three listeners, and the separation is not cosmetic:

| Listener | Default bind | Serves | Exposure |
| --- | --- | --- | --- |
| challenge | `:8080` | `POST /present`, `POST /cleanup` | public, behind Traefik, TLS terminated there |
| admin | `:8081` | `GET /api/v1/**`, the Angular bundle | behind Traefik, restricted to our networks/VPN |
| internal | `:9090` | `/healthz`, `/readyz`, `/metrics` | never exposed |

Splitting challenge from admin means a stolen API key reaches no management routes even if a
routing rule is wrong, and the internal listener keeps metrics and health off the internet
regardless of what the proxy in front is configured to do. The DNS listeners (UDP+TCP 53) are
necessarily raw and public.

**The HTTPS hostnames must live in the parent zone, not the delegated one.** This is the least
obvious constraint in the deployment and the most annoying one to discover late, so it is written
down here rather than left implicit:

```
acme-api.cloudpowered.services          A   198.51.100.7     ← challenge API, parent zone
acme-console.cloudpowered.services      A   198.51.100.7     ← console, parent zone
acmeservice.cloudpowered.services       NS  ns1.…            ← delegated to us, TXT/SOA/NS only
```

The temptation is to call the challenge endpoint `acmeservice.cloudpowered.services`, matching the
zone. It cannot be: that name is delegated to *this* server, and per D7 this server answers TXT,
SOA and NS and returns `NOERROR` with an empty answer for every other type. It has no A record and
no way to acquire one without turning a deliberately tiny challenge responder into a general
nameserver. An `HTTPREQ_ENDPOINT` pointing there does not resolve to an address, and every Traefik
issuance fails at connection time with a DNS error that looks like a network fault.

Worse, it is painful to reverse: once the zone is delegated, the parent zone can no longer hold
records *inside* it — the delegation hands the whole subtree away — so fixing it means changing
`HTTPREQ_ENDPOINT` on every load balancer afterwards. The names are therefore chosen in the parent zone
from the start, where ordinary A records and ordinary certificates work.

*Rejected: teaching the DNS server to answer A records for its own apex.* It would work, and it
would make the zone's contents depend on the deployment's own address, add a record type to the D7
table, and give the amplification surface something bigger to return. Two names in a zone we
already administer cost nothing.

**Serving the console.** `ng build admin` writes into `backend/internal/webui/dist`, which is
embedded via `//go:embed` in `internal/webui` and served on the admin listener: hashed assets with a long
`Cache-Control`, `index.html` with `no-store`, and a **history fallback** that returns
`index.html` for any unmatched `GET` that is not under `/api/` and does not look like a file
request. The `/api/` carve-out matters — a fallback that swallows unknown API paths turns a
typo'd endpoint into an HTML page and a very confusing bug report. Unknown `/api/` paths return a
JSON `404`.

**Binding port 53 as a non-root user.** The container runs as an unprivileged user with the
`CAP_NET_BIND_SERVICE` capability granted, which permits binding low ports without any other root
privilege. *Rejected: listening high and redirecting with NAT*, as an extra piece of
per-environment plumbing that breaks silently.

*Rejected: splitting into two services* (an API writer and a DNS server sharing the database). The
more orthodox shape, and it would let each scale independently, but it turns the "answerable the
moment `/present` returns" guarantee into a distributed cache-invalidation problem and doubles the
deployment surface, for scaling we do not need.

### D10. OIDC sign-in, sessions, and groups mapped to roles

**The flow** is Authorization Code with PKCE, run **server-side** — a "backend for frontend". The
browser never sees a token:

```
1. Angular calls /api/v1/me            → 401, with the login URL
2. Browser → /api/v1/auth/login        → 302 to the IdP, state + PKCE verifier in a short cookie
3. IdP authenticates the human, redirects to /api/v1/auth/callback?code=…&state=…
4. Backend exchanges the code, verifies the ID token (issuer, audience, expiry, nonce, JWKS)
5. Backend creates a session row, sets an opaque HttpOnly cookie, redirects to /
6. Angular calls /api/v1/me            → 200 with display name, groups and roles
```

**Provider discovery does not gate startup.** `go-oidc`'s discovery call is made lazily on the
first sign-in and retried, not at boot. The config check validates that the issuer *parses* as a
URL; it does not dial it. An identity provider that is down for maintenance must not be able to
stop the DNS server and the challenge API — the two surfaces that have nothing to do with human
sign-in — from coming up, which is exactly what a startup-time discovery call would cause.

*Rejected: the SPA holding the tokens* (implicit flow, or code+PKCE in the browser with tokens in
`localStorage`). Access and refresh tokens in JavaScript-reachable storage turn any XSS into a
durable credential theft, and refresh handling in the browser is fiddly. With a BFF the credential
is an opaque cookie that is useless outside the browser that holds it.

**Sessions** are rows in Postgres, referenced by a 256-bit random opaque id in a cookie marked
`HttpOnly`, `Secure`, `SameSite=Lax`, host-only. Server-side sessions rather than a signed JWT
cookie specifically so that sessions can be **revoked** — by an admin, on role change, or on
sign-out — which a self-contained cookie cannot be. Idle timeout and absolute lifetime are both
configured.

The session stores the subject, display name and the **group list as it was at sign-in**. It does
**not** store the provider's access or refresh tokens: we never call the provider's APIs on the
user's behalf, so there is nothing to spend a token on, and storing one turns the sessions table
into a credential store that needs encryption at rest and a refresh loop. *Rejected: keeping the
refresh token to re-read the group list periodically.* The absolute session lifetime
(`ACME_SESSION_MAX_LIFETIME`, default 8 hours) bounds how stale a group list can get; see the
revocation discussion below, which is the honest account of what that costs.

**CSRF.** Cookie authentication needs it. Echo's CSRF middleware issues an `XSRF-TOKEN` cookie and
requires an `X-XSRF-TOKEN` header on state-changing requests — which is exactly the convention
Angular's `HttpClient` implements natively, so the client side is configuration rather than code.
`SameSite=Lax` is defence in depth behind it, not the whole defence. Two settings that are easy
to get wrong and produce a console where nothing can be saved: Echo's defaults are cookie name
`_csrf` and lookup `header:X-CSRF-Token`, so both must be set to Angular's names
(`CookieName: "XSRF-TOKEN"`, `TokenLookup: "header:X-XSRF-TOKEN"`), and the CSRF cookie must
**not** be `HttpOnly` — unlike the session cookie — because the browser-side code is what reads
it back out.

**Groups to roles.** The identity provider is **Keycloak**. It must be configured with a *Group
Membership* mapper on the client — token claim name `groups`, "Full group path" off, added to the
ID token. (For portability the claim name is configurable, default `groups`, and an optional
prefix is stripped from each value so path-style group names reduce to leaf names; on Authentik the
equivalent is the built-in `goauthentik.io/providers/oauth2/scope-groups` mapping with the `groups`
scope requested. Keycloak is what we run and what the dev and acceptance stacks ship.)

There are **four fixed roles**, all **global** — none of them is scoped to a project:

| | `ADMIN` | `CONFIG_ADMIN` | `KEY_ROTATION` | `READ_ONLY` |
| --- | --- | --- | --- | --- |
| Create / delete projects | yes | no | no | no |
| Create/edit/delete load balancers, nodes, domains | yes | yes | no | no |
| Create / revoke API keys | yes | yes | yes | no |
| See a key secret (once, at creation) | yes | yes | yes | no |
| View the DNS plan and run verification | yes | yes | yes | yes |
| View the inventory and the audit log | yes | yes | yes | yes |

**The mapping is configuration, not data.** One environment variable per role names the Keycloak
group that grants it:

```
ACME_ADMIN_GROUP=foobar-keycloak-admin-group
ACME_CONFIG_ADMIN_GROUP=foobar-keycloak-config-admin-group
ACME_KEY_ROTATION_GROUP=foobar-keycloak-key-rotation-group
ACME_READ_ONLY_GROUP=foobar-keycloak-read-only-group
```

A user's roles are the union of every role whose configured group appears in their groups claim.
Holding several at once is normal and additive — a person in the admin group and the read-only
group simply has `ADMIN`. A variable left unset means nobody holds that role.

*Rejected: the `role_bindings` table of the earlier draft*, which bound group names to roles and
optionally to a single project, editable in the console. Per-project delegation was the only thing
that table bought, and we are not delegating per project: the people who administer this fleet
administer all of it. What the table cost was a migration, a repository, a management API, a
console screen, an editing permission, and an authorization rule that had to carry a project id
through every check. Four environment variables replace all of it, and group membership — the part
that genuinely varies per person — stays in Keycloak, where it was already governed.

**The bootstrap problem is dissolved, not solved.** The earlier draft needed a
`BOOTSTRAP_ADMIN_GROUPS` setting because bindings lived in a table that starts empty, so a fresh
database had nobody who could create the first binding. With the mapping in configuration there is
no empty state to escape: `ACME_ADMIN_GROUP` *is* the bootstrap, on the first start and every start
after it. The setting is gone entirely, and it is worth recording that the fix was deleting the
problem rather than adding a break-glass path to it.

A person who authenticates successfully but is in none of the four groups gets a `403` and a page
listing the groups they were seen as carrying. That listing is load-bearing: when a mapping is
misconfigured — a typo in a variable, "Full group path" left on so the claim reads
`/infra/admins` instead of `admins` — it is the only thing that tells an operator what the service
actually received, and without it the symptom is an empty console and a support ticket.

**Revocation, honestly.** This model is simpler and its revocation semantics are *worse* than the
table's, which is a real trade and must not be papered over:

| Change | Takes effect |
| --- | --- |
| Removing a user from a Keycloak group | at their next sign-in — groups are read from the ID token, so an existing session keeps the roles it was issued with, bounded by `ACME_SESSION_MAX_LIFETIME` |
| Changing which group grants a role | on restart — it is environment configuration |
| **Revoking a session** | **immediately** |

So the answer to "we have just removed someone's access, are they out?" is: remove them from the
Keycloak group *and* revoke their sessions. Sessions are still server-side rows precisely so that
an `ADMIN` can kill them, and that is now the only immediate lever in the system. That is why the
console carries an **Active sessions** view (D11) listing who is signed in, since when and until
when, with revoke buttons per session and per person: without it the documented procedure has a
step nobody can actually perform. `ACME_SESSION_MAX_LIFETIME` is the knob that bounds the exposure
window for anyone who forgets the second half — set it to hours, not weeks.

### D11. The Angular console

Standalone components, the modern `HttpClient` with `withXsrfConfiguration`, an interceptor that
redirects to the login route on `401`, and a route guard driven by the roles returned from
`/api/v1/me`. Client-side role checks hide controls; they are not the enforcement — every route on
the admin API checks the permission again server-side, because the browser is not trusted.

Screens:

- **Projects** — list, create, drill in.
- **Project** — its load balancers.
- **load balancer** — nodes and their addresses; the domain table; the **DNS plan** panel with per-row
  verification status, a copy button and a "download zone snippet" action; the **API keys** panel.
- **Domain editor** — with the wildcard rules from D4 stated inline and validated as you type.
- **API keys** — create (secret revealed once, with an explicit "I have stored it" step before the
  dialog can be dismissed), CIDR restrictions edited as a list with the load balancer's node addresses
  offered as one-click defaults, rotate, revoke, and `last_used_at` so a key nobody uses is
  visible.
- **Live challenges** — what is answerable right now, with its alias, age and publishing key. This
  is the screen that answers "is a renewal happening at this moment", which is otherwise invisible.
- **Audit log** — filterable by project, actor and action.
- **Active sessions** — who is signed in, since when, last seen when, expiring when, with revoke
  for one session or for everyone's sessions belonging to one person. `ADMIN` only. A list, a
  button and a confirmation — but the button is half of the "remove them from the group *and*
  revoke their sessions" procedure D10 tells operators to follow.
- **Settings** — a read-only view of the effective configuration (redacted). Now the more useful
  screen of the two it used to be: with the group-to-role mapping living in environment variables,
  this is where an operator sees which groups the *running process* actually maps, which is the
  first thing to check when someone reports they cannot do something they should be able to.

### D12. The development loop: everything in containers, one origin, fake internet

Production embeds the compiled bundle in the binary (D9). Development does not — waiting for a Go
rebuild to see a CSS change is not a workable loop. So both halves run with their own live reload,
**both as containers**, with the source bind-mounted in and the build caches mounted under
`development/data/`:

```
                     http://localhost:8088
                             │
                    ┌────────┴────────┐   dev traefik (file provider)
                    │                 │
        PathPrefix(`/api`)      PathPrefix(`/`)
                    │                 │
        golang:alpine + air     node + ng serve
        :8081 admin API         :4200 (project `admin`)

  postgres   keycloak   coredns (static address; fake zones, forwards the rest)
                        ── one bridge network ──
```

Everything is on one bridge network, with **CoreDNS at a static address** serving the fake test
zones and forwarding everything else to the real internet, so a container can resolve both
`example.test` and `github.com`.

**Why containers, when an earlier draft of this document argued for host-run processes.** That
draft ran `air` and `ng serve` on the host, on the grounds that live reload needs to be fast and a
container would get in the way. That reasoning does not survive contact with the facts: with the
source bind-mounted, `air` and `ng serve` rebuild exactly as fast inside a container as outside,
and HMR works unchanged. What the host-run version cost was real, though — every developer needed
a matching Go toolchain and Node version installed, the two processes could not resolve the fake
zones the way the rest of the stack did, and `dev.sh` had to manage two host children with a trap
instead of letting compose supervise them. The argument is recorded rather than deleted because it
sounds right and somebody will otherwise re-make it.

Two constraints in that diagram are not stylistic, and both were learned by hitting them:

- **CoreDNS's `forward` directive accepts only `IP:port`, not a hostname.** So the DNS container
  needs a static address on the network regardless — which is an independent reason from the
  obvious one, that every other service points its `dns:` at it and needs a fixed value to point
  at. Neither reason goes away if you fix the other.
- **The OIDC issuer string must be byte-identical inside and outside the stack.** `go-oidc`
  compares the issuer in the discovery document against the one in the ID token and rejects any
  mismatch — not normalizes, rejects. This bites immediately in a containerised stack, where the
  browser reaches Keycloak at one hostname and the API container reaches it at another; the two
  must be made to agree (one hostname resolving to Keycloak from both sides) rather than papered
  over. The symptom is a sign-in that completes at Keycloak and then fails at the callback with an
  issuer mismatch, which reads like a misconfigured client and is not.

`development/` holds all of it: the compose file, the dev Traefik routing rules, the CoreDNS zones,
the identity provider realm export, and the scripts. A fresh clone is one command from a working
console with data in it.

**Every stateful container bind-mounts its state under `development/data/`** — `./development/
data/postgres/`, `./development/data/keycloak/`, and so on — rather than using named Docker
volumes:

```yaml
services:
  postgres:
    volumes: [./data/postgres:/var/lib/postgresql/data]
  keycloak:
    volumes: [./data/keycloak:/opt/keycloak/data]
```

The state is then ordinary files in the working tree. You can `ls` it, `du` it, copy a database
directory aside before trying a destructive migration, and — the part that matters most in daily
use — reset the whole stack with `rm -rf development/data/*`, instead of recalling which
`docker volume rm` incantation matches the project-name-prefixed volume this compose file happens
to have created. Named volumes also survive `docker compose down` in a way that regularly leaves
someone debugging a "fresh" stack that is quietly running last week's schema.

`development/data/` is gitignored with a committed `.gitkeep` so the directory exists on a fresh
clone; `up.sh` creates the per-service subdirectories before starting (Postgres in particular
fails on a missing mount point rather than creating it), and `reset.sh` removes their contents.

The point of the overlay — as opposed to Angular's built-in `proxy.conf.json`, which would also
route `/api` to the backend — is that **development runs on the same origin as production**. The
session cookie is host-only, `SameSite=Lax`, and paired with a CSRF token; a two-origin development
setup would need CORS with credentials, a relaxed `SameSite`, and cookie behaviour that differs
from production in exactly the area most likely to hide a bug. One origin in both places means the
authentication code has no development-only branch. It also mirrors the production topology, where
a Traefik in front is what terminates TLS and routes to the service anyway.

Details that make it work: the router for `/api` gets the higher priority so the SPA catch-all does
not shadow it; Traefik passes the dev server's HMR WebSocket through unchanged, so live reload
survives the overlay; `--disable-host-check` is required because the dev server sees a `Host` of
`localhost:8088` rather than its own; and the Keycloak dev realm is seeded from a JSON export with
the client, the group mapper and a couple of test users in the relevant groups, so sign-in works on
a fresh checkout with no manual IdP clicking.

**Everything is driven by bash scripts, not a Makefile.** `scripts/dev.sh` starts the whole loop —
the stack, the `air` container and the `ng serve` container — and on exit deliberately leaves the
containers running, because the database and the identity provider are slow to warm and there is
no reason to lose them every time you stop editing. `scripts/build.sh`
runs `ng build admin` first and then `go build`, so the embedded path is the one CI and production
always take, and the embed directory is gitignored with a placeholder file so `go build` still
works before the frontend has ever been built. The rest are one script per verb:
`scripts/test.sh` (honouring `SKIP_FRONTEND=1`), `scripts/test-integration.sh`,
`scripts/test-acceptance.sh` and `scripts/lint.sh`, with `scripts/lib.sh` holding the shared
`say`/`warn`/`die`/`need` helpers. A missing linter is an error rather than a silent skip: a lint
step that quietly passes when the tool is absent is worse than no lint step, because it reports
green.

The stack itself has its own set under `development/scripts/`: `up.sh`, `down.sh` (stop, keep
state), `destroy.sh` (stop and wipe, prompting unless `--yes`), `reset.sh` (destroy + up + seed),
`seed.sh` (idempotent, refuses to seed twice), `status.sh` (containers, URLs, the four test logins,
and whether it has been seeded) and `logs.sh`.

Three details in there are the reason the scripts exist at all rather than a page of README
instructions:

- **`up.sh` waits on compose *health*, not on `up -d` returning.** Postgres accepts TCP connections
  well before it accepts queries, and Keycloak serves its port well before its realm is
  importable. A script that starts the stack and immediately runs migrations or seeds works on a
  warm machine and fails on a cold one, which is the worst possible distribution of outcomes. So
  `up.sh` starts, waits for health, applies migrations, and prints status.
- **`destroy.sh` deletes `data/postgres` through a throwaway root container.** The Postgres
  entrypoint chowns PGDATA to a container-internal uid, so the invoking user simply cannot
  `rm -rf` it and gets a pile of permission errors instead. Deleting it from inside a container
  that runs as root is the only way that works without asking the developer for sudo.
- **A Keycloak realm is imported only into an *empty* Keycloak data directory.** This one will
  otherwise cost somebody an afternoon: edit `development/realm/*.json`, re-run `up.sh`, and
  Keycloak keeps serving the realm it already has, silently, with no error and no hint that your
  edit was ignored. The fix is `reset.sh` — which destroys the data directory and so makes the
  import happen again. Any change to the realm export means a reset, not a restart.

### D13. Testing strategy

- **Unit tests** for normalization, wildcard validation and challenge-name derivation, the role
  resolution table, API key verification and CIDR matching, and the DNS decision table of D7.
- **Integration tests** against the real PostgreSQL named by `TEST_DATABASE_URL`, skipped when it
  is unset — the store's behaviour under concurrent identical upserts is exactly the kind of thing
  an in-memory fake would get wrong — plus the two schema-consistency checks from D8. Each test
  runs in its own schema so the suite can run in parallel against one database.

**Where each suite runs, stated plainly, because the split is deliberate and someone will
otherwise "fix" it:**

| Suite | Locally | In CI |
| --- | --- | --- |
| Go unit tests, frontend unit tests, lint | yes | yes |
| Integration tests (need `TEST_DATABASE_URL`) | yes, against the dev compose stack | **no — skipped, the variable is not set** |
| Schema golden and model drift (D8) | yes, same bucket | **no — same bucket** |
| Acceptance (Docker Compose, D14) | yes, `scripts/test-acceptance.sh` | **yes, its own job** |

The acceptance row describes the intended end state. This change creates the script and the CI
stage; the environment and the suite they invoke are built by `add-acceptance-environment`, and
until that lands the stage has nothing to run.

Integration tests are a development activity: `development/scripts/up.sh` already runs the
PostgreSQL they need, and `scripts/test-integration.sh` defaults `TEST_DATABASE_URL` to it. The pipeline
deliberately does not stand a database up for them — the coverage that genuinely must not
regress unattended is the acceptance run, which exercises the same store through the real service.
The cost is that a broken migration or a drifted model reaches CI as an acceptance failure rather
than as a targeted one; the benefit is a pipeline with one integration-shaped job instead of two.
A test in the skipped bucket must therefore be written so that skipping is *visible* — `t.Skip`
with a message naming `TEST_DATABASE_URL` — and never so that it silently passes.
- **DNS tests** that start the server on an ephemeral port and query it with a `miekg/dns` client,
  asserting on response codes and flags, not just answer contents: response codes are where the
  subtle bugs live. Includes a multi-value answer that must truncate over UDP and succeed over TCP.
- **HTTP API tests** covering every status code in the spec, the ordering guarantee that
  authentication precedes authorization, and that an out-of-scope name and a bad key are
  indistinguishable to the caller.
- **Frontend unit tests** for the wildcard validator, the role guard and the auth interceptor.
  There is no browser-driven end-to-end test of the console: see D14.
- **Acceptance**, below — the only test that exercises the interaction between all of it.

### D14. The acceptance environment

> **Specified here, built elsewhere.** This decision stays in this document — it is cited by number
> from D2, D6, D7, D13 and the testing section above, and renumbering it would break those. The
> `acceptance-harness` requirements and the tasks that implement everything below now live in the
> change `add-acceptance-environment`. Read this decision for *how* and *why*; read that change for
> *what is left to do*.

A Docker Compose environment under `development/acceptance/`, driven by a Go test in
`backend/acceptance/` behind an `acceptance` build tag and run as its own CI job. Everything is
real except the three things that cannot be: the certificate authority, the public DNS, and the
trust anchor. Seven containers — postgres, acmeservice, pebble, coredns, traefik, whoami and
keycloak.

**The CI runners have the Docker socket mounted**, which is what makes this job possible at all,
and is worth recording because the constraint is invisible from the repository: a future reader
who assumes the pipeline cannot start containers will conclude this job does not belong in CI and
delete it. It does belong there — with the integration tests deliberately skipped (D13), the
acceptance run is the only thing in the pipeline that touches a real database or a real DNS
exchange, so removing it would leave the schema, the store and the whole ACME path unverified on
every merge.

```
  ┌──────────────┐  ACME (DNS-01)   ┌──────────────┐
  │   traefik    │─────────────────▶│    pebble    │  fake CA, real ACME protocol
  │  (real,      │                  │              │  really looks up the TXT record
  │  unmodified) │◀── cert ─────────└──────┬───────┘
  └──────┬───────┘                         │ DNS
         │ httpreq POST /present           ▼
         │                         ┌──────────────┐
         ▼                         │   coredns    │  fake "public" DNS:
  ┌──────────────┐   DNS query     │              │   authoritative for example.test,
  │ acmeservice  │◀────────────────│              │   wild.test — holds the customer
  │  (+postgres) │                 └──────────────┘   CNAMEs; delegates acmeservice.test
  └──────────────┘                                    to acmeservice
```

**pebble** — Let's Encrypt's own test server — stands in for the CA. It speaks the real ACME
protocol and performs real DNS-01 validation, but issues from a throwaway CA with no rate limits.
Started with `-dnsserver coredns:53` so its validation authority queries our fake internet, and
with `PEBBLE_VA_NOSLEEP=1` and `PEBBLE_WFE_NONCEREJECT=0` — pebble deliberately injects random
delays and rejects ~5% of nonces to shake out client bugs, which is the right default for testing
a client and pure flakiness when the client is not what we are testing.

**CoreDNS** plays the entire rest of the internet's DNS: a `file` zone for `example.test` carrying
the customer-side `_acme-challenge` CNAMEs and the address records pointing at the Traefik
container, a second zone `wild.test` with a single CNAME at `_acme-challenge.wild.test` for the
wildcard case, and the delegation of `acmeservice.test` to our own DNS listener. It is the resolver
that pebble, lego and the test client all use.

*The one honest simplification*: our zone is reached through a stub/forward in CoreDNS rather than
by a resolver walking a real NS delegation from a root. Building a full root-to-leaf resolution
chain in a test would be days of work to exercise code that is not ours. The property it would
prove — that our server returns a correct authoritative SOA and NS set at the apex — is asserted
directly by a DNS unit test instead, and the real delegation is verified by hand once, at rollout
(step 2 of the migration plan).

**The fake chain** is pebble's own generated root and intermediate, and it is used in both
directions: Traefik is given pebble's `pebble.minica.pem` via `LEGO_CA_CERTIFICATES` so it trusts
the test CA's API, and the test client fetches the root from pebble's management API at
`https://pebble:15000/roots/0` and puts it in a `x509.CertPool` — so "the certificate is valid" is
genuinely verified rather than skipped with `InsecureSkipVerify`, which would let the entire test
pass against a self-signed certificate Traefik generated on its own.

**Traefik** is the real image, pinned to a 3.x tag, with real configuration — `caServer` pointed at pebble's directory,
`dnsChallenge.provider: httpreq`, `dnsChallenge.resolvers: ["coredns:53"]` so lego's propagation
check consults the fake internet too, and the `HTTPREQ_*` variables carrying an API key seeded into
the database. It fronts a `whoami` container. Nothing about Traefik is stubbed, because the claim
under test is precisely that stock Traefik works against this service.

*What this does not cover.* The supported floor is Traefik 2.11 (D2), but the acceptance suite runs
one 3.x image, so **2.11 is supported and not exercised in CI**. That is a deliberate choice — one
acceptance run, not a version matrix, because the thing under test is our service and lego's CNAME
behaviour is the same on both sides of that gap. It is written down because a stated minimum
otherwise reads as a tested one. If any load balancer in the fleet actually runs 2.11, the honest check is
to watch one real issuance on that load balancer during rollout rather than to add a second acceptance job.

What a run asserts:

1. `GET https://shop.example.test/` returns 200, and its certificate chains to pebble's root and
   carries the expected SAN. (The concrete host name, so the ordinary path works.)
2. A certificate covering both `wild.test` and `*.wild.test` is issued, and during validation
   **two** TXT values were answerable at `_acme-challenge.wild.test` simultaneously — the D6
   invariant, checked by querying our DNS server directly while issuance is in flight.
3. `/present` and `/cleanup` were each called, visible in the service's metrics and audit log.
4. After issuance, no challenge rows remain and the alias answers `NOERROR` with an empty answer —
   not `NXDOMAIN`, which is the D7 distinction.
5. A request to `/present` with a valid key for a name belonging to a different load balancer is refused,
   and no record appears in DNS.
6. A request with a key restricted to a CIDR, sent from outside it, is refused with `401`.
7. Restarting the acmeservice container mid-issuance does not fail the certificate — the D6
   durability claim, and the reason challenges are in Postgres at all.

8. The DNS plan the service generates for the seeded load balancer matches, record for record, the zone
   files CoreDNS is actually serving — closing the loop between "what we tell the customer to
   publish" and "what actually validates". The plan is read with
   `acmeservice loadbalancer dns <id>` inside the running container and compared against the checked-in
   CoreDNS zone files.
9. A human can sign in against the real identity provider, and the roles their groups map to are
   actually enforced by the management API — see below.

**Sign-in is exercised end to end, without a browser.** Everything in the OIDC path — discovery,
the PKCE exchange, ID-token verification against real JWKS, the groups claim arriving in the shape
the mapper produces, the session cookie, CSRF, and the role check that reads it all back — is
security-relevant and is not exercised by anything else in the suite. So Keycloak stays in the
acceptance stack, seeded from a realm export, and the test drives the authorization-code flow over
plain HTTP with an `http.Client` and a `cookiejar`:

```
GET  /api/v1/auth/login                  → 302 to Keycloak
GET  the Keycloak authorization URL      → the login page; parse the <form action="…"> URL
POST that action with username+password  → 302 back to /api/v1/auth/callback?code=…&state=…
GET  the callback                        → session cookie set, 302 to /
```

The jar carries the cookies; the only parsing needed is pulling one `action` attribute out of the
login page, which is why this is roughly forty lines and not a browser. With that client in hand
the suite asserts, through the management API:

- `GET /api/v1/me` reports, for each of the four seeded users, the expected display name, the
  expected **groups**, and the role that group maps to — the whole point of the mapper
  configuration, and the thing most likely to be silently wrong.
- A `READ_ONLY` user is refused (`403`) when creating a domain.
- A `CONFIG_ADMIN` user succeeds at the identical call.
- A `KEY_ROTATION` user is refused when creating a domain but **succeeds** when creating an API
  key. This is the assertion that actually pins the `CONFIG_ADMIN`/`KEY_ROTATION` boundary; the
  other two would still pass under a naive "any role may write" implementation.

*Rejected: driving the console with Playwright.* An earlier draft made these same assertions
through the UI in a real browser. Rejected for the browser, not for the coverage: a browser runtime
and its images in CI, plus a scripted click-through that breaks whenever the login page changes,
buys nothing over the HTTP flow above, because the enforcement being tested is entirely server-side
— the console's role checks only hide controls (D11). The console's own logic (wildcard validator,
role guard, auth interceptor) is covered by frontend unit tests.

### D15. Production runs as Docker Compose on a dedicated VM

The same shape as the development stack (D12) and the acceptance stack (D14), which is most of the
argument for it: one `compose.yaml`, two services, on a small VM with a static public address.

```yaml
services:
  acmeservice:
    image: <registry>/acmeservice:<tag>
    cap_add: [NET_BIND_SERVICE]          # bind 53 without a privileged container
    environment:
      ACME_DATABASE_URL: postgres://acmeservice:…@db:5432/acmeservice?sslmode=disable
    ports:
      - "198.51.100.7:53:53/udp"         # the delegated addresses, explicitly
      - "198.51.100.7:53:53/tcp"
      - "[2001:db8::7]:53:53/udp"        # both families: the glue names both
      - "[2001:db8::7]:53:53/tcp"
      - "127.0.0.1:8080:8080"            # challenge API, behind the host's proxy
      - "127.0.0.1:8081:8081"            # console
    depends_on: {db: {condition: service_healthy}}
  db:
    image: postgres:16
    volumes: [./data/postgres:/var/lib/postgresql/data]
    # no `ports:` — reachable only on the Compose network
```

Five details in there are deliberate, and four of them are invisible until something breaks.

**The `db` service publishes no port.** PostgreSQL is reachable only over the Compose network, from
one container on one host, which is why the connection string says `sslmode=disable`. Requiring TLS
on that hop would mean provisioning and renewing a certificate for a container-to-container
connection that never crosses a network interface — real operational work in exchange for nothing.
`sslmode=require` belongs to the managed-database variant below, where the hop is real.

**Port publishing names the address.** `"53:53/udp"` — the form everyone writes — publishes on
*every* interface. On a multi-homed host, which a VM with a private management network already is,
that means the server can answer from an address the NS delegation does not name. Resolvers discard
a response whose source address is not the one they queried, so the symptom is intermittent
validation failures against a server that looks healthy and logs the query as answered. Binding
each published port to a specific address costs one prefix and removes the failure mode. The HTTP
listeners bind to loopback because the host's proxy terminates TLS in front of them; the internal
listener (`:9090`) is not published at all.

**`cap_add: NET_BIND_SERVICE`** is how D9's "unprivileged process, low port" works in Compose. Not
`privileged: true`, and not running as root — one capability, nothing else.

**The host must have nothing else on port 53.** On a systemd host this is almost always
`systemd-resolved`, whose stub listener holds `127.0.0.53:53` and, in some configurations, more
than that. It needs `DNSStubListener=no` in `/etc/systemd/resolved.conf` or to be moved off the
public address. If it is not dealt with, `docker compose up` fails to bind, or worse, binds
partially and the service answers on one protocol only.

**The database directory is a bind mount** under `./data/postgres/`, not a named volume, for
exactly the reasons D12 gives for development: the state is ordinary files that can be listed,
copied aside before a risky migration, and backed up with the same tools as everything else on the
host.

**Swapping to a managed PostgreSQL** — the likely first change if this outgrows one VM — is
deleting the `db` service and pointing `ACME_DATABASE_URL` at the managed instance with
`sslmode=require`. Nothing else in the file or the application changes, which is the property that
makes starting with a container here safe rather than a decision to regret.

## Risks / Trade-offs

- **Single instance is a single point of failure for every renewal.** → Tolerable because ACME
  clients retry for days before a certificate actually expires, and certificates renew at 2/3 of
  their lifetime, leaving ~30 days of slack. Mitigated by alerting on the readiness probe and DNS
  failure rate. The design keeps the door open for a second replica (D6).
- **Port 53 must be publicly reachable over both UDP and TCP.** → The most likely thing to go wrong
  at deployment, and it fails in a way that looks like "certificates just stop renewing". Mitigated
  by a post-deploy verification step (`dig` over UDP and forced TCP, plus a query through public
  resolvers) and a synthetic monitor querying the zone apex from outside.
- **The console is now the crown jewel.** An `admin` session can add any domain to the inventory
  and mint a key for it, which is a path to a certificate for that domain. → Bounded by requiring
  the customer's CNAME to exist before anything can be validated — the attacker would also need
  control of the victim's DNS. Mitigated further by the audit trail, by the admin listener not
  being on the public internet, and by keeping the group named by `ACME_ADMIN_GROUP` small.
- **Group-to-role mapping is only as good as Keycloak's group hygiene.** A group that quietly gains
  members gains our permissions with it, and because groups are read at sign-in, removing someone
  from a group does not end a session already in flight. → Mitigated by keeping the four groups
  small and reviewed, by a short `ACME_SESSION_MAX_LIFETIME`, and by session revocation as the
  immediate lever when access must end now (D10).
- **`X-Forwarded-For` trust is a classic quiet security bug**, and CIDR-scoped keys depend on it
  entirely. → Fail-closed startup check (D3), and an acceptance assertion that a spoofed header
  from an untrusted peer does not bypass the restriction.
- **Compromise of a key yields certificates for that load balancer's domains.** → Bounded by the
  inventory, by per-load-balancer keys, and optionally by source address. Bounded further where the customer
  is willing to publish the CAA records the DNS plan generates (D5), which stop any CA but ours
  issuing for that domain at all — the one mitigation that still holds if our own authorization is
  defeated.
- **We are running a public UDP service, which is an amplification surface.** → Mitigated by
  answering only for our own zone (`REFUSED` everywhere else) and by responses being small. Rate
  limiting per source address is the obvious next hardening step if abuse appears.
- **The build now needs Node as well as Go**, and the acceptance job needs Docker Compose. → A
  slower, more brittle pipeline in exchange for one deployable artifact and a test that would
  otherwise not exist. The frontend build is cached on `package-lock.json`, and the acceptance job
  is allowed to be slow because it runs on merge rather than on every push.
- **Onboarding still depends on a human at the customer creating records.** → No technical
  mitigation, but the generated plan and the live verification (D5) turn a multi-day email
  exchange into a checklist with a green tick.
- **Zone delegation is a foot-gun.** A wrong or missing NS record in `cloudpowered.services` breaks
  everything downstream, and DNS caching means mistakes take hours to clear. → Change the
  delegation once, verify it, and treat the parent zone as a change-controlled asset.
- **A DS record on an unsigned delegation breaks every aliased domain at once, invisibly.** If the
  parent zone is DNSSEC-signed and the delegation is given a DS record, validating resolvers treat
  our unsigned answers as forged and SERVFAIL the entire subtree — while a direct query to our
  server still looks perfect, so the symptom points away from the cause. → The delegation carries
  no DS record, verifying its absence is an explicit rollout step, and the zone stays unsigned
  until we choose to sign it and add the DS in that order.

## Migration Plan

There is nothing to migrate from — the risk is in the ordering of the rollout, not in data.

1. **Build and deploy.** Provision the VM with its static IPv4 and IPv6 addresses, clear anything already
   holding port 53 (usually `systemd-resolved` — D15), create `./data/postgres/`, and bring the
   two-service Compose stack up with no delegation and no inventory. Verify by querying the public
   addresses directly, over UDP and forced TCP and over **both address families**: the zone apex
   must return the configured SOA authoritatively, and it must do so *from* each address that will
   be named in the delegation's glue.
2. **Delegate the zone.** `cloudpowered.services` is our own zone, so this is a self-service
   change we make ourselves rather than a request to a third party — which is what makes it safe to
   do early and verify before any customer depends on it. Add NS records for
   `acmeservice.cloudpowered.services` there, with **both A and AAAA glue** pointing at the
   service's two addresses, and **no DS record**. Verify from outside that
   a public resolver reaches us for a name in the zone, and separately that no DS record exists
   (`dig DS acmeservice.cloudpowered.services` must return an empty answer). Also publish the
   `acme-api` and `acme-console` A and AAAA records in the parent zone at this point (D9) — they
   are ordinary records outside the delegation. Do this before any customer CNAME exists, so a mistake
   affects nothing.

   **Why the DS record matters.** DNSSEC is DNS's chain of cryptographic signatures; a **DS**
   (Delegation Signer) record in the parent is the link that says "the child zone is signed, and
   here is the fingerprint of its key." Our zone is deliberately unsigned (a non-goal). If
   `cloudpowered.services` is itself DNSSEC-signed, some registrar and DNS-hosting workflows offer
   to create a DS record for a new delegation, and some create one automatically. A DS pointing at
   an unsigned child is the worst possible failure mode available here: validating resolvers —
   which includes Let's Encrypt's — conclude the answers have been tampered with and return
   SERVFAIL for **every name under the delegation at once**, so every domain aliased to us stops
   validating simultaneously. Meanwhile a direct `dig @our-server` looks perfect, because our
   server is answering correctly; only a validating resolver sees the problem. Anyone debugging
   this without knowing to look for it loses a day. Signing the zone later is a fine thing to do,
   but the DS record goes in *after* the zone is signed, never before.
3. **Wire up identity.** Register the OIDC client in Keycloak with the Group Membership mapper,
   create the four groups, set `ACME_ADMIN_GROUP`, `ACME_CONFIG_ADMIN_GROUP`,
   `ACME_KEY_ROTATION_GROUP` and `ACME_READ_ONLY_GROUP` to name them, restart, and sign in. There
   is no in-console setup step: the mapping is configuration, so it is live as soon as the process
   is.
4. **Enter the existing fleet into the inventory** — projects, load balancers with their node addresses,
   and the domains each already answers for. This is read-only reality capture; nothing changes for
   those domains yet.
5. **Onboard one internal test domain**: add it, read its DNS plan, publish the records, and use
   the built-in verification until every row is green.
6. **Point one Traefik router at the new resolver.** Traefik selects a certificate resolver per
   router, so this is a one-domain change with no effect on anything else. Watch the full cycle:
   `/present` logged, propagation check passes, certificate issued, `/cleanup` logged.
7. **Roll out per domain**, each gated on its records verifying green. No big-bang cutover and no
   deadline: domains that have not moved keep using whatever resolver they use today. Where a
   customer has many hostnames, prefer converting them to one wildcard — one CNAME instead of many.

**Rollback.** At any point, move a router back to its previous certificate resolver. Already issued
certificates are unaffected — they are files in Traefik's store and remain valid for their full
lifetime, so a rollback is never urgent. The customer CNAME can be left in place; it is inert while
unused. Full rollback is: revert Traefik's configuration, remove the delegation, `docker compose
down` on the VM.

Rolling back the *service* to an earlier image is separate, and is only a tag change when the
release being reverted carried no migration — see D8 for why an older binary refuses to start
against a newer schema, and what to do about it.

## Open Questions

None outstanding.

Eight questions from the first draft are **settled**, and are recorded here so nobody reopens them
during implementation:

- **Where does it run, and on what address?** Docker Compose on a small dedicated VM with static
  public IPv4 **and** IPv6 addresses — the service is dual-stack, and both are named in the
  delegation's glue. See D15 for the topology. *Kubernetes is deferred, not rejected*, and the
  reasoning is worth keeping for whenever it comes back: running there needs a `LoadBalancer`
  service carrying UDP **and** TCP on one address, and mixed-protocol services are not cleanly
  supported by every cloud provider — some refuse them outright, some require two load balancers
  and therefore two addresses, which a DNS delegation cannot use. For a single-instance service
  with no scaling requirement, an orchestrator buys nothing today and costs the one thing this
  service actually needs, which is a stable address answering on both protocols.
- **Alias label convention** — the suggestion is derived from the challenge name
  (`_acme-challenge.example.com` → `example-com`), collisions get a numeric suffix, and the
  operator may type anything else that is a valid label. A mandatory project-slug prefix was
  considered and dropped: it makes every label longer for a collision that the suffix already
  handles, and the operator can always type `acme-shop-example-com` themselves when they want it.
- **Which identity provider?** **Keycloak**, definitively — it is what we run, and it is what the
  development and acceptance stacks ship as a realm export. The claim name stays configurable and
  D10 keeps a one-line note on Authentik's equivalent mapper, so portability costs nothing, but
  everything here targets Keycloak.
- **Who administers `cloudpowered.services`, and how is a delegation change requested?** We do. The
  delegation is a self-service change in a zone we already own, not a ticket to a third party,
  which is why Migration Plan step 2 can be done and verified before anything depends on it.
- **May a project-scoped administrator add arbitrary domains?** Moot — per-project roles no longer
  exist. All four roles are global (D10), so the question of what a project-scoped role may reach
  does not arise.
- **Does the console need a browser-driven end-to-end test?** No — see the rejection recorded at
  the end of D14.
- **How many NS records should the delegation carry?** Two names, `ns1` and `ns2`, both resolving
  to the one instance's addresses. Some delegation checkers and registrar interfaces complain about
  a single NS record, and satisfying that convention costs two lines in the parent zone. **It buys
  no redundancy whatsoever** — and that sentence is the reason this is written down rather than
  just done. Two NS records here mean one server with two names, not two servers: if the instance
  is down, both names are down, and anyone reading the zone later must not mistake the delegation
  for a redundant lb. Running a second instance is a separate decision that the design keeps the
  door open for (D6) and does not take.
- **Do we want CAA records on aliased domains?** Yes, and the DNS plan generates them — see D5.
  They are offered to the customer as defence in depth, never required: a domain with no CAA record
  works exactly as before.
