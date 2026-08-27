# acmeservice

`acmeservice` lets our Traefik load balancers obtain Let's Encrypt certificates, via the DNS-01
challenge, for domains whose DNS we do not administer — without ever collecting a customer's DNS
credentials.

## The goal

**To be a piece of software that is ready to deploy as a commercial offering** — sold to and run for
paying customers who are not us — rather than an internal tool that could in principle be opened up
later.

That goal is why several things in this repository look heavier than the feature set appears to
justify, and it is worth naming them, because each one is far cheaper to build now than to retrofit:

- **Every row belongs to an account**, and reads are confined to it in the query itself rather than
  filtered afterwards. Multi-tenancy is not a mode that gets switched on; it is the shape of the
  schema.
- **Permissions are data, not four hard-coded roles.** A customer administers their own roles and
  grants, and nobody can grant more than they hold themselves.
- **Account membership is written by us, never inferred from the identity provider.** A compromised
  directory can create a user; it cannot place that user inside somebody's account.
- **Everything privileged is recorded** in an audit trail that names the acting account.

The service currently runs in a **single-account "internal" edition**, which is the same code with one
account in it. The path from here to strangers signing up — invitations, metering and credits, self
registration, payment, abuse limits — is planned, sequenced and mostly unbuilt. What that path is,
what order it goes in, and what triggers each step, is
[`openspec/changes/SEQUENCE.md`](openspec/changes/SEQUENCE.md); read it before starting work on
anything in it.

Two consequences worth stating plainly, since a reader will otherwise infer the wrong thing from the
paragraph above. The product is deliberately **narrower** than this repository once planned for: it
answers ACME DNS-01 challenges and observes the certificates that result, and it does *not* host
customers' DNS. And "ready to deploy commercially" is a description of the **architecture**, not a
claim about today's state — there is no payment integration, no self-registration and no customer
using it yet.

## The CNAME aliasing model, in five lines

1. Let's Encrypt validates a domain by asking for a TXT record at `_acme-challenge.<domain>`.
2. We don't control the customer's DNS, so we can't put that record there directly.
3. Instead the customer publishes one permanent CNAME: `_acme-challenge.<domain>` →
   `<alias>.acmeservice.cloudpowered.services`.
4. We do control `acmeservice.cloudpowered.services`, so Let's Encrypt's query follows the CNAME
   into our zone, and we answer it with the current challenge value.
5. Traefik tells us the value via an HTTP call (lego's `httpreq` provider) when a certificate is
   about to be issued or renewed; we publish it in DNS for the duration of that renewal, then
   remove it.

The customer sets up the CNAME once, ever. Every renewal after that is invisible to them.

### The alias label

`<alias>` is the label the service assigns when the domain is added, and nobody can supply one:
not the console, not the API, not the CLI. By default it is randomly generated — 16 characters of
lowercase base32, 80 bits, e.g. `k7m3q9x2r4tv8w1p` — so it cannot be computed from the domain.
That matters because our nameservers are public: a label anyone can derive turns the zone into a
membership oracle, where querying `shop-example-com.acmeservice.cloudpowered.services` answers
"is `shop.example.com` a customer?" for free.

Setting `ACME_ALIAS_USE_DOMAIN=true` derives the label from the challenge name instead —
`_acme-challenge.demo.example.com` → `demo-example-com` — which is legible in a log line, a zone
file and the interaction history, and is a reasonable trade for a deployment serving a single
organisation. It is not one for a deployment serving mutually untrusting customers.

Either way the setting governs only aliases created *afterwards*. An existing alias is never
re-labelled, because its label lives in a CNAME published in someone else's zone, and rewriting it
would break every renewal for that domain up to sixty days later. So make the choice before
onboarding domains; changing it later leaves a deployment with both kinds.

## Wildcard rules

A wildcard certificate for `*.example.com` and a certificate for the apex `example.com` both
validate at the *same* challenge name, `_acme-challenge.example.com` — the `*.` label is stripped
before the challenge name is formed. So `example.com` and `*.example.com` share one alias and one
customer-side CNAME; only the leftmost label may be `*`; a bare `*` is rejected; and a wildcard
whose remainder is a public suffix (e.g. `*.co.uk`) is rejected.

Because the pair is so nearly always wanted together, one action can create both: the console's
Add domain dialog offers a checkbox, and `adminctl domain create` a `--include-wildcard` flag,
that adds the typed name's counterpart — the wildcard of a plain name, the apex of a wildcard —
as a second domain against the same alias. It is opt-in at every entry point: a wildcard is never
added to a customer's inventory unless somebody asked for it.

## One listener, five path groups

There were three listeners, on three ports. They are now one (`ACME_LISTEN`, `:8080`), separated by
path prefix with per-group middleware.

| Path group | Serves | Exposure |
| --- | --- | --- |
| `/present`, `/cleanup`, `/acme/httpreq/**` | lego's `httpreq` contract; API-key auth | public, behind Traefik |
| `/api/v1/**` | the public, versioned, OpenAPI-described management API | behind Traefik |
| `/api/admin/**` | the staff tree — unversioned, undocumented, and empty until `staff-plane` | address-restricted at the ingress |
| `/healthz`, `/readyz`, `/metrics` | liveness, readiness, Prometheus | should not be public |
| `/*` | the Angular consoles, chosen by hostname | behind Traefik |

The old arrangement guaranteed isolation by topology: an API key could not reach the management API
because the management API was on a port nobody published. That guarantee now comes from the
permission model instead — a key reaches only what its grants permit. **The trade is explicit and
worth stating: network-level exposure control moves to the reverse proxy.** In particular `/metrics`
and `/api/admin/**` are on the same port as everything else, so a deployment that wants them
unreachable has to say so in front of the service rather than relying on an unpublished port.

Plus the authoritative DNS listener itself (UDP+TCP 53), necessarily raw and public, answering
TXT, SOA and NS only for the zone delegated to us.

## Repository layout

```
backend/     the Go module — DNS server, challenge/admin/internal HTTP APIs, migrations
frontend/    the Angular workspace, three projects: `console` (the customer surface),
             `admin` (the staff surface) and `shared` (what both reach — api, session,
             guards, models). Two applications rather than one with a staff section, so
             a customer's browser never downloads the staff bundle; `ng build` writes
             into backend/internal/webui/dist/{console,admin}, both embedded in the Go
             binary and chosen by hostname at request time.
             Built with ng-zorro (Ant Design), dark theme only, from its prebuilt
             stylesheet, and set in Barlow — both are npm dependencies, so nothing is
             fetched from a CDN at runtime. Every list filters and pages server-side,
             every screen opens with the same breadcrumb/headline/subheadline block, and
             every create or delete is a modal
site/        the marketing site: hand-written HTML and CSS under src/, built to a
             gitignored dist/ by a build that is a copy, served by busybox httpd
material/    brand source — the wordmark and logo the consoles and the site share
development/ everything needed to run the whole stack locally: compose file, dev-only
             Traefik overlay, the authentik blueprint, seed/reset scripts
```

## Prerequisites

- Go 1.24+
- Node 22 and npm (Angular's CLI needs Node ≥22.12 for its latest major version; if your Node is
  older than that, pin `@angular/cli` to a major version whose `engines.node` your Node
  satisfies — see `frontend/package.json`)
- Docker and Docker Compose, for the local dev stack — which is now all of it: PostgreSQL, the
  IdP, DNS, the Go service under `air` and the Angular dev server all run as containers. Go and
  Node on the host are only needed to run the test and lint scripts.
- [`golangci-lint`](https://golangci-lint.run) for `scripts/lint.sh` — likewise not installed by
  this repo; the script fails until you install it, which is the point: a missing linter should be
  loud, not silently skipped

## Starting the dev loop

Production embeds the compiled Angular bundle in the Go binary, so there is nothing to proxy.
Development runs the two halves as separate live-reloading processes and fronts them with a
third, dev-only Traefik instance at one origin, so the app always runs on a single origin the way
it does in production (session cookies are host-only):

```
./scripts/dev.sh up                # the whole stack, including both live-reloading halves
./scripts/dev.sh seed              # sample project, load balancer, wildcard domain, API key
./scripts/dev.sh watch             # up, then follow the api and web logs
```

Everything is reached by name on the standard ports — `*.localhost` resolves to loopback on the
host by itself (RFC 6761) and is answered inside the stack by CoreDNS, so one URL works from both
sides:

| URL | What |
| --- | --- |
| `http://acme-console.localhost` | the console, and its admin API under `/api` |
| `http://acme-api.localhost` | the same admin API on its own hostname — for `curl`, not the browser |
| `http://acme-challenge.localhost` | the ACME challenge API (lego's `httpreq`), for poking at `/present` by hand |
| `http://keycloak.localhost` | the IdP; `admin-user` / `devpass` signs in with the ADMIN role |
| `https://demo.example.com` (`:443`, via `--acme`) | the customer-like LB, on a real DNS-01 certificate |

The console must keep calling its **own** origin's `/api`: the session and CSRF cookies are
`SameSite=Lax` and host-only, and `localhost` is not a public suffix, so `acme-console.localhost`
and `acme-api.localhost` are two different *sites* as far as a browser is concerned. An XHR
between them would carry no session cookie, and no CORS header can put one back.

Each route is a Docker label on the service it belongs to (see `development/compose.yaml`); the
overlay reads them off the Docker socket, mounted read-only, so a service and the hostname it
answers on are one thing to read and one thing to change. Nothing else publishes an HTTP port any
more — `:80` and `:443` are the only way in, and both are bound on **all interfaces**, so another
machine on the (trusted) LAN can reach the stack once it has the hostnames in its own
`/etc/hosts`. The fake internet's resolver is on `:8053` on all interfaces for the same reason.

Everything runs in containers, including `air` and `ng serve` — the source is bind-mounted, so a
saved `.go` file rebuilds inside the `api` container and a saved component reloads in `web`. The
result is a working console with seed data at `http://acme-console.localhost`. `./scripts/dev.sh watch`
brings the stack up and then follows both logs; Ctrl-C detaches without stopping anything.

### Rehearsing a real certificate issuance locally

`./scripts/dev.sh up --acme` adds an opt-in profile: [pebble](https://github.com/letsencrypt/pebble)
as a throwaway CA, a second, customer-like Traefik that really orders a certificate for
`demo.example.com` and `*.demo.example.com` over DNS-01, and a `whoami` behind it.

The stack runs its own DNS (CoreDNS at a fixed address, always on), which is what makes that
possible without touching anything public:

- **`example.com` is hijacked** — reserved by RFC 2606, so this is safe — and serves the customer
  side of the seeded inventory, including the one `_acme-challenge` CNAME into our zone.
- **`dev.acmeservice.internal` is forwarded to the service itself**, so the CA resolves our
  aliases from the real code.
- **Everything else goes to a real upstream**, so containers still reach the internet.

`./scripts/dev.sh issue` drives one end to end: it discards the stored certificate (traefik-lb
keeps one for ~90 days, so restarting alone proves nothing), restarts the load balancer, waits
for the certificate on the wire to become one pebble signed, prints the `challenge_publish` /
`challenge_withdraw` pairs our own API served, and fetches the site through the result. That is
unmodified Traefik, real lego, real ACME — nothing stubbed.

### The acceptance suite

`./scripts/dev.sh issue` prints a certificate and a human decides whether it looks right.
`./scripts/test-acceptance.sh` is the version that fails a build. It is a Go test package behind
an `acceptance` build tag (`backend/acceptance/`, excluded from `go build ./...` and every
ordinary `go test`) that drives **this same development stack** — there is no second environment —
and asserts what no script asserts today: that the served certificate really chains to pebble's
own root *and* intermediate (Traefik's fallback when issuance fails is a self-signed certificate
of its own, so verifying is the whole point); that one certificate covers `demo.example.com` and
`*.demo.example.com` with both challenge values answerable at the one shared name simultaneously;
that `/present` and `/cleanup` both fired and the alias afterwards answers NOERROR-and-empty
rather than NXDOMAIN; that a key belonging to another load balancer is refused and a
CIDR-restricted key is enforced in both directions, forged `X-Forwarded-For` included; that
restarting the api container between publishing a challenge and its validation still yields a
certificate; that `adminctl loadbalancer dns demo/prod` matches the zone CoreDNS is serving,
record for record, on the challenge aliases; that all four seeded users' roles resolve through the
real Keycloak with the `CONFIG_ADMIN`/`KEY_ROTATION` boundary pinned in both directions; and that
the one TXT query answering two authorizations is credited to both of them.

**It costs minutes, not seconds.** The suite owns the stack: it runs `./scripts/dev.sh reset
--yes`, then `up --acme`, then `down` around itself, which is a full rebuild plus several real
ACME exchanges. While iterating, `ACCEPTANCE_KEEP_STACK=1 ./scripts/test-acceptance.sh` reuses
whatever is already running and skips both the reset and the teardown.

**CI does not run it** — the pipeline is lint, unit-test, build, publish, and declares no
acceptance stage on purpose (the reason is written where the job used to be, in `.gitlab-ci.yml`).
So this paragraph is the only thing standing between the suite and being forgotten: run it before
anything that touches the challenge API, the DNS server, the seed or the CoreDNS zones.

`./scripts/dev.sh status` prints whether the profile is up. Zone contents, and the reasons for the few dev-only fudges in them, are documented in
[`development/dns/Corefile`](development/dns/Corefile) and
[`development/dns/zones/example.com.zone.tmpl`](development/dns/zones/example.com.zone.tmpl) — the
template the served `example.com.zone` is rendered from. The stack runs the shipped default for
alias labels (randomly generated), so the `_acme-challenge` CNAME cannot be checked in: the seed
reads the label back from `adminctl loadbalancer dns demo/prod` and substitutes it. Every reset
issues against a different label.

There are no makefiles; everything is a bash script.

| Script | Does |
| --- | --- |
| `scripts/dev.sh <verb>` | the dev stack's front door — see the table below |
| `scripts/build.sh` | `ng build admin`, then `go build ./...` |
| `scripts/test.sh` | Go and frontend unit tests |
| `scripts/test-integration.sh` | database-backed tests (needs PostgreSQL; not run in CI) |
| `scripts/test-acceptance.sh` | the end-to-end acceptance suite (minutes; not run in CI) |
| `scripts/lint.sh` | `golangci-lint` |
Every dev-stack verb goes through `scripts/dev.sh`, which delegates to the script of the same
name under `development/scripts/` — that is where the work and the explanatory comments live.

| Command | Does |
| --- | --- |
| `./scripts/dev.sh up [--acme]` | create/start the stack, wait, migrate (`--acme` adds the fake CA) |
| `./scripts/dev.sh down` | stop it, keeping state |
| `./scripts/dev.sh restart [--acme]` | `down` then `up` — what picks up a `compose.yaml` or `.env` edit |
| `./scripts/dev.sh reset [--yes]` | destroy + up + seed — use after editing `realm/*.json` or a migration |
| `./scripts/dev.sh seed` | sample project, load balancer, wildcard domain, API key (idempotent) |
| `./scripts/dev.sh issue` | force a real DNS-01 issuance through the fake CA and verify it |
| `./scripts/dev.sh status` | what is running, URLs, test logins, whether it is seeded |
| `./scripts/dev.sh logs [svc...]` | follow logs; no argument means `api` and `web` |
| `./scripts/dev.sh destroy [--yes]` | stop it and wipe `development/data/` |
| `./scripts/dev.sh watch [--acme]` | `up`, then follow `api` + `web` — the working loop |

## Production rollout runbook

Full step-by-step instructions live in `docs/` (human-facing; `openspec/` is the authoritative
spec). This is the ordering that ties those pages together for a first production rollout — do
the steps in this order, not the order the docs are numbered in, and don't proceed to a step
until the previous one verifies green.

1. **Host prerequisites.** A static public IPv4 *and* IPv6 address, nothing else already bound to
   port 53 (usually `systemd-resolved` — needs `DNSStubListener=no` or moving off the address),
   and `./data/postgres/` created before first start. See [`docs/installation.md`](docs/installation.md),
   "Before you start" and step 1.
2. **Bring the stack up undelegated.** `docker compose up -d` with no delegation and no inventory
   yet, then verify the apex answers SOA authoritatively over UDP *and* forced TCP, *from each
   address* the delegation's glue will name — see [`docs/installation.md`](docs/installation.md)
   step 7 and [`docs/dns-zone-setup.md`](docs/dns-zone-setup.md) step 4 for the exact `dig`
   commands. Do this before the delegation exists: a mistake here affects nothing yet.
3. **Publish the delegation.** Two NS names, both resolving to the single instance, with A and
   AAAA glue for both — see [`docs/dns-zone-setup.md`](docs/dns-zone-setup.md) steps 1–2 for why
   two names are worth publishing even though they buy no redundancy (one server, two names).
   Publish the `acme-api` and `acme-console` A/AAAA records in the **parent** zone at the same time
   (they sit outside the delegated zone; this service serves no A records at all). Then verify:
   dual-stack SOA over UDP and TCP from outside, and — because our zone is deliberately
   unsigned — confirm `dig DS acmeservice.cloudpowered.services` returns an **empty** answer. Some
   registrar workflows add a DS record automatically on a new delegation, which is exactly the
   failure this check catches: a DS pointing at an unsigned child makes every validating resolver
   SERVFAIL the whole subtree while a direct `dig` at our server still looks fine. Full checklist
   in [`docs/dns-zone-setup.md`](docs/dns-zone-setup.md) step 4.
4. **Register the Keycloak client**, with a Group Membership mapper emitting a plain (non-path)
   `groups` claim, and set `ACME_ADMIN_GROUP`, `ACME_CONFIG_ADMIN_GROUP`,
   `ACME_KEY_ROTATION_GROUP`, `ACME_READ_ONLY_GROUP` to the groups it should map to roles from.
   Restart and sign in — there's no separate console bootstrap step. See
   [`docs/installation.md`](docs/installation.md) step 2 and step 6.
5. **Enter the existing fleet into the inventory** — projects, load balancers with node addresses, and
   the domains each already answers for. Read-only reality capture; nothing changes for those
   domains yet. See [`docs/load-balancer-setup.md`](docs/load-balancer-setup.md) step 1.
6. **Onboard one test domain**, publish its DNS plan, and use the console's own verification until
   every row is green before moving anything live. See [`docs/load-balancer-setup.md`](docs/load-balancer-setup.md)
   steps 2–4.
7. **Move one Traefik router** to the new certificate resolver and watch a full cycle: `/present`
   logged, propagation check passes, certificate issued, `/cleanup` logged. Then **roll out per
   domain**, each gated on its DNS plan verifying green — no deadline, no big-bang cutover.

   **If that load balancer runs Traefik 2.11**, do one extra check first, because nothing in this
   repository exercises it: the dev stack's customer-like proxy is `traefik:v3.1` and there is no
   version matrix. 2.11 is the supported floor only because of the lego it bundles, so confirm the
   bundled lego before trusting it —

   ```
   curl -s https://raw.githubusercontent.com/traefik/traefik/v2.11/go.mod | grep go-acme/lego
   ```

   — the one-liner from design D2 — and require **lego 4.9 or newer**, the first that follows the
   `_acme-challenge` CNAME by default (it has the `LEGO_DISABLE_CNAME_SUPPORT` opt-*out* rather
   than the older opt-in). That CNAME-following is the entire mechanism this service depends on; a
   lego below it asks the customer's own zone for the TXT value and never arrives here. Check the
   actual tag rather than reasoning from the Traefik version: the mapping is not monotonic —
   Traefik 2.11 bundles a *newer* lego than Traefik 3.0 does. Then watch **one full
   present -> validate -> cleanup cycle** on that load balancer, in our own api log
   (`challenge_publish` then `challenge_withdraw` for the same name) and not only in Traefik's,
   before moving any further domain onto it.

Per-domain onboarding procedure, the wildcard preference (one CNAME covers every hostname under a
domain — worth pushing for above two or three hostnames), the **Traefik 2.11-or-newer
requirement** (what matters is that the bundled lego follows CNAMEs by default, which every lego
from 4.9 onwards does — and Traefik 2.11 ships a newer lego than Traefik 3.0 does), why `LEGO_DISABLE_CNAME_SUPPORT` must never be set, and how to ask a customer
for the optional, defence-in-depth CAA records: see
[`docs/load-balancer-setup.md`](docs/load-balancer-setup.md).

**Rollback.** Per-domain: move the router back to its previous certificate resolver — already-issued
certificates are files in Traefik's own store and stay valid regardless. See
[`docs/load-balancer-setup.md`](docs/load-balancer-setup.md), "Rolling back". Rolling the *service* back to an
earlier image is only a tag change when the release being reverted carried no migration: an older
binary refuses to start against a schema a newer one has already migrated (on purpose — the
alternative is silent data corruption), so rolling back across a migration is a
restore-from-backup operation, not a redeploy. Take a database backup immediately before deploying
any release that carries a migration. See [`docs/installation.md`](docs/installation.md),
"Migrations run automatically at startup" and "Backups".

**Alerts to add** once live: readiness probe, DNS query failure rate, authentication failure rate,
and an external synthetic query against the zone apex over both UDP and TCP from outside the
network — the one that catches a firewall change nobody associates with certificates. Already
specified in full in
[`openspec/changes/archive/2026-08-21-add-acme-alias-service/design.md`](openspec/changes/archive/2026-08-21-add-acme-alias-service/design.md)
— nothing to add here.

## Usage reporting: what the counters mean

Two numbers, and they are not the same number.

| | what it counts | the wildcard case |
| --- | --- | --- |
| **challenge attempts** | every TXT value published — one row per `/present` | counts **2** |
| **issuance attempts** | publications at one challenge name whose lifetimes overlap, grouped | counts **1** |

A certificate covering `example.com` and `*.example.com` needs two DNS-01 authorizations, and the
CA answers both at the *same* challenge name. Traefik publishes two values seconds apart and
withdraws both when the certificate is issued. That is one attempt at getting a certificate and
two published values, so a report that counts rows tells every wildcard customer they renewed
twice as often as they did. Both numbers are in the API and both are labelled; the console shows
issuance attempts by default and offers challenge attempts beside them.

**Neither is a count of certificates, and nothing in this service is labelled that way.** The
certificate is finalized between the load balancer's ACME client and the CA, which happens after
our part is over and which we never observe. `validated` means "we published it and the CA came
and read it" — the strongest statement this service is in a position to make.

Each attempt carries one of five outcomes, derived at read time and never stored:

| outcome | means |
| --- | --- |
| `validated` | published, queried, withdrawn — the normal life |
| `unqueried` | published and withdrawn without ever being queried |
| `abandoned` | never cleaned up; the sweep closed it |
| `live` | still published, already queried |
| `pending` | still published, nobody has asked yet |

`unqueried` is the one worth watching. It means Traefik published a value and the CA never came
looking, which almost always means the customer's `_acme-challenge` CNAME is missing, wrong, or
was removed — the one failure that breaks the delegation this whole service depends on.

**Scope: per project and per load balancer. There is no per-domain report and none is planned.**
Project usage is on the project page and at `GET /api/v1/projects/{id}/usage` (required `from` and
`to`, `bucket=day|month`); inventory size over time is beside it and at
`GET /api/v1/projects/{id}/inventory-history`. The drill-down from a project-level signal is the
load balancer's interactions list — `GET /api/v1/loadbalancers/{id}/interactions?unqueried=true`,
or the "never queried only" switch on the load balancer screen — which names the alias and the
domains it served. Both reporting routes need only the permission that reads inventory, so every
role that can see a project can see its numbers, and neither writes anything.

### Rolling this out: the tables start empty and cannot be backfilled

`challenge_interactions` and `project_daily_stats` begin empty on the day they are migrated, and
**nothing can be put into them retroactively** — the events they describe were never recorded, and
inventory rows carry no deletion record, so nobody can say in April how big a project was in
March. Consequences to plan for:

- Reporting is only meaningful after a **full renewal cycle**, which for Let's Encrypt is around
  sixty days. Before that the charts are honest but sparse.
- A period with no recorded data is reported as **"no data recorded yet"**, never as a zero, in
  both the API (`"recorded": false`) and the console. A zero is a claim that nothing happened; on
  a service deployed last week it would be a claim nobody can substantiate, and a fresh
  deployment that reports zeros looks like a broken one.
- The inventory snapshot runs **hourly** and writes the current day's row, so the first data point
  appears within an hour of the first start and a restart at any time of day loses nothing. Each
  row is a *level* — "this project had 12 aliases" — so running it twice in a day overwrites
  rather than duplicating or double-counting; the last run of the day wins.

### Alerting on the never-queried rate — later, not at rollout

The alert worth defining is a **rising `unqueried` rate**: delegations are breaking. Two things
about it:

- **Wait for a baseline.** `unqueried` fires legitimately during onboarding — a challenge
  published while a customer's CNAME is still propagating genuinely was never queried. Enable the
  alert only after a full renewal cycle has been observed, so the threshold is chosen from real
  data rather than assumed. Alerting from day one produces noise, and a noisy alert on the only
  signal that catches a broken delegation is worse than no alert.
- **The metric says how many; the API says which.** `acmeservice_challenge_interactions_total{outcome="unqueried"}`
  carries no domain, alias, project or key label, and deliberately so: one time series per domain
  is exactly the cardinality growth that makes a Prometheus server unusable, and the series would
  outlive the domains. When the alert fires, the drill-down is
  `GET /api/v1/loadbalancers/{id}/interactions?unqueried=true`, which names the alias and its
  domains. There is no per-domain endpoint and none is coming.
