## Context

This change is a strict addition on top of `add-acme-alias-service`, which introduces the
inventory (projects → load balancers → domains → aliases), the challenge API, the authoritative DNS
server, and a live `challenges` table holding whatever is answerable right now. None of that
records history: the live table is emptied on cleanup, and the base change's audit requirement is a
single line about recording privileged actions.

Two shapes of question motivate the work, and they want different machinery:

- **Forensic** — "who changed this, when, and what did it look like before". Wants completeness and
  integrity; volume is tiny; it is written on paths that are already transactional.
- **Analytical** — "how many renewals happened for this domain, and how is this project trending".
  Wants aggregation over time; is written on the hottest path in the service (DNS queries); and
  must not be allowed to compromise the properties the base design guarantees.

The second constraint is the one that shapes most of what follows. `add-acme-alias-service` D6
guarantees that DNS keeps answering from an in-memory view when PostgreSQL is unavailable, and D5
guarantees a published value is answerable by the time `/present` returns. Neither may be weakened
to collect a statistic.

## Goals / Non-Goals

**Goals:**

- Every configuration and credential change attributable to a named human, with before-and-after
  values, and not silently editable afterwards through the application.
- A durable per-challenge history rich enough to distinguish a renewal that worked from one where
  nobody ever came looking.
- Per-domain and per-project counts over time, and per-project inventory size over time.
- Zero database work on the DNS query path.
- No new dependencies.

**Non-Goals:**

- Counting certificates. The service cannot observe them (proposal, *"the thing worth measuring"*).
- Tamper-proofing the audit trail against someone holding database credentials. We provide
  tamper-evidence against the application and are explicit about the boundary (D1).
- A general-purpose metrics or BI platform. Three questions, answered directly.
- Retroactive data. Both tables start empty at deployment; nothing can be backfilled, because the
  events they describe were never recorded.
- Alerting infrastructure. The `never_queried` signal is exposed; wiring it to a pager is an
  operational task, not code.

## Decisions

### D1. Audit entries are append-only rows with structured before/after

```sql
create table audit_entries (
  id            bigserial primary key,
  occurred_at   timestamptz not null default now(),
  actor_subject text,                    -- OIDC 'sub'; null for system actions
  actor_name    text,                    -- display name at the time
  actor_groups  text[],                  -- the groups claim as seen at sign-in
  actor_roles   text[],                  -- roles resolved from those groups
  actor_source  text not null,           -- 'console' | 'cli' | 'system'
  request_id    text,
  client_ip     inet,
  action        text not null,           -- 'domain.create', 'apikey.revoke', ...
  object_type   text not null,           -- 'domain', 'api_key', 'load_balancer', ...
  object_id     bigint,
  object_label  text not null,           -- human-readable, survives deletion
  project_id    bigint,                  -- denormalized for filtering
  before        jsonb,                   -- null on create
  after         jsonb                    -- null on delete
);
create index on audit_entries (occurred_at desc);
create index on audit_entries (project_id, occurred_at desc);
create index on audit_entries (object_type, object_id);
```

Four things in that schema are deliberate and would be easy to get wrong.

**`object_label` is denormalized text, not a join.** The most valuable audit entries describe
things that no longer exist. "Domain #4127 deleted" is useless once row 4127 is gone;
`shop.example.com` is not. The same applies to `project_id` — kept as a plain column so that
filtering by project still works after the project is deleted, at the cost of a foreign key we
deliberately do not declare.

**`actor_roles` and `actor_groups` are snapshots.** Roles are derived from Keycloak group membership
mapped through environment configuration, and *both sides can change* — a group's membership in the
IdP, or the `ACME_*_GROUP` mapping in the environment file. An entry that records only the actor's
name cannot answer "how was this person allowed to do that", which is the question an incident
actually asks. Recording the resolved roles at the moment of the action makes the entry
self-contained.

**`before`/`after` are `jsonb` snapshots of the object's own fields**, not a diff. Storing a diff
means deciding a diff format and writing a differ; storing both sides means the reader can compute
whatever they want and the writer is a serialization call. Secrets are never included — key hashes
and secrets are excluded at the serializer, not filtered at the reader.

**Append-only, and honestly scoped.** The application contains no code path that updates or deletes
an audit row, and the database role the service uses is granted `INSERT` and `SELECT` on this table
only. That defends against a bug, a compromised session, and an operator with console access — the
realistic threats. It does **not** defend against someone with database credentials, because the
same role owns the schema in order to run migrations. Anyone claiming this log is immutable should
read this paragraph first.

*Rejected: a hash chain* (each row carrying a hash of its predecessor, making silent modification
detectable). It is genuinely cheap — a hash over the row's canonical form, one extra column — and it
would upgrade "tamper-evident against the application" to "tamper-evident, full stop". It is left
out because nothing in the threat model currently includes an insider with database access
rewriting history, and because a chain that is never verified is decoration. The trigger to revisit
is straightforward: if this log is ever used as evidence in a process that matters, add the column
and a verification subcommand.

*Rejected: a separate audit database or an append-only external store.* Correct for a system under
a compliance regime; disproportionate for a service with a handful of administrators.

### D2. The audit write joins the transaction that made the change

Audit rows are written by the same repository call that performs the change, inside the same
transaction. If the change rolls back, so does its audit entry; if the audit write fails, the change
fails with it.

*Rejected: HTTP middleware that records what happened based on route and status code.* It is much
less code and it is what most services do. It is wrong here for two reasons. First, it cannot
produce `before` — by the time the response is written, the previous state is gone. Second, it
records *requests*, not *effects*: a `200` from a handler that partially applied a change produces
an audit entry describing something that did not happen, and a change made by the CLI or an
expiry sweep produces no entry at all because there was no HTTP request. Actions that matter here
happen from three sources (console, CLI, system), and only the repository layer sees all three.

*Reconsidered on 2026-08-21, and the decision stands.* The code shipped the opposite: `authz.Record`
is called from the HTTP layer with the root database handle, after the mutation has committed
(`backend/internal/httpapi/admin.go:216-229`, whose own comment says "for a mutation that has
already happened"). The case for leaving it that way was real and is recorded here as the **rejected
alternative**: it is a great deal less plumbing — no actor parameter through
`backend/internal/inventory/` and `backend/internal/apikey/`, no transaction handle threaded down —
and a failed audit write cannot then take down a change that would otherwise have succeeded.

Atomicity won anyway, on the failure mode that argument glosses over. A write after commit does not
merely risk a missing entry; it produces a **500 for a change that was applied**. The operator
retries, the change is applied again or reported as failed when it was not, and the audit trail —
the one artefact whose job is to say what actually happened — is the thing that disagrees with
reality. "The change succeeded but we did not record it" is not a state an audit trail may reach
quietly. The two spec scenarios *"Rolled-back change leaves no entry"* and *"Failed entry fails the
change"* both describe behaviour the current code cannot exhibit, so the choice was between fixing
the code and deleting two scenarios; the scenarios are right. Fixed by tasks 2.3, 3.1 and 2.5.

Consequences worth stating: every mutating repository method takes an actor. There is no ambient
context lookup, because a missing actor should be a compile error rather than a `null` in a column
somebody later relies on. System actions — expiry sweeps, automatic key expiry — pass an explicit
system actor with `actor_source = 'system'`.

### D3. Challenge history is a separate, append-only table

The live `challenges` table from the base change is transient state, deleted on cleanup, and must
stay that way — the DNS view is built from it and it is read on the write path. History goes
elsewhere:

```sql
create table challenge_events (
  id             bigserial primary key,
  alias_id       bigint,                  -- no FK: aliases get deleted, history does not
  alias_label    text not null,
  challenge_fqdn text not null,
  domain_names   text[] not null,         -- the domains served by this alias at publish time
  project_id     bigint,
  project_slug   text not null,
  load_balancer_id     bigint,
  load_balancer_slug   text not null,
  api_key_id     text,                    -- the key identifier, as in the base design
  value_digest   text not null,           -- sha256 of the TXT value, not the value
  presented_at   timestamptz not null,
  cleaned_up_at  timestamptz,
  first_query_at timestamptz,
  last_query_at  timestamptz,
  query_count    bigint not null default 0,
  outcome        text                     -- null while in flight
);
create index on challenge_events (project_id, presented_at desc);
create index on challenge_events (alias_label, presented_at desc);
create index on challenge_events (outcome, presented_at desc);
```

Same denormalization rule as D1, for the same reason: a report covering the last twelve months must
survive a domain being removed in month three. The project and load balancer identifiers are copied in at
publish time, so history reflects where a domain lived *then*, not where its successor lives now.

`value_digest` rather than the value. The challenge value is not much of a secret — it is published
in public DNS for a minute — but it is a bearer proof of domain control for as long as it is live,
and there is no reason for a permanent table to be the one place it outlives its own record.
A digest is enough to correlate a present with its cleanup.

**Outcome classification**, set once and never revised:

| Set when | Condition | Outcome |
| --- | --- | --- |
| `/cleanup` | `query_count > 0` | `validated` |
| `/cleanup` | `query_count = 0` | `never_queried` |
| expiry sweep | never cleaned up | `abandoned` |

Three values rather than five: an `abandoned` row keeps its `query_count`, so "abandoned but
queried" (Traefik died after validation) is visible without a fourth enum value nobody would
remember the meaning of.

`never_queried` is the diagnostic that justifies the table. It means Traefik published, the CA
never came looking, and the customer's `_acme-challenge` CNAME is therefore missing, wrong, or
removed. It is the only signal in the system that distinguishes "renewal failed somewhere in
Traefik" from "the delegation this whole service depends on is broken for this domain".

### D4. DNS queries are counted in memory and flushed periodically

The DNS handler must not write to PostgreSQL. `add-acme-alias-service` D6 guarantees that DNS keeps
answering when the database is unavailable, and D5 that answers come from an in-memory view; a write
per query would break the first outright and couple query latency to database latency for no
proportionate benefit.

So the handler increments a counter:

```go
type observation struct{ count int64; first, last time.Time }
// label → observation, guarded by its own mutex, reset on flush
```

and a flusher runs on the zone-view refresh interval (default 30s), attributing each label's
observations to the challenge events that were live during the observed window:

```sql
update challenge_events
   set query_count    = query_count + $count,
       first_query_at = least(coalesce(first_query_at, $first), $first),
       last_query_at  = greatest(coalesce(last_query_at, $last), $last)
 where alias_label = $label
   and presented_at <= $last
   and (cleaned_up_at is null or cleaned_up_at >= $first);
```

**A query is attributed to every event live in that window, not to one of them.** This is not an
approximation to apologise for — it is the correct answer. A TXT query returns *all* values at the
alias, so when a wildcard and its apex are being validated together, one query genuinely serves
both authorizations. There is no information in the query identifying which value the asker cared
about, and inventing an attribution rule would be worse than recording what actually happened.

Queries for an alias with no live challenge — probes, stale caches, scanners — match no row and are
dropped, counted only in the aggregate Prometheus metric the base change already exposes.

**Loss window.** A crash loses up to one flush interval of counts. That is acceptable for a
reporting signal and is the price of the availability property above; it is written down here so
that nobody later "fixes" the loss by moving the write onto the query path. The one case where it
matters is a value queried exactly once, moments before a crash, whose event would then be
classified `never_queried` — a false alarm, not a missed one, which is the right direction for this
error to point.

### D5. An issuance attempt is a group of overlapping challenges at one alias

Reporting exposes two numbers and never conflates them:

- **challenge attempts** — one per `/present`, the raw row count.
- **issuance attempts** — challenge events at the same alias whose live windows overlap, grouped.

The wildcard case from the proposal is the reason: `example.com` plus `*.example.com` is two
authorizations at one challenge name for one certificate, so a per-`/present` count reports double
for every wildcard customer. Grouping is computed on read from `presented_at`/`cleaned_up_at`
overlap; nothing is materialized, because the rule may want tuning and a stored grouping would be
wrong retroactively.

An issuance attempt's outcome is the best of its members: `validated` if any member was queried,
otherwise `never_queried`, otherwise `abandoned`. Both numbers appear in the API. The console shows
issuance attempts, with challenge attempts available, because "how many certificates did this
project renew" is the question people mean.

### D6. Aggregates are computed on read; only inventory size is snapshotted

At the volume this service operates at — a few hundred domains renewing every sixty days is
single-digit rows per day, and ten thousand domains would still be under a hundred — aggregating
raw `challenge_events` on read is fast for many years with the indexes above. No rollup tables, no
materialized views, no partitioning. They can be added the day a query gets slow, and that day is
unlikely to arrive.

The exception is **inventory size**, which is not derivable from history at all: `domains` and
`aliases` are mutable rows with no deletion record, so "how many labels did this project have in
March" is unanswerable retroactively. A daily job writes one row per project:

```sql
create table project_daily_stats (
  project_id   bigint not null,
  project_slug text not null,
  day          date not null,
  alias_count  int not null,
  domain_count int not null,
  load_balancer_count int not null,
  primary key (project_id, day)
);
```

One row per project per day is the smallest thing that makes the trend answerable, and it is the
only place in this change where data is stored that could otherwise be computed.

*Rejected: soft-deleting domains and aliases instead*, reconstructing history from
`created_at`/`deleted_at`. More faithful, and it would give exact answers rather than daily
resolution — but it puts `deleted_at is null` into every inventory query in the base change, which
is a tax on code that has nothing to do with reporting, and a filter somebody will eventually
forget. A snapshot table touches nothing else.

### D7. Prometheus stays aggregate; per-name history stays in PostgreSQL

Metrics gain outcome-labelled counters only:

```
acme_challenge_events_total{outcome="validated|never_queried|abandoned"}
acme_dns_query_observations_total{attributed="true|false"}
acme_audit_entries_total{action_class="config|credential"}
```

**No metric carries a domain, alias, project or key as a label.** It is the obvious way to satisfy
"counters per DNS name" and it is a trap: one time series per domain, multiplied by outcome and
status, is precisely the cardinality growth that makes a Prometheus server unusable — and the
series persist after a domain is removed. Prometheus answers "is something broken right now" for
alerting; PostgreSQL answers "what happened to this domain over the last year", where a
high-cardinality dimension is simply a column with an index on it.

The one alert worth defining here: a rising `never_queried` rate, which means delegations are
breaking. Per-domain attribution for that alert comes from the API, not from the metric.

### D8. Where it appears

- ~~**Domain page**~~ — **cut on 2026-08-21.** There is no domain route in the console and one is
  not being added; the never-validated signal is served by the load balancer screen's interactions
  panel filtered to `unqueried`. See `proposal.md`, *Reporting: per project, over time*.
- **Project page** — issuance attempts over time by outcome; current and historical alias, domain
  and load balancer counts.
- **Audit log** — the existing screen, extended with the before/after view, the actor's roles at the
  time, and filtering by action class, object type, actor and project.
- **API** — every screen's data is a documented endpoint with explicit time ranges, so the numbers
  can be pulled into whatever reporting we already run without scraping the UI.

Everything is labelled *challenge attempts* or *issuance attempts*. Nothing in this system is
labelled "certificates issued", because the service cannot see certificates and a chart that claims
otherwise will be believed.

### D9. Retention and privacy

Audit entries and challenge events are kept indefinitely — the volume does not justify a retention
policy, and both are most valuable when old. Neither table contains a secret: audit `before`/`after`
exclude key hashes at the serializer, and challenge events store a digest rather than a value.

Personal data is limited to what the IdP already holds about an operator — subject, display name,
group membership — plus the client address of an administrative action. DNS query source addresses
are deliberately not recorded (proposal, *what this does not do*).

## Risks / Trade-offs

- **`never_queried` will produce false positives at first.** A challenge published while a customer's
  CNAME is still propagating is genuinely never queried, and correctly so. → The console reports the
  pattern over time rather than the single event, and the DNS-plan verification from the base change
  is the authoritative check for whether the delegation exists.
- **The counters describe our half of the transaction only.** A `validated` outcome means the value
  was published and queried, not that a certificate was issued — finalization happens between
  Traefik and the CA. → Naming everywhere is *challenge*/*issuance attempt*, never *certificate*.
- **Denormalized history diverges from live inventory.** A domain moved between load balancers shows its
  old load balancer on old rows. → That is the point of history, and the UI says "at the time" where it shows
  these fields.
- **The audit trail is only as append-only as the database role.** → Stated plainly in D1 rather than
  implied; the hash-chain upgrade path is recorded with the trigger for revisiting it.
- **Query counts can be lost across a crash.** → Bounded by one flush interval, and the error points
  toward a false alarm rather than a silent miss (D4).
- **Two more tables on every inventory write path.** → One insert per mutation, inside a transaction
  that already exists. The DNS path takes no database work at all.

## Migration Plan

Purely additive; nothing to migrate and nothing to backfill.

1. Migrations create `audit_entries`, `challenge_events` and `project_daily_stats`, and narrow the
   service role's grants on `audit_entries` to `INSERT, SELECT`.
2. Deploy. Both history tables begin empty; reports show "no data yet" rather than zeros, which are
   a different and misleading claim.
3. The daily snapshot job writes its first row on the next scheduled run; the first trend chart is
   meaningful after a few days.
4. Alerting on `never_queried` is enabled only after a full renewal cycle has been observed, so the
   baseline is real rather than assumed.

**Rollback** is dropping the three tables and restoring the grant. No behaviour in
`add-acme-alias-service` depends on them; the audit writer degrades to the thin requirement it
supersedes.

## Open Questions

- **Should `never_queried` raise something by itself?** It is the strongest signal in the system, and
  a domain accumulating them is definitely broken — but it fires legitimately during onboarding, so
  the threshold ("N consecutive, over M days, for a domain that has validated before") needs a
  cycle of real data before it can be chosen honestly.
- **Does the audit trail need export?** CSV or JSON for a period, for handing to somebody outside
  this console. Trivial to add; nobody has asked.
- **How long is "indefinitely"?** No retention policy is proposed because the volume does not
  require one. If a data-retention standard applies to operator identities, that decision belongs to
  whoever owns it, not to this design.
