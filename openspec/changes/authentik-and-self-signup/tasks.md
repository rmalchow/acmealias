# Tasks

> **Sequencing (design K8).** The running acceptance suite shells out to
> `docker compose -f development/compose.yaml`, so editing that file, `development/.env` or the
> helper scripts while a run is in flight changes what those commands do underneath it. Group 1 is
> therefore written first and touches only files nothing references yet. **Groups 2 onward wait until
> `tenancy-and-permissions` task 11.4 is closed.**

## 1. The blueprint, written ahead of the stack that loads it

- [ ] 1.1 Write `development/authentik/blueprints/acmeservice.yaml` creating the four groups `acme-admin`, `acme-config-admin`, `acme-key-rotation`, `acme-read-only` (design K3)
- [ ] 1.2 Create the four users `admin-user`, `config-admin-user`, `key-rotation-user`, `read-only-user`, each in exactly one of those groups, each with the password `devpass` — the names and the password are asserted by `backend/acceptance/identity_test.go` and printed by `./scripts/dev.sh status`, so they are reproduced exactly rather than approximately
- [ ] 1.3 Create the OAuth2 provider and the `acmeservice-console` application, with client id `acmeservice-console` and client secret `dev-console-secret`, and the redirect URL `http://acme-console.localhost/api/v1/auth/callback`
- [ ] 1.4 Add the scope mapping that emits a `groups` claim of **bare group names** — `acme-admin`, not `/acme-admin` — so `ACME_OIDC_GROUP_PREFIX` stays empty and development exercises the same code path production will (design K3)
- [ ] 1.5 VALIDATE THE SCHEMA AGAINST A RUNNING AUTHENTIK BEFORE BELIEVING ANY OF THE ABOVE. Blueprint model names, the `!Find` / `!KeyOf` tags and whether a password may be set inline are all version-specific, and this file is written from documentation rather than from a running instance. Expect a correction pass

## 2. The compose stack — blocked on 11.4

- [ ] 2.1 Remove the `keycloak` service, its `./data/keycloak` volume and its `./realm` mount from `development/compose.yaml`; delete `development/realm/acmeservice-realm.json`
- [ ] 2.2 Add `redis` at `172.28.0.21` on the stack's fixed subnet
- [ ] 2.3 Add `authentik-server` and `authentik-worker`, sharing one image and one environment block, with the blueprint directory mounted read-only
- [ ] 2.4 Give authentik a database inside the Postgres container the stack already runs, created from `development/postgres-init/` — not a second Postgres (design K4)
- [ ] 2.5 Route `auth.localhost` to the authentik server through the development Traefik overlay, and add it to `development/dns/hosts` so the name resolves identically from the host and from inside the network (design K1)
- [ ] 2.6 Confirm the container count moved by a net two, and that `./scripts/dev.sh reset` still completes — if the reset has become materially slower, say so in the change rather than absorbing it quietly, because reset cost is what made task 11.4 unprovable

## 3. Configuration and the helper scripts — blocked on 11.4

- [ ] 3.1 Replace the `KC_*` block in `development/.env` with authentik's settings, and point `ACME_OIDC_ISSUER` at `http://auth.localhost/application/o/acmeservice-console/` — with the trailing slash authentik actually serves
- [ ] 3.2 Update `development/scripts/up.sh` to wait for authentik's health endpoint rather than Keycloak's, remembering that authentik applies blueprints *after* it reports healthy, so readiness is "the console application answers discovery", not "the container is up"
- [ ] 3.3 Update `status.sh` to print the new URLs and the unchanged test logins; update `down.sh`, `destroy.sh` and `reset.sh` for the new data directories
- [ ] 3.4 Update `development/traefik/traefik.yaml` and the dev overlay for the new hostname

## 4. The acceptance suite signs in again — blocked on 11.4

- [ ] 4.1 Rewrite `signIn` in `backend/acceptance/signin.go` against authentik's flow executor: follow the redirect, read the flow slug, then step through `/api/v3/flows/executor/<slug>/` posting the identification and password components until it redirects to `/api/v1/auth/callback` (design K5)
- [ ] 4.2 Keep `laxJar` exactly as it is, and keep its doc comment: it exists because Go's cookie jar will not return a `Secure` cookie over plain `http://` while browsers accept `*.localhost` as trustworthy, and that trap is unchanged by the provider swap
- [ ] 4.3 Keep the account discovery added when the suite was ported to accounts — `signIn` reads `selectedAccountId` from `/api/v1/me` and every account-scoped path is built from it
- [ ] 4.4 Rewrite the doc comment that describes the sign-in sequence, which currently documents Keycloak's form post step by step and would otherwise become the most confidently wrong prose in the repository
- [ ] 4.5 Update the realm-mapper explanation in `backend/acceptance/identity_test.go`, which explains the claim shape in terms of Keycloak's `full.path=false` option
- [ ] 4.6 Run the full acceptance suite green against authentik. This is the gate: sign-in, the four users' roles, and role enforcement all pass, or the swap is not done

## 5. Self-registration and branding — blocked on 11.4

- [ ] 5.1 Add an enrolment flow to the blueprint, reachable from the console sign-in page, collecting a username, an email and a password (design K6)
- [ ] 5.2 Place enrolled users in **no group**, and add an acceptance test asserting that a self-registered user authenticates successfully at the identity provider and is then refused by our service — the P18 property, demonstrated rather than assumed
- [ ] 5.3 Review the wording of the no-mapped-group refusal. It was previously seen only by a misconfigured colleague and is now what every self-registered person sees; "you are in none of the configured groups" is the wrong sentence for someone who just signed up
- [ ] 5.4 Add a brand bound to the console hostname carrying the title and logo, written so that a second brand for a staff hostname is an addition rather than a restructuring (design K7)
- [ ] 5.5 Point authentik's outbound mail at the stack's mail sink — `mailpit:1025`, no credentials, no TLS — so verification and recovery messages are captured rather than delivered. The SMTP host is the compose service name, NOT `mail.localhost`: that hostname is the web interface and is served through the HTTP overlay, which SMTP does not pass through
- [ ] 5.6 Walk the enrolment flow once by hand and read the resulting message at `http://mail.localhost`, so the fallback path is known to work before it is the only way a stranger recovers an account

## 6. Documentation — blocked on 11.4

- [ ] 6.1 Rewrite the identity provider section of `docs/installation.md`, including the settings table entries for the four group variables
- [ ] 6.2 Update `docs/console-guide.md` and `docs/troubleshooting.md`, where "Keycloak group" appears throughout as the thing a reader is told to go and check
- [ ] 6.3 Correct the six Keycloak comments in `backend/` — `config.go`, `oidcauth/groups.go`, `authz/middleware.go`, `httpapi/admin_sessions.go`, `cmd/acmeservice/main.go`. Prose only; no behaviour changes
- [ ] 6.4 Update the `README.md` prerequisites list, which currently requires "Keycloak, with a realm you can create a client in"
- [ ] 6.5 Note in `public-edition`'s design that U12's provider choice is now exercised in development, since the reason U12 was cheap was that the instance already existed for staff
