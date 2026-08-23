> **docs/ is for humans, not for AI.** Authoritative specification: [`../openspec/`](../openspec/).

# Install & configure

Getting the service itself running: the Compose stack, identity provider, configuration, first
sign-in, and the checks that prove it works before anyone depends on it.

**The platform is Docker Compose**, on a small dedicated VM with a static public IPv4 and IPv6
address reachable from the whole internet on UDP/53 and TCP/53, and nothing else bound to port 53.
One `compose.yaml` brings up the service and its database; every operational task below is a
`docker compose` command.

> **Configuration key names are as specified in the design and may shift during
> implementation.** If a name here disagrees with the running binary,
> `../openspec/changes/add-acme-alias-service/` and `acmeservice --help` are
> authoritative and this page needs correcting.

**On this page**

- [Before you start](#before-you-start)
- [1 · Deployment layout](#1-deployment-layout)
- [2 · Identity provider](#2-identity-provider)
- [3 · Configuration](#3-configuration)
- [4 · Run it](#4-run-it)
- [5 · Proxy the two hostnames](#5-proxy-the-two-hostnames)
- [6 · First sign-in](#6-first-sign-in)
- [7 · Post-deploy verification](#7-post-deploy-verification)
- [Configuration reference](#configuration-reference)
- [Operating it](#operating-it)

## Before you start

- [ ] A host with Docker Engine and the Compose plugin, and a dedicated static public IPv4 and
      IPv6 address — not shared with any other nameserver or resolver — with UDP/53 and TCP/53
      open to the whole internet. TCP/53 is not optional: a wildcard certificate needs two TXT
      values at one name, the answer is truncated, and the client retries over TCP.
- [ ] Nothing else on that host bound to port 53 — check with
      `ss -lnup 'sport = :53'`; on a systemd host this is usually
      `systemd-resolved`, which must be moved off the address or configured with
      `DNSStubListener=no`
- [ ] Keycloak, with a realm you can create a client in
- [ ] Two hostnames on our existing proxy, *not* inside the delegated zone
- [ ] The zone name agreed — the [delegation itself](dns-zone-setup.md) can come after
      this, and should be verified before any customer is onboarded

## 1 · Deployment layout

Everything lives in one directory on the host. The whole deployment is these three things:

```
/opt/acmeservice/
├── compose.yaml        # the stack, below
├── .env                # all configuration and secrets, mode 0600
└── data/
    └── postgres/       # database files, bind-mounted into the db container
```

```
install -d -m 0755 /opt/acmeservice/data/postgres
install -m 0600 /dev/null /opt/acmeservice/.env
```

> **Bind mounts under `data/`, not named volumes**
>
> The same convention the development stack uses. State sits in ordinary directories you can
> inspect, copy and delete with ordinary tools; a reset is `rm -rf data/postgres` rather
> than a `docker volume` incantation, and a backup is visible to whatever already backs up
> `/opt`.

### The database

PostgreSQL runs as a service in the same stack and is reachable only on the Compose network — no
published port. It creates the database and role from its environment on first start, so there is
no `createdb` step. No extensions are required.

> **`POSTGRES_*` only takes effect on an empty data directory**
>
> Changing the password in `.env` later will not change it in the database. To rotate
> it, change it in PostgreSQL with `ALTER ROLE` and then update
> `ACME_DATABASE_URL` to match.

**Using a managed PostgreSQL instead** is a two-line change: delete the `db`
service from `compose.yaml` and point `ACME_DATABASE_URL` at the managed
instance with `sslmode=require`. Nothing else differs.

> **Migrations run automatically at startup**
>
> Ordered SQL migrations are compiled into the binary and applied before any listener opens, under
> a PostgreSQL advisory lock so two instances starting at once cannot both migrate. If you prefer
> migrations as a separate step, set `ACME_MIGRATE_ON_START=false` and run
> `docker compose run --rm acmeservice acmeservice migrate up` yourself.

## 2 · Identity provider

The console has no local accounts. Everyone signs in through the IdP, and their permissions come
from the groups the IdP reports. What matters is that the ID token carries a
**groups claim**.

### Keycloak

1. Create a client, e.g. `acmeservice`. Client authentication **on**
   (confidential), standard flow enabled, direct access grants off.
2. Valid redirect URI:
   `https://acme-console.cloudpowered.services/api/v1/auth/callback`
3. Valid post-logout redirect URI:
   `https://acme-console.cloudpowered.services/`
4. Client scopes → dedicated scope → **Add mapper → By configuration → Group
   Membership**:
   - Token Claim Name: `groups`
   - Full group path: **Off** — otherwise every group arrives as
     `/parent/child` and the group names in `.env` have to match that
     shape
   - Add to ID token: **On**
5. Copy the client secret and the issuer URL
   (`https://idp.example/realms/<realm>`).

*Any OIDC provider that can put a plain groups claim on the ID token works in principle —
Authentik, for instance, needs only its built-in
`goauthentik.io/providers/oauth2/scope-groups` scope mapping — but Keycloak is the
supported, tested identity provider, and the rest of this page assumes it.*

> **Verify the claim before going further**
>
> Decode an ID token from a test user and confirm the `groups` claim is present and
> contains plain group names. Getting this wrong produces a working sign-in where nobody has any
> permissions, which is a confusing thing to debug from the console side.

## 3 · Configuration

Everything is environment variables, in `/opt/acmeservice/.env`. That one file does
double duty: Compose reads it for `${...}` interpolation in
`compose.yaml`, and passes it to the container as `env_file`. Mode
`0600` — it holds the database password and the OIDC client secret.

A minimal but complete production configuration:

```
# --- zone ---
ACME_ZONE=acmeservice.cloudpowered.services
ACME_SOA_PRIMARY_NS=ns1.cloudpowered.services
ACME_SOA_HOSTMASTER=hostmaster@cloudpowered.services
ACME_NS_NAMES=ns1.cloudpowered.services,ns2.cloudpowered.services

# --- listeners ---
ACME_DNS_LISTEN=:53
ACME_LISTEN=:8080
# One HTTP listener serves every path group (design P16): the management API,
# the staff tree, the challenge protocol, the console, and the health and
# metrics endpoints.
#
# The three-port split is gone, and with it the guarantee it carried — that a
# stolen API key reached no management route even if a routing rule in front of
# the service was wrong. The permission model carries that now: a key reaches
# only what its grants permit.
#
# What this moves onto YOU: /metrics and /api/admin/** are on the same port as
# the console. If they should not be reachable from outside, the proxy in front
# has to say so. Nothing below this line does it for you.

# --- database ---
ACME_DATABASE_URL=postgres://acmeservice:<pw>@db:5432/acmeservice?sslmode=disable
POSTGRES_PASSWORD=<the same password — used by the db service>

# --- trust and verification ---
ACME_TRUSTED_PROXY_CIDRS=198.51.100.20/32
ACME_VERIFY_RESOLVER=1.1.1.1:53

# --- identity ---
ACME_OIDC_ISSUER=https://idp.example/realms/main
ACME_OIDC_CLIENT_ID=acmeservice
ACME_OIDC_CLIENT_SECRET=<secret>
ACME_OIDC_REDIRECT_URL=https://acme-console.cloudpowered.services/api/v1/auth/callback
ACME_OIDC_GROUPS_CLAIM=groups

# --- roles: four fixed global roles, mapped from Keycloak groups ---
ACME_ADMIN_GROUP=infra-admins
ACME_CONFIG_ADMIN_GROUP=infra-config-admins
ACME_KEY_ROTATION_GROUP=infra-key-rotators
ACME_READ_ONLY_GROUP=support

ACME_LOG_LEVEL=info
```

> **`sslmode=disable` is correct here**
>
> The database connection never leaves the host — it runs over the Compose network between two
> containers. Requiring TLS would mean provisioning a certificate for `db` to no benefit.
> Use `sslmode=require` only when pointing at a database on another host.

> **`TRUSTED_PROXY_CIDRS` is a security setting, not a convenience**
>
> API keys can be restricted to source addresses, and the service sits behind a proxy — so the real
> client address comes from a forwarding header, which anybody able to reach the service directly can
> forge. This setting names the peers whose forwarding headers may be believed. Set it to the address
> of the proxy in front, nothing wider.
>
> The service **refuses to start** if any key carries an address restriction while
> this is empty, rather than silently degrading to "trust whatever the caller claims".

## 4 · Run it

`/opt/acmeservice/compose.yaml`:

```
name: acmeservice

services:
  db:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: acmeservice
      POSTGRES_USER: acmeservice
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD in .env}
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:                       # so acmeservice waits for a ready database
      test: ["CMD-SHELL", "pg_isready -U acmeservice -d acmeservice"]
      interval: 10s
      timeout: 5s
      retries: 5
    # no ports: — reachable only on the compose network

  acmeservice:
    image: registry.gitlab-mcww-admin.versioncontrol.io/tools/acmeservice:<tag>
    restart: unless-stopped
    depends_on:
      db:
        condition: service_healthy
    env_file: .env
    cap_add:
      - NET_BIND_SERVICE             # bind port 53 as an unprivileged user
    ports:
      - "203.0.113.53:53:53/udp"     # both protocols, on the dedicated address
      - "203.0.113.53:53:53/tcp"
      - "127.0.0.1:8080:8080"        # challenge API   — proxy reaches it locally
      - "127.0.0.1:8080:8080"        # everything      — proxy reaches it locally
    healthcheck:
      test: ["CMD", "acmeservice", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
```

```
cd /opt/acmeservice
docker compose up -d
docker compose ps
docker compose logs -f acmeservice
```

> **Pin the image tag**
>
> Use an explicit release tag, never `latest`. Upgrades should be a deliberate edit to
> `compose.yaml`, so that `docker compose up -d` after an unrelated change
> cannot silently move you onto a new version — including one whose migrations you have not read.

> **Block `/metrics` and `/api/admin/` at the proxy**
>
> These used to live on their own listener, bound to loopback, so no proxy
> misconfiguration could expose them. They do not any more: one listener serves
> every path group (design P16), and what was a topology guarantee is now a
> reverse-proxy rule you have to write.
>
> `/metrics` names your customers' projects and load balancers in its labels, and
> `/api/admin/` is the staff surface. Neither should be reachable from the
> internet. Deny both on the public virtual host and scrape metrics from the host
> itself over loopback.
>
> Note the port mappings above bind explicitly to `127.0.0.1` and to the dedicated
> public address. A bare `"53:53/udp"` would publish on every interface the host has,
> which on a multi-homed machine means answering from an address the delegation does not name.

On first start the log should show, in order: configuration loaded (secrets redacted), migrations
applied, zone view loaded, then three HTTP listeners and the DNS listeners opening. Any configuration
problem exits non-zero *before* a listener opens, naming the offending setting.

## 5 · Proxy the two hostnames

Terminate TLS on the existing proxy, as with everything else we run.

| Hostname | Backend | Who may reach it |
| --- | --- | --- |
| `acme-api.cloudpowered.services` | `127.0.0.1:8080` | Our load balancer node addresses only. Restrict at the proxy or the firewall. |
| `acme-console.cloudpowered.services` | `127.0.0.1:8080` | Office ranges and VPN. Block `/metrics` and `/api/admin/` here. |

> **These certificates cannot come from this service**
>
> Issuing them through acmeservice would be circular — the service would have to be reachable over
> HTTPS to obtain the certificate that makes it reachable over HTTPS. Since
> `cloudpowered.services` is a zone we administer, issue both the ordinary way, with
> whatever resolver the proxy already uses for our own domains.

## 6 · First sign-in

There is no role-binding table to seed. Permissions come from four fixed, **global**
roles — `ADMIN`, `CONFIG_ADMIN`, `KEY_ROTATION`, `READ_ONLY`
— each mapped to one Keycloak group by an environment variable you already set in
[step 3](#3-configuration):

```
ACME_ADMIN_GROUP=infra-admins
ACME_CONFIG_ADMIN_GROUP=infra-config-admins
ACME_KEY_ROTATION_GROUP=infra-key-rotators
ACME_READ_ONLY_GROUP=support
```

A user's roles are the union of every role whose configured group appears in their Keycloak groups
claim — holding several is normal and additive. There is nothing to bootstrap in the old sense: on
a database with no rows at all, the operator whose Keycloak account is in
`infra-admins` is already an `ADMIN`, because that is what
`ACME_ADMIN_GROUP` says, not because of anything stored. The problem the old bootstrap
group existed to solve — an empty bindings table meaning nobody, including you, has permissions —
is dissolved rather than solved: there is no table to be empty.

| | `ADMIN` | `CONFIG_ADMIN` | `KEY_ROTATION` | `READ_ONLY` |
| --- | --- | --- | --- | --- |
| create / delete projects | yes | no | no | no |
| load balancers, nodes, domains | yes | yes | no | no |
| API keys create/revoke | yes | yes | yes | no |
| see a key secret (once) | yes | yes | yes | no |
| DNS plan + run verification | yes | yes | yes | yes |
| view inventory + audit log | yes | yes | yes | yes |

Open `https://acme-console.cloudpowered.services/`. You are redirected to the IdP and
back. If you land on a page saying you match no role, it lists the groups it saw for your sign-in —
compare those against the four `ACME_*_GROUP` values in `.env`.

> **Removing someone's access is not instant**
>
> Group membership is read from the ID token **at sign-in**, not re-checked per
> request. Removing someone from a Keycloak group takes effect the next time they sign in, which is
> bounded by `ACME_SESSION_MAX_LIFETIME` — up to that long, they keep whatever roles their
> current session already has. Changing which group maps to which role is a `.env` edit
> plus a restart, same as any other configuration change; it does not touch existing sessions either.
> If access needs to end *now*, the lever is an `ADMIN` revoking the person's
> session from the console — sessions are server-side, so that takes effect immediately. See
> [the console guide](console-guide.md#settings) for where.

## 7 · Post-deploy verification

```
# the service is up and its dependencies are reachable
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz

# the zone answers authoritatively over BOTH transports
dig @203.0.113.53      acmeservice.cloudpowered.services SOA +norecurse
dig @203.0.113.53 +tcp acmeservice.cloudpowered.services SOA

# it is not an open resolver
dig @203.0.113.53 www.example.org A          # expect REFUSED

# the challenge API authenticates but is not exposed to the world
curl -si -u "k_xxx:$SECRET" -X POST https://acme-api.cloudpowered.services/present -d '{}'
                                              # expect 400 from an LB node

# health and metrics are NOT reachable from outside
curl -si https://acme-api.cloudpowered.services/metrics     # expect 404
curl -si https://acme-console.cloudpowered.services/metrics # expect 404
```

Then set up the monitoring that actually matters — see
[operating it](#operating-it) below.

## Configuration reference

### Zone

| Variable | Default | Meaning |
| --- | --- | --- |
| `ACME_ZONE` | *required* | The delegated zone this server is authoritative for. |
| `ACME_SOA_PRIMARY_NS` | *required* | Primary nameserver name in the SOA record. |
| `ACME_SOA_HOSTMASTER` | *required* | Contact address, written normally; encoded into the SOA form. |
| `ACME_NS_NAMES` | *required* | Comma-separated NS set served at the apex. Must match the parent zone. |
| `ACME_SOA_MINIMUM` | `30s` | Negative caching TTL. Keep low; see [why](dns-zone-setup.md#step-3-configure-the-zone-on-the-service). |
| `ACME_CHALLENGE_TTL` | `30s` | TTL on challenge TXT answers. |

### Listeners

| Variable | Default | Meaning |
| --- | --- | --- |
| `ACME_DNS_LISTEN` | `:53` | UDP and TCP. Bind to the dedicated address on a multi-homed host. |
| `ACME_LISTEN` | `:8080` | Every HTTP path group: management API, staff tree, challenge protocol, console, health, metrics. |

### Database and lifecycle

| Variable | Default | Meaning |
| --- | --- | --- |
| `ACME_DATABASE_URL` | *required* | PostgreSQL DSN. |
| `ACME_MIGRATE_ON_START` | `true` | Apply migrations before opening listeners. |
| `ACME_CHALLENGE_LIFETIME` | `1h` | How long an uncleaned challenge record survives. Far longer than an issuance needs, short enough that a crashed client leaves nothing lingering. |
| `ACME_VIEW_REFRESH_INTERVAL` | `30s` | Safety-net reload of the in-memory zone view. Writes update it immediately; this catches changes made behind the service's back. |
| `ACME_SWEEP_INTERVAL` | `5m` | Deletion of expired rows. Expired values stop being answered immediately regardless, so this is housekeeping, not correctness. |
| `ACME_ALIAS_USE_DOMAIN` | `false` | Derive each new alias label from the domain (`demo-example-com`) instead of generating a random one. Readable in a log line, but computable by anyone, so the served zone will answer "is this domain a customer?" for free — set it only for a deployment serving one organisation. It governs aliases created afterwards; an existing alias is never re-labelled, so decide before onboarding domains. |
| `ACME_SHUTDOWN_GRACE` | `15s` | Drain period on SIGTERM. |

### Trust, identity and sessions

| Variable | Default | Meaning |
| --- | --- | --- |
| `ACME_TRUSTED_PROXY_CIDRS` | *empty* | Peers whose forwarding headers are believed. Required if any API key is address-restricted. |
| `ACME_VERIFY_RESOLVER` | *system resolver* | Resolver used to check customer records. Prefer a public resolver over an internal one with a split-horizon view. |
| `ACME_OIDC_ISSUER` | *required* | Discovery base URL. |
| `ACME_OIDC_CLIENT_ID` / `_SECRET` | *required* | Confidential client credentials. |
| `ACME_OIDC_REDIRECT_URL` | *required* | Must exactly match what the IdP has registered. |
| `ACME_OIDC_GROUPS_CLAIM` | `groups` | Claim carrying group membership. |
| `ACME_OIDC_GROUP_PREFIX` | *empty* | Stripped from each group value — useful when the IdP emits path-style names. |
| `ACME_ADMIN_GROUP` | *required* | Keycloak group mapped to the global `ADMIN` role. |
| `ACME_CONFIG_ADMIN_GROUP` | *required* | Keycloak group mapped to the global `CONFIG_ADMIN` role. |
| `ACME_KEY_ROTATION_GROUP` | *required* | Keycloak group mapped to the global `KEY_ROTATION` role. |
| `ACME_READ_ONLY_GROUP` | *required* | Keycloak group mapped to the global `READ_ONLY` role. A user in none of the four configured groups has no access at all. |
| `ACME_SESSION_IDLE_TIMEOUT` | `8h` | Idle expiry. |
| `ACME_SESSION_MAX_LIFETIME` | `24h` | Absolute expiry. Also bounds how stale a user's group list can be. |
| `ACME_LOG_LEVEL` | `info` | `debug` logs each expiry sweep and every DNS decision. |

## Operating it

### What to alert on

| Signal | Why it matters |
| --- | --- |
| `/readyz` failing | Usually the database. Publishes fail; DNS keeps answering from the in-memory view, so this can be invisible from outside. |
| **External synthetic DNS query** against the zone apex, UDP *and* TCP | The single most valuable check. It is what catches a firewall change that nobody connected to certificates. |
| Authentication failure rate | A misconfigured load balancer after a key rotation, or someone probing. |
| Authorization refusals | A domain in Traefik that nobody added to the inventory. Benign, but it means a certificate is quietly not renewing. |
| DNS queries by response code | A rise in `NXDOMAIN` can mean an alias was deleted while a customer CNAME still points at it. |

> **The failure that matters is the slow one**
>
> Certificates renew about 30 days before they expire and clients retry for days, so an outage
> never takes a site down immediately. The dangerous case is an outage nobody notices for a month.
> Alerting exists to shorten that month, not to protect an SLA.

### Everyday commands

| Task | Command |
| --- | --- |
| Status | `docker compose ps` |
| Follow logs | `docker compose logs -f acmeservice` |
| Restart cleanly | `docker compose restart acmeservice` |
| Apply a config change | `docker compose up -d` after editing `.env` |
| Run a CLI subcommand | `docker compose exec acmeservice acmeservice adminctl …` |
| Open a database shell | `docker compose exec db psql -U acmeservice acmeservice` |

The doubled name in the `exec` line is not a typo — the first
`acmeservice` is the Compose service, the second is the binary inside it.

### Upgrades

```
cd /opt/acmeservice
# 1. take a backup first — see below
# 2. edit compose.yaml to the new tag
docker compose pull
docker compose up -d
docker compose logs -f acmeservice     # watch migrations apply, then listeners open
```

Migrations apply automatically under an advisory lock, and the service drains in-flight work on
`SIGTERM`. **A restart during an active issuance is safe**: challenge values
live in PostgreSQL precisely so that a redeploy at the wrong moment does not fail a renewal.

> **Rolling back is not symmetric**
>
> Down migrations exist but are never run automatically. An older binary started against a
> database migrated by a newer one exits with a diagnostic rather than querying a schema it does not
> understand — which is the safe behaviour, but it means a rollback across a schema change is a
> restore-from-backup operation, not a tag change. Read the migrations before upgrading.

### Backups

```
docker compose exec -T db pg_dump -U acmeservice acmeservice \
  | gzip > /var/backups/acmeservice-$(date +%F).sql.gz
```

Restoring into an empty stack:

```
docker compose up -d db
gunzip -c /var/backups/acmeservice-2026-08-20.sql.gz \
  | docker compose exec -T db psql -U acmeservice acmeservice
docker compose up -d
```

Nightly is plenty — the data changes only when somebody onboards a domain or rotates a key. Losing
the database means re-entering the inventory and re-issuing API keys; customer CNAME records remain
valid, so nothing has to be renegotiated externally.

---

3 · Install & configure — [← Per-load-balancer setup](load-balancer-setup.md) · Next: [Admin console guide →](console-guide.md)
