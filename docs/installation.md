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

The console has no local accounts. Everyone signs in through the IdP.
**Groups no longer grant anything**: what a person may do is decided by their membership of an
account, which is data this service owns. The groups claim is still read and recorded — on the
session, and beside the roles in every audit entry — because what the IdP said about a person is
worth keeping, but no configuration turns a group into a permission.

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
     `/parent/child`, which makes the recorded claim harder to read (it no
     longer breaks sign-in, since nothing is matched against it)
   - Add to ID token: **On**
5. Copy the client secret and the issuer URL
   (`https://idp.example/realms/<realm>`).

*Any OIDC provider that can put a plain groups claim on the ID token works in principle —
Authentik, for instance, needs only its built-in
`goauthentik.io/providers/oauth2/scope-groups` scope mapping — but Keycloak is the
supported, tested identity provider, and the rest of this page assumes it.*

> **The claim is a record, not a key**
>
> A missing or oddly shaped `groups` claim no longer stops anybody doing anything: it makes the
> audit trail less informative. This used to be the single most common cause of "sign-in works and
> nobody has permissions"; that failure mode no longer exists.

## 3 · Configuration

Everything below can be set as an environment variable, in
`/opt/acmeservice/.env`. That one file does double duty: Compose reads it for
`${...}` interpolation in `compose.yaml`, and passes it to the container as
`env_file`. Mode `0600` — it holds the database password and the OIDC client
secret.

Environment variables are shown here because they are the shortest way to get a
deployment running and because they take precedence over everything else. Once
more than one environment exists, most of these belong in a `config.yaml` and a
per-environment overlay instead, with only the secrets left here — see
[the configuration reference](#configuration-reference) for the equivalent path
of every variable.

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

There is no role-binding table to seed and no group-to-role configuration. Roles are **rows inside
an account**, and every new account is created with four of them — `ADMIN`, `CONFIG_ADMIN`,
`KEY_ROTATION`, `READ_ONLY` — carrying exactly the permissions the four fixed roles of earlier
versions carried. What is new is that they can be edited, and that you can add others.

### Creating the first account

A fresh database has **no accounts**. Earlier versions seeded one at migration time and derived
membership from Keycloak groups; both are gone. Make the first one from the command line:

```
docker compose run --rm acmeservice \
  acmeservice adminctl account create \
    --slug infra --name "Infrastructure" --owner <idp-subject> --credits 25
```

`--owner` is the **identity-provider subject** — the `sub` claim of the ID token, not an email
address unless your provider is configured to use one. If you do not know it, sign in to the console
once first: the sign-in creates the user row, and the subject is then visible in the audit trail and
on `GET /api/v1/me`.

That one command creates the account, funds it, seeds the four roles, and makes the named person its
first unrestricted member.

`--credits` is the opening balance, and there is no zero-credit form: a **team account works only
while its balance is above zero**, so one created empty would be suspended from the moment it
existed and its owner would sign in to the pay screen of an account they had just made. It defaults
to 25. In the console the same act is a purchase — creating a team account and paying for it are one
request — and this flag is what stands in for that on a deployment where nobody can reach the
console yet.

Nobody needs this command to get *somewhere to work*. Every user is given a **personal account** the
first time they sign in: free, limited to a handful of projects, holding no members but themselves.
The command above is for the first *shared* account. Everything `adminctl` does afterwards names the account it acts in:

```
acmeservice adminctl --account infra project create --slug shop --name "Shop"
```

or set `ACME_ADMINCTL_ACCOUNT=infra` once. There is no deployment-wide account to fall back on,
and a verb without one is refused rather than guessing.

### What the seeded roles confer

| | `ADMIN` | `CONFIG_ADMIN` | `KEY_ROTATION` | `READ_ONLY` |
| --- | --- | --- | --- | --- |
| create / delete projects | yes | no | no | no |
| load balancers, nodes, domains | yes | yes | no | no |
| API keys create/revoke | yes | yes | yes | no |
| see a key secret (once) | yes | yes | yes | no |
| DNS plan + run verification | yes | yes | yes | yes |
| view inventory + audit log | yes | yes | yes | yes |

`ADMIN` is the account's **unrestricted** role and is not a list of permissions at all: it confers
every action on every object of that account, it cannot be edited or deleted, and the account must
always keep at least one member of it. That is what guarantees there is always a way back in — a
role whose authority could be edited away one row at a time would not.

Everything below `ADMIN` is a set of grants you can change. A grant is
`[object type, object, action]` plus a flag saying whether it reaches objects contained by the one
it names, and every grant is anchored to one account: a wildcard means "everything in this account",
never "everywhere". So a role that reads one project, or an API key that publishes challenges for a
whole project rather than one load balancer, is now expressible — the latter being the request that
prompted the model in the first place. The console's Roles screen composes them; the composer offers
only what the person using it already holds, because a subject may grant only what it holds.

Open `https://acme-console.cloudpowered.services/`. You are redirected to the IdP and back, and land
on the **account picker** rather than inside an account: a session begins with nothing selected, and
choosing is the first thing you do. Pick the account you were made a member of.

> **Adding the second person**
>
> Add them to a role from the console's Settings → Members screen, naming their identity-provider
> subject. They must have signed in at least once so the service knows the subject exists. There is
> no group to add them to.

> **Removing someone's access is not instant**
>
> Membership is re-checked when somebody SELECTS an account, not on every request. Removing a member
> takes effect the next time they switch accounts, bounded by `ACME_SESSION_MAX_LIFETIME`. If access
> needs to end sooner, remove the membership and have them sign out; a person can end their own
> sessions from the console, and ending somebody else's is a staff-plane operation that does not
> exist yet.

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

Configuration comes in layers. Lowest to highest:

1. the defaults compiled into the binary;
2. the base file, `config.yaml`, or whatever `ACME_CONFIG_FILE` names;
3. one overlay per name in `ACME_PROFILES`, applied in that order — `config.yaml`
   plus the profile `staging` means `config-staging.yaml`, beside the base;
4. the `ACME_*` environment.

**The environment is last and therefore wins.** A deployment configured entirely
by `ACME_*`, which is what every deployment looked like before the file existed,
behaves exactly as it always did — the tables below name the variable for every
setting, and setting one still overrides whatever a file said. Nothing has to
move.

**With one exception: an empty value means "unset", not "empty".**
`ACME_LOG_LEVEL=` is read as though the variable were not there at all, so the
environment can *replace* a value a file set but cannot *blank* it. This is
deliberate — Compose writes an empty string for any variable it cannot
interpolate, and the alternative silently turns `migrate_on_start` off and
`groups_claim` into nothing on a stack with a stray blank line. A setting you
want empty is emptied by removing it from the file, not by assigning nothing to
the variable. Files follow the same rule: a key that resolves to nothing (an
unset `${VAR}` with no fallback) is dropped rather than merged, so an overlay
mentioning a placeholder nobody filled in leaves the base file's value standing.

**Two spellings reach the same setting.** Every variable in the tables below is
the historical name and is what you should use. The loader also accepts the
variable formed from the path itself — `ACME_ZONE_NAME`, `ACME_LISTENERS_HTTP`,
`ACME_DATABASE_MIGRATE_ON_START` — because the mapping is derived from the
configuration structure rather than from a list. It is the same setting either
way; the historical name is the documented one, and a name matching neither is
ignored rather than guessed at.

**A missing overlay is fine; a missing base file that you configured is not.** An
overlay names a way this deployment differs, and most deployments differ in no
way at all. A base file you pointed `ACME_CONFIG_FILE` at and that is not there
means something is wrong with the deployment, and starting anyway would run on
defaults nobody chose. The service logs which layers it read, and which
configured files were absent, on its first few lines.

**Structures merge; lists replace.** An overlay that sets one value of a block
leaves the rest of the block alone. An overlay that sets `zone.ns_names` replaces
the whole list rather than appending to it — a merged nameserver set is a set
nobody wrote, and a merged `challenge.trusted_proxy_cidrs` is an allowlist
somebody widened by accident.

**Files can refer to the environment.** `"${ACME_LOG_LEVEL:info}"` is the
variable when it is set and `info` when it is not, which is how the shipped
`config.yaml` shows you every default in the file instead of hiding it in the
source. Write `${ACME_OIDC_CLIENT_SECRET}` with no fallback for a secret: unset,
it arrives empty and startup refuses it rather than coming up blank. Quote these
values — substitution happens before the YAML is parsed, so a secret containing
`: ` would otherwise break the document. Use **single** quotes: double quotes are
a YAML escape context, so a password containing `\t`, `\n`, `\\` or `\u` would be
altered without any error, where inside single quotes nothing is escaped and a
value carrying a `'` fails the parse loudly. `$${...}` is a literal
dollar-brace.

The shipped `config.yaml` is not copied into the image. Mount it if you want it:

```yaml
services:
  acmeservice:
    volumes:
      - ./config.yaml:/etc/acmeservice/config.yaml:ro
      - ./config-prod.yaml:/etc/acmeservice/config-prod.yaml:ro
    environment:
      ACME_CONFIG_FILE: /etc/acmeservice/config.yaml
      ACME_PROFILES: prod
```

### `zone` — what this server is authoritative for

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `zone.name` | `ACME_ZONE` | *required* | The delegated zone this server is authoritative for. |
| `zone.soa_primary_ns` | `ACME_SOA_PRIMARY_NS` | *required* | Primary nameserver name in the SOA record. |
| `zone.soa_hostmaster` | `ACME_SOA_HOSTMASTER` | *required* | Contact address, written normally; encoded into the SOA form. |
| `zone.ns_names` | `ACME_NS_NAMES` | *empty* | NS set served at the apex; must match the parent zone. A list in the file, comma-separated in the variable. |
| `zone.soa_serial` | `ACME_SOA_SERIAL` | `1` | Fixed, and deliberately not derived from data: publishing a challenge record must not bump it. |
| `zone.soa_refresh` | `ACME_SOA_REFRESH` | `1h` | SOA REFRESH. |
| `zone.soa_retry` | `ACME_SOA_RETRY` | `15m` | SOA RETRY. |
| `zone.soa_expire` | `ACME_SOA_EXPIRE` | `168h` | SOA EXPIRE. |
| `zone.soa_minimum` | `ACME_SOA_MINIMUM` | `30s` | Negative caching TTL. Keep low; see [why](dns-zone-setup.md#step-3-configure-the-zone-on-the-service). |
| `zone.caa_issuers` | `ACME_CAA_ISSUER` | *empty* | CA identifiers the published CAA records authorize, e.g. `letsencrypt.org`. Empty emits no CAA rows at all. |
| `zone.view_refresh_interval` | `ACME_VIEW_REFRESH_INTERVAL` | `30s` | Safety-net reload of the in-memory zone view. Writes update it immediately; this catches changes made behind the service's back. |

### `listeners`

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `listeners.dns` | `ACME_DNS_LISTEN` | `:53` | UDP and TCP. Bind to the dedicated address on a multi-homed host. |
| `listeners.http` | `ACME_LISTEN` | `:8080` | Every HTTP path group: management API, staff tree, challenge protocol, console, health, metrics. |
| `listeners.shutdown_grace` | `ACME_SHUTDOWN_GRACE` | `15s` | Drain period on SIGTERM. |

### `database`

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `database.url` | `ACME_DATABASE_URL` | *required* | PostgreSQL DSN. |
| `database.migrate_on_start` | `ACME_MIGRATE_ON_START` | `true` | Apply migrations before opening listeners. |
| `database.role` | `ACME_DB_ROLE` | *empty* | The unprivileged application role. Read by the bootstrap that creates it, not by the binary, which sees only the DSN above. |
| `database.password` | `ACME_DB_PASSWORD` | *empty* | Its password. Same: bootstrap only, and never given a fallback in a committed file. |

### `oidc` — the console's sign-in, and what it decides

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `oidc.issuer` | `ACME_OIDC_ISSUER` | *required* | Discovery base URL. |
| `oidc.client_id` | `ACME_OIDC_CLIENT_ID` | *required* | Confidential client id. |
| `oidc.client_secret` | `ACME_OIDC_CLIENT_SECRET` | *required* | Its secret. Never give this a fallback in a file. |
| `oidc.redirect_url` | `ACME_OIDC_REDIRECT_URL` | *required* | Must exactly match what the IdP has registered. |
| `oidc.groups_claim` | `ACME_OIDC_GROUPS_CLAIM` | `groups` | Claim carrying group membership. |
| `oidc.group_prefix` | `ACME_OIDC_GROUP_PREFIX` | *empty* | Stripped from each group value — useful when the IdP emits path-style names. |
| `oidc.post_logout_redirect_url` | `ACME_POST_LOGOUT_REDIRECT_URL` | `http://www.localhost` | Where sign-out lands. The console also ends the IdP session (RP-initiated logout), and the IdP must have this exact URL registered as a post-logout redirect URI. Empty keeps sign-out local and lands on `/`. |
| `oidc.session_idle_timeout` | `ACME_SESSION_IDLE_TIMEOUT` | `1h` | Idle expiry. |
| `oidc.session_max_lifetime` | `ACME_SESSION_MAX_LIFETIME` | `8h` | Absolute expiry. Also bounds how stale a user's group list can be. |

Each of the four group settings is **optional and defaults to empty, meaning
nobody holds that role** — startup does not refuse a deployment that has not
mapped them, it only warns. In practice you want all four: a user in none of the
configured groups has no access at all. A group naming a role that does not exist
grants nothing, and is reported at startup.

### `staff` — the second sign-in surface

One deployment, two hostnames, two applications at the identity provider, so a
token minted for one is not a token for the other. `staff.oidc` has the same
shape as `oidc` above, because it is the same idea; three of its fields are wired
today, and the staff plane shares the console's issuer and group mapping. The
other three — `staff.oidc.issuer`, `staff.oidc.groups_claim`,
`staff.oidc.group_prefix` — exist in the schema and are read by nothing: setting
one is accepted and has no effect.

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `staff.host` | `ACME_STAFF_HOST` | *empty* | The staff hostname. Empty means this deployment has no staff surface, and everything answers as the customer plane. |
| `staff.oidc.client_id` | `ACME_STAFF_OIDC_CLIENT_ID` | *empty* | The staff application's client id. |
| `staff.oidc.client_secret` | `ACME_STAFF_OIDC_CLIENT_SECRET` | *empty* | Its secret. |
| `staff.oidc.redirect_url` | `ACME_STAFF_OIDC_REDIRECT_URL` | *empty* | Its callback, on the staff hostname — the session cookie is host-only, so a callback landing on the wrong hostname sets the cookie for the wrong surface. |

### `challenge` — validation records and who is believed about them

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `challenge.ttl` | `ACME_CHALLENGE_TTL` | `30s` | TTL on challenge TXT answers. |
| `challenge.lifetime` | `ACME_CHALLENGE_LIFETIME` | `1h` | How long an uncleaned challenge record survives. Far longer than an issuance needs, short enough that a crashed client leaves nothing lingering. |
| `challenge.sweep_interval` | `ACME_SWEEP_INTERVAL` | `5m` | Deletion of expired rows. Expired values stop being answered immediately regardless, so this is housekeeping, not correctness. |
| `challenge.interaction_retention` | `ACME_INTERACTION_RETENTION` | `0s` | How long a closed interaction row is kept; `0` means forever. These rows hold a customer load balancer's client IP, so nothing is deleted until you say so. `4320h` — two Let's Encrypt renewal cycles — is the recommendation on record. |
| `challenge.activation_fresh_interval` | `ACME_ACTIVATION_FRESH_INTERVAL` | `5m` | How often a domain whose delegation has never been seen is re-checked. It is offline until one check sees it, and it is never given up on — somebody who adds a domain on Friday and publishes the CNAME on Monday is activated on Monday. |
| `challenge.activation_settled_interval` | `ACME_ACTIVATION_SETTLED_INTERVAL` | `24h` | How often an already-activated domain is re-checked. This only informs: a failed re-check never takes a domain offline, so the number is about how soon you would like to tell somebody their delegation broke, not how fast you must react. |
| `challenge.trusted_proxy_cidrs` | `ACME_TRUSTED_PROXY_CIDRS` | *empty* | Peers whose forwarding headers are believed. Required if any API key is address-restricted. A list in the file, comma-separated in the variable. |
| `challenge.verify_resolver` | `ACME_VERIFY_RESOLVER` | *system resolver* | Resolver used to check customer records, and **the thing that activates a domain at all**. Prefer a public resolver over an internal one with a split-horizon view — see the note below. |
| `challenge.alias_use_domain` | `ACME_ALIAS_USE_DOMAIN` | `false` | Derive each new alias label from the domain (`demo-example-com`) instead of generating a random one. Readable in a log line, but computable by anyone, so the served zone will answer "is this domain a customer?" for free — set it only for a deployment serving one organisation. It governs aliases created afterwards; an existing alias is never re-labelled, so decide before onboarding domains. |

**The resolver is not a tuning knob.** A domain is offline until a check has seen its delegation,
and this setting names the only thing that ever looks. With no resolver — unset, and no usable
`/etc/resolv.conf` in the container — the activation worker does not start, says so once at boot
(`domain activation checks are not running: no resolver to ask`), and **every domain added
afterwards is refused with `409` for ever**. Domains that existed before the upgrade are
unaffected: the migration activates them.

A split-horizon resolver fails the same way, quietly: it answers for the internal view of a
customer's name, sees no `_acme-challenge` `CNAME`, and the domain never activates while the
public internet has had the record all along. Point this at a public recursor.

### `mail` — who the deployment is when it sends

Two programs send mail to the same person about the same account: the identity provider about
signing in, and this service about the product. They use the **same** sender identity, configured
here and in the identity provider, because mail clients thread by sender — two spellings of you
arrive as two correspondents, and a customer looking for an invitation finds a conversation that
does not contain it.

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `mail.from` | `ACME_MAIL_FROM` | `no-reply@acmealias.com` | The address every message is sent as. The same value the identity provider is configured with. |
| `mail.sender` | `ACME_MAIL_SENDER` | `AcmeAlias` | The display name beside it. Also shared with the identity provider — `acmealias` and `AcmeAlias` are two correspondents to a mail client. |
| `mail.reply_to` | `ACME_MAIL_REPLY_TO` | `support@acmealias.com` | Where a reply goes, and **not** a no-reply address. Everything sent here — an invitation, a warning that an account is about to stop working, a notice that a delegation never appeared — is something a recipient may reasonably want to answer. |
| `mail.footer` | `ACME_MAIL_FOOTER` | `AcmeAlias\nsupport@acmealias.com` | The footer beneath every message, carrying whatever identifying detail the jurisdiction requires. One value for both senders, so the fifth template cannot quietly disagree with the first four. |
| `mail.smtp_addr` | `ACME_MAIL_SMTP_ADDR` | *empty* | The relay, as `host:port`. **Empty discards every message** — the service still starts and still serves, it simply sends nothing. In a container stack this is the mail service's own name (`mailpit:1025`), not the hostname its web interface is served on: that is HTTP and does not speak SMTP. |
| `mail.smtp_user` | `ACME_MAIL_SMTP_USER` | *empty* | Relay username. Empty means the relay wants no authentication, which is the development case. |
| `mail.smtp_password` | `ACME_MAIL_SMTP_PASSWORD` | *empty* | Relay password. Redacted in the startup log line and in `GET /api/v1/config`. |

An identity that is incomplete is a **warning at startup, not a refusal to start**: this service
exists to answer DNS and ACME challenges, and it would be wrong to take those down over a footer.
So read the boot log — `mail identity is incomplete` names the settings that are missing.

Configuring the sender is only half of it. Every host that sends for the domain must also be
authorised by the domain's SPF record and sign with a published DKIM key, and the result must
*align* with the address in `From`. That part is DNS work rather than configuration, and it fails
in a way nothing here can detect — see [Mail that is sent but never
arrives](troubleshooting.md#mail-that-is-sent-but-never-arrives).

### `limits` and `log`

| Path | Variable | Default | Meaning |
| --- | --- | --- | --- |
| `limits.allow_account_creation` | `ACME_ALLOW_ACCOUNT_CREATION` | `false` | Opens account creation to any authenticated person. Creating an account creates a tenant, so a deployment authenticating a whole organisation usually wants this off. |
| `limits.projects_per_personal_account` | `ACME_PROJECT_QUOTA` | `5` | How many projects one personal account may hold. Team accounts have no project quota; credit is their limit. Exceeding it is refused as a lack of room (409), not as a lack of authority (403). Zero or less turns the quota off. |
| `log.level` | `ACME_LOG_LEVEL` | `info` | `debug` logs each expiry sweep and every DNS decision. |

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
