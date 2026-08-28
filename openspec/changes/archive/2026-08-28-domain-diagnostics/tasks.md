# Tasks

## 0. How this is split, and who owns which files

The tasks below were written before the base they land on existed, so a second
pass has grounded every one of them in the tree as it stands at
`cdc645c`. Where a task named a file, a package, a table or a helper that does
not exist or is now called something else, the correction is written inline with
a bold **Corrected:** prefix and the original sentence is kept after it — the
original is what somebody reading `design.md` will expect to find, and deleting
it would make the two documents disagree without saying so.

This change is large enough to be worked by several agents at once, so the work
is cut into GROUPS. **A group owns its files outright**: no two groups write the
same file, which is what lets them run in parallel without a merge. Every task
below carries its group letter.

| Group | What it is | Owns, exclusively | Suggested |
| --- | --- | --- | --- |
| **A** | Schema, model, store | `backend/internal/store/migrations/**`, `backend/internal/store/schema.sql`, `backend/internal/store/domain.go`, `backend/internal/store/observation.go` (new), their `_test.go`, `backend/internal/model/model.go`, `backend/internal/dnsname/wildcard.go` + `wildcard_test.go` | opus |
| **B** | `internal/certobserve` | `backend/internal/certobserve/**` and nothing else | opus |
| **C** | Inventory service, adminctl, the CLI | `backend/internal/inventory/**`, `backend/internal/adminctl/**`, `backend/cmd/acmeservice/cli.go` | opus |
| **D** | HTTP API and its contract | `backend/internal/httpapi/**`, including `backend/internal/httpapi/openapi.yaml` | opus |
| **E** | Configuration, wiring, sweeper | `config.yaml`, `backend/internal/config/**`, `backend/cmd/acmeservice/main.go`, `backend/internal/sweeper/**` | sonnet |
| **F** | Console: the dots and the dialogs | `frontend/projects/shared/src/lib/models.ts`, `frontend/projects/console/src/app/features/projects/**` | opus |
| **G** | Console: the switch | `frontend/projects/console/src/app/features/domains/**`, `frontend/projects/shared/src/lib/core/wildcard.validator.ts` | sonnet |
| **H** | Acceptance | `backend/acceptance/**` | opus |
| **I** | Documentation | `docs/**` | sonnet |

**C and E share a directory and not a file.** `backend/cmd/acmeservice/` holds
both `cli.go` (the `adminctl` dispatch, C's, because the subcommand this change
deletes is registered there and not in `internal/adminctl`) and `main.go` (the
worker wiring, E's). Neither group opens the other's file.

**The one coupling that crosses a group boundary and cannot be avoided**:
`groupForChecks` (`backend/internal/httpapi/admin_plan.go:315`) takes
`[]inventory.Record`, which C deletes in 10.4. D's 11.7 and C's 10.4 are
therefore the same edit seen from two sides — C lands first and names the
replacement type in its commit message, D follows.

**Order.** A first, because B, C and D all read the store API it defines — and
because the exact signatures A must produce are written into A's tasks, B and C
can start against them the moment A's migration and store file are in, without
either group having to ask. Then B and C in parallel, then D. E may start as
soon as A is in. F starts after D, and its first task (F/7.0) is the one edit to
`models.ts` that G needs, so G follows F by minutes rather than by a day. H is
last. I is independent of all of it.

**The two hand-off points that are not files**, and which therefore have to be
written down rather than discovered:

1. **The store API B and C call.** Named with full signatures in A's tasks
   (A/5.1a, A/5.2a, A/2.2a). Neither group invents its own.
2. **The JSON the console reads.** Named with field names in D's tasks
   (D/10.1, D/5.2, D/5.3). F does not guess them.

**Three product decisions were referred up and have come back settled.** They are
folded in where they belong, and named here because each changes something a
reader of an earlier draft would expect:

1. **The personal-account history refusal is `418`** (5.4a, design G15) — a status
   with no meaning anyone else attaches to it, so it cannot be mistaken for
   "unfunded", "unauthorised" or "wrong shape". The meaning lives in the sentinel
   and the message body rather than in the number, which is a constraint on D and
   on F both.
2. **Retention is two configuration keys**, 336h at full resolution and 4320h
   after (4.5, 6.1a), each with a comment admitting the number is unmeasured.
3. **Nothing about observation is billable — only challenges cost** (6.2). The
   `domain.observed` event is cut outright: no event, no counter, no ledger, and
   no retitling of anything in `metering-and-credits`. G17 is rewritten to the
   opposite of what it said and records the reversal. **One new constraint comes
   with it**: a domain is observed at most once an hour, floored in configuration
   validation (4.5b), because with no fee that floor is the only thing bounding
   what this service aims at somebody else's machine.

## 1. Reconciling the changes this one lands on top of

*(complete — no group)*

- [x] 1.1 Archive the eight completed changes so the main specs are the base this change's deltas apply to — `tenancy-and-permissions`, `layered-configuration`, `personal-and-team-accounts`, `domain-setup-feedback`, `domain-activation`, `swagger-ui`, `api-key-kinds`, `acme-dns-protocol`. Two ordering faults surfaced and were fixed in `personal-and-team-accounts`: it REMOVED a requirement `tenancy-and-permissions` had already removed, and it MODIFIED one that never existed. Both aborted the archive rather than corrupting anything
- [x] 1.2 Archive `health-checks` unimplemented, with `--skip-specs` so its requirements never enter the main specs. Nothing of it was built — there is no `internal/certobserve` and no `certificate_observations` table — so there is no code to reconcile, only a design whose decisions are carried forward under new numbers (G6 inverts H2, G17 replaces H6, G11/G14 keep H3/H1 unchanged)
- [x] 1.3 Rewrite `SEQUENCE.md` entry 14 to point here, and say in "What was removed" that `health-checks` was absorbed rather than cut — the distinction matters, because its certificate half is being built, just not as a screen of its own
- [x] 1.4 Note in the archived `health-checks` which change absorbed it, so somebody reading it later is not left working out why nothing was implemented
- [x] 1.5 Five changes remain open with real work in them and are NOT prerequisites of this one: `collapse-project-and-loadbalancer` (30/35), `account-invitations` (73/78), `self-service-signup` (28/34), `transactional-email` (9/17), `authentik-and-self-signup` (21/38). Two of `collapse`'s five are decisions its own design left open, and the specs still say "load balancer" where this change says "project" — a wording debt this change inherits and does not pay

## 2. The domain gains a switch and a test hostname

- [x] 2.1 **[A]** Migration: `domains.observe_tls boolean not null default false` and `domains.tls_test_host text null`. Both defaulted so every existing row is untouched and nothing starts connecting to anybody on deploy (G-constraint 3)
  - **Corrected:** the migration is `backend/internal/store/migrations/000010_domain_observation.{up,down}.sql`. `000009_invitation_expiry_recorded` is the latest; the convention is six zero-padded digits and a snake-case name, up and down as separate files, loaded through the `//go:embed migrations/*.sql` in `backend/internal/store/migrate.go`. Never edit `000001_baseline.up.sql` — it is a historical record, and `000004` shows the house style for amending an earlier table from a later file
  - The `down` drops both columns. There is nothing to preserve: an observation history keyed by domain survives the switch being forgotten
- [x] 2.1a **[A]** Regenerate the golden schema in the same commit as the migration, or `TestIntegrationSchemaGolden` fails for everybody downstream: `cd backend && TEST_DATABASE_URL=… UPDATE_GOLDEN=1 go test ./internal/store -run SchemaGolden`. It is an environment variable and not a `-update` flag, and the test is skipped in CI where there is no database — which means a stale `backend/internal/store/schema.sql` is caught by nobody until somebody runs it locally
- [x] 2.2 **[A]** Model and store: carry both on `model.Domain`, read them wherever a domain is read, so a console row renders without a second request
  - **Corrected:** `model.Domain` is at `backend/internal/model/model.go:429`. Add `ObserveTLS bool \`gorm:"column:observe_tls"\`` and `TLSTestHost *string \`gorm:"column:tls_test_host"\``. `backend/internal/store/model_drift_test.go` compares the struct against the live schema, so a column added in one and not the other fails there rather than at runtime
  - Nullable pointer rather than `string`, deliberately: "no hostname was ever set" and "the hostname is the empty string" are different, and the second is a write nothing should be able to make
- [x] 2.2a **[A]** New store writer, called by C and by nothing else. The signature is fixed here so C can be written against it before A lands:
  ```go
  // store/domain.go
  func UpdateDomainObservation(ctx context.Context, db *gorm.DB, sc store.Scope,
      domainID model.ID, observe bool, testHost *string) (*model.Domain, error)
  ```
  Scoped, NOT under `CrossAccount` — this is a tenant write on a request path and row-level security is the thing protecting it. `CrossAccount` belongs to the tickerless background readers (`DueDelegations`, `Snapshot`) and to nothing that runs with a session
- [x] 2.3 **[A/C]** Validation, in the store and not only in the API: enabling on a wildcard without a test hostname is refused, and a hostname the wildcard does not cover is refused. Reuse the wildcard rules `service-inventory` already enforces rather than writing a second matcher — a hostname accepted here and rejected by the domain rules is a row nothing can ever observe (G8)
  - **A's half is done:** `dnsname.Covers(pattern, host)` exists and `store.UpdateDomainObservation` refuses both cases before writing. C's half is the API-side rejection and its message
  - **Corrected:** the wildcard rules live in `backend/internal/dnsname/wildcard.go` (`ValidateDomain`, `Counterpart`), not in an inventory validator. There is no "is this host covered by this wildcard" helper today. **[A]** adds one there, beside the rules it has to agree with:
    ```go
    // dnsname/wildcard.go
    // Covers reports whether host is a name the wildcard pattern matches: exactly
    // one label beneath it, no more. *.example.com covers www.example.com and does
    // NOT cover a.b.example.com, which is what a TLS client would also conclude.
    func Covers(pattern, host string) bool
    ```
  - **[A]** enforces it in the database too — a `check` constraint on `domains` that `observe_tls` is false or `name` does not start with `*.` or `tls_test_host is not null`. The database cannot do the covering test; it can do the "a wildcard with the switch on has *a* hostname" half, and that is the half a future code path can forget
  - **[C]** enforces the covering test in `inventory`, which is where the name rules are already called from, and returns `inventory.ErrInvalid` (aliased to `dnsname.ErrInvalid`) so it maps to 400 without D writing a case
- [x] 2.4 **[C]** Never derive a hostname. `www.<domain>` is the obvious guess and it is a guess; a customer whose wildcard serves `a.example.com` and not `www` would get a red dot for a name they never deployed, which is the worst possible first impression of a check (G8)
- [x] 2.5 **[A]** Disabling the switch keeps the hostname. Somebody who turns it back on gets their hostname back rather than being asked again (G8). `UpdateDomainObservation` writes `tls_test_host` only when a non-nil `testHost` is supplied; a nil one leaves the column alone
- [x] 2.6 **[C/D]** Both writable through the permission that already writes a domain — not a new one. This is a property of a domain, not a new object
  - **Corrected:** the permission is `authz.ActionInventoryWrite` (`"inventory.write"`), the same one `CreateDomain` and `DeleteDomain` authorize against. Do not add an action; adding one means a `role_grants` migration and an edit to `backend/internal/store/defaultroles.go` for a property of a row
- [x] 2.7 **[C]** **A domain has no update path at all today — this change has to add one.** `backend/internal/httpapi/admin.go:294-296` registers exactly `GET /projects/:id/domains`, `POST /projects/:id/domains` and `DELETE /domains/:id`. There is no `inventory.UpdateDomain`, no `PATCH`, and nothing in the console that opens an existing domain for editing. Everything in group 9 assumes one. **[C]** adds:
  ```go
  // inventory/domain.go
  func UpdateDomain(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject,
      domainID model.ID, observe bool, testHost *string) (*model.Domain, error)
  ```
  authorizing `ActionInventoryWrite` on the domain's project through the existing `objectFor` / `authorize` pair in `backend/internal/inventory/authorize.go`, and recording an audit event
- [x] 2.8 **[D]** Register `acct.PATCH("/domains/:id", a.updateDomain)` beside the DELETE at `backend/internal/httpapi/admin.go:296`. PATCH and not a dedicated `/observation` sub-route, because `PATCH /projects/:id` and `PATCH /nodes/:id` are already the house shape for "change a property of a thing", and a sub-route for one boolean is a route whose only purpose is to need a second one later

## 3. `internal/certobserve` — the handshake and what is read from it

**[B] owns `backend/internal/certobserve/**` and writes nothing outside it.**
Suggested files: `observe.go` (the handshake and the classification), `state.go`
(the derived state and the banding), `worker.go` (`Run`/`Pass`), and a test
beside each.

- [x] 3.1 **[B]** New package, deliberately NOT part of `internal/resolvercheck`. That one is advisory and persists nothing because DNS is somebody else's mutable state; an observation is a fact about an exchange we took part in, it is stored, and it is shown to the customer as history. Same shape of outbound probe, different status, and they must not be conflated in the code or in anyone's head (G10)
  - Say this in the package doc comment, naming `resolvercheck`, the way `internal/queryobs`'s doc comment names the rule it exists to keep. The next person to see two packages that both dial out is going to want to merge them, and the reason not to has to be in the file rather than in a design document
- [x] 3.2 **[B]** One handshake per (domain, node address, family), port 443, with the domain's name — or its `tls_test_host` — as the server name (G6). Each family is a separate attempt and a separate row: a node reached over v4 and over v6 goes through different transit and different firewalls, and a certificate renewed on one listener and not the other is invisible to a dual-stack visitor, including the customer checking in their own browser
  - Dial the ADDRESS and set `tls.Config.ServerName` to the host. Never dial the host name: resolving it would observe whatever the customer's DNS points at today, which is the thing G6 rejects
- [x] 3.3 **[B]** Complete the handshake with the transport's verification DISABLED, and evaluate expiry, name coverage and chain trust ourselves (G12). A verifying client aborts on an expired or untrusted chain, which would collapse the two most interesting findings in this change into the same "could not look" as a firewall drop. Confine this to this package — it holds no credentials and sends no data
  - `InsecureSkipVerify: true`, with the reason in a comment immediately above it, because it is the single line in this repository most likely to be "fixed" by somebody who has not read G12. Chain trust is then judged separately with `x509.Certificate.Verify` against the system pool, and name coverage with `VerifyHostname` — not by scanning the SAN list by hand, which gets wildcards wrong
- [x] 3.4 **[B]** Record on success: not-before, not-after, issuer, serial, the SANs, whether the server name asked for is among them, and whether the chain was trusted. Record the address, the family and the server name presented alongside, so a row explains itself without a join
- [x] 3.5 **[B]** Record on failure: a CLASSIFICATION of ours — refused, timed out, handshake rejected, certificate unreadable — with the underlying text kept for the log and not for the customer. A failed handshake is an observation, never an omitted row: "there is no certificate here" and "we could not look" are different sentences with different audiences, and a gap in a table is indistinguishable from a scheduler that did not run (G11)
  - The classification is a string enum in this package, mirroring how `resolvercheck` spells `Status` and `CAAStatus`: `OutcomeObserved`, `OutcomeRefused`, `OutcomeTimedOut`, `OutcomeRejected`, `OutcomeUnreadable`. It is a `check` constraint on the column too (A/5.1), so a fifth value cannot be written without both halves being changed together
- [x] 3.6 **[B]** Bounded timeout per attempt, from configuration. A node that has started blackholing must cost one bounded pass, not an unbounded one
  - One deadline covering dial AND handshake, not two. A dial that succeeds and a handshake that never completes is exactly the blackhole case, and a timeout that only bounds the dial does not bound it
- [x] 3.7 **[B]** Derive a domain's current state from the latest observation per address, worst-first, with one exception: **grey never outranks a colour**. An address we could not reach beside three we could is a line in the dialog, not a reason to grey out a dot that has real information in it (G10)
- [x] 3.8 **[B]** Band the remaining lifetime server-side — >28d, 7–28d, <7d, expired — so no client applies its own thresholds and the console and `adminctl` cannot disagree. A certificate that does not cover the name asked for bands with expired (G9)
  - The band is a string on the wire (`healthy`, `approaching`, `urgent`, `expired`, `uncovered`, `unexamined`, `unobserved`) — not a number the console maps. D/5.2 puts it in the JSON verbatim; F/7.6 switches on it and computes nothing
- [x] 3.9 **[B]** 28 rather than 30 days, deliberately: every ACME client renews at 30 by default, so a certificate with 29 days left has just been told to renew and has not yet, which is exactly when the middle band starts being informative (G9)

## 4. The seventh worker

- [x] 4.1 **[B]** Schedule it like `internal/activation` — ticker, bounded batch, bounded parallelism inside the pass, an immediate first pass rather than one after the first tick, cancelled with the process (G13). A restart otherwise leaves somebody who just enabled the switch waiting out a full interval for a service that is already running
  - **Corrected:** `internal/activation` has no worker TYPE to copy. It is two package-level functions and an options struct — `activation.Run(ctx, db, checker, activation.Options{…}, log)` and `activation.Pass(…) (seen, checked int, err error)` in `backend/internal/activation/activation.go` — started as a bare goroutine from `backend/cmd/acmeservice/main.go:266`. Match that shape exactly: `certobserve.Run(ctx, db, certobserve.Options{…}, log)` and `certobserve.Pass(…) (seen, observed int, err error)`. The split matters for the same reason it does there — `Pass` is what a test calls, and a test that has to start a ticker to assert one pass is a test with a sleep in it
  - The immediate-first-pass shape is literally: do the work, THEN `select { case <-ctx.Done(): return; case <-t.C: }`. `sweeper.Run` gets this wrong (it selects first) and `activation.Run` and `reporting.RunSnapshots` get it right; copy the two that are right
  - `Pass` splits into a probe phase with bounded parallelism and a single-goroutine write phase, as `activation.Pass` does, so the process never holds a connection from the pool per outstanding handshake. That is not a micro-optimisation: a pass of 256 blackholing addresses against a pool of ten is a deadlock, and it is the reason the phases are separate in `activation`
- [x] 4.2 **[E]** Cadence six hours, configurable. Nothing depends on an observation inside a renewal window, a certificate does not change between one hour and the next, and four observations a day against a thirty-day window is ample
- [x] 4.3 **[B]** Batch and parallelism are CONSTANTS, not configuration, for the same reason they are in `activation`: they bound the burst this process aims at somebody else's machine and how long one tick may run, not behaviour anybody tunes
  - Match the names and the reasoning of `activation.DefaultBatch = 256` and its unexported `parallelism = 8`. **Take parallelism lower here — 4** — because these are TLS handshakes against a customer's production load balancer rather than DNS queries against our own recursor, and eight simultaneous connections from one source is the shape of something a rate limiter notices
- [x] 4.4 **[B]** Queue order: a domain whose switch was just turned on before a domain due for routine re-observation, so a batch too small for the backlog delays the settled work and never the person watching the screen (G13)
  - The pattern to copy is `store.DueDelegations` (`backend/internal/store/domain.go:153`), which does this with `order by bool_or(d.last_success_at is null) desc, min(d.last_checked_at) nulls first`. Here the equivalent is "never observed first, then longest since last observed" — `order by (last observation is null) desc, last observed asc nulls first`. A/5.1a owns the query; B owns using it
- [x] 4.5 **[E]** Configuration: interval, handshake timeout, and a global off switch. The off switch is the operational form of "nothing acts on an observation" — turning it off costs a stale dialog and nothing else (G14)
  - **Corrected:** adding one configuration key is six edits, not one, and all six are in E's files. A new top-level `observation:` block in `config.yaml` (NOT inside `challenge:`, which is where the activation cadences live — those are there because what they gate is whether a challenge is published at all, and this gates nothing); the koanf block plus its `Defaults()` and its line in the flattening at `backend/internal/config/schema.go:378`; the flat fields on `config.Config` with their `ACME_*` names in the doc comment; the three rows in the env-to-path table at `backend/internal/config/schema.go:483`; a `positive(…)` line each in `backend/internal/config/validate.go`; and a `slog.Duration`/`slog.Bool` line each in `backend/internal/config/redact.go`
  - Keys: `observation.enabled` (`ACME_OBSERVATION_ENABLED`, default `true`), `observation.interval` (`ACME_OBSERVATION_INTERVAL`, default `6h`), `observation.handshake_timeout` (`ACME_OBSERVATION_HANDSHAKE_TIMEOUT`, default `5s`), `observation.full_resolution_window` (`ACME_OBSERVATION_FULL_RESOLUTION_WINDOW`, default `336h`), `observation.retention` (`ACME_OBSERVATION_RETENTION`, default `4320h`)
  - `TestShippedFileRestatesTheBuiltInDefaults` compares `config.yaml` against the Go defaults with an empty environment and fails on any key written in one place and not the other — including a `${ACME_X:6h}` whose colon was mistyped. It is the test that catches this group's whole failure mode, so run it
- [x] 4.5b **[E]** **The observation interval takes a floor of one hour, enforced in `backend/internal/config/validate.go` and not by a comment (G17).** Refuse to start and name the setting; do NOT silently clamp to `1h`, because a deployment that asked for five minutes and got sixty should be told rather than left to find out from a graph. `positive(…)` beside it is not enough — write the floor check explicitly
  - Say WHY in the code, because the reason is not obvious and is new: certificate observation is unmetered (G17), so nothing on the customer's side bounds how much of it we do. The per-domain fee an earlier draft of this change was going to charge was quietly serving as that bound; with no fee, **this floor is the only thing standing between a mistyped configuration value and this service hammering somebody else's production load balancer**. That is the entire reason it exists and the reason it is validated rather than trusted
  - The bounded batch, bounded parallelism (4.3) and per-attempt timeout (3.6) all still hold. They bound one pass; the floor bounds how often a pass comes round, which is the half that had no owner before
- [x] 4.5c **[E]** Both retention windows carry a comment in `config.yaml` saying the number is **unmeasured**, and that it is configuration rather than a constant precisely for that reason. This is the shape `challenge.interaction_retention` already established — it is configurable, and its comment says plainly that nobody has signed off on a period and records the recommendation on file. Copy that honesty; a placeholder that looks like a considered value is worse than one that admits what it is
- [x] 4.5a **[E]** Wire the goroutine in `backend/cmd/acmeservice/main.go`, beside the `activation.Run` block at line 266. When `observation.enabled` is false, **log at info that observation is off and start nothing** — the same shape as the existing "domain activation checks are not running: no resolver to ask" warning. A worker that starts and then does nothing is a worker somebody will spend an afternoon on
- [x] 4.6 **[B]** Never on a request path. `/present` is called by lego in the middle of an issuance; nothing in this change may be reachable from it
  - Enforced by construction: `certobserve` is imported by `main.go` and by nothing in `internal/dnsserver` or in the challenge handlers. Assert it — a small test in this package that walks `go list -deps` for the challenge packages and fails if `certobserve` appears is cheaper than the incident

## 5. Storing and reading observations

- [x] 5.1 **[A]** Migration: `certificate_observations`, with the materialized ownership columns and forced row-level security every tenant table carries, so reading a domain's certificate state can be granted without granting the inventory it describes
  - **Corrected:** "the materialized ownership columns" are `account_id uuid not null` and `project_id uuid not null` as real columns, plus COMPOSITE foreign keys that make them agree with the parent row — `foreign key (domain_id, account_id) references domains (id, account_id)`. That second half is the part that is easy to leave out and is the whole point: without it the columns are a denormalisation nothing checks. `000004_collapse_project_and_load_balancer.up.sql` is the worked example
  - **Corrected:** row-level security is not written per table. `000001_baseline.up.sql:732` applies it in a `do $$ … $$` loop over an array of table names. A new table adds its own three statements in its own migration — `enable row level security`, `force row level security`, and `create policy tenant_isolation on certificate_observations using (cross_account_read() or account_id = current_account()) with check (…)` — spelled exactly as the loop spells them, because `schema_golden_test.go` dumps the policy text and any difference is a diff
  - Migration `000011_certificate_observations.{up,down}.sql`, separate from `000010` so the two are legible apart; both belong to A, so there is no race
  - Columns: `id uuid primary key default uuidv7()`, `account_id`, `project_id`, `domain_id uuid not null references domains (id) on delete cascade`, `node_id uuid not null references lb_nodes (id) on delete cascade`, `family text not null check (family in ('A','AAAA'))`, `address inet not null`, `server_name text not null`, `observed_at timestamptz not null default now()`, `outcome text not null check (outcome in ('observed','refused','timed-out','rejected','unreadable'))`, `failure_detail text`, `not_before timestamptz`, `not_after timestamptz`, `issuer text`, `serial text`, `sans text[]`, `covers_server_name boolean`, `chain_trusted boolean`
  - `on delete cascade` and not the `restrict` this repository otherwise prefers, deliberately: an observation is a fact ABOUT a domain and means nothing without it, and `restrict` would make deleting a domain fail the moment it had ever been observed. `lb_nodes` already cascades from its parent for the same reason
  - Index `(domain_id, observed_at desc)` — every read in this change is "the latest per address for one domain" or "the history for one domain", and both start there
- [x] 5.1a **[A]** The store API B calls. Fixed here so B can be written before A lands:
  ```go
  // store/observation.go
  type DueObservation struct {
      DomainID   model.ID `gorm:"column:domain_id"`
      AccountID  model.ID `gorm:"column:account_id"`
      ProjectID  model.ID `gorm:"column:project_id"`
      ServerName string   `gorm:"column:server_name"`  // tls_test_host, or the domain's own name
      NodeID     model.ID `gorm:"column:node_id"`
      Family     string   `gorm:"column:family"`       // "A" | "AAAA"
      Address    string   `gorm:"column:address"`
  }

  // DueObservations returns one row per (enabled domain × node address × family)
  // that is due, newly-enabled work first. Wrapped in CrossAccount: the worker
  // runs on a ticker with no session, so a bare statement selects from an empty
  // view of every tenant table and writes nothing at all, for ever, without
  // erroring — see reporting.Snapshot's doc comment for the same trap.
  func DueObservations(ctx context.Context, db *gorm.DB, now time.Time,
      interval time.Duration, limit int) ([]DueObservation, error)

  func RecordObservation(ctx context.Context, db *gorm.DB, o model.CertificateObservation) error
  ```
  A wildcard domain with `observe_tls` true and a null `tls_test_host` cannot exist (2.3), so the query needs no coalesce beyond `coalesce(d.tls_test_host, d.name)`
- [x] 5.2 **[D]** A read returning the current state per node address for one domain — available on any account, because it is what colours the dot
  - `GET /accounts/:accountId/domains/:id/certificate`, registered beside `POST /domains/:id/acme-dns` at `backend/internal/httpapi/admin.go:303`. Authorizes `authz.ActionViewInventory`, which is what already gates seeing the domain
  - Response shape, which F reads and does not guess: `{ "serverName": "...", "band": "healthy|approaching|urgent|expired|uncovered|unexamined|unobserved", "addresses": [ { "nodeId", "nodeLabel", "family": "A|AAAA", "address", "observedAt", "outcome", "band", "notAfter", "issuer", "coversServerName", "chainTrusted" } ] }`. `band` at the top is B's worst-of (3.7); the per-address `band` is the same vocabulary applied to one row
- [x] 5.2a **[A]** The store reads D calls, fixed here for the same reason:
  ```go
  // store/observation.go — both SCOPED, not CrossAccount: they run on a request.
  func LatestObservations(ctx context.Context, db *gorm.DB, sc Scope, domainID model.ID) ([]model.CertificateObservation, error)
  func ObservationHistory(ctx context.Context, db *gorm.DB, sc Scope, domainID model.ID, limit int) ([]model.CertificateObservation, error)
  ```
  `LatestObservations` is a `distinct on (node_id, family) … order by node_id, family, observed_at desc`. `ObservationHistory` is newest-first with a caller-supplied cap; give it `reporting.MaxBuckets`'s reasoning and a hard ceiling, because "every observation ever" is a typo rather than a question
- [x] 5.3 **[D]** A read returning the history for one domain, refused on a personal account (G15). Refuse it in the SERVICE, not by having the console hide a section it received
  - `GET /accounts/:accountId/domains/:id/certificate/history`
- [x] 5.4 **[D]** The refusal names the account kind, not a missing permission. `teamAccountGuard` keeps a personal account off the member roster because an account of one CANNOT have members, and the service says `409` rather than `403` precisely so a client can tell "this does not exist here" from "you may not". Nothing is meaningless about history on a personal account — it is withheld, and the refusal has to say so rather than borrow a structural justification it does not have (G15)
  - **Corrected:** there is no `teamAccountGuard`. It is `func (a *admin) requireTeamAccount(c echo.Context, sc store.Scope, what string) error` at `backend/internal/httpapi/scope.go:125`, returning `store.ErrPersonalAccountHasNoRoster`, mapped to 409 at `backend/internal/httpapi/errors.go:122`
  - **And it must not be reused here.** Its own doc comment says so: *"Anyone adding a route here should ask whether it is about PEOPLE or about CREDENTIALS; only the first belongs."* The history gate is neither, and the sentinel is literally named `…HasNoRoster` — a customer reading a message about roles and members when they asked about a certificate is exactly the "unable to explain its own behaviour" failure G15 names. Add a sibling: `store.ErrTeamAccountFeature`, with its own message stating the commercial reason plainly, and add a line to `requireTeamAccount`'s doc comment pointing at it so the next reader is not left choosing between two guards by guesswork
- [x] 5.4a **[D]** **The status is `418`** (G15). Map `store.ErrTeamAccountFeature` to `http.StatusTeapot` in `backend/internal/httpapi/errors.go`, add it to the status table in `handleError`'s doc comment at line 33 like every other sentinel, and write the reasoning in the `case` arm as the neighbouring arms all do
  - It is deliberately a status nothing else in this service or in anybody's client library means anything by. The three plausible codes each already carry a meaning a client acts on wrongly: `403` sends somebody to check permissions that are working perfectly, `402` is this service's "suspended or unfunded" and drops the console into a funding flow that cannot resolve this, and `409` is the *structural* refusal — "an account of one cannot have members" — which is exactly the justification G15 says this gate does not have and must not borrow
  - **Record the cost in the comment, because it is real**: some proxies and client libraries treat `418` as unknown, strip the reason phrase, or collapse it into a generic error. So **the number is not where the meaning lives.** The sentinel and the `message` body carry it. That is a constraint on two other groups and is repeated where they will read it — F/8.8 and D/11.8
- [x] 5.4b **[D]** Document the `418` explicitly in `backend/internal/httpapi/openapi.yaml` on the history path — a `418` response with a description saying what it means. No generator infers a teapot and every one of them will omit it silently, which is the failure mode that makes an undocumented status worse than an unusual one. This is the one place in 11.8 where a stanza gets more than `description: OK`
- [x] 5.5 **[B]** Record history for every account, not only team ones. Upgrading otherwise produces a feature with no data in it for weeks (G15). Nothing in `certobserve` reads the account kind — say so in a comment, because "only record what we show" is the optimisation somebody will propose

## 6. Retention and billing

- [x] 6.1 **[E]** Retention in the EXISTING sweeper, not a new worker: full resolution for a recent window, then at most one observation per domain per address per day (G16). Downsampling rather than deletion, because the value of the history is a trend measured in weeks and truncating the period destroys the shape of it
  - **Corrected:** the sweeper is package-level functions, not a type — `sweeper.Run(ctx, db, interval, interactionRetention, m, log)` and `sweeper.Sweep(…)` in `backend/internal/sweeper/sweeper.go`, wired at `main.go:251`. Add one step to `Sweep`, after `DeleteOldClosedInteractions`, and one line to its closing `log.Debug`. Follow `DeleteOldClosedInteractions`'s convention that a retention of zero or less is a no-op that keeps everything
  - One statement, not a loop: `delete from certificate_observations where observed_at < ? and id not in (select distinct on (domain_id, node_id, family, observed_at::date) id from certificate_observations where observed_at < ? order by domain_id, node_id, family, observed_at::date, observed_at desc)`. Under `store.CrossAccount`, for the reason 5.1a gives
- [x] 6.1a **[E]** The two retention numbers are **336h at full resolution and 4320h thereafter**, as the two configuration keys in 4.5 rather than as constants. `design.md`'s Open Question about them is closed and records that they are unmeasured, which is exactly why they are configuration: the precedent is `challenge.interaction_retention`, configurable for the identical reason that nobody has signed off on a period. 4.5c is the comment that has to say so
- [x] 6.2 **[—]** **CUT. Nothing about observation is billable — only challenges cost.** ~~Emit `domain.observed` from the periodic rate job: one row per account per period per domain with the switch on.~~
  - This reverses G17, which now says the opposite, and the reversal is recorded there rather than dropped silently. Two things made it right: there is no rate job to emit into — no `usage_events`, no ledger, no `internal/metering`, nothing in `backend/` that emits a rate event, because the tick job is unimplemented task 4.1 of `metering-and-credits` at `SEQUENCE.md` step **15**, after this one — and, more decisively, the property the metered version was protecting (an invoice that does not move when we change the cadence or somebody adds an IPv6 address) holds trivially when the price is zero, without a rate event, a table, an idempotent emitter and a line on a bill needing explanation
  - **Nothing replaces it.** No event, no counter on `project_daily_stats`, no ledger row, no placeholder priced at zero. `metering-and-credits`'s own task naming `node.observed` should be **dropped rather than retitled** — but that is that change's edit to make, not this one's, and no task here touches it
  - What DOES come with this is the hourly interval floor, 4.5b. It is not a consolation prize: the fee was the only thing bounding how often we connect to a customer's machine, and the floor is what takes over that job
- [x] 6.3 **[—]** Observations produce no ledger entry. They are operational data with their own table and their own retention. **Corrected:** there is no ledger to make an entry in and, after 6.2, nothing in this feature is metered at all — so this is satisfied by construction and needs no work. Kept as a statement because it is the thing a later metering change must not undo

## 7. The console: the line of dots

**[F] owns `frontend/projects/shared/src/lib/models.ts` and everything under
`frontend/projects/console/src/app/features/projects/`.** G needs one field from
`models.ts`, so 7.0 is F's first task and G starts after it.

- [x] 7.0 **[F]** Land the wire types first, in one edit, so G is unblocked: on `Domain` (`models.ts:50`) add `observeTls: boolean` and `tlsTestHost?: string`, plus `cnameTarget?: string` and `expectedCaa?: string[]` from 10.1/10.2; add a `CertificateState` interface matching D/5.2's JSON verbatim; delete `PlanRecord` (line 219) and `DNSPlan` (line 226) and, with 11.7, `CheckResult` (241) and `VerificationResult.results` (303)
- [x] 7.1 **[F]** Replace the Validation column's labelled chips with a line of dots on the domain row: a filled circle, the record TYPE beside it, a tooltip, and a click (G3)
  - The cell is `project-detail.component.ts:209-227`, fed by `checks(d)` (line 648) and `answerFor(d)` (line 677). The Validation `<th>` at line 165 carries `nzWidth="18rem"`; a row of three dots wants far less, and leaving it at 18rem is how a table ends up with a column of whitespace
- [x] 7.2 **[F]** Label the record type, never the verdict. A label reading "configured incorrectly" makes a row's width depend on how broken it is, and a table whose columns move as state changes is a table nobody can scan (G3)
- [x] 7.3 **[F]** A record type this domain does not have gets NO dot, not a grey one: CAA with no configured issuer, TLS with the switch off. A grey dot would claim we meant to look (G3)
- [x] 7.4 **[F]** `CNAME`: green correct, **red incorrect**, **yellow missing**, grey for not-checked / could-not-check / no-alias (G4). This inverts what `domain-setup-feedback` shipped, where absent and incorrect were both red — `missing` is overwhelmingly somebody mid-task who added a domain five minutes ago, and it is the state every new domain passes through; `incorrect` is a record that exists, looks finished, and will never work
  - The states come from `resolvercheck.Status` and are already modelled in `domain-status.ts`'s `delegationChip`, which today returns red for both `missing` and `incorrect`. Change the mapping there, not at the call site, and keep the existing `hasAlias` argument — it is what distinguishes "we have not looked" from "there is nothing to look for"
- [x] 7.5 **[F]** CAA: green for `correct` AND for `not-published`, red for `blocking`, grey for `undetermined` (G5). The two green states are not the same fact and the tooltip does not pretend they are — "authorises us" / "no restriction" — but they are the same instruction, which is *do nothing*, and that is what a dot carries
  - `caaChip` in `domain-status.ts` returns blue for `not-published` today. Blue on a three-colour scale means "unclear" to everybody who has not read the tooltip, which is the whole reason G5 collapses it
- [x] 7.6 **[F]** TLS: grey never-observed, grey handshake-failed, red uncovered or expired or <7d, amber 7–28d, green >28d (G9). Take the band from the service (3.8) rather than computing thresholds here — switch on the `band` string, and never on `notAfter`
- [x] 7.7 **[F]** A failed handshake is GREY, not red. It is the one place our own probe's failure meets a colour scale, and red would claim the customer's node is broken when we cannot distinguish that from our transit, their firewall, or a listener that is simply not on 443 (G9)
- [x] 7.8 **[F]** Every dot is clickable, including a green one. A control that appears only when something is wrong teaches people to read its presence as an alarm (G3)
- [x] 7.9 **[F]** Keep the activation chip and its "last looked / last seen" stamp. It answers a different question from every dot beside it — what the service will DO for this domain, from what it has stored, which is what `/present` actually consults — and it is the one signal that does not soften as a delegation stays absent (G4). `activationChip` in `domain-status.ts` and the Challenges cell at `project-detail.component.ts:179-208` are untouched by this change
- [x] 7.10 **[F]** Offer **Check now** on every row, dropping the "never seen or stopped resolving" condition. An active domain whose certificate expires in nine days is exactly the row somebody re-checks after fixing something, and `Run verification` is about to disappear with the panel (G18)
  - Delete `canCheckNow(d)` (`project-detail.component.ts:639`) outright and drop the `@if` at line 203, rather than making it return `true`. A predicate that is always true is a predicate somebody restores

## 8. The console: one dialog per record type

- [x] 8.1 **[F]** Split `domain-help.modal.ts` into three. One dialog covering every record type has to lead with a section the reader did not click on (G7)
  - `frontend/projects/console/src/app/features/projects/domain-help.modal.ts` (225 lines) becomes `delegation-help.modal.ts`, `caa-help.modal.ts` and `certificate.modal.ts`, each with its own `open…Modal(modal, data)` function in the shape of the existing `openDomainHelpModal` (line 215). `explain(d)` at `project-detail.component.ts:698` goes with them, replaced by three handlers hung off the dots
  - `caaLines(name, issuers)` and the `incorrect` prose survive verbatim — move them, do not rewrite them
- [x] 8.2 **[F]** The `CNAME` dialog prints `_acme-challenge.example.com. IN CNAME k7f2q9.acmealias.com.` complete, with a copy control, in every state — including green, where it names the record in effect
- [x] 8.3 **[F]** Keep the deleted-and-re-added explanation verbatim for `incorrect`. It is the one cause a customer will not guess: a new alias is issued, and a delegation left pointing at the old one is published, well-formed, and will never validate
- [x] 8.4 **[F]** The CAA dialog prints both `issue` and `issuewild` for every configured issuer, with the standing note to leave existing entries alone because CAA is a list. `issue` alone is not the fix — a present `issuewild` governs wildcard issuance INSTEAD of `issue`, so somebody adding only the `issue` line adds a line that is already there and stays blocked (G7)
- [x] 8.5 **[F]** The CAA dialog never offers a delegation record as the remedy for `blocking`. That sends somebody to the wrong file with the wrong value while issuance goes on being refused
- [x] 8.6 **[F]** The TLS dialog prints no record — there is nothing in DNS to fix — and instead lists every node address, in node order, family within node, so "renewed on v4 and not on v6" is one glance rather than a comparison the reader constructs (G10)
- [x] 8.7 **[F]** The TLS dialog names the hostname being tested in every state. A wildcard whose test hostname has gone stale shows red for a name nobody deployed, and this is what makes that dot explain itself the moment it is opened (G8 risk)
- [x] 8.8 **[F]** History section: rendered on a team account, and on a personal account replaced by a line saying history is part of a team account — stated, not omitted silently and not dressed as unavailable (G15). The dialog fetches D/5.3 and renders the refusal's own message; it does not carry a second copy of the sentence, which would then be the one that goes stale
  - **Key on the sentinel in the body, NOT on the status number.** The refusal is `418` (D/5.4a), chosen because no other meaning is attached to it — but some proxies and client libraries treat it as unknown or collapse it, so a console branching on `status === 418` is a console that shows the wrong thing the first time this runs behind something that rewrites it. Match on the error body the way the existing interceptor already matches other refusals, and treat an unrecognised failure as "history could not be loaded" rather than as the personal-account case
- [x] 8.9 **[F]** Never print the diagnostic text of a lookup that failed. On `undetermined` that field holds our resolver's error, addressed to an operator, and showing it reads as a verdict on the customer's zone when it is a verdict on our lookup. `project-detail.component.spec.ts` already asserts this for the current modal — carry the case over rather than writing a new one
- [x] 8.10 **[F]** Copy controls on every printed record, with the existing clipboard-refused fallback wording — the zone snippet is no longer there to fall back to, so the message must not name it
  - **Corrected:** the wording that names it is `'The browser refused clipboard access — use the zone snippet instead.'` at `project-detail.component.ts:708`, and it is deleted with `copyPlan`. So there is nothing to edit: the three dialogs need a message WRITTEN, offering the only remaining fallback, which is selecting the text. There is no shared clipboard helper — `apikey-reveal.modal.ts:79` and `acmedns-credentials.modal.ts:288` each have their own inline `copy()`. **Do the same here and do NOT refactor those two into a shared helper**: it is unrequested, it is two files outside this group's ownership, and three inline copies of four lines have never cost anybody anything

## 9. The console: the switch in the domain editor

- [x] 9.1 **[G]** A switch in `domain-editor.component.ts`, off by default, worded so it is clear nothing connects to the customer's machines until it is on — and that it is free (G17). Two sentences, both load-bearing: a switch that reaches out to somebody's production machine has to say so, and a switch on a metered product that says nothing about price is read as a charge
  - **Corrected:** `frontend/projects/console/src/app/features/domains/domain-editor.component.ts` is a CREATE-only dialog — `openDomainEditor(modal, projectId)`, titled "Add domain", which POSTs `{name, includeWildcard}` and closes. It cannot edit an existing domain, and there is no row action that opens it for one. A switch that can only be set at creation is a switch nobody can turn on for a domain they already have, which is every domain
  - So this group also adds the edit path in the console: a row action beside the acme-dns key and the delete at `project-detail.component.ts:228-259` **is F's file, so F adds the button and emits; G owns the dialog it opens.** G adds `openDomainSettings(modal, domain)` in its own file, PATCHing `/domains/${id}` (D/2.8) with `{observeTls, tlsTestHost}`
  - Keep it template-driven with a `clientProblem()` getter, as the create dialog already is. There is no reactive form anywhere in this feature and introducing one for two fields is a second idiom for nothing
- [x] 9.2 **[G]** On a wildcard, the switch reveals a required hostname field, validated client-side against the same wildcard rules and never prefilled (G8, task 2.4)
  - The shared rules are in `frontend/projects/shared/src/lib/core/wildcard.validator.ts` (`normalizeDomain`, `stripLeadingWildcard`, `wildcardOf`, `domainNameProblem`). Add `coveredByWildcard(pattern, host): boolean` there, mirroring `dnsname.Covers` (A/2.3) — exactly one label beneath, no more. Client-side validation here is a courtesy; the service refusal (2.3) is the rule, and the console must render the server's message rather than assuming its own check was the same one
- [x] 9.3 **[G]** Turning it off leaves the hostname in the field rather than clearing it, and the PATCH omits `tlsTestHost` rather than sending null — A/2.5 keeps a stored hostname only when nothing overwrites it

## 10. Moving what the plan carried onto the domain

Order matters through this group and the next: the reader of a value must exist
before the writer of it goes. The domain carries the target before the console
stops reading the plan, and the console stops reading the plan before the plan
stops being served. At no point is a value neither served nor needed.

- [x] 10.1 **[C/D]** Put the `CNAME` target on the domain read — the alias label joined to the served zone, the same string `inventory.Plan` puts in front of a customer today. A domain with no alias reports the absence rather than being omitted: the row still exists on the screen and still deserves an answer (G2)
  - **[D]** adds `CNAMETarget string \`json:"cnameTarget"\`` to `domainView` at `backend/internal/httpapi/admin_inventory.go:63`. Empty string is the absence; `omitempty` would make "no alias yet" indistinguishable from an older service, which is the thing the field exists to say
  - No new query is needed: `toDomain(d model.Domain, aliases map[model.ID]model.Alias)` (line 141) already has the alias in hand, and `a.planOptions()` (`admin.go:414`) already has the zone. That is the whole of 10.1 on D's side — three lines. Note that `toDomain` computes `ChallengeName` with `dnsname.ChallengeName(d.Name)` rather than reading `alias.ChallengeFQDN`; leave that as it is, and take the TARGET from the alias (10.3)
- [x] 10.2 **[C/D]** Put the expected CAA entries on the domain read, at the registrable name, both tags, per configured issuer, and none at all when no issuer is configured. The rules do not change — only what carries them
  - **[D]** adds `ExpectedCAA []string \`json:"expectedCaa"\`` to `domainView` — the rendered rdata lines (`0 issue "letsencrypt.org"`), which is what the dialog prints. `null` when no issuer is configured, which is what makes F/7.3 able to hide the dot without a second question
- [x] 10.3 **[C]** Take the target from the ALIAS rather than recomputing it. `dp.ChallengeName = a.ChallengeFQDN` exists in `buildPlan` for exactly this reason: agreeing by construction beats agreeing by an equality some other test defends, and the failure if the two diverged is silent — every row reading "not checked" for a delegation that is correct
- [x] 10.4 **[C]** Reshape `internal/inventory` around the per-domain expectation rather than the plan. The plan builds a full record set and then joins back onto domains, which is the indirection that made a per-domain answer hard to give in the first place (G2)
  - **Closed.** The `resolvercheck` sweep the C/D split deferred is done: `CheckAddress`, `AddressCheck` and their one helper `ipSetsEqual` are deleted, along with the table test that drove them. They lost their last caller when D removed the plan handler, and an address check has no meaning in this change — it could only ever have found that a customer's DNS disagrees with node addresses the same customer typed in
  - **C's half is done:** `DomainPlan` → `DomainExpectation`, `Record`/`DNSPlan`/`RenderZoneSnippet` and the address records deleted, `inventory.Expectations` and `inventory.DelegationTarget` published. What is left here is the sweep of `resolvercheck.CheckAddress`/`AddressCheck`, whose only remaining caller is `admin_plan.go` — do it after D's 11.7 lands
  - **Corrected:** most of the shape already exists and does not need inventing. `inventory.DomainPlan` (`backend/internal/inventory/plan.go:50`) is already `{DomainID, Name, ChallengeName, AliasID, Target, Registrable}` — the per-domain expectation with one thing missing, which is the CAA rdata itself rather than just the registrable name it sits at. Rename it `DomainExpectation`, add `ExpectedCAA []string`, and lift the rdata formatting out of `caaRecords` (line 182) into a function taking a registrable name and the issuers. What is deleted is `Record` (line 25), `DNSPlan` (line 61) and `RenderZoneSnippet` (line 220); what survives is `PlanOptions`, which is still the zone and the issuers
  - `backend/internal/inventory/plan_test.go` already holds the two assertions this reshape most easily breaks — `TestPlanDomainsShareOneDelegation` (195) and `TestPlanDomainWithoutAliasStillAppears` (230). Carry them onto `DomainExpectation` rather than deleting them with the plan; they are 12.16 and 12.17 proved a level cheaper than acceptance can prove them. `TestPlanAddressFamilies` (66) and `TestRenderZoneSnippet` (140) go with what they test
  - Keep `authz.ActionDNSPlan` (`"dns.plan"`) and do NOT rename it. It still gates verification, and renaming a permission string means migrating `role_grants` rows and editing `backend/internal/store/defaultroles.go:55` for a spelling. Add a line to its doc comment at `backend/internal/authz/permissions.go:24` saying it now gates verification alone

## 11. Deleting the plan

- [x] 11.1 **[F]** Console: delete the DNS plan section from `project-detail.component.ts` — the table, `Copy records`, `Zone snippet`, `Run verification` — and with it `copyPlan`, `zoneHref`, `statusFor`, `statusColour` and `foundFor`
  - Template lines 266–336; methods `zoneHref` (420), `loadPlan` (491), `statusFor` (568), `statusColour` (573), `foundFor` (599), `copyPlan` (703); the `plan` signal at ~415. `verify()` (503), `checkNow()` (516) and `runCheck()` (533) all stay — they feed the dots
- [x] 11.2 **[F]** `answerFor` reads the target from the DOMAIN, not from `plan()`, and `loadPlan()` goes with the rest. This is what lets the `CNAME` dialog print the record to publish on a deployment that never configured a resolver — which record a domain needs is knowable without asking DNS anything, and it stays that way. `answerFor` is at line 677; `domainCheckFor` in `domain-status.ts` already matches by `domainId` and never by name, so nothing there changes
- [x] 11.3 **[F]** Remove the CAA footnote paragraph that lived under the plan table (lines 326–330). Its content — absent CAA is not an error — is now carried by the CAA dot being green and by its dialog
- [x] 11.4 **[D]** Remove the routes `GET …/plan` and `…/plan/zone`, with `getPlan`, `getPlanZone`, `planView` and `recordView`. The routes are at `backend/internal/httpapi/admin.go:321-322`; the handlers are in `backend/internal/httpapi/admin_plan.go`, which keeps `verifyPlan` and `recheckPlan`. Leave those two named as they are — the route is still `/verification`, and renaming handlers is churn a reviewer has to read
- [x] 11.5 **[C]** Remove `inventory.RenderZoneSnippet` and the address records from the plan builder. Do NOT relocate the address records: a customer typed those addresses into the Nodes panel on this screen, and grading their DNS against them is a check whose only possible finding is that the customer disagrees with the customer (G2)
  - **Closed** with 10.4 above, and by the same deletion
  - **C's half is done** in `internal/inventory` and `authorize_test.go`. The `resolvercheck` deletion is what remains, and it waits on D/11.7 for the same reason
  - `resolvercheck.CheckAddress` and its `AddressCheck` become unreachable from anything in this repository. Delete them too — a probe with no caller is a probe somebody wires up
  - `backend/internal/inventory/authorize_test.go:28` names `RenderZoneSnippet` in its table of functions that need no authorization; remove the entry with the function, or that test fails on a name that no longer exists
- [x] 11.6 **[C]** Remove `internal/adminctl/plan.go` and its subcommand. `cnameLine` in `inventory.go` stays — it prints the one record a customer needs when a domain is created, which is the surviving half
  - **Corrected:** `cnameLine` is at `backend/internal/adminctl/inventory.go:255` — adminctl's own `inventory.go`, not the `internal/inventory` package. Its doc comment says it uses "the same shape `inventory.RenderZoneSnippet` uses for a zone file"; that sentence has to change with 11.5, since it will name a function that no longer exists
  - **Corrected:** "its subcommand" is not in `adminctl` either. `plan.go` holds only `ProjectDNS(…)`; the subcommand is the `case "project dns":` arm of `dispatchAdminctl` at `backend/cmd/acmeservice/cli.go:269`, and removing the function without the arm does not compile. Both are C's
- [x] 11.7 **[D]** Drop the per-record half of the verification response — the `results` array and `checkResult`. It answered the same question twice, keyed two ways, and only the per-domain keying has a reader (G2)
  - In `backend/internal/httpapi/admin_plan.go`: the `"results"` key of the two identical response maps at lines 180-184 (`verifyPlan`) and 421-425 (`recheckPlan`), the `checkResult` type (102), and `groupForChecks` (315) which builds the per-record checks out of `[]inventory.Record`. `recordCheck` (122) and `domainCheck` (142) STAY — they are the per-domain half
  - `groupForChecks` is the coupling named in group 0: it has to be rebuilt around C's `DomainExpectation` rather than around records. What it must keep is the deduplication it does today — one lookup per distinct challenge name and one per distinct registrable domain, not one per domain — or a project with eight domains sharing four aliases quadruples the queries it puts on our resolver. `TestRunChecksKeepsOrderAndHonoursCancellation` and `TestDomainChecksSharedDelegationReachesBothRows` in `admin_plan_test.go` are what hold that line; keep them passing rather than rewriting them
- [x] 11.8 **[D]** Remove the two plan paths from `openapi.yaml` and add the observation reads. The contract is served through Swagger UI now, so a stale path is a published lie rather than an unused handler
  - `backend/internal/httpapi/openapi.yaml`, paths at lines 688 and 697; the verification path's summary at line 711 says "Answers per plan record and again per domain", which 11.7 makes false
  - The document carries no response schemas — every entry is `"200": { description: OK }` — so adding the two observation reads and the `PATCH /domains/{id}` is four short stanzas and not a modelling exercise. Do not introduce schemas for these three while the other forty paths have none; one documented shape among forty undocumented ones reads as an oversight in the other thirty-nine. `openapi_test.go` asserts the document covers the registered route table, so a route added without a stanza fails there
  - `backend/internal/httpapi/admin_plan_test.go` is named for a thing that is going. Leave the filename; renaming it is a diff a reviewer has to read for no change in behaviour, and the tests in it are about verification, which stays
- [x] 11.9 **[H]** Rewrite `backend/acceptance/plan_test.go` around the domain read, and check `delegation_test.go` and `setup_feedback_test.go` for assertions on the removed shape
  - The test is `TestGeneratedPlanMatchesTheServedZone` (`plan_test.go:41`). What it proves is worth keeping and is not about the plan: that the CNAME target this service tells a customer to publish is the one its own zone answers for. Restate it against the domain read and rename the file `expectation_test.go`. `delegation_test.go`'s `onboardDomain` renders that same target into the fake internet's zone and is what would break silently if the two drifted
  - **Corrected:** it does not call the HTTP plan endpoint. It shells `compose exec api go run ./cmd/acmeservice adminctl project dns demo` and splits the output on the literal `"Zone snippet:"` — both of which 11.6 deletes. So this is a rewrite and not an edit: drive `GET /accounts/{a}/projects/{p}/domains` through the `session` helper in `signin.go` (`s.acct(path)`), read `cnameTarget` off each domain, and keep the existing set-equality against `development/dns/zones/example.com.zone` via `zoneFileRecords` (plan_test.go:108), which is the half worth preserving
  - `setup_feedback_test.go` and `delegation_test.go` both decode an anonymous struct with only `resolver` and `domains`, and neither reads `results` — so 11.7 does not break them. Confirm rather than assume: they fatal on an empty `resolver`, so a reshaped response that drops that key by accident fails there and nowhere else
- [x] 11.10 **[D]** This is the one-way step, and it is safe only because there is no customer: `SEQUENCE.md` puts the first customers at step 13, which is the window in which removing a published path is a rename rather than a deprecation. Do it last

## 12. Proving it

- [x] 12.1 **[F]** A wildcard and its apex are separate rows with separate switches, separate hostnames and separate TLS dots — and the SAME `CNAME` dot, because they share one alias and one delegation. `domain-status.spec.ts` already has the shared-alias case for chips; extend it rather than starting a file
- [x] 12.2 **[A]** Enabling on a wildcard without a hostname is refused, and a hostname outside the wildcard is refused, at the store and not only at the API. Two tests: the `check` constraint in `store/domain_test.go`, and `dnsname.Covers` in `wildcard_test.go` with the cases `ValidateDomain`'s own table already establishes — `www.example.com` covered, `a.b.example.com` not, `example.com` not
- [x] 12.3 **[B]** A dual-stack node yields two observations, and a v4-only node yields one with no v6 attempt recorded as a failure
- [x] 12.4 **[B]** An expired certificate is an OBSERVATION, not a failure to observe — the case a verifying client would have turned into an error (G12)
- [x] 12.5 **[B]** An untrusted chain is recorded with the certificate and flagged, and does not colour the dot in this change
- [x] 12.6 **[B]** A certificate not covering the server name asked for bands with expired, whatever its remaining lifetime
- [x] 12.7 **[B]** Worst-of across addresses, and the exception: one unreachable address beside three healthy ones does not grey the dot, and still appears in the dialog
- [x] 12.8 **[B]** Every band edge: 29 days green, 28 amber, 8 amber, 7 red, 6 red, expired red. One table test, not six
- [x] 12.9 **[B]** A refused connection and a timeout each produce a stored observation with a classification, and the state reads "could not be examined" rather than "no certificate installed". 12.3 through 12.9 all want a certificate served from a listener under the test's control — one `httptest`-style helper in this package generating a self-signed certificate with a chosen `NotAfter` and SANs, which every case then parameterises. Write that first
- [x] 12.10 **[H]** History is refused on a personal account with a refusal naming the account kind, and returned on a team account — and observations are recorded on both. `personal_test.go` and `team_test.go` are the existing pair and already build both account kinds; add the case to each rather than to a new file
- [x] 12.11 **[A]** Observations are confined by row-level security: a caller scoped to one account sees none of another's. `backend/internal/store/isolation_test.go` is where every other table's version of this lives
- [x] 12.12 **[E]** Disabling observation for the deployment stops every handshake and changes no other path (G14)
- [x] 12.13 **[B]** A domain with the switch off is never connected to, at all, from any pass. Assert it on `DueObservations`' result rather than by counting dials — the query is where it can go wrong
- [x] 12.14 **[E]** **Replaced.** ~~`domain.observed` is one event per domain per period regardless of node count, family count or interval (G17).~~ Nothing is metered, so there is no event to count. What is worth a test is the thing that took its place: an `observation.interval` below the floor is **refused at startup and named**, and `6h` and `1h` are both accepted. `backend/internal/config/validate_test.go` (or `config_test.go`, wherever the existing `positive(…)` cases live) — one table case, and it is the test that stops somebody "helpfully" clamping instead of refusing (4.5b)
- [x] 12.15 **[H]** Acceptance: enable the switch on a domain served by the fake internet, run one pass, and read the banded state back through the API
  - This is achievable end to end and is the most valuable test in the change, because the stack already has the whole loop in it. `development/compose.yaml`'s `acme` profile runs **`traefik-lb`**, a customer-like load balancer terminating TLS on the published `:443` (`lbAddr` in `main_test.go`) with a certificate **actually issued by Pebble through this service's own challenge answering** — which `cert_test.go`'s `TestCertificateIsIssuedAndChainsToPebble` already fetches and verifies. Add `traefik-lb`'s address as a node on the demo project, switch observation on for `demo.example.com`, run one pass, and assert the band comes back `healthy` and `coversServerName` true
  - Reuse `pebbleChain()` from `cert_test.go` for nothing — the point is that our own observer does not verify (3.3) and reports the chain as untrusted, since Pebble's root is not in the container's system pool. **Assert exactly that**: `chainTrusted` false beside a `healthy` band is 12.5 proved against a real certificate rather than a synthesised one, and it is the case that would silently invert if somebody "fixed" `InsecureSkipVerify`
  - Trigger the pass rather than waiting six hours for it. The cheapest lever is `ACME_OBSERVATION_INTERVAL` set low in the acceptance stack's environment, plus `eventually(t, …)` — the same shape `delegation_test.go:82` already uses for waiting on the fake internet
- [x] 12.16 **[H]** The domain read carries its `CNAME` target and its expected CAA entries, and a wildcard and its apex carry the SAME target — the assertion `plan_test.go` used to make about one CNAME row, made about two domains (10.1, 10.2)
- [x] 12.17 **[H]** A domain with no alias is still returned, carrying no target, rather than omitted (10.1)
- [x] 12.18 **[H]** `GET …/plan` and `…/plan/zone` answer 404, and no domain read carries an address record (11.4, 11.5)
- [x] 12.19 **[H]** The verification response carries per-domain answers and no per-record array (11.7)

## 13. Documentation

`docs/` is human-facing and explicitly non-normative (`docs/CLAUDE.md`), so this
group is a chore rather than a gate — but it is a chore that cannot be skipped
here, because five files describe a panel this change deletes. A guide that walks
somebody through pressing a button that is gone is worse than no guide.

- [x] 13.1 **[I]** `docs/` gains the TLS check: what it connects to, on what port, that it is off until switched on, and that we never hold a certificate or a key. `docs/console-guide.md`, as a new section replacing "The DNS plan and verification" (line 263) — same place in the reading order, because it answers the question that section was there to answer
- [x] 13.2 **[I]** Say plainly, where a customer will read it, that a failed handshake means we could not look and is not a claim their node is down (G14)
- [x] 13.3 **[I]** Document the wildcard test hostname and why it exists — a wildcard is not a name a TLS client can ask for
- [x] 13.4 **[I]** Remove the DNS plan from the twenty-one places that describe it. **Corrected:** the original group named only "docs/ gains the TLS check", which would leave the deleted panel documented in five files. They are: `docs/console-guide.md` (the table of contents at line 16, the permission table at 50, the screen map at 81, the address-record sentences at 100 and 132, the "same lookup as Run verification" at 224, the whole section at 263–307, and the two recipes at 554 and 584), `docs/load-balancer-setup.md` (70, 189–204, 235–241, 306–311, the checklist at 333), `docs/README.md` (93, 114), `docs/installation.md` (378), `docs/troubleshooting.md` (29, 203)
- [x] 13.5 **[I]** Rewrite `docs/console-guide.md`'s "Validation — what a live lookup says about the records" (line 228) as the dots, and add the **five** new configuration keys to `docs/installation.md`'s settings table — the table is where an operator looks for `ACME_*`, and a key that is not in it is a key nobody finds. `ACME_OBSERVATION_INTERVAL`'s row says the floor is one hour and that a shorter value is refused at startup, because the operator who tries it should meet that sentence before the service does
- [x] 13.6 **[I]** Say, where a customer reads about the switch, that **turning it on costs nothing** — observation is not metered and only challenges are. A new switch on a billing-bearing product reads as a new charge unless it says otherwise, and the one person who most needs this check is the one who will not enable it while they are guessing what it costs
