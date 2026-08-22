## Why

### The problem, told as a story

Traefik (our reverse proxy) obtains TLS certificates automatically from Let's Encrypt. To
prove we control a domain, Let's Encrypt offers a few **challenge types**. The one we care
about is **DNS-01**: Let's Encrypt hands us a random string (a "challenge token"), and we
prove control by publishing that string as a **TXT record** — a DNS record whose payload is
arbitrary text — at a well-known name under the domain.

Concretely, to get a certificate for `shop.example.com`, Traefik must make this record exist
and be visible to the whole internet:

```
_acme-challenge.shop.example.com.  TXT  "gfj9Xq...Rg85nM"
```

Let's Encrypt then queries public DNS for that exact name, sees the string it generated, and
issues the certificate. Then the record is deleted. The whole dance takes a minute or two and
repeats every ~60 days per domain.

The catch: **Traefik has to be able to write into the DNS zone of every single domain it
serves.** A "zone" here is the chunk of DNS a given nameserver is authoritative for — roughly,
everything under `example.com`. So Traefik needs API credentials for `example.com`'s DNS
provider, and for `customer-two.de`'s provider, and for `some-client.co.uk`'s provider. Those
credentials are usually **all-or-nothing**: a Cloudflare API token that can write
`_acme-challenge.shop.example.com` can generally also rewrite the customer's MX records and
point their apex A record at an attacker. We would be asking every customer to hand our proxy
the keys to their entire DNS presence, and we would be storing a growing pile of third-party
credentials on one host.

### The way out: CNAME aliasing

DNS has an indirection primitive: the **CNAME record**, which means "this name is an alias for
that other name; go look there instead." Let's Encrypt explicitly supports following a CNAME
when it looks up a challenge record. That gives us a one-time, permanent, harmless delegation.

The customer adds exactly one record to their zone, once, and never touches it again:

```
_acme-challenge.shop.example.com.  CNAME  shop-example-com.acmeservice.cloudpowered.services.
```

That record grants precisely one power: whoever controls
`shop-example-com.acmeservice.cloudpowered.services` can satisfy ACME challenges for
`shop.example.com`. It cannot touch mail, cannot repoint the website, cannot read anything.
From then on, when Let's Encrypt looks up `_acme-challenge.shop.example.com`, DNS forwards it
to **our** zone, and we answer.

Nothing today serves that zone. That is the first gap this change fills.

### The second problem: this is a fleet, not a single proxy

We do not run one Traefik. We run several **load balancers** — two Traefik nodes that
together front one environment, so either node can fail without taking the sites down — and
each load balancer belongs to a **project** (a customer engagement, or one of our own products) and
answers for a set of domain names. A realistic slice of the inventory:

```
project  acme-shop
  load balancer  "eu-prod"    nodes 203.0.113.10 / 203.0.113.11
      shop.example.com
      example.com
      *.example.com
  load balancer  "eu-staging" nodes 203.0.113.20 / 203.0.113.21
      *.staging.example.com

project  internal-tools
  load balancer  "tools"      nodes 198.51.100.5 / 198.51.100.6
      grafana.cloudpowered.services
      wiki.cloudpowered.services
```

Two things fall out of that picture, and neither is served by a challenge relay on its own.

**First, every one of those rows implies DNS records that somebody outside our control has to
create**, and getting them wrong is the single most common way an onboarding stalls. For
`shop.example.com` on the `eu-prod` load balancer, the customer's DNS administrator needs *two* kinds of
record: address records pointing the hostname at the load balancer's nodes, and the `_acme-challenge`
CNAME pointing at us.

```
shop.example.com.                  A      203.0.113.10
shop.example.com.                  A      203.0.113.11
_acme-challenge.shop.example.com.  CNAME  shop-example-com.acmeservice.cloudpowered.services.
```

Today that list lives in somebody's head and gets retyped into an email. It should be generated
from the inventory, per load balancer, ready to paste — and the service should be able to check
whether the customer actually created them.

**Second, the credential story needs the same structure.** One shared password for the whole
fleet means a compromise of any node yields certificates for every domain we serve. The natural
boundary is the load balancer: each load balancer gets its own credential, and that credential may only write
challenges for the domains that load balancer actually answers for.

### The third problem: managing this by hand does not scale

The first cut of this design managed aliases through a CLI subcommand and hand-written SQL.
That is fine for five domains and untenable for a hundred spread across projects with different
people responsible for them. Adding a domain, minting a credential for a new load balancer, checking
why a renewal failed, and telling a customer exactly which records to create are all routine
operations that should not require a shell on a production host — and should be attributable to
a named human, authenticated against the identity provider we already run, with permissions
derived from the groups they are already in.

### Why now

We are standing up new Traefik-fronted services under domains we do not administer, and the
current options are all bad: hand out DNS credentials, fall back to HTTP-01 challenges (which
cannot issue wildcard certificates and require public port 80 reachability for every host), or
issue certificates by hand. A small service we control removes the choice entirely — and if we
are building it anyway, building it as the system of record for "which load balancer answers for which
domain, and what DNS does that require" costs little more than building it as a bare relay.

## What Changes

This change creates the `acmeservice` application from scratch. The repository currently
contains only GitLab's default `README.md`; everything below is new.

The repository is laid out in three top-level directories — `backend/` (the Go module),
`frontend/` (the Angular workspace) and `development/` (a Docker Compose stack and scripts that
bring up everything needed to run the whole thing locally). The application is a single Go binary
plus an Angular single-page application compiled into it.
The binary listens on three surfaces, because it sits between three audiences that speak
different languages.

### Side A — the HTTP API that Traefik talks to

Traefik's ACME client library is [lego](https://go-acme.github.io/lego/), and lego ships a
generic DNS provider called **`httpreq`**. Rather than speaking Cloudflare's or Route53's API,
`httpreq` just POSTs the challenge to an HTTP endpoint of your choosing and expects `HTTP 200`
back. We implement that endpoint. The entire contract is two routes:

```
POST /present          → remember this TXT value, start answering DNS with it
  {"fqdn": "shop-example-com.acmeservice.cloudpowered.services.", "value": "gfj9Xq...Rg85nM"}

POST /cleanup          → forget it
  {"fqdn": "shop-example-com.acmeservice.cloudpowered.services.", "value": "gfj9Xq...Rg85nM"}
```

**Note whose name that is: ours.** lego resolves the customer's `_acme-challenge` CNAME *itself*,
before it calls us, and posts the name the CNAME points at — the alias in our own zone. It is not
sending `_acme-challenge.shop.example.com`; by the time the request reaches us the indirection has
already been followed.

An earlier version of this document claimed the opposite — that Traefik sends the customer's name
and "has no idea an alias exists" — and it is worth saying so plainly, because that version is the
intuitive one and somebody will re-derive it. It is wrong. In lego, the `httpreq` provider sends
`info.EffectiveFQDN`, and `EffectiveFQDN` is the result of `lookupCNAME(fqdn)` whenever CNAME
support is on, which is the default and which this whole design depends on. Verified in lego
v5.2.2 and observed in a live issuance.

So the service accepts **either** form, and treats both as equally normal:

- A name inside our zone (`<label>.<zone>`) is resolved **by alias label**.
- Any other name is resolved by challenge name.

Authorization, normalization and responses are identical either way. Two forms are needed because
the behaviour depends on the client, not on anything being wrong: lego with CNAME support turned
off sends the un-aliased name every time for a perfectly healthy domain, and other ACME clients
differ — some resolve the alias themselves, some deliberately leave that to the DNS provider. We
do not control which client a load balancer runs, so the endpoint takes both and infers nothing
from which one it got. (Which form a load balancer sends is recorded as a log field and a metric label,
because "what convention does this client use" is a useful thing to know when debugging one load balancer.
Whether a customer's CNAME actually exists is answered properly by the DNS plan's verification
check, which resolves the record directly.)

Authentication is by **API key**, issued per load balancer, and — optionally — usable only from
specific source addresses. An API key is a long random secret we generate, show once, and store
only as a hash; "optionally scoped to certain IP addresses" means each key can carry a list of
**CIDR blocks** (a CIDR like `203.0.113.10/32` is the standard way to write "this exact
address", and `203.0.113.0/24` means "these 256 addresses"), and a request presenting the key
from outside that list is rejected as if the key were wrong. For the `eu-prod` load balancer above, the
list is its two node addresses, so a leaked key is worthless anywhere else on the internet.

Because lego's `httpreq` provider has no way to set a custom header, the key rides in ordinary
HTTP Basic authentication — key identifier as the username, secret as the password. That is a
transport detail, not a second auth scheme; `design.md` D3 works through why.

Traefik's side of the configuration is three environment variables and one line of static
config — no per-customer DNS credentials anywhere:

```yaml
# traefik static config
certificatesResolvers:
  letsencrypt:
    acme:
      dnsChallenge:
        provider: httpreq
```
```
HTTPREQ_ENDPOINT=https://acme-api.cloudpowered.services/
HTTPREQ_USERNAME=k_7f3a91c4                  # the key's identifier
HTTPREQ_PASSWORD=<the secret, shown once>
```

Note that the endpoint hostname is `acme-api.cloudpowered.services`, in the **parent** zone — not
`acmeservice.cloudpowered.services`, the zone we are about to delegate to our own DNS server. Our
server answers TXT, SOA and NS and nothing else, so a name inside the delegated zone has no address
record and would simply not resolve. `design.md` D9 spells the constraint out.

### Side B — the authoritative DNS server that Let's Encrypt talks to

"Authoritative" means this server is the final word on the contents of a zone, as opposed to a
resolver that caches answers from elsewhere. We delegate the zone
`acmeservice.cloudpowered.services` to this service (by publishing NS records for it in the
parent `cloudpowered.services` zone), and the service answers DNS queries for that zone
directly on UDP and TCP port 53. When Let's Encrypt's validation servers follow the customer's
CNAME and ask us for `shop-example-com.acmeservice.cloudpowered.services TXT`, we reply with the
value Traefik just posted.

### Side C — the web UI that humans talk to

An Angular application, compiled at build time and embedded in the Go binary, backed by a REST
API on the same listener. It is the management surface for everything above:

- **Inventory**: projects, their load balancers (with each load balancer's node addresses), and the domains
  each load balancer answers for.
- **DNS plan**: for a selected load balancer, the complete list of records the outside world must
  publish — address records for each domain pointing at the load balancer's nodes, and one
  `_acme-challenge` CNAME per challenge name — renderable as a zone-file snippet to send to
  whoever administers that domain, with a **live check** that resolves each record and reports
  whether it exists, is missing, or points somewhere unexpected.
- **Credentials**: create, list, rotate, restrict by CIDR, and revoke the API keys used by LB
  loadBalancers. The secret is displayed exactly once, at creation.
- **Observability for the flow that is otherwise invisible**: which challenge values are live
  right now, and a log of every publish, withdrawal and refusal, with the account that caused
  it.

Sign-in is **OAuth 2.0 / OpenID Connect** against **Keycloak**, the identity provider we already
run. There are no local passwords. The token Keycloak issues carries a **groups claim** — a list
of the groups the person belongs to, e.g. `["acme-config-admins"]` — and the service maps those
group names onto four fixed **roles**, all of them global:

| Role | May do |
| --- | --- |
| `ADMIN` | everything, including creating and deleting projects |
| `CONFIG_ADMIN` | manage load balancers, nodes, domains and API keys; not projects |
| `KEY_ROTATION` | create and revoke API keys, and nothing else that changes state |
| `READ_ONLY` | read the inventory, the DNS plan, verification results and the audit log |

Everyone can read; the three write roles differ only in how much they may change, and holding
several at once is normal and additive.

**The mapping is configuration, not data** — one environment variable per role, naming the
Keycloak group that grants it:

```
ACME_ADMIN_GROUP=foobar-keycloak-admin-group
ACME_CONFIG_ADMIN_GROUP=foobar-keycloak-config-admin-group
ACME_KEY_ROTATION_GROUP=foobar-keycloak-key-rotation-group
ACME_READ_ONLY_GROUP=foobar-keycloak-read-only-group
```

so there is no table of role assignments to administer, no screen to administer it with, and no
empty-database bootstrap problem — `ACME_ADMIN_GROUP` is the bootstrap, on every start. Who is in
each group stays in Keycloak, where it is already governed. The cost of that simplicity is
recorded plainly in `design.md` D10: because groups are read from the token at sign-in, removing
someone from a group takes effect at their next sign-in, so ending access *now* means removing
them from the group **and** revoking their sessions.

### Wildcards

A wildcard certificate for `*.example.com` covers every single-label host under `example.com`
in one certificate, and **can only be obtained through DNS-01** — HTTP-01 cannot issue
wildcards at all. It also folds the onboarding problem down to almost nothing, because of a
detail of the ACME protocol worth stating plainly: the challenge name for `*.example.com` is

```
_acme-challenge.example.com          (the "*." is stripped — not _acme-challenge.*.example.com)
```

so **one** CNAME covers the wildcard and any number of hosts beneath it, instead of one CNAME
per hostname. Two consequences the design has to handle, both covered in `design.md` D4 and D6:

- `example.com` and `*.example.com` produce the *same* challenge name, and a certificate
  covering both makes Let's Encrypt issue two authorizations validated at that one name. Both
  TXT values must be answerable **simultaneously**, and cleaning up one must not remove the
  other. Multiple concurrent values per name is therefore a correctness requirement, not a
  nicety.
- Several domain rows can share one alias. The alias is keyed by challenge name, not by domain.

### Putting it together

One certificate issuance, end to end:

```
1. lego (inside Traefik) looks up  _acme-challenge.shop.example.com  and finds the customer's
               CNAME to  shop-example-com.acmeservice.cloudpowered.services.
2. Traefik  →  POST /present {fqdn: "shop-example-com.acmeservice.cloudpowered.services.",
               value: "gfj9Xq..."}  — the alias, already resolved — authenticated with the
               eu-prod load balancer's API key, from 203.0.113.10
3. acmeservice: key valid, source address allowed, that alias belongs to that load balancer
               → stores the value, answerable in DNS before the response is written, 200 OK
4. lego's own propagation check queries our authoritative nameserver directly until it sees it
5. Let's Encrypt asks public DNS for  _acme-challenge.shop.example.com TXT
6. example.com's nameserver answers:  CNAME shop-example-com.acmeservice.cloudpowered.services.
7. Let's Encrypt follows it, lands on us, asks for that name's TXT — over TCP, in practice
8. acmeservice answers:  TXT "gfj9Xq..."
9. Certificate issued. Traefik  →  POST /cleanup, the value is deleted.
```

### The concrete deliverables

- **New Go module**, built as a single binary and a container image, with a GitLab CI pipeline
  for lint, test, build and publish.
- **`httpreq`-compatible HTTP API** (`POST /present`, `POST /cleanup`) authenticated by API key
  with optional per-key CIDR restriction.
- **Inventory**: projects → load balancers → domains, with aliases derived per challenge name, and
  the alias membership doubling as the authorization boundary for challenge writes.
- **DNS plan generation and verification** per load balancer: the records the customer must publish —
  address records, the `_acme-challenge` CNAMEs, and CAA records naming the certificate authority
  we use — and a resolver-backed check of whether they did.
- **CAA records as optional defence in depth.** A CAA record names which certificate authorities
  may issue for a domain; publishing one means that even if our own authorization is defeated —
  a stolen key, a wrong inventory row — no other CA will issue for that name. We generate the
  records and ask for them; we never require them, because a domain with no CAA record works
  exactly as it does today.
- **Authoritative DNS server** on UDP/TCP 53 for `acmeservice.cloudpowered.services`, serving
  the stored TXT records plus the zone's own housekeeping records (SOA, NS).
- **Wildcard support** end to end: validation, challenge-name derivation, shared aliases, and
  concurrent values at one name.
- **Angular admin console** embedded in the binary, with OIDC login and group-to-role mapping.
- **PostgreSQL persistence** through GORM, with schema owned by golang-migrate migrations.
- **Operational surface**: health and readiness endpoints, structured logs, an audit trail,
  Prometheus metrics, expiry of stale challenge records, graceful shutdown.
- **A full acceptance environment** — described next — that exercises the real thing. This is the
  one deliverable in the list that this change specified and did **not** build: it is now specified
  and built by the change `add-acceptance-environment`, which holds its requirements and its
  sixteen tasks. The reasoning below is why it exists and is still current.

### The acceptance environment

> **Moved.** The `acceptance-harness` capability and every task that builds this environment now
> live in the change `add-acceptance-environment`. Nothing described here was built by this change.
> The description is kept because it is the argument for why the environment is worth having, and
> because `design.md` D14 — the environment's architecture, still in this document and still cited
> by number from D2, D6, D7 and D13 — assumes it.

The interaction between ACME, CNAME following, propagation checks and a real proxy is exactly
where the bugs will be, and none of it is reachable from a unit test. So the change includes a
Docker Compose environment in which the whole dance runs for real, against fakes for the three
parties we cannot use in a test:

- **A fake ACME certificate authority**: [pebble](https://github.com/letsencrypt/pebble),
  Let's Encrypt's own test server, standing in for the real CA. It performs genuine DNS-01
  validation — it really does look up the TXT record — but issues from a throwaway CA and has
  no rate limits.
- **A fake "public" DNS**: a CoreDNS instance that is authoritative for test domains
  (`example.test`, `wild.test`) and serves the customer-side `_acme-challenge` CNAMEs pointing
  into our zone, plus the delegation to our own DNS server. It plays the part of the internet's
  nameservers for every participant in the test.
- **A fake certificate chain**: pebble's generated root and intermediate, which the test client
  trusts, so "the certificate is valid" is actually verified rather than assumed.
- **A real Traefik**, unmodified, in its real configuration with `provider: httpreq` pointed at
  our service — not a stub, because the point of the test is that Traefik's behaviour is
  correct without any patching. What the design actually requires is that the lego version Traefik
  bundles follows the customer's CNAME by default — **Traefik 2.11 or newer** satisfies that, and
  `design.md` D2 explains why the requirement is stated in terms of lego rather than Traefik.

The test asserts what a human would check: that `https://shop.example.test/` serves a
certificate chaining to the test CA with the right names on it, that a wildcard certificate for
`*.wild.test` is issued with two TXT values coexisting at one challenge name, that `/present`
and `/cleanup` were both called, and that no challenge rows survive the issuance.

The environment also runs a real identity provider, and the suite signs in against it — over plain
HTTP, with a cookie jar rather than a browser — to check the half of the system the certificate
assertions never touch: that a person's Keycloak groups arrive in the shape the mapper produces, and
that the roles they map to are actually enforced by the server. A user in a viewer group is
refused a change that a user in an admin group is allowed.

### Not in scope

Deliberately, to keep the first release shippable: DNSSEC signing of our zone; zone transfers
or any dynamic-update protocol; support for ACME clients other than Traefik/lego; challenge
types other than DNS-01; automatic creation of records in customers' zones (we generate and
verify the list, we do not push it); more than one running instance of the service; and
self-service onboarding by customers themselves.

### What we considered and rejected

- **Run [acme-dns](https://github.com/joohoi/acme-dns) instead of building anything.** It is
  the closest existing product and Traefik has a built-in `acme-dns` provider. Rejected
  because its model is "each domain self-registers and gets a random subdomain and its own
  credentials," which means Traefik must persist a per-domain credential file and the aliases
  are unreadable random UUIDs. It has no notion of projects, load balancers or roles, so the entire
  management half of this change would sit beside it in a second system, duplicating its data.
  It also has no active maintenance story.
- **The `rfc2136` provider (dynamic DNS UPDATE with TSIG keys).** The most "native" option —
  Traefik would speak DNS itself to update the zone. Rejected because it means implementing the
  DNS UPDATE opcode and TSIG message authentication ourselves, or running a full nameserver
  alongside our service and syncing state into it. Much more surface area for the same result,
  and it gives us no place to hang per-load-balancer authorization.
- **Pushing records into an external nameserver (PowerDNS, BIND, Route53) via its API instead
  of serving DNS ourselves.** Rejected because it adds a second system to operate and a
  propagation delay between "Traefik posted the challenge" and "the record is visible," which
  is exactly the window that makes DNS-01 flaky. Serving from our own store makes the record
  live the instant the API call returns.
- **HTTP-01 challenges.** Rejected: cannot issue wildcard certificates at all, and requires
  every certificate's hostname to be publicly reachable on port 80 at issuance time, which is
  not true for internal services.
- **In-memory challenge storage.** Rejected because a deploy or crash mid-issuance would
  silently fail renewals, and because inventory, credentials and role mappings need durable
  storage anyway — once Postgres is in the picture, keeping challenges there too costs nothing.
- **A CLI-only management surface** (the previous shape of this change). Rejected once the
  inventory grew past a flat list of aliases: the DNS plan, the "did the customer create the
  record" check, and per-project delegation of who may change what are all things a table of
  rows in `psql` cannot express, and every action would be attributable only to whoever had a
  shell. A CLI is retained for bootstrap and emergencies, not as the primary interface.
- **Local user accounts in the web UI.** Rejected: another password store to protect, another
  offboarding process to forget. Group membership already lives in Keycloak.

## Capabilities

### New Capabilities

- `acme-challenge-api`: The `httpreq`-compatible HTTP interface Traefik uses to publish and
  withdraw DNS-01 challenge values — request/response shapes, authorization against the
  inventory, idempotency, wildcard co-existence, and error semantics.
- `api-credentials`: API keys as the credential load balancers authenticate with — issuance, hashing,
  one-time display, scope to a single load balancer, optional source-address restriction by CIDR,
  rotation, expiry and revocation.
- `challenge-dns-server`: The authoritative DNS server for
  `acmeservice.cloudpowered.services` — which query types and names it answers, how stored
  challenge values become TXT responses, zone housekeeping records (SOA/NS), and behaviour for
  unknown names.
- `service-inventory`: Projects, load balancers and domains; wildcard validation; derivation of
  challenge names and aliases; the generated DNS plan per load balancer and the live verification of
  it.
- `challenge-record-store`: Durable storage and lifecycle of challenge TXT values — creation,
  multiple concurrent values for one name, deletion on cleanup, and automatic expiry of records
  that were never cleaned up.
- `identity-and-roles`: OIDC sign-in against Keycloak, session handling, the configured mapping
  from groups to the four global roles, and the permission matrix those roles imply.
- `admin-console`: The Angular web application — its screens, how it is built and served, and
  the guarantees it must uphold (secret shown once, no privileged action without a role).
- `service-operations`: Cross-cutting runtime concerns — configuration, listener separation,
  database schema ownership and migration, health and readiness endpoints, structured logging,
  the audit trail, metrics, and graceful shutdown.

`acceptance-harness` — the end-to-end environment, and the properties an acceptance run must
assert — was originally the ninth capability here. It moved, unchanged, to
`add-acceptance-environment`, because it was the one part of this change nobody built and archiving
this change would have published a baseline describing a harness that does not exist.

### Modified Capabilities

None. `openspec/specs/` is currently empty; this is the first change in this repository.

## Impact

**New code.** A Go module and an Angular application in this repository, which currently
contains only GitLab's template `README.md`.

**New dependencies.** `github.com/miekg/dns` for the authoritative server; GORM with its
PostgreSQL driver and `golang-migrate` for schema; `echo` as the HTTP server; `go-oidc` and
`oauth2` for sign-in; Prometheus client; Angular and its build toolchain. All chosen concretely
in `design.md`, which also records the ones that were dropped from the earlier draft and why.

**New build requirements.** The CI image now needs Node.js as well as Go, because the Angular
bundle is compiled and embedded during the Go build. The `acceptance` CI stage created here
additionally needs Docker Compose; what it runs is built by `add-acceptance-environment`.

**New infrastructure.**
- A small dedicated VM with static public IPv4 and IPv6 addresses, running the service and its PostgreSQL as a
  two-service Docker Compose stack. It must bind UDP and TCP port 53 on that address and be
  reachable from the public internet on both — a harder requirement than a normal HTTP service,
  and the main operational risk in this change. Kubernetes is deferred rather than rejected;
  `design.md` D15 records the topology and the reasoning.
- NS delegation records for `acmeservice.cloudpowered.services` published in the parent
  `cloudpowered.services` zone — our own zone, so this is a self-service change — plus A **and**
  AAAA glue pointing at the service's two public addresses. The service is dual-stack, and the
  generated DNS plan emits AAAA records alongside A records for every LB node that has one.
- A public HTTPS endpoint for the challenge API (`acme-api.cloudpowered.services`) and a second
  hostname for the admin console (`acme-console.cloudpowered.services`). Both live in the parent
  zone, never inside the delegated one, which has no address records at all. Their certificates
  cannot be obtained through this service (circular dependency), so they are issued separately.
- An OIDC client registered in Keycloak, with a Group Membership mapper, the four groups, and
  the console's redirect URI.

**Changed systems.** Traefik's static configuration gains an `httpreq` DNS challenge resolver
and three environment variables per load balancer. Existing certificate resolvers keep working
unchanged; domains migrate to the new resolver one at a time as their CNAME is added.

**Per-domain onboarding.** Each domain still needs records added to its zone by whoever
administers it — but the list is now generated, copyable and verifiable rather than
hand-assembled, and a wildcard collapses a whole subtree to a single CNAME.

**Security posture.** Net improvement — third-party DNS credentials disappear from our proxies,
and the blast radius of any one credential shrinks from "every domain we serve" to "the domains
of one load balancer, from that load balancer's own addresses". But it concentrates a new risk: whoever holds
`admin` in the console, or controls the zone, can obtain certificates for every domain in the
inventory. The Keycloak group named by `ACME_ADMIN_GROUP`, and the zone's integrity, become high-value
assets, and every privileged action is written to an audit trail for that reason.
