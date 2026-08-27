# Design

Decisions are numbered `K1`, `K2`, … and referred to by number from `tasks.md` and from code
comments, the way `P`-numbers from `tenancy-and-permissions` and `U`-numbers from `public-edition`
already are.

## K1. authentik replaces Keycloak in development, and the backend does not change

The backend is already provider-agnostic, and this is a fact about the code rather than a hope.
`backend/internal/config/config.go` takes an issuer URL, a client id, a client secret, a redirect
URL, a groups claim name and a group prefix. Nothing else. A search for `Keycloak` across `backend/`
finds six occurrences and every one of them is a comment.

So the swap is confined to four places:

| Where | What changes |
| --- | --- |
| `development/` | the compose service, `.env`, the provider's configuration, the helper scripts, `dns/hosts` |
| `backend/acceptance/signin.go` | rewritten against authentik's flow executor (K5) |
| `docs/` | `installation.md`, `console-guide.md`, `troubleshooting.md` |
| six comments in `backend/` | prose only |

The values in `development/.env` that our service reads change value but not meaning:

```
ACME_OIDC_ISSUER=http://auth.localhost/application/o/acmeservice-console/
ACME_OIDC_CLIENT_ID=acmeservice-console
ACME_OIDC_CLIENT_SECRET=dev-console-secret
ACME_OIDC_GROUPS_CLAIM=groups
ACME_OIDC_GROUP_PREFIX=
```

The issuer is the shape authentik publishes for an OAuth2 provider, with the trailing slash it
actually serves. The `KC_`-prefixed variables go entirely.

**The hostname changes from `keycloak.localhost` to `auth.localhost`,** and the reason the old one was
odd is the reason the new one has to be too. The issuer string in an identity token is one URL that
must be openable both by the browser on the developer's machine and by the `api` container. A
`*.localhost` name resolves to loopback on the host by RFC 6761, and inside the container network
CoreDNS answers it from `development/dns/hosts`. Both routes land on the development Traefik, which
forwards the name onward. `auth.localhost` is chosen over keeping `keycloak.localhost` because a
hostname naming the wrong product is a trap for the next reader, and over `authentik.localhost`
because the production design already uses `auth.acmealias.com` and the development name should
rhyme with it.

## K1a. Rejected: Pocket ID, evaluated properly and turned down on one property

Both Keycloak and authentik feel sluggish — authentik's login page noticeably so at times — and that
is a real cost paid on every development reset and by every customer at every sign-in. Pocket ID was
evaluated as the lighter alternative. It was stood up and taken apart rather than read about, and it
is recorded here in some detail because the attractive half is genuinely attractive and somebody will
propose it again.

**What it got right, measured rather than assumed:**

- **One container.** A Go binary over SQLite, healthy twelve seconds after `docker run`. authentik is
  four services and a much longer boot. Against the reset cost that made `tenancy-and-permissions`
  task 11.4 unprovable, that is not a small difference.
- **The role model needs no change.** Its discovery document lists `groups` in `claims_supported`,
  so group-derived roles work exactly as they do today.
- **Self-registration exists** — disabled, signup-token, or open registration.
- **The acceptance suite would NOT have needed a WebAuthn implementation**, which was the first
  objection raised against it and turned out to be wrong. `POST /api/users/:id/one-time-access-token`
  mints a token as an administrator and `POST /api/one-time-access-token/:token` exchanges it for a
  session — roughly twenty lines of Go and no cryptography. Pocket ID's own end-to-end tests drive
  Chrome DevTools virtual authenticators only because they already run Playwright.

**What decided it: there is no password authentication, and it is not a setting.**

Two independent pieces of evidence, both from the running instance rather than from documentation:
the `users` table has columns for `username`, `email`, names, `is_admin`, `ldap_id`, `locale`,
`disabled` and `email_verified` and **no password column at all**, so there is nowhere for one to be
stored; and the only login routes the server registers are `/api/webauthn/login/start` and
`/api/webauthn/login/finish`.

The real model is therefore passkey as primary with an emailed one-time link as the fallback. That is
a coherent modern design, and it is even *closer* to something `public-edition` already wanted —
U12's reasoning for delegating authentication includes "the breach consequences of holding password
hashes for strangers", and Pocket ID goes further by holding none. It is nonetheless a requirement
placed on every customer at the moment they are least willing to accept friction, and this service
sells to people who arrived wanting a certificate, not wanting to enrol an authenticator.

**Two further consequences, either of which would have needed answering anyway:**

- **`public-edition` U12 would have to be redesigned.** Its staff-and-customer separation rests on
  authentik's `internal`/`external` user types and on brands separating hostnames. Pocket ID has
  neither: one instance, one look, no user-type distinction.
- **Branding is a logo, a colour and a name**, not control of the pages.

**What is kept from the exercise.** The sluggishness complaint was legitimate and is not answered by
rejecting the alternative. Most of what is felt in development is start-up rather than steady-state,
which `dev.sh up` already avoids paying per run, and task 2.6 exists to say so out loud if the
authentik stack makes a reset materially slower. If login latency remains annoying in production once
authentik is actually deployed, that is a reason to revisit this section — with the note that the
technical objection recorded against Pocket ID's test story was investigated and disproved, so a
future evaluation starts from a shorter list.

## K2. The configuration is a blueprint, not a clicked UI and not a database dump

Keycloak's configuration lives in `development/realm/acmeservice-realm.json` and is imported by
`--import-realm` on every start. Whatever replaces it has to keep the property that makes that work:
**`./scripts/dev.sh reset` produces an identical, working identity provider from an empty
directory, with nobody clicking anything.**

authentik's mechanism for this is a **blueprint** — a YAML document listing objects that should
exist, applied at start-up and re-applied when it changes. The blueprint lives at
`development/authentik/blueprints/acmeservice.yaml` and is mounted read-only, the way the realm
export is today.

**Rejected: configuring authentik through its web interface and committing the database.** It works
once and is then unreviewable. A change to the identity configuration would arrive as a binary diff,
and the question "what does this development stack actually grant `read-only-user`?" would be
answerable only by starting it up and looking. The realm JSON has the same failing to a lesser
degree, and moving to a hand-written YAML file is an improvement this change gets for free.

**Rejected: a first-run setup script driving authentik's REST API.** It is a second orchestrator that
has to know when authentik is ready, be idempotent, and be maintained. Blueprints are the mechanism
the product provides for exactly this, and `add-acceptance-environment` A1 already established the
principle that the stack is driven through its own front door rather than through a bespoke one.

## K3. The four development users, their groups and their password are reproduced exactly

Not approximately. These names are asserted by `backend/acceptance/identity_test.go` and
`backend/internal/authz/roles_test.go`, and are printed to a developer by `./scripts/dev.sh status`:

| User | Group | Role it grants |
| --- | --- | --- |
| `admin-user` | `acme-admin` | `ADMIN` |
| `config-admin-user` | `acme-config-admin` | `CONFIG_ADMIN` |
| `key-rotation-user` | `acme-key-rotation` | `KEY_ROTATION` |
| `read-only-user` | `acme-read-only` | `READ_ONLY` |

All four keep the password `devpass`. The group-to-role mapping in `development/.env`
(`ACME_ADMIN_GROUP=acme-admin` and its three siblings) is not touched.

**The group claim must come out as bare names.** This is the difference K1 says the group-prefix
setting exists to absorb, and the decision here is to absorb it in the *provider* rather than in our
configuration: the blueprint's scope mapping emits `acme-admin`, so `ACME_OIDC_GROUP_PREFIX` stays
empty and the development stack keeps testing the same code path production will use. A stack that
needed a non-empty prefix would be testing the prefix rather than the mapping.

## K4. authentik's supporting services are reused where possible

authentik needs a PostgreSQL database and a Redis. It does not need its *own* PostgreSQL: the stack
already runs one for the service, and `development/postgres-init/` already exists as the place where
additional databases are created at first start. So authentik gets a database inside the container
that is already there.

Redis has no such incumbent and is added, at `172.28.0.21` on the stack's fixed subnet.

That makes the arithmetic: **one container removed (keycloak), three added (authentik server,
authentik worker, redis).** A net two, rather than the net four a naive translation produces.

**Rejected: authentik's bundled single-container images.** They exist for evaluation, they are not
what the production design would deploy, and the point of K1 is to stop development from exercising a
configuration production will not use.

## K5. The acceptance suite drives the flow executor, not an HTML form

This is the rewrite the proposal names as the riskiest part.

Today `backend/acceptance/signin.go` does this:

```go
var formAction = regexp.MustCompile(`(?i)<form[^>]+action="([^"]+)"`)
```

— fetch Keycloak's login page, find the one form, post `username` and `password` to its action.

authentik serves a browser application instead. The sign-in sequence becomes: follow the redirect to
authentik, which lands on a flow at a URL containing the flow slug; then talk to
`/api/v3/flows/executor/<slug>/?query=<the original query string>`, which answers JSON describing one
step at a time — an identification step wanting a `uid_field`, then a password step wanting a
`password` — and accepts a JSON body per step, until it answers with a redirect back to our
`/api/v1/auth/callback`.

Two properties of the existing helper are kept because they are load-bearing:

- **The lax cookie jar stays.** `signin.go`'s `laxJar` exists because Go's `net/http/cookiejar` will
  not return a `Secure` cookie over plain `http://`, while browsers treat `*.localhost` as a
  trustworthy origin. That trap is unchanged by the provider swap, and
  `TestSignInCarriesTheSession` is its regression pin.
- **The suite still signs in for real.** No shortcut that mints a session directly, no test-only
  bypass route. The value of the acceptance suite is that it walks the path a person walks.

**Rejected: driving a real browser.** It would remove the protocol coupling entirely and it would add
a browser to the acceptance stack, which `add-acceptance-environment` A1 rejected the analogue of
when it refused a second environment. The flow executor is a documented API and talking to it is a
hundred lines.

**Accepted risk, recorded:** this couples the suite to an authentik API version. When authentik
changes its flow executor, this breaks, and it breaks with a confusing error. The mitigation is that
it breaks *loudly and in one place*, and that place is a file whose doc comment says exactly what it
is doing and why.

## K6. Self-registration is enabled, and produces a user who belongs to nothing

The blueprint enables an enrolment flow reachable from the console's sign-in page.

What it creates is a user with a username, an email and a password, in **no group**. That user can
sign in to authentik and is then refused by our service, which reports the groups it observed — an
empty list — under the existing rule in `identity-and-roles`, "Authenticated user with no mapped
group is told why".

**This is the designed outcome, not an unfinished edge.** `tenancy-and-permissions` P18 makes account
membership rows we write rather than claims we read, and `public-edition` U12 leans on exactly that
when it observes that a compromised identity provider "can create a user, and that user belongs to
nothing". Self-registration is the mechanism that produces such users, and the refusal is the
property being demonstrated.

It follows that **the refusal message is now user-facing in a way it was not before.** Previously only
a misconfigured staff member ever saw it. Now it is what every self-registered person sees, and the
task list treats its wording as part of this change rather than as a detail.

**Rejected: leaving self-registration off in development and turning it on in `public-edition`.** It
is the thing this change was asked for, and an enrolment flow that has never run is not "built and
inert", it is unbuilt.

## K7. Branding is one brand bound to the console hostname

authentik's **brand** carries the title, logo and stylesheet for a hostname. The blueprint defines one
for the development console, so the sign-in and sign-up pages look like the product.

The production design (`public-edition` U12) uses brands to separate the *staff* and *customer*
hostnames from each other. Development has no staff plane and needs only the one. The blueprint is
written so the second is an addition rather than a restructuring.

## K8. This change is not applied while the acceptance suite is running

An operational note, recorded because it caused a real sequencing decision rather than because it is
interesting.

The acceptance suite shells out to `docker compose -f development/compose.yaml` to restart
`traefik-lb`, read logs and run `adminctl` inside the `api` container. Editing `development/compose.yaml`
or `development/.env` while a run is in flight changes what those commands do underneath it.

So the files that the running stack reads — `compose.yaml`, `.env`, the scripts — are edited only
once `tenancy-and-permissions` task 11.4 has been closed. Files that nothing references yet, the
blueprint among them, can be written at any time.
