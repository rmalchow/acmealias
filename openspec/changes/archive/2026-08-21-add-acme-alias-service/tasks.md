> Groups are ordered so that a group's dependencies are earlier in the list. Each heading carries
> a suggested complexity and the directories it writes to; groups whose `touches` sets are
> disjoint can run in parallel. Every task names the files it produces.
>
> Group 23 (the acceptance environment, sixteen tasks) and task 24.7 were **moved out** of this
> change, unbuilt, into `add-acceptance-environment`, along with the `acceptance-harness`
> capability. Their numbering is not reused. The architecture they implement stays here, as
> `design.md` D14.

## 1. Repository scaffolding and build <!-- complexity: sonnet | touches: repo root, backend/go.mod, frontend -->

> Note on 1.1: `go mod tidy` prunes requires that nothing imports, so the dependency list cannot be
> added up front and made to stick. Each group adds what it imports. Present as of group 4: gorm +
> postgres driver, golang-migrate, pgx, x/net. To be added by their first importer: `miekg/dns`
> (group 11), `labstack/echo/v4` (group 12), `coreos/go-oidc/v3` + `golang.org/x/oauth2` (group 13),
> `prometheus/client_golang` (group 17).

- [x] 1.1 `backend/go.mod`: `go mod init gitlab-mcww-admin.versioncontrol.io/tools/acmeservice`, Go 1.24 or newer; add `miekg/dns`, `labstack/echo/v4`, `gorm.io/gorm`, `gorm.io/driver/postgres`, `golang-migrate/migrate/v4` (+ `source/iofs`, `database/postgres`), `coreos/go-oidc/v3`, `golang.org/x/oauth2`, `golang.org/x/net`, `prometheus/client_golang`
- [x] 1.2 `frontend/`: scaffold the Angular workspace with a single application project `admin` (standalone components, no SSR); set `outputPath` in `frontend/angular.json` to `../backend/internal/webui/dist` with `"browser": ""` so the files land directly in the embed directory
- [x] 1.3 `backend/internal/webui/dist/.gitkeep` plus `index.html` placeholder committed, and `backend/internal/webui/dist/*` gitignored except those two, so `go build` works on a fresh clone before any `ng build`
- [x] 1.4 Root `scripts/` — bash, no Makefile: `lib.sh` (shared `say`/`warn`/`die`/`need`/`chrome_bin`), `build.sh` (`ng build admin` then `go build ./...`), `test.sh` (Go + frontend unit tests, honouring `SKIP_FRONTEND=1`), `test-integration.sh` (database-backed tests, defaulting `TEST_DATABASE_URL` to the dev stack), `test-acceptance.sh` (the Compose end-to-end run), `lint.sh` (`golangci-lint`, where a missing linter is an error rather than a silent skip) and `dev.sh` (dev stack + `air` + `ng serve`, with a trap that stops the host processes and leaves the containers running); plus `.golangci.yml` at the repo root
- [x] 1.5 `.gitignore` and `.editorconfig` at the repo root
- [x] 1.6 Replace the GitLab template `README.md`: what the service is, the CNAME aliasing model in five lines, the wildcard rules, the three listeners, and how to start the dev loop

## 2. Configuration <!-- complexity: sonnet | touches: backend/internal/config -->

- [x] 2.1 `backend/internal/config/config.go`: the `Config` struct — zone, SOA fields, NS names, DNS listen address, challenge/admin/internal HTTP addresses, database URL, `ACME_MIGRATE_ON_START`, challenge TTL, SOA minimum, challenge lifetime, view refresh interval, sweep interval, trusted proxy CIDRs, verification resolver, OIDC issuer/client id/client secret/redirect URL, group claim name and prefix, the four role-group variables (`ACME_ADMIN_GROUP`, `ACME_CONFIG_ADMIN_GROUP`, `ACME_KEY_ROTATION_GROUP`, `ACME_READ_ONLY_GROUP`), `ACME_SESSION_IDLE_TIMEOUT` and `ACME_SESSION_MAX_LIFETIME`, `ACME_CAA_ISSUER` (zero or more CA identifiers), log level, shutdown grace period. Every service-owned variable carries the `ACME_` prefix; `HTTPREQ_*` and `LEGO_*` are lego's and keep their names
- [x] 2.2 `backend/internal/config/config.go`: `Load()` reading only environment variables (no file overlay), with defaults for everything that has one
- [x] 2.3 `backend/internal/config/validate.go`: local-only validation — required values present, zone is a valid absolute DNS name, durations positive, CIDRs and the resolver address parse, the OIDC issuer parses as a URL. No network calls: a dependency being down must never stop startup
- [x] 2.4 `backend/internal/config/redact.go`: `LogValue()` rendering the effective config with the database password, OIDC client secret and any other secret redacted
- [x] 2.5 `backend/internal/config/config_test.go`: each validation failure names its setting; defaults apply; redaction hides the database password and client secret

## 3. DNS name rules <!-- complexity: opus | touches: backend/internal/dnsname -->

- [x] 3.1 `backend/internal/dnsname/name.go`: `Normalize(string) (string, error)` — lowercase, strip one trailing dot, reject empty, over-length (253 octets) and invalid labels (empty, >63 octets, bad characters, leading/trailing hyphen); allow a leading `_` label for challenge names
- [x] 3.2 `backend/internal/dnsname/wildcard.go`: `ValidateDomain(string) (Normalized, []Warning, error)` implementing every row of design D4 — leftmost `*` only, exactly one wildcard label, reject bare `*`, reject a wildcard whose remainder is a public suffix via `golang.org/x/net/publicsuffix`
- [x] 3.3 `backend/internal/dnsname/challenge.go`: `ChallengeName(domain) string` — strip a leading `*.`, prefix `_acme-challenge.`; and `RegistrableDomain(name) (string, error)` over the same `publicsuffix` data, used by the CAA rows of the DNS plan
- [x] 3.4 `backend/internal/dnsname/label.go`: `ValidateLabel(string) error` (single label, lowercase alphanumeric and hyphen, no leading/trailing hyphen, ≤63 octets) and `SuggestLabel(challengeName string) string` producing `example-com` from `_acme-challenge.example.com`
- [x] 3.5 `backend/internal/dnsname/*_test.go`: table tests over mixed case, trailing dots, invalid characters, boundary lengths, every D4 wildcard row, and that `example.com` and `*.example.com` derive the identical challenge name

## 4. Schema, migrations and models <!-- complexity: opus | touches: backend/internal/store/migrations, backend/internal/model, backend/internal/store/{db.go,migrate.go,schema.sql} -->

- [x] 4.1 `backend/internal/store/migrations/000001_initial.up.sql` / `.down.sql`: `projects`, `load_balancers`, `lb_nodes`, `aliases`, `domains`, `api_keys`, `api_key_networks`, `challenges`, `sessions`, `audit_entries` with the constraints from design D4/D6 — `lb_nodes` carries nullable `ipv4` and `ipv6` columns, and there is **no** `role_bindings` table: the group-to-role mapping is configuration (design D10) — note `challenges.published_by` is the key *identifier* as text, not a foreign key, and `unique (alias_id, value)`
- [x] 4.2 `backend/internal/store/migrate.go`: embed `migrations/` with `//go:embed`, apply through `source/iofs`, run at startup unless `ACME_MIGRATE_ON_START=false`; exit with a diagnostic when the database version is *ahead* of the highest embedded migration
- [x] 4.3 `backend/internal/model/model.go`: the GORM structs matching the migrated schema, with an explicit assertion in review that `AutoMigrate` is called nowhere in the tree
- [x] 4.4 `backend/internal/store/db.go`: open with `gorm.io/driver/postgres`, pool sizes from config, GORM logger bridged to `log/slog`
- [x] 4.5 `backend/internal/store/testdb_test.go`: helper that `t.Skip`s with a message naming `TEST_DATABASE_URL` when it is unset — CI does not set it, so skipping must be visible and never a silent pass — and otherwise migrates a fresh schema per test and drops it afterwards
- [x] 4.6 `backend/internal/store/schema_golden_test.go` + `backend/internal/store/schema.sql`: dump the migrated schema from `information_schema` (no `pg_dump` binary) and compare to the checked-in golden file. Like every database-backed test this is a local guard that CI skips (design D13), so the failure message must say how to regenerate the golden file
- [x] 4.7 `backend/internal/store/model_drift_test.go`: walk every registered model through `db.Migrator()` and assert each mapped field has a compatible column and each model's table exists
- [x] 4.8 `backend/internal/store/migrate_test.go`: migrations are idempotent when applied twice, and the down migration reverses the up migration on a populated database

## 5. Inventory repositories <!-- complexity: sonnet | touches: backend/internal/store/{project.go,loadbalancer.go,domain.go,alias.go,apikey.go,session.go,audit.go} -->

- [x] 5.1 `backend/internal/store/project.go`, `loadbalancer.go`: CRUD for projects, load balancers and nodes, with `Preload` for the console's "project with its load balancers with their domains" reads
- [x] 5.2 `backend/internal/store/domain.go`, `alias.go`: create/list/delete for domains and aliases, find-alias-by-challenge-name, and list-aliases-for-load-balancer
- [x] 5.3 `backend/internal/store/apikey.go`: create, list by load balancer, find by identifier (with networks preloaded), revoke, and `TouchLastUsed`
- [x] 5.4 `backend/internal/store/session.go`, `audit.go`: session create/find/delete/expire plus revoke-by-id, and append-only audit entry insert with a filtered list
- [x] 5.5 `backend/internal/store/*_test.go`: repository round-trips against `TEST_DATABASE_URL`, including that deleting a project with children is refused by the constraint

## 6. Challenge record store <!-- complexity: opus | touches: backend/internal/store/challenge.go, backend/internal/store/challenge_test.go -->

- [x] 6.1 `backend/internal/store/challenge.go`: `Present(aliasID, value, publishedBy, lifetime)` as a `clause.OnConflict` upsert on `(alias_id, value)` that refreshes `expires_at` — never a read-then-write
- [x] 6.2 `backend/internal/store/challenge.go`: `Cleanup(aliasID, value)` deleting only the exactly-matching row and reporting no error when nothing matched
- [x] 6.3 `backend/internal/store/challenge.go`: `ActiveByAlias()` returning every alias label left-joined to its non-expired values — the query that feeds the zone view, including aliases with no values
- [x] 6.4 `backend/internal/store/challenge.go`: `DeleteExpired() (int64, error)` for the sweep, and `ListActive()` for the live-challenges screen
- [x] 6.5 `backend/internal/store/challenge_test.go`: concurrent identical presents leave exactly one row and no error; concurrent distinct presents for one alias leave both; cleanup of one of two leaves the sibling; cleanup of an absent value succeeds; run under `-race`

## 7. Inventory service and DNS plan <!-- complexity: opus | touches: backend/internal/inventory -->

- [x] 7.1 `backend/internal/inventory/inventory.go`: project, load balancer and node operations over the repositories, refusing deletion of a non-empty parent with a named error
- [x] 7.2 `backend/internal/inventory/domain.go`: create a domain — validate via `dnsname`, derive the challenge name, find-or-create its alias (suggesting a label, disambiguating a collision with a numeric suffix, accepting an operator-supplied label), and refuse when that challenge name's alias belongs to another load balancer, naming the load balancer
- [x] 7.3 `backend/internal/inventory/domain.go`: return a warning (not an error) when the new name is already covered by an existing wildcard on the same load balancer
- [x] 7.4 `backend/internal/inventory/domain.go`: delete a domain; when it was the last one referencing its alias, delete the alias and cascade its challenge records, and signal the caller so the zone view is refreshed
- [x] 7.5 `backend/internal/inventory/plan.go`: `Plan(loadBalancerID)` — an A record per domain per node IPv4 and an AAAA per node IPv6 (omitting the family a node lacks), and one CNAME per *distinct* challenge name pointing at `<label>.<zone>.`, returned as structured records
- [x] 7.6 `backend/internal/inventory/plan.go`: CAA rows — for each distinct *registrable* domain in the plan (never per host, since CAA is inherited and the nearest ancestor wins), one `issue` and one `issuewild` entry per identifier in `ACME_CAA_ISSUER`; emit nothing at all when it is unset
- [x] 7.7 `backend/internal/inventory/plan.go`: `RenderZoneSnippet(Plan) string` producing valid zone-file syntax with fully-qualified names and a comment header naming the project, load balancer and nodes
- [x] 7.8 `backend/internal/inventory/*_test.go`: `example.com` + `*.example.com` produce one alias and one CNAME; removing one of them keeps the alias; the cross-load-balancer conflict is refused; the plan for three domains sharing two challenge names contains exactly two CNAMEs; a dual-stack load balancer emits A and AAAA while a v4-only node emits no AAAA; three domains under one registrable domain emit exactly one CAA set, with both tags, and none when `ACME_CAA_ISSUER` is unset

## 8. Live DNS verification <!-- complexity: sonnet | touches: backend/internal/resolvercheck -->

- [x] 8.1 `backend/internal/resolvercheck/check.go`: per-record check against the configured resolver returning `correct` / `missing` / `incorrect` (with what was found) / `undetermined` (lookup failure or timeout, retryable), for the CNAME rows and the A/AAAA rows of a plan
- [x] 8.2 `backend/internal/resolvercheck/caa.go`: CAA checking, whose two outcomes are the reverse of the intuitive reading — **no** CAA set at or above the registrable domain is `not-published` and explicitly **not** a failure (any CA may issue, nothing is broken), while a set that exists and omits our configured issuer is `blocking`, reported loudly with what was found, because it stops issuance outright
- [x] 8.3 `backend/internal/resolvercheck/cache.go`: short in-process result cache keyed by record, never persisted — a green result from yesterday means nothing
- [x] 8.4 `backend/internal/resolvercheck/check_test.go`: drive the whole matrix against a `miekg/dns` test server standing in for the customer's zone, including absent CAA, authorizing CAA, and CAA naming only another authority

## 9. API keys and request authentication <!-- complexity: opus | touches: backend/internal/apikey -->

- [x] 9.1 `backend/internal/apikey/key.go`: generate an identifier (`k_` + 8 hex) and a 32-byte `crypto/rand` secret, base64url-encoded; store `sha256(secret)` and a 6-character display prefix; return the secret to the caller exactly once
- [x] 9.2 `backend/internal/apikey/key.go`: `Verify(identifier, secret)` — lookup, `subtle.ConstantTimeCompare` of the digest, comparison against a fixed dummy digest on an unknown identifier so both paths cost the same; reject expired, disabled and deleted keys
- [x] 9.3 `backend/internal/apikey/middleware.go`: Echo middleware accepting the key as HTTP Basic (`identifier:secret`), as `Authorization: Bearer <identifier>.<secret>` and as `X-API-Key: <identifier>.<secret>`, all converging on `Verify`
- [x] 9.4 `backend/internal/apikey/middleware.go`: evaluate the key's CIDR blocks against `c.RealIP()`; refuse with `401` — the same status and body as a bad key — and log the distinct reason
- [x] 9.5 `backend/internal/apikey/ipextractor.go`: build Echo's `ExtractIPFromXFFHeader` from `ACME_TRUSTED_PROXY_CIDRS` with loopback, link-local and private-net trust all off, plus a startup check that refuses to start when any key carries a CIDR restriction while no trusted proxies are configured
- [x] 9.6 `backend/internal/apikey/lastused.go`: update `last_used_at` at most once per minute per key, so verification is not a write on every request
- [x] 9.7 `backend/internal/apikey/*_test.go`: valid / unknown / wrong-secret / expired / revoked; all three transports; in-range and out-of-range addresses; a forged `X-Forwarded-For` from an untrusted peer does not bypass the restriction; unknown-identifier and wrong-secret responses are byte-identical

## 10. In-memory zone view <!-- complexity: opus | touches: backend/internal/zoneview -->

- [x] 10.1 `backend/internal/zoneview/view.go`: snapshot of `map[label][]value` holding **every** alias label, including those with no active values, guarded by an `RWMutex`; a present label with an empty slice is `NOERROR`-empty, an absent label is `NXDOMAIN`
- [x] 10.2 `backend/internal/zoneview/view.go`: filter on `expires_at` at read time so a value stops being answered the instant it expires, regardless of the sweep
- [x] 10.3 `backend/internal/zoneview/refresh.go`: load at startup, refresh on the configured interval, and expose `Refresh(ctx)` for callers to invoke synchronously after a write or an alias change
- [x] 10.4 `backend/internal/zoneview/refresh.go`: keep serving the last known snapshot when the refresh fails, and expose datastore reachability for the metric and the readiness probe
- [x] 10.5 `backend/internal/zoneview/view_test.go`: read-time expiry filtering; write-then-read visibility; alias-with-no-values distinguishable from unknown label; concurrent readers and refreshers under `-race`

## 11. Authoritative DNS server <!-- complexity: opus | touches: backend/internal/dnsserver -->

- [x] 11.1 `backend/internal/dnsserver/server.go`: UDP and TCP listeners on the configured address sharing one `dns.Handler`; graceful shutdown; never terminate on a bad packet
- [x] 11.2 `backend/internal/dnsserver/handler.go`: TXT for a known label — one record per active value from the zone view, configured challenge TTL, AA set, RA clear, no recursion
- [x] 11.3 `backend/internal/dnsserver/handler.go`: SOA and NS at the zone apex from configuration; the SOA serial does not change when challenge records change
- [x] 11.4 `backend/internal/dnsserver/handler.go`: the full negative table of design D7 — `NOERROR`+SOA for a known label with no values or a non-TXT type, `NXDOMAIN`+SOA for an unknown in-zone name, `REFUSED` out of zone, `FORMERR` for unparseable messages
- [x] 11.5 `backend/internal/dnsserver/handler.go`: honour the EDNS(0) advertised UDP payload size and set TC when the answer set does not fit, so the client retries over TCP
- [x] 11.6 `backend/internal/dnsserver/handler_test.go`: start on an ephemeral port, query with a `miekg/dns` client, assert rcodes and flags as well as answers; include a multi-value answer that truncates over UDP and succeeds over TCP, **and a plain single-value TXT query sent over TCP with nothing to truncate** — CAs query over TCP directly, so TCP is a transport in its own right, not only the truncation fallback; plus a non-empty NS RRset at the apex, which lego's propagation check requires, and a query for a cleaned-up alias returning `NOERROR`-empty rather than `NXDOMAIN`

## 12. Challenge HTTP API <!-- complexity: opus | touches: backend/internal/httpapi/{challenge.go,challenge_test.go,errors.go} -->

- [x] 12.1 `backend/internal/httpapi/challenge.go`: `POST /present` — bind `{fqdn, value}`, normalize, resolve the alias **by label when the name is inside the served zone and by challenge name otherwise** (both forms are normal: lego posts the CNAME target it already resolved, other clients post the un-aliased name — design D2), verify it belongs to the authenticated key's load balancer, upsert, refresh the zone view **before** writing the response, `200 OK`
- [x] 12.2 `backend/internal/httpapi/challenge.go`: `POST /cleanup` — delete only the matching `(alias, value)`, idempotent, `200 OK`, zone view refreshed before responding
- [x] 12.3 `backend/internal/httpapi/challenge.go`: `400` for malformed JSON or a missing/empty field, with a message naming the expected `fqdn` and `value` so a `HTTPREQ_MODE=RAW` misconfiguration is diagnosable rather than mysterious
- [x] 12.4 `backend/internal/httpapi/challenge.go`: `403` for an unmapped or out-of-scope name, logging key identifier, requested name and reason; `500` with a generic code on a storage failure so lego retries, never leaking datastore detail
- [x] 12.5 `backend/internal/httpapi/challenge.go`: authentication middleware runs before alias resolution, so an unauthenticated request is answered identically for a mapped and an unmapped name
- [x] 12.6 `backend/internal/httpapi/challenge_test.go`: normalization of trailing dot and case; repeated present is idempotent; two values coexist; cleanup removes one and leaves the sibling; cleanup of an unknown value succeeds; cross-load-balancer refusal; every error path

## 13. OIDC sign-in and sessions <!-- complexity: opus | touches: backend/internal/oidcauth -->

- [x] 13.1 `backend/internal/oidcauth/provider.go`: lazy, retried OIDC discovery — never called at startup, so an unavailable IdP cannot stop the DNS and challenge surfaces from coming up
- [x] 13.2 `backend/internal/oidcauth/flow.go`: `GET /api/v1/auth/login` (302 to the IdP, state + PKCE verifier + nonce in a short-lived cookie) and `GET /api/v1/auth/callback` (exchange, verify issuer/audience/expiry/nonce against JWKS, create the session, redirect to `/`)
- [x] 13.3 `backend/internal/oidcauth/session.go`: server-side sessions — 256-bit opaque id in an `HttpOnly`, `Secure`, `SameSite=Lax`, host-only cookie; idle and absolute lifetimes; explicit revocation; `POST /api/v1/auth/logout`. Store subject, display name and the group list as at sign-in; store no provider tokens
- [x] 13.4 `backend/internal/oidcauth/groups.go`: read groups from the configured claim, stripping the configured prefix from each value
- [x] 13.5 `backend/internal/oidcauth/csrf.go`: Echo CSRF middleware configured for Angular — cookie `XSRF-TOKEN` (not `HttpOnly`, the browser must read it), lookup `header:X-XSRF-TOKEN`, on state-changing management routes only
- [x] 13.6 `backend/internal/oidcauth/*_test.go`: token validation failures (bad signature, wrong issuer, wrong audience, expired, wrong nonce) establish no session; idle and absolute expiry; revocation takes effect on the next request; a state-changing request without the CSRF header is rejected

## 14. Roles, permissions and audit <!-- complexity: opus | touches: backend/internal/authz -->

- [x] 14.1 `backend/internal/authz/roles.go`: the four global roles `ADMIN`, `CONFIG_ADMIN`, `KEY_ROTATION`, `READ_ONLY`, and `RolesFor(groups []string) RoleSet` — the union of every role whose configured group appears in the session's groups; an unset variable means nobody holds that role
- [x] 14.2 `backend/internal/authz/roles.go`: log the configured group for each role at startup, and expose it for the effective-configuration view; there is no table and no API that can change it
- [x] 14.3 `backend/internal/authz/permissions.go`: the design D10 matrix as explicit `Can(roles, action)` checks — no project argument, because no role is project-scoped
- [x] 14.4 `backend/internal/authz/middleware.go`: Echo middleware enforcing a required permission per route, returning `403`; an authenticated user in none of the four groups gets a `403` body naming the groups that were observed — the only way to diagnose a mapping typo or a "Full group path" left switched on
- [x] 14.5 `backend/internal/authz/audit.go`: record every inventory, key and session change — creation, revocation by an administrator, sign-out — with actor, object, action and time, and never a secret or token
- [x] 14.6 `backend/internal/authz/*_test.go`: every role against every row of the D10 matrix, including the `CONFIG_ADMIN`/`KEY_ROTATION` boundary (a key yes, a domain no) and that roles are additive; a session keeps its roles when its group mapping changes, and revoking the session ends access at the next request; audit entries carry no secret

## 15. Management API <!-- complexity: sonnet | touches: backend/internal/httpapi/{admin.go,admin_*.go,errors.go} -->

- [x] 15.1 `backend/internal/httpapi/admin_inventory.go`: `/api/v1` CRUD for projects, load balancers, nodes and domains, each route declaring its required permission
- [x] 15.2 `backend/internal/httpapi/admin_keys.go`: create (secret in the creation response only), list, restrict by CIDR, rotate and revoke API keys; no read path ever returns a secret
- [x] 15.3 `backend/internal/httpapi/admin_plan.go`: the DNS plan as structured JSON, the zone-file rendering, and the on-demand verification endpoint
- [x] 15.4 `backend/internal/httpapi/admin_misc.go`: `GET /api/v1/me` (display name, groups, roles), live challenges, the filtered audit trail, and the redacted effective configuration including the configured group for each role
- [x] 15.5 `backend/internal/httpapi/admin_sessions.go`: `GET /api/v1/sessions` (subject, started, last used, expires — never anything usable to authenticate as that session), `DELETE /api/v1/sessions/:id` and `DELETE /api/v1/sessions?subject=…`, all `ADMIN`-only, each writing an audit entry naming the administrator and the subject whose session was ended
- [x] 15.6 `backend/internal/httpapi/errors.go`: one JSON error shape through Echo's `HTTPErrorHandler`, mapping validation, conflict, not-found and permission errors; unmatched `/api/` paths return a JSON `404`
- [x] 15.7 `backend/internal/httpapi/admin_test.go`: every route refuses the roles it should, called directly without the console — including that a non-`ADMIN` is refused both session routes, and that revoking one session leaves another subject's session usable

## 16. Console embedding, listeners and process wiring <!-- complexity: opus | touches: backend/cmd/acmeservice/main.go, backend/internal/webui, backend/internal/httpapi/server.go -->

- [x] 16.1 `backend/internal/webui/webui.go`: `//go:embed dist` plus a handler serving hashed assets with a long `Cache-Control`, `index.html` with `no-store`, and a history fallback for unmatched `GET`s that are neither under `/api/` nor file-shaped
- [x] 16.2 `backend/internal/httpapi/server.go`: build the three Echo instances — challenge (`/present`, `/cleanup`, API-key auth, strict body limit), admin (`/api/v1/**` + console, session + CSRF + role middleware), internal (`/healthz`, `/readyz`, `/metrics`) — each with its own middleware chain and no route of another
- [x] 16.3 `backend/cmd/acmeservice/main.go`: parse config, run migrations, open the store, build the zone view, start the DNS listeners and the three HTTP listeners in that order; fail fast with a named setting on any startup check
- [x] 16.4 `backend/cmd/acmeservice/main.go`: graceful shutdown on SIGTERM/SIGINT — stop accepting, drain HTTP and DNS within the grace period, close the pool, exit zero; forced exit with a logged warning on timeout
- [x] 16.5 `backend/internal/httpapi/server_test.go` and `backend/cmd/acmeservice/main_test.go`: management, health and metrics routes are absent from the challenge listener; metrics are absent from the admin listener; a deep link returns the entry document while an unknown `/api/` path returns JSON `404`; an in-flight request completes across a shutdown signal

## 17. Observability and expiry sweep <!-- complexity: sonnet | touches: backend/internal/obs, backend/internal/sweeper -->

- [x] 17.1 `backend/internal/obs/metrics.go`: Prometheus collectors — HTTP requests by listener/route/status, DNS queries by type and rcode, active challenge count, rows swept, datastore availability, authentication outcomes by reason (invalid key and disallowed source address counted separately)
- [x] 17.2 `backend/internal/obs/health.go`: liveness (no dependency checks) and readiness (listeners serving and database reachable) handlers for the internal listener
- [x] 17.3 `backend/internal/obs/logging.go`: `log/slog` JSON handler at the configured level, plus the shared attribute set for publish, withdraw, authorization refusal and authentication failure events — key identifier, FQDN, submitted name form, resolved alias, outcome; never a secret, session id or token
- [x] 17.4 `backend/internal/sweeper/sweeper.go`: ticker deleting expired challenge rows, recording the count as a metric and logging each sweep at debug level
- [x] 17.5 `backend/internal/sweeper/sweeper_test.go`: an expired value stops being answered before the sweep runs, and the sweep then deletes the row

## 18. Console shell <!-- complexity: opus | touches: frontend/src/app/core, frontend/src/app/app.config.ts, frontend/src/app/app.routes.ts -->

- [x] 18.1 `frontend/src/app/app.config.ts`: `provideHttpClient(withXsrfConfiguration({cookieName: 'XSRF-TOKEN', headerName: 'X-XSRF-TOKEN'}))` and router configuration
- [x] 18.2 `frontend/src/app/core/auth.interceptor.ts`: on `401`, remember the requested URL and redirect to the sign-in endpoint; on a `401` mid-session, surface a re-authentication prompt rather than failing silently
- [x] 18.3 `frontend/src/app/core/session.service.ts`: load `/api/v1/me` once, expose display name, groups and roles as a signal, and return the user to the remembered URL after sign-in
- [x] 18.4 `frontend/src/app/core/role.guard.ts`: route guard driven by those roles — hiding controls only, never the enforcement
- [x] 18.5 `frontend/src/app/core/api.service.ts`: typed wrappers over `/api/v1` and one error-surfacing path so a server-side rejection reason reaches the user verbatim
- [x] 18.6 `frontend/src/app/core/wildcard.validator.ts`: the D4 wildcard rules as an Angular validator, mirroring `internal/dnsname`
- [x] 18.7 `frontend/src/app/core/*.spec.ts`: unit tests for the wildcard validator, the role guard and the interceptor's `401` handling

## 19. Console screens <!-- complexity: sonnet | touches: frontend/src/app/features -->

- [x] 19.1 `frontend/src/app/features/projects/`: project list and project detail (its load balancers); create and delete offered only to `ADMIN`
- [x] 19.2 `frontend/src/app/features/loadbalancer/`: nodes and their IPv4/IPv6 addresses, the domain table, and the DNS plan panel (address, CNAME and CAA rows) with per-record verification status, a copy button and a zone-snippet download — a `not-published` CAA row must read as informational, not as an error
- [x] 19.3 `frontend/src/app/features/domains/`: the domain editor with the wildcard rules stated inline — a wildcard covers exactly one label and does not cover its own base — validated as you type, surfacing server-side rejections and the "already covered by a wildcard" warning
- [x] 19.4 `frontend/src/app/features/apikeys/`: create with a one-time secret reveal behind an explicit "I have stored it" acknowledgement, CIDR editor pre-filled with the load balancer's node addresses, rotate, revoke, and `last_used_at`
- [x] 19.5 `frontend/src/app/features/challenges/`: live challenge values with alias, age and publishing key
- [x] 19.6 `frontend/src/app/features/audit/`: the audit trail filterable by project, actor and action
- [x] 19.7 `frontend/src/app/features/settings/`: the redacted effective-configuration view, showing which Keycloak group is configured to grant each role — read-only, because the mapping is environment configuration
- [x] 19.8 `frontend/src/app/features/sessions/`: the Active sessions screen — subject, started, last used, expires, with revoke-one and revoke-all-for-this-person behind a confirmation; `ADMIN` only, hidden for everyone else. A list, a button and a confirmation: it exists because it is the performable half of the "remove from the group *and* revoke the sessions" procedure (design D10)

## 20. Command-line administration <!-- complexity: sonnet | touches: backend/internal/adminctl, backend/cmd/acmeservice/cli.go -->

- [x] 20.1 `backend/cmd/acmeservice/cli.go`: subcommand dispatch — `serve` (default), `migrate up|down|version`, and the `adminctl` verbs — operating directly against the datastore with no server process running
- [x] 20.2 `backend/internal/adminctl/inventory.go`: create and list projects, load balancers, nodes and domains through the same `internal/inventory` service the API uses, so validation cannot diverge; printing the CNAME that must be published after a domain is created
- [x] 20.3 `backend/internal/adminctl/plan.go`: `loadbalancer dns <id>` rendering the DNS plan (table and zone snippet) — also the assertion path for the acceptance suite
- [x] 20.4 `backend/internal/adminctl/key.go`: `key create|list|revoke`, printing the secret exactly once

## 21. Development stack <!-- complexity: sonnet | touches: development/ (excluding development/acceptance), backend/.air.toml -->

- [x] 21.1 `development/compose.yaml`: PostgreSQL, Keycloak, and the dev-only Traefik overlay; `development/.env` holding the environment for all of them. **No named volumes** — every stateful service bind-mounts to `./data/<service>/` (`./data/postgres:/var/lib/postgresql/data`, `./data/keycloak:/opt/keycloak/data`, and likewise for anything else with state), so the stack's state is ordinary inspectable files and a reset is `rm -rf`
- [x] 21.2 `development/data/.gitkeep` committed and `development/data/*` gitignored, so the directory exists on a fresh clone while its contents never do
- [x] 21.3 `development/traefik/dynamic.yaml`: `PathPrefix('/api')` → the Go admin listener at higher priority, `PathPrefix('/')` → `ng serve` on 4200, both presented at `http://localhost:8088` so development runs on one origin exactly as production does; WebSocket passthrough for HMR
- [x] 21.4 `backend/.air.toml`: rebuild and restart the binary on change
- [x] 21.5 `development/realm/acmeservice-realm.json`: Keycloak realm export with the OIDC client, the Group Membership mapper (claim `groups`, full group path **off**, in the ID token), the four role groups, and one test user in each; `development/.env` sets `ACME_ADMIN_GROUP`, `ACME_CONFIG_ADMIN_GROUP`, `ACME_KEY_ROTATION_GROUP` and `ACME_READ_ONLY_GROUP` to match
- [x] 21.6 `development/scripts/up.sh`: create the `data/<service>` directories before `docker compose up` (Postgres fails on a missing mount point rather than creating it), then start the stack; `development/scripts/down.sh` stops it
- [x] 21.7 `development/scripts/reset.sh`: wipe `development/data/*` and re-create the directories, so a reset needs no `docker volume` command
- [x] 21.8 `development/scripts/seed.sh`: seed a sample project, load balancer, wildcard domain and API key through `adminctl` so a fresh clone reaches a populated console
- [x] 21.9 `development/scripts/` completed by `lib.sh` (shared helpers including health-gated waiting), `down.sh` (stop, preserving state), `destroy.sh` (stop and wipe `development/data/`, prompting unless `--yes`), `status.sh` (containers, URLs, the four test logins, whether it is seeded) and `logs.sh`; `scripts/dev.sh` wires `up.sh`, `air -c .air.toml` and `ng serve admin --host 0.0.0.0 --disable-host-check --port 4200` together, and `scripts/test-integration.sh` points `TEST_DATABASE_URL` at the dev stack's PostgreSQL — the only place the database-backed tests run. Both documented in `README.md`

## 22. Packaging and CI <!-- complexity: sonnet | touches: Dockerfile, .gitlab-ci.yml, .dockerignore -->

- [x] 22.1 `Dockerfile`: Node stage runs `ng build admin` into `backend/internal/webui/dist`, Go stage builds the static binary, final minimal image runs as a non-root user
- [x] 22.2 `Dockerfile`: grant `CAP_NET_BIND_SERVICE` on the binary so the unprivileged process can bind port 53, and verify it in the built image
- [x] 22.3 `.gitlab-ci.yml`: stages for lint (`golangci-lint`, `ng lint`), Go unit tests with `-race`, frontend unit tests, image build, and publish on tags. **Do not set `TEST_DATABASE_URL`** — the database-backed tests skip in CI by design (design D13); they are a dev-stack activity
- [x] 22.4 `.gitlab-ci.yml`: the acceptance job (the environment it runs is built by the change `add-acceptance-environment`; it was group 23 here) runs here as its own stage on merge, invoking `scripts/test-acceptance.sh` (the image needs `bash` and the Docker CLI, not `make`). The runners have the Docker socket mounted, so `docker compose` works — record that in a comment beside the job, because with the integration tests skipped this is the only CI job touching a real database or a real DNS exchange, and deleting it on the assumption that CI cannot start containers would leave those unverified
- [x] 22.5 `.gitlab-ci.yml`: cache the frontend build on `package-lock.json` and the Go build and module caches; confirm the pipeline fails when any suite fails

## 24. Rollout runbook <!-- complexity: sonnet | touches: README.md -->

- [x] 24.1 `README.md` (rollout section): the ordered deployment steps from design's Migration Plan — bring the two-service Compose stack up on the VM undelegated and verify the apex SOA answers over UDP and forced TCP *from the address the delegation will name*, publish the NS delegation with both A and AAAA glue, publish the `acme-api` and `acme-console` A and AAAA records in the **parent** zone, register the Keycloak client with its Group Membership mapper and set the four `ACME_*_GROUP` variables, capture the existing fleet, onboard one test domain, move one Traefik router, then roll out per domain — each with the command that verifies it
- [x] 24.2 `README.md`: the host prerequisites from design D15 — a static public address, nothing else bound to port 53 (`systemd-resolved` needs `DNSStubListener=no` or to be moved off the address), and `./data/postgres/` created before first start
- [x] 24.3 `README.md`: publish **two** NS names for the delegation, both resolving to the single instance, and state in the runbook that this satisfies validators that reject a lone NS record and provides **no** redundancy — one server with two names. Plus a delegation-verification step asserting `dig DS acmeservice.cloudpowered.services` returns an **empty** answer. Our zone is unsigned; a DS record on it makes every validating resolver treat the whole subtree as bogus, breaking every aliased domain at once while a direct query to our server still looks correct. Some registrar workflows add a DS automatically, so its absence is checked, not assumed
- [x] 24.4 `README.md`: the rollback path — move the router back to its previous resolver; issued certificates are files in Traefik's store and stay valid. Include the upgrade caveat from design D8: an older image refuses to start against a schema a newer one migrated, so rolling back *across a migration* is a restore-from-backup operation, and a database backup is taken immediately before deploying any release that carries one
- [x] 24.5 `README.md`: the alerts to add — readiness probe, DNS failure rate, authentication failure rate, and an external synthetic query against the zone apex
- [x] 24.6 `README.md`: the per-domain onboarding procedure an operator follows, why a wildcard is preferred where the customer has many hostnames, that **Traefik 2.11 or newer** is required — the real requirement being that the bundled lego follows CNAMEs by default, since the Traefik-to-lego mapping is not monotonic (design D2) — that `LEGO_DISABLE_CNAME_SUPPORT` must never be set, and how to ask a customer for the generated CAA records — optional, defence in depth, and harmless to decline

> Three things are deliberately outside this group. The infrastructure steps it references —
> provisioning the VM and its address, and getting records published in `cloudpowered.services` —
> are operational work outside this repository, tracked by design's Migration Plan rather than by a
> checkbox here. The production `compose.yaml` is part of that rollout runbook and is specified by
> design D15, not built by an application task. And `docs/` is human-facing documentation that no
> task in this change writes to.
