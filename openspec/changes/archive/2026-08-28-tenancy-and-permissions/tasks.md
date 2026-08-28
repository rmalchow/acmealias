## 1. Schema: UUID identifiers, accounts and materialized ownership

- [x] 1.0 Convert every primary key and foreign key to `uuid`, generated as UUIDv7 (design P17); use `uuidv7()` on Postgres 18 and `google/uuid` generation otherwise, so the schema does not depend on the server version
- [x] 1.1 Add `accounts` to `internal/model` and a migration creating it; insert the single account (slug `internal`, name from `ACME_ORG_NAME` or a default)
- [x] 1.2 Add `projects.account_id`, backfill to that account, set `NOT NULL`, add `UNIQUE (id, account_id)`
- [x] 1.3 Add materialized `account_id` to `load_balancers`, `lb_nodes`, `domains`, `aliases`, `api_keys`; backfill by walking parents; set `NOT NULL`
- [x] 1.4 Add materialized `project_id` to `lb_nodes`, `domains`, `aliases`, `api_keys` (design P3), so every grant reduces to a single-column predicate
- [x] 1.5 Add the composite foreign keys of design P2 for both materialized ancestors, so neither can disagree with its parent; add a test asserting a disagreeing insert is rejected by the database
- [x] 1.6 Replace `domains_name_key` with `UNIQUE (account_id, name)`
- [x] 1.7 Regenerate `internal/store/schema.sql` via the golden test and confirm `model_drift_test` passes

## 2. Query confinement and ACL-gated reads

- [x] 2.1 Add `internal/store/scope.go` with a `Scope` that has no exported constructor from a bare identifier
- [x] 2.2 Add the two producers: from a membership, and from the single-account bootstrap
- [x] 2.3 Change every inventory query in `internal/store` to take a `Scope` and filter on the materialized `account_id`
- [x] 2.4 Build the ACL predicate compiler: fold a subject's grants for one action and object type into a disjunction of single-column predicates (design P13)
- [x] 2.5 Implement `List` applying the ACL predicate together with the caller's filters, paging and count; assert the count uses the same predicate as the page
- [x] 2.6 Implement `GetWithPermission(id, perm)` as `List` narrowed by `id`, returning the object or nil, with absence and denial indistinguishable
- [x] 2.7 WITHDRAWN with the DNS product: there are no record objects to select. The grant grammar keeps the third identifier form reserved and refuses any grant carrying one, since a stored selector nothing can evaluate would look like a permission somebody had. Original text: the `dns_record` predicate is compiled — reversed names, prefix match, apex excluded, record-type set — and unit-tested. The reversed COLUMN and its `text_pattern_ops` index arrive with `dns-records`, which creates the table; there is nothing to index yet

  **The reservation was withdrawn afterwards.** Keeping the third form reserved was the right call
  while it was plausible that something would define a selector; nothing did, and a form that is
  refused everywhere it appears is a form that only costs reading. Migration `000003` drops the
  column and tightens the check constraint to the two forms that exist, and `authz.Selector`,
  `FormSelector` and their encoding are gone. This task's reasoning is left as written because it
  was true when it was done — what changed is the world, not the judgement.
- [x] 2.8 Thread `Scope` through `internal/inventory` and `internal/reporting`
- [x] 2.9 Add a test asserting every exported store read takes a `Scope`, so a new query cannot be added without one
- [x] 2.10 Add tests that an object of another account is refused as though it did not exist, for read, list, count and write
- [x] 2.11 Add the RLS migration (design P20): `ENABLE ROW LEVEL SECURITY` and a `tenant_isolation` policy comparing the materialized `account_id` to `current_setting('app.account_id')` on `load_balancers`, `lb_nodes`, `domains`, `aliases` and `api_keys`; not on `challenges` or `challenge_interactions`
- [x] 2.12 Cross-account components go through `store.CrossAccount`, a deliberately greppable session setting, rather than a `BYPASSRLS` role — creating that role needs superuser and splitting the app across two pools is a larger change. Deviation recorded in migration 000007 and in design P20. Additionally: `store.CheckTenantIsolation` refuses to start as a superuser or a `BYPASSRLS` role, since either makes every policy inert
- [x] 2.13 Set `app.account_id` with `SET LOCAL` from the `Scope` when a connection is checked out, inside the explicit transaction every scoped query runs in
- [x] 2.14 Add a test that a store query with its ownership predicate deliberately removed still returns no row of another account
- [x] 2.15 Add a pooler test: borrow a connection for account A, return it, borrow it for account B, assert no residual `app.account_id`; document PgBouncer transaction mode as supported and statement mode as not
- [x] 2.16 Verify the challenge benchmarks are unchanged by the transaction wrapper, since `challenges` carries no policy (design P20)

## 3. Permission model

- [x] 3.0 Create `users` as a global table, and `account_members` binding a user to an account and role; a user is not owned by an account and outlives its deletion (design P18)
- [x] 3.1 Create `roles`, `role_grants`, `api_key_grants` in the model and a migration, with the identifier-form check constraint from design P4
- [x] 3.2 Implement the `Grant` type, its three identifier forms, and JSON encoding of the `dns_record` selector
- [x] 3.3 Ancestor resolution needs no chain walk and no memo: design P3 materialized every ancestor a grant may name onto the row, so `authz.ObjectOf` reads them as fields. The memo was for a walk that no longer exists
- [x] 3.4 Implement `Can(subject, action, object)` as a union over grants, with inheritance and anchored wildcards
- [x] 3.5 Implement name-pattern matching per design P9; reject interior wildcards at write time
- [x] 3.6 Table-driven tests for `Can` across every combination of wildcard, identity, selector and inheritance
- [x] 3.7 Property test asserting an inheritable grant matches exactly the objects contained by the one it names

## 4. The subset rule

- [x] 4.1 Implement single-grant containment `g ⊑ e` over action, type, object and selector (design P10)
- [x] 4.2 Implement pattern containment (`*.a.b ⊑ *.b`, `a.b ⊑ *.b`, `*.b ⋢ *.a.b`) with an exhaustive test table
- [x] 4.3 Enforce the rule on every write to `role_grants` and `api_key_grants`
- [x] 4.4 Test that union coverage is refused, and document the refusal as intended in the test name

## 5. Roles replace the fixed four

- [x] 5.1 Seed the four roles with grants reproducing today's permission matrix, in the migration
- [x] 5.2 Change `authz.Mapping` to resolve a configured group to a role record rather than a constant; report at startup when a setting names no role
- [x] 5.3 Derive memberships per request from the group claim, keeping the existing no-role refusal and observed-groups response
- [x] 5.4 Role creation, edit and deletion are implemented in the store with their invariants; the routes and the permission gating them arrive with group 7, which is where every permission check moves
- [x] 5.5 Implement the unrestricted role's invariants: uneditable, undeletable, and the last-member guard under `SELECT … FOR UPDATE`
- [x] 5.6 Test concurrent removal of the two remaining unrestricted members: one succeeds, one is refused
- [x] 5.7 Acceptance test that each seeded role permits exactly what its predecessor permitted, and nothing more

## 6. API keys become subjects

- [x] 6.1 Add the key anchor and grants to the model; preload grants in `store.FindAPIKeyByIdentifier`
- [x] 6.2 Migrate every existing key to one `[load_balancer, <id>, challenge.publish]` grant, then drop `api_keys.load_balancer_id`, in one transaction
- [x] 6.3 Replace the load-balancer comparison in `internal/httpapi/challenge.go` with a permission check against the resolved alias
- [x] 6.4 Scope `findAlias` to the key's account (design P8); keep the label spelling resolving globally and compare the account afterwards
- [x] 6.5 Enforce the subset rule on key creation and amendment
- [x] 6.6 Benchmark `POST /present` and assert the datastore read count is unchanged

## 7. Management API

- [x] 7.1 Remove `authz.Require(action)` route middleware; move permission checks into `internal/inventory`, the service layer the API and the CLI share (design P7)
- [x] 7.1a Split the route tree: versioned public API under `/api/v1/**`, unversioned administrative API under `/api/admin/**` (design P15)
- [x] 7.1b Publish an OpenAPI document for the public API only, and add a test asserting every console request is an operation it describes
- [x] 7.2 Add role, membership and grant routes
- [x] 7.3 Report permitted actions per object on every listing and read (design P12)
- [x] 7.4 Make project creation require membership of the account's unrestricted role, not a grant; refuse any grant naming an account as its object (design P14)
- [x] 7.4a Add account creation for any authenticated user, making the creator an unrestricted member in the same transaction
- [x] 7.4b Add the member roster: readable by any member of the account without a grant, confined by `Scope`, including outstanding invitations
- [x] 7.4c Add `sessions.selected_account_id` (nullable); gate account-scoped routes on it and refuse with a distinct "no account selected" response
- [x] 7.4d Add account selection and switching, re-evaluating membership on switch and auditing it; select automatically in a single-account edition
- [x] 7.4e Move account-scoped routes under `/api/v1/accounts/{accountId}/…` and refuse a mismatch with the session's selection (design P19)
- [x] 7.4f Resolve memberships per request from either source — derived from group claims, or read from `account_members` — without the evaluator distinguishing them (design P18)
- [x] 7.5 Confine audit reads to the acting account

## 8. Audit

- [x] 8.1 Add role, grant and membership actions to `internal/authz/vocab.go` and assign them to the credential class
- [x] 8.2 Record grants in full — type, identifier or selector, action, inheritable — in before/after state
- [x] 8.3 Record the acting account on every entry; record role names as written rather than as references
- [x] 8.4 Test that a refused escalation writes no change entry and is logged

## 9. Console

- [x] 9.0 Build the no-account-selected shell: profile, the list of accounts the user belongs to, create-account, accept-invitation, and nothing else
- [x] 9.0a Build the account switcher, and render neither it nor the selection screen in a single-account edition
- [x] 9.1 Consume the per-object permitted-actions report and replace role-name gating throughout
- [x] 9.2 Build the role editor: create, edit, delete, and membership management
- [x] 9.3 Build the grant composer, offering only what the acting user holds
- [x] 9.4 Present the unrestricted role as uneditable and hide removal of its last member
- [x] 9.5 Extend the key dialog with anchor and grant selection; show a key's authority wherever it is listed

## 10. Listeners

- [x] 10.0 Collapse the three listeners into one, separated by path prefix with per-group middleware (design P16); keep `/present` and `/cleanup` serving alongside the namespaced `/acme/httpreq/**`
- [x] 10.1 Replace the tests asserting no listener mounts another's routes with tests asserting each path group carries its intended middleware
- [x] 10.2 WITHDRAWN with `satellite-sync`: there is no fleet to authenticate. Original text: `Servers.Satellite` exists as the shape of the answer — a second listener, not a path prefix, because mutual TLS completes before any path is known. It is nil until `satellite-sync` gives it routes to serve; there is nothing yet to require a client certificate for
- [x] 10.3 Update the `service-operations` documentation and deployment manifests for the new port layout

## 11. Documentation and closing the overlap

- [x] 11.1 Update `docs/` for accounts, roles as data, and per-project keys
- [x] 11.2 Superseded by archiving. `add-accounts-and-public-edition` was archived unimplemented with a note saying where each half went, so amending its deltas would be editing a document nobody should build from — the task asked for surgery on a live change that is no longer live
- [x] 11.3 Update the CLI (`internal/adminctl`) to operate within the single account and to accept grants when creating keys
- [x] 11.4 Done, on a machine where the stack could be reset — and it did not pass quietly. Two things had to happen first, and the second is the reason this task was worth keeping open.

  **The suite had never been ported to this change.** Every account-scoped route moved under `/api/v1/accounts/{accountId}/…` (7.4e) and every identifier became a UUID (1.0), so the suite was addressing routes that no longer exist and decoding ids into `int64`. It compiled, which is why nothing caught it. The port: `session` gained an `account` resolved from `/api/v1/me`'s `selectedAccountId` and an `acct()` helper that builds the account-scoped path, ids became the strings the API answers with, and the four non-account-scoped routes (`/me`, `/accounts`, `/auth/*`) were left spelled out so the distinction stays visible.

  **Then it found four live regressions, all of them one defect.** A read that must cross accounts, left as a bare query against a table migration 000007 policied. Row-level security does not fail such a read — it empties it — so each one reported "nothing here" and looked like ordinary absence:

  - `store.FindAliasByChallengeName` and the label lookup in `httpapi.findAlias` resolve the name that ESTABLISHES the account, so they run before one is known. Filtered to nothing, every `/present` was refused `403 "not authorized for that name"` and **no certificate could be issued at all** — the product's one job. Its own doc comment already said it reads globally; the policy silently made that false.
  - `reporting.Snapshot` reads `from projects` on a ticker with no session: `project_daily_stats` had never received a row, so usage reporting was permanently empty.
  - `store.AliasIDsByLabel` resolves labels for the query-count flush. Filtered to nothing, every observation was counted as dropped and logged as `query observations discarded, alias no longer exists` — the exact misreading the function's comment warns about — and no interaction was ever credited with a query.
  - Migration 000009's insert reads `from roles`, and 000008's last statement had just policied that table. It selected zero rows, inserted zero rows, and reported success: `CONFIG_ADMIN` and `KEY_ROTATION` never got the challenge grants the migration exists to give them, so the two roles whose purpose is minting keys could mint only useless ones. Fixed in place for fresh chains, plus `000012_repair_challenge_grants` for the databases already past it.

  All four are the same one-line fix — say `store.CrossAccount`, which is the greppable escape 2.12 built for exactly this — and the reason to record them together is that the defect is not "somebody forgot a wrapper". It is that a policied table answers a scopeless read with silence, and silence is indistinguishable from an empty deployment. The four sites had all been reviewed and none of them looked wrong.

  Final run: `./scripts/test-acceptance.sh -count=1 -v` with no `ACCEPTANCE_KEEP_STACK`, so `TestMain` destroyed the database, migrated a fresh chain to version 12, seeded, and brought up `--acme` itself. All ten tests pass in 82.8s. Constraint 1 is now demonstrated end to end rather than assumed — and note it was NOT holding before this ran.
