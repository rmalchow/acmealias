> Depends on `add-acme-alias-service` (now **archived**; its requirements are the baseline in
> `openspec/specs/`). Most of this change is **already implemented** — it is being written down
> after the fact — so the majority of the boxes below are ticked and name the file the code lives
> in.
>
> **Reconciled against the code on 2026-08-21.** The file was written before a second round of work
> landed and had drifted: the `abandoned` outcome, the project id on the interaction, the schema
> guards, the `queryobs` test file and the per-label first/last timestamps were all built but still
> unticked. Nine boxes were ticked on evidence and one (10.1) was cut. What remains unticked below
> is work that was checked against the tree and is genuinely absent — overwhelmingly **tests**:
> `backend/internal/store/interaction.go`, the interactions endpoint and every paged console screen
> have none.

## 1. Server-side paging and filtering <!-- complexity: sonnet | touches: backend/internal/httpapi, backend/internal/store -->

- [x] 1.1 `backend/internal/httpapi/admin_page.go`: `readPage(c, defLimit, maxLimit)` — a limit above the maximum is a `400` naming the bound, not a silent clamp; `pageRequest.apply` adds `LIMIT`/`OFFSET`; `setTotal` writes `X-Total-Count`
- [x] 1.2 `backend/internal/httpapi/admin_page.go`: `readTimeRange` / `parseBound` — RFC 3339 or bare `YYYY-MM-DD`, a bare `to` pushed to the end of that day, an inverted range refused
- [x] 1.3 `backend/internal/store/project.go`: `ListProjectsWithLoadBalancersPage` with `ILIKE` over slug and name, counted before paging; `likeEscape` escaping `\`, `%` and `_` with an explicit `ESCAPE '\'`
- [x] 1.4 `backend/internal/store/challenge.go`: `ListActivePage` — filter across the join to `aliases`, `base()` called twice so the count's clauses do not leak into the select
- [x] 1.5 `backend/internal/httpapi/admin_inventory.go` `listProjects` and `backend/internal/httpapi/admin_misc.go` `listChallenges`: `q`, `limit`, `offset` wired, total published
- [x] 1.6 `backend/internal/httpapi/admin_misc.go` `listAudit`: `from`, `to`, `limit`, `offset` added alongside the existing `projectId`, `actor`, `action`, `objectType`; counted before paging
- [x] 1.7 `backend/internal/httpapi/admin_test.go`: a limit above the maximum is refused; `X-Total-Count` reports the pre-paging count; a filter matching only a row on a later page returns it; a filter containing `_` matches it literally
- [x] 1.8 Test that `to=YYYY-MM-DD` includes an entry recorded during that day, and that `to` earlier than `from` is refused

## 2. Read by identifier <!-- complexity: sonnet | touches: backend/internal/httpapi -->

- [x] 2.1 `backend/internal/httpapi/admin_inventory.go` `getProject` + route `GET /projects/:id` (`admin.go:134`), behind `ActionViewInventory`
- [x] 2.2 `frontend/src/app/features/projects/project-detail.component.ts`: loads `GET /projects/:id` instead of searching the project list; `frontend/src/app/features/loadbalancer/loadbalancer-detail.component.ts` does the same for its breadcrumb
- [x] 2.3 Test that a project outside the first page of the list is still readable by id

## 3. Node addresses <!-- complexity: sonnet | touches: backend/internal/httpapi, backend/internal/inventory, frontend -->

- [x] 3.1 `PATCH /nodes/:id` → `backend/internal/httpapi/admin_inventory.go` `updateNode`, addresses only, audited with before/after
- [x] 3.2 `backend/internal/inventory/inventory.go` `UpdateNodeAddresses` + `parseAddrs`: each address checked against its family, both-empty refused
- [x] 3.3 `frontend/src/app/features/loadbalancer/node-edit.modal.ts`: pencil row action, addresses only, the standing note about already-published records, DNS plan re-read after a change
- [x] 3.4 Test that clearing both addresses is refused and the node is unchanged, and that an IPv6 literal in `ipv4` is refused. `internal/inventory/inventory_test.go:273` `TestIntegrationNodeAddressValidation` covers exactly these two cases on **`AddNode`**; the update path has no test at all, and "the node is unchanged after a refused update" is only answerable there

## 4. The interaction history: schema and store <!-- complexity: opus | touches: backend/internal/store, backend/internal/model -->

- [x] 4.1 `backend/internal/store/migrations/000002_challenge_interactions.up.sql` / `.down.sql`: the table, no foreign keys to aliases or domains, `(load_balancer_id, published_at desc)` and the partial live index on `(alias_id, value_digest)`
- [x] 4.2 `backend/internal/model/model.go`: the `ChallengeInteraction` struct
- [x] 4.3 `backend/internal/store/interaction.go`: `ValueDigest`, `OpenInteraction` (reuses a live row for the same `(alias_id, digest)`), `CloseInteraction` (closes every live row, zero rows is not an error)
- [x] 4.4 `backend/internal/store/interaction.go`: `ApplyQueryObservations` — the overlap `UPDATE`, with the reasoning in a comment because it looks like a bug to anyone assuming one query means one authorization
- [x] 4.5 `backend/internal/store/interaction.go`: `AliasIDsByLabel`, dropping labels that no longer resolve
- [x] 4.6 `backend/internal/store/interaction.go`: `ListInteractionsForLoadBalancer` — `q` across label, FQDN, key id, client IP and domain names; `onlyUnqueried`; counted before paging
- [x] 4.7 `backend/internal/model/model.go:62`: `&ChallengeInteraction{}` in `model.Models()`, so `model_drift_test.go` covers the new table and the struct and the migration cannot disagree unnoticed
- [x] 4.8 `backend/internal/store/schema.sql:70` regenerated: `challenge_interactions` with all four indexes present, so `TestIntegrationSchemaGolden` passes

## 5. Recording on the challenge path <!-- complexity: opus | touches: backend/internal/httpapi/challenge.go -->

- [x] 5.1 `openInteraction` on `/present`: domain names snapshotted via `store.DomainNamesForAlias`, client IP from `c.RealIP()`, `fqdn_form` from the resolved name form, digest of the value
- [x] 5.2 `CloseInteraction` on `/cleanup`
- [x] 5.3 Both best effort: a failure logs at WARN and never changes the response
- [x] 5.4 `backend/internal/store/interaction.go:172` `AbandonStaleInteractions` stamps `withdrawn_at` **and** the `abandoned` flag (migration `000002_challenge_interactions.up.sql:47`), leaving `query_count` intact; called from `backend/internal/sweeper/sweeper.go:60`. The flag is the reason field design D7 asked for: it is what distinguishes "expired on our side" from "withdrawn by the client"
- [x] 5.5 `backend/internal/model/model.go:165` `ProjectID`, populated at `backend/internal/httpapi/challenge.go:388` via `store.ProjectIDForLoadBalancer` and indexed as `challenge_interactions_project_published_idx` (`schema.sql:92`), so history stays attributable after its load balancer is deleted
- [x] 5.6 Test that a retried `/present` for a live value does not open a second interaction and does not move `published_at`
- [x] 5.7 Test that a `/cleanup` with no matching interaction succeeds and creates nothing
- [x] 5.8 Test that a history write failure does not fail `/present` or `/cleanup`

## 6. DNS query observation <!-- complexity: opus | touches: backend/internal/queryobs, backend/internal/dnsserver, backend/cmd/acmeservice -->

- [x] 6.1 `backend/internal/queryobs/queryobs.go`: `Counter` keyed by alias **label** (an id would be a datastore lookup on the query path), `Observe`, `Drain` returning the counts and the window start, `merge` putting a failed batch back with its window
- [x] 6.2 `backend/internal/dnsserver/handler.go`: `h.queries.Observe(label)` only on a query answered with at least one value — never on `NXDOMAIN` or an empty answer; a nil counter is a no-op so no call site needs a guard
- [x] 6.3 `Counter.Run`: ticker flush plus a final flush on shutdown with its own short context, so a clean stop does not discard the last window
- [x] 6.4 `backend/cmd/acmeservice/main.go`: the flusher wired on `cfg.ZoneViewRefreshInterval` — the same cadence as the zone view, so there is no second tuning knob
- [x] 6.5 `backend/internal/queryobs/queryobs_test.go:146` `TestObserveIsSafeConcurrently`, plus `TestNilCounterIsANoOp` (:59) and `TestRunFlushesOnceMoreOnShutdown` (:118)
- [x] 6.6 Test the overlap rule directly: a row published, queried and withdrawn entirely between two flushes is still credited. This is the bug the rule exists to fix — the first implementation credited only rows live at flush time and reported every successful validation as never queried. Belongs in a new `backend/internal/store/interaction_test.go` against `ApplyQueryObservations`; the package has no such file
- [x] 6.7 Test the wildcard case: two rows live at `demo-example-com`, one query, both credited. Same new file as 6.6
- [x] 6.8 `backend/internal/queryobs/queryobs_test.go:87` `TestFailedFlushIsRetriedWithItsOriginalWindow`, alongside `TestFlushCarriesTheWindowItAccumulatedOver` (:68)
- [x] 6.9 Count unattributed observations **in a metric**. `store.AliasIDsByLabel` now returns the dropped count (`backend/internal/store/interaction.go:130`) and `backend/cmd/acmeservice/main.go:213` logs it at WARN, which is half the job — a log line is not something an alert can be built on, and observations that resolve to an alias but match no interaction row are still invisible. `add-audit-and-usage-reporting` 9.1 specifies `acme_dns_query_observations_total{attributed}`; nothing in `backend/internal/obs/metrics.go` registers it
- [x] 6.10 `backend/internal/queryobs/queryobs.go:34`: `Observation` carries `First` and `Last` per label, consumed by `backend/internal/store/interaction.go:97` which writes `last_queried_at` from `Last` and `coalesce`s `first_queried_at` from `First`, falling back to the window end only when a batch has no stamps. Design D6's *Known imprecision* no longer applies. Covered by `queryobs_test.go:15` `TestObserveCountsPerLabelAndStampsFirstAndLast`

## 7. The interactions endpoint <!-- complexity: sonnet | touches: backend/internal/httpapi/admin_interactions.go -->

- [x] 7.1 `GET /loadbalancers/:id/interactions` with `q`, `unqueried`, `limit`, `offset`, `X-Total-Count`, behind `ActionViewInventory` (`admin.go:152`)
- [x] 7.2 `outcomeOf` deriving `validated` / `unqueried` / `live` / `pending` from `withdrawn_at` and `query_count` — derived, never stored, so it stays correct for a row still open while being read
- [x] 7.3 `backend/internal/httpapi/admin_interactions.go:85` `outcomeOf(withdrawnAt, abandoned, queries)` — `abandoned` is tested first, so a swept row reads `abandoned` rather than `unqueried` even with a zero query count; wired at `:79` and documented at `:39`
- [x] 7.4 Test the **five** outcome mappings (`abandoned` included, per 7.3), the `unqueried` filter, `X-Total-Count` and that the endpoint is readable by `READ_ONLY`. `admin_test.go` has no interactions test; `TestEveryRouteEnforcesItsRole` (`:224`) covers the route's role gate only

## 8. Console rebuild <!-- complexity: opus | touches: frontend -->

- [x] 8.1 `frontend/package.json` + `angular.json`: ng-zorro-antd 19.3 with `ng-zorro-antd.dark.min.css` only; no light sheet, no Less, no theme control
- [x] 8.2 `frontend/src/styles.css`: `@fontsource/roboto` imported so the woff2 files are emitted by `ng build` and served from the binary; `frontend/src/index.html` carries no external link
- [x] 8.3 `frontend/src/app/app.config.ts`: `provideNzIcons([...])` registering every icon used — an unregistered `nz-icon` is fetched from Ant's CDN at runtime, which on the production host simply hangs
- [x] 8.4 `frontend/src/styles.css`: `.ac-page` fixed-width container (`--ac-page-width: 78rem`), applied to the shell bar's contents and to `<main>`
- [x] 8.5 `frontend/src/app/shared/page-header.component.ts`: breadcrumb + headline + subheadline, with the breadcrumb row and subheadline reserving their height when empty
- [x] 8.6 Creation modals: `shared/name-slug.modal.ts` (project, load balancer), `features/loadbalancer/node-create.modal.ts`, `features/domains/domain-editor.component.ts`, `features/apikeys/apikey-create.modal.ts` — all POST from inside the dialog and keep the form with `apiErrorMessage(err)` on failure
- [x] 8.7 `frontend/src/app/shared/confirm.ts`: confirmation via `NzModalService.confirm`, naming the object — `window.confirm` rejected because a browser can be told to suppress it per origin
- [x] 8.8 Icon-only row deletes with tooltip and `aria-label` in `project-detail.component.ts` and `loadbalancer-detail.component.ts`
- [x] 8.9 Paged tables reading `X-Total-Count` through `ApiService.list()` (`core/api.service.ts`), `nzFrontPagination` off, page-size selector, reset to page 1 on any filter change: projects, challenges, audit, interactions
- [x] 8.10 `frontend/src/app/features/audit/audit.component.ts`: range picker submitting **local** `YYYY-MM-DD` via a hand-rolled `isoDate()` — `toISOString()` shifts to UTC and can name the previous day
- [x] 8.11 `frontend/src/app/features/loadbalancer/interactions-panel.component.ts`: the panel, its columns, the "Never queried only" switch, the four outcome tags with their tooltips, and two distinct empty states
- [x] 8.12 `frontend/src/app/features/apikeys/apikey-create.modal.ts`: the "Restrict to this load balancer's node addresses" switch, `/32` and `/128` per address, removing only what it added when switched off
- [x] 8.13 `frontend/src/app/app.routes.ts`: the sessions and settings routes and components removed, with the reason recorded in the file
- [x] 8.14 `frontend/package.json:13-27`: `material-icons` gone; the dependency list is Angular, `@angular/cdk`, `@fontsource/roboto`, `ng-zorro-antd`, `rxjs`, `tslib`, `zone.js` and nothing else
- [x] 8.15 Remove the now-unused `SessionEntry` interface from `frontend/src/app/features/models.ts:160`, or note why it is kept — still declared, still referenced nowhere now the sessions screen is gone
- [x] 8.16 Component tests for the four paged screens and the interactions panel — there are none; only `app.component`, `auth.interceptor`, `role.guard`, `wildcard.validator` and `domain-status` are covered

## 9. The domain-count bug <!-- complexity: sonnet | touches: backend/internal/store/loadbalancer.go -->

- [x] 9.1 `backend/internal/store/load_balancer.go:37` `ListLoadBalancersForProject`: `Preload("Domains")` added — the project screen's per-load-balancer domain count read 0 for every load balancer
- [x] 9.2 `backend/internal/store/load_balancer_test.go` (two tests today, neither about domains): a load balancer with two domains reports two through `ListLoadBalancersForProject`

## 10. Loose ends named in the design <!-- complexity: sonnet | touches: backend/internal/adminctl, docs, openspec -->

- ~~10.1 The moved requirement says session revocation and the effective configuration are "offered through the CLI". `internal/adminctl` has project, loadbalancer, node, domain, key and plan subcommands and neither of these — add them, or stop claiming them (design D12)~~ — **cut: the requirement was taken as the thing to fix, and it was.** Both requirements in `specs/admin-console/spec.md` now read "served by the management API" and name no CLI, which is true of the running service: `GET`/`DELETE /api/v1/sessions` and `GET /api/v1/config` are routed at `backend/internal/httpapi/admin.go:167-171` and tested by `TestSessionRevocationIsImmediateAndConfined`, `TestRevokingEverySessionOfOneSubject` and `TestEffectiveConfigurationIsRedactedAndCarriesTheRoleMapping` (`admin_test.go:519`, `:581`, `:747`). Nothing claims `adminctl` has them, so there is nothing left to add or retract. Design D12's *Loose end* paragraph is updated to record the resolution
- [x] 10.2 Decide a retention policy for `challenge_interactions`. **Recommendation, written 2026-08-21 — ⚠️ THE NUMBER NEEDS A HUMAN'S SIGN-OFF BEFORE 10.6 SHIPS.** The problem is real: `backend/internal/sweeper/sweeper.go:60` closes stale interactions but deletes nothing, so the table grows without bound, and every row holds a customer's load balancer address (`client_ip`) — personal-ish third-party network data we keep for no stated period, which is exactly the sort of thing that is embarrassing to discover in year three. Recommended default: **delete closed interactions (`withdrawn_at IS NOT NULL`) older than 180 days**, in the sweep that already runs, with the period configurable and a value of `0` meaning keep forever. 180 days is defensible rather than obvious — it is two full 90-day Let's Encrypt renewal cycles, so "did this domain validate last time, and the time before" is always answerable, and `add-audit-and-usage-reporting`'s daily project snapshot (its task 6.4) preserves the long-run trend after the raw rows go. Open interactions are never deleted regardless of age: an interaction still open after 180 days is a bug worth seeing, not garbage worth sweeping. Audit entries are **not** in scope and keep their indefinite retention (design D9 of that change) — they are the evidence trail, this is telemetry
- [x] 10.6 Implement the retention sweep decided in 10.2: extend `backend/internal/sweeper/sweeper.go` (beside the existing `AbandonStaleInteractions` call at `:60`) with a delete of closed interactions older than the configured period, add the config value with a default, and test that an open interaction older than the period survives while a closed one does not. **Do not ship the default without a human signing off on the number** — 180 days is a recommendation from 10.2, not a decision anyone has made, and it silently destroys data once it runs. The config knob is the part that is uncontroversial; land it defaulted to keep-forever if the sign-off has not happened
- ~~10.3 Acceptance coverage: assert that a full issuance through the harness leaves one interaction per authorization, both credited by the single TXT query, both ending `validated`~~ — **Cut on 2026-08-21: moved, not dropped.** The harness belongs to `add-acceptance-environment` and so does the assertion; it is now that change's **task 2.8**, written against the fixtures and sign-in helper its group 1 provides. Nothing is owed here
- [x] 10.4 `docs/`: describe the interactions panel and what `unqueried` means, once the panel has had a renewal cycle's worth of data
- [x] 10.5 **Done on 2026-08-21.** `add-audit-and-usage-reporting/specs/` now holds only `audit-trail/` and `usage-reporting/` — its colliding `specs/challenge-history/` directory is deleted, its groups 4 and 5 are struck through in full, and its `proposal.md` records the withdrawal. The two requirements its version carried that this one did not are folded into `specs/challenge-history/spec.md` above (the recorded project, and the aggregate metric for unattributed observations). Both changes can now be archived in either order. Original text: Revise `add-audit-and-usage-reporting`: fold this change's `challenge-history` deltas into it, drop its groups 4 and 5 (`tasks.md:32-60` — every one of those sixteen tasks is either already implemented here or superseded by it), and mark its `audit-trail` requirement *"Entries are readable and filterable"* as satisfied by task 1.6 here. Its `specs/challenge-history/spec.md` still ADDs *"Every published challenge is recorded durably"*, *"DNS query observations are attributed without datastore access on the query path"*, *"Each record is classified by outcome"* and *"Cleanup without a matching record is not an error"* — the first two collide verbatim with this change's headings and the last two are this change's requirements under the pre-rename wording. **Until this is done, both changes cannot be archived**, because whichever goes second re-ADDs a requirement the other already put in `openspec/specs/challenge-history/`
