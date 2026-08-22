## Why

### Two questions nobody can currently answer

`acmeservice` sits in the middle of every certificate renewal we perform. That position makes it
the only place where two questions can be answered, and as currently specified it answers neither.

**"Who changed this, and when?"** The base change records a thin audit trail — enough to know that
somebody created a key. It is not enough for the questions that actually get asked. When a
customer's certificate stops renewing, the useful question is not "is the service up" but "what
changed": was a domain removed, was a key revoked, was a source-address restriction added that the
load balancer's egress address no longer satisfies. When a credential leaks, the question is "who
issued it and when". Those need before-and-after values, the identity that acted, and a record that
cannot be quietly edited afterwards by the same person.

**"How much is actually being issued, and for what?"** Nothing counts renewals. We cannot say
whether a domain renewed last month, whether a project's certificate volume is growing, or — the
important one — whether a domain onboarded three months ago has *ever* successfully validated. A
domain can sit in the inventory looking perfectly configured while every renewal silently fails,
and today the first symptom is an expired certificate.

### The thing worth measuring is not quite the thing that was asked for

The request is for "counters for certificate issuing per DNS name". Taken literally that is not
measurable here, and the gap is worth being precise about, because it changes what gets built.

**This service never issues a certificate.** It answers a challenge. The certificate is issued by
Let's Encrypt to Traefik, over a connection this service is not part of and cannot observe. What we
*can* observe is three events, and a real renewal produces all three:

```
1.  POST /present     Traefik publishes a challenge value       ← we see this
2.  DNS TXT query     someone looks the value up                ← we see this
3.  POST /cleanup     Traefik withdraws it                      ← we see this
```

Step 2 is the interesting one, and it is why this is worth building rather than reading off
Traefik's logs. A challenge that was published and **never queried** means nobody came looking —
which almost always means the customer's `_acme-challenge` CNAME is missing, wrong, or has been
removed. That failure is currently invisible to us: Traefik reports a generic DNS-01 failure with
no hint that a separate service is involved, and our side looks perfectly healthy, because we did
exactly what we were asked to do.

So the counter that matters is not "certificates issued". It is **challenge attempts, classified by
what happened to them**:

| Outcome | What it means |
| --- | --- |
| `validated` | Published, queried at least once, cleaned up. A renewal that worked. |
| `never_queried` | Published and withdrawn, nobody ever looked. **The customer's CNAME is broken.** |
| `abandoned` | Published, never cleaned up, expired on our side. Traefik died mid-issuance. |

A history of those three per load balancer, and a per-project total over time, answers the original
request and also answers the question behind it. (It was originally to be per *domain*; that was
dropped on 2026-08-21 for want of a console screen to show it on — see *Reporting: per project, over
time* below.)

### One certificate is not one challenge

The other reason to be careful with the word "counter": the mapping from challenges to certificates
is not one to one, and a naive count is wrong in a specific, common case.

A certificate covering both `example.com` and `*.example.com` — the ordinary wildcard setup —
makes Let's Encrypt create **two authorizations**, both validated at the **same** challenge name
`_acme-challenge.example.com`, so Traefik publishes **two values** at one alias, at the same time.
Counting `/present` calls reports two issuances where a human would say one.

```
14:02:11  present  example-com  "V1..."   ┐
14:02:11  present  example-com  "V2..."   ├─ ONE certificate
14:02:40  query    example-com  (2 TXT answered)
14:03:05  cleanup  example-com  "V1..."   │
14:03:05  cleanup  example-com  "V2..."   ┘
```

So the reporting groups overlapping challenges at one alias into a single **issuance attempt**, and
reports both numbers where they differ. Getting this wrong would make every wildcard customer look
twice as busy as they are — the sort of quiet inaccuracy that discredits a whole dashboard.

## What Changes

Three additions, all building on the inventory and challenge machinery from
`add-acme-alias-service`. This change is meaningless without it and must be applied after it.

### 1. A real audit trail

The base change's single "privileged actions are recorded" requirement grows into a capability:

- **Every configuration change** — projects, load balancers, nodes, domains, aliases — recorded with the
  acting identity, the object, and the **before and after values of every changed field**. "Domain
  removed" is not useful on its own; "domain `shop.example.com` removed from load balancer
  `acme-shop/eu-prod`, alias `shop-example-com` deleted with it" is.
- **Every credential event** as a first-class action: created, restricted, expiry changed, revoked,
  expired. Key rotation is the sequence most likely to be examined months later, so the key
  identifier, the load balancer and the CIDR list are recorded on each.
- **The roles the actor actually held at the time**, not merely their name. Roles come from
  environment configuration mapped onto Keycloak groups, and both sides of that mapping can change.
  An entry recording only "changed by alice" cannot answer "how was alice allowed to do that",
  which is exactly what an incident asks.
- **Append-only in the application.** No code path updates or deletes an audit row, and the database
  role the service runs as holds `INSERT` and `SELECT` on that table and nothing else. This is
  tamper-*evidence* against the application, not tamper-proofing against somebody holding database
  credentials — `design.md` is explicit about the difference rather than implying a guarantee we do
  not have.

### 2. A challenge history

A durable, append-only record of every challenge attempt — a different thing from the live
`challenges` table the base change already has, which is transient state deleted on cleanup. This
is history, and the counters are computed from it.

One row per `/present`, carrying the alias, the domains it serves, the load balancer and project it
belonged to at the time, the API key that published it, and then — as they happen — the first and
last time the value was queried in DNS, how many times, when it was cleaned up, and the resulting
outcome.

**Query observations must not touch the database on the DNS path.** The base design guarantees that
DNS keeps answering when PostgreSQL is unavailable, and a write per query would destroy that
outright, besides making DNS latency depend on database latency. Instead the DNS handler increments
an in-memory counter and a periodic flush persists the totals, on the cadence the zone view already
refreshes on. The cost is that a crash loses at most one flush interval of query counts —
acceptable for a reporting signal, unacceptable for the availability property it protects.

### 3. Reporting: per project, over time

~~**Per domain**: its challenge history — when it last validated, how many attempts, how many went
unqueried.~~ **Dropped on 2026-08-21, for want of a screen to put it on.** This was the headline of
the section and it is worth being explicit about why it went, because the *question* it answered is
still the most valuable one here.

The console has no domain route: `frontend/src/app/app.routes.ts` routes projects and load
balancers, and a domain is edited inline from the load balancer screen
(`frontend/src/app/features/domains/domain-editor.component.ts`). A per-domain report therefore
needed a whole new screen — a domain detail page, its route, its resolver, its navigation — before
a single number could be read by anyone. Adding that screen was considered and declined: it would
be a page whose only content is one panel, reached by a click path that does not exist today, for a
signal that is already visible one level up.

Because it *is* already visible one level up. The load balancer screen's interactions panel
(`frontend/src/app/features/loadbalancer/interactions-panel.component.ts`, shipped by
`rebuild-console-and-add-interaction-history`) lists every challenge interaction with its outcome
and can be filtered to `unqueried` — published, withdrawn, and nobody ever looked. That filter is
the never-validated signal, under a different name and at a coarser scope: instead of "this domain
has never validated", an operator sees "these three interactions on this load balancer went
unqueried", each row naming the alias and the domain names it served. Same evidence, same
conclusion, on a screen they are already on when a load balancer misbehaves.

What is genuinely lost is the *rollup*: nobody can ask "has `shop.example.com` ever validated, in
its whole history" and get one answer. Answering it today means reading the interactions list and
looking at the domain names on each row. That is worse, and it is the price of not building a
screen. If the question gets asked often enough to hurt, the fix is the domain detail page first
and the aggregate second — in that order, because the aggregate without the page is what was just
cut.

Cut with it: task 6.1 (the aggregate), 6.6 (its test), 7.1 (`GET /domains/{id}/history`), 8.1 (the
panel) and 8.2 (the never-validated callout), and the `usage-reporting` requirement *"History is
reported per domain"*. Nothing else in the capability changed — project-level aggregation, the
daily inventory snapshot, the no-data-versus-zero rule and the metric-cardinality rule are
untouched.

- **Per project**: issuance attempts over time, bucketed by day and month, split by outcome.
- **Per project inventory size**: the number of configured aliases (labels) and domains. Alias and
  domain rows carry no deletion history, so a trend cannot be reconstructed after the fact — a small
  daily snapshot per project is recorded instead, one row per project per day, the cheapest thing
  that makes "how did this grow" answerable a year from now.

Surfaced on the console's project page — there is no domain page, see above — and available from the management API so the
numbers can be pulled into whatever reporting we already run.

### What this deliberately does not do

- **It does not put per-domain labels on Prometheus metrics.** That is the obvious-looking way to
  get "counters per DNS name" and it is a trap: a label whose value is a domain name creates one
  time series per domain, and a few hundred domains multiplied by outcome and status labels is the
  cardinality explosion that makes a Prometheus server fall over. Prometheus keeps aggregate
  counters for alerting; per-name and per-project history is served from PostgreSQL, where a
  high-cardinality dimension is just a column.
- **It does not report certificates**, because the service cannot see them. Every number is labelled
  as challenge attempts or issuance attempts, in the API and in the UI, rather than quietly
  presenting a proxy measure under a more satisfying name.
- **It does not record the source addresses of DNS queries.** Tempting — it would show which
  validation vantage points reached us — but it turns a counter into a store of third-party network
  data, for a diagnostic the query count already provides.
- **It adds no rollup tables for the challenge history.** At this volume — a few hundred domains
  renewing every sixty days is single-digit rows per day — aggregating raw rows on read is fast for
  years. The daily inventory snapshot exists only because that data genuinely cannot be
  reconstructed later.

## Capabilities

### New Capabilities

- `audit-trail`: What is recorded when configuration or credentials change, the fields each entry
  carries, append-only semantics and their limits, retention, and how entries are read and filtered.
- ~~`challenge-history`~~ — **withdrawn from this change on 2026-08-21.**
  `rebuild-console-and-add-interaction-history` shipped this capability as `challenge_interactions`
  and declares it there. Both changes declared it as `## ADDED Requirements`, with two requirement
  headings byte-identical, so whichever archived second would have re-added requirements already in
  the baseline. That change's version is authoritative: it describes the code that exists, and it
  goes further — a fifth outcome (`abandoned`, from the sweep at `backend/internal/sweeper/sweeper.go:60`),
  a per-load-balancer read requirement, the client address and submitted name form, and republish
  idempotence. The `usage-reporting` capability below still reads that history; it just no longer
  defines it.

  Two things this change's version said that the surviving one does not, neither of them yet homed:

  - **The interaction carries the project it belonged to, not only the load balancer.** The other
    change's text names the load balancer alone — but the column exists and is populated
    (`backend/internal/store/migrations/000002_challenge_interactions.up.sql:28`,
    `backend/internal/httpapi/challenge.go:389`, indexed at `:60-61`), and `usage-reporting`'s
    requirement *"Attempts are aggregated per project over time"* cannot be met without it. The
    surviving spec is behind its own code here.
  - **An observation matching no interaction is counted in an aggregate metric.** This change's
    scenario ended *"…and the observation is counted only in aggregate metrics"*. The other change's
    same-named scenario is about a label that no longer resolves and says nothing about counting.
    Today the count is logged at WARN and nowhere else (`backend/cmd/acmeservice/main.go:204-214`),
    which is why that change's own task 6.9 calls it half done — but with this sentence gone, no
    requirement asks for it.

  One deliberate reversal, recorded so it is not mistaken for a loss: this change specified an
  outcome *assigned once and never revised*; the shipped design derives the outcome on read, so a
  late observation correctly changes it. That is the better answer and the surviving spec says so.
- `usage-reporting`: The aggregates derived from that history — per domain, per project, over time —
  the daily inventory snapshot, and how all of it is exposed through the API and the console.

### Modified Capabilities

None declared, and that is now a loose end rather than a considered choice. When this was written,
`add-acme-alias-service` had not been archived, so its capabilities did not exist in
`openspec/specs/` and there was nothing to write a delta against; the one overlapping requirement
was therefore named in prose instead.

That change **was archived on 2026-08-21** (`openspec/changes/archive/2026-08-21-add-acme-alias-service`),
and the eight baseline capabilities now sit in `openspec/specs/`. The prose note below no longer
does the job a delta would.

**Supersedes**: the requirement *"Privileged actions are recorded"*, now live at
`openspec/specs/identity-and-roles/spec.md:193`. It says the service records inventory, key and
session changes with the acting identity, object, action and time, and that records contain no
secrets — all of which `audit-trail` restates and then goes past, adding before/after state, the
roles the actor held at the time, append-only semantics and the read API. Two requirements covering
one behaviour, one of them weaker, is how a spec starts disagreeing with itself.

The fix is a `## REMOVED Requirements` delta in this change at
`specs/identity-and-roles/spec.md`, deleting that requirement so archiving this change retires it in
the same motion that adds `audit-trail`. It is deliberately **not** written yet, because
`audit-trail` does not yet cover one thing the baseline requirement does: session revocation
(`backend/internal/httpapi/admin_sessions.go:149`) is audited in code but appears in no `audit-trail`
requirement. Removing the baseline requirement first would drop that behaviour out of the specs
entirely. Either add a session-revocation scenario to `audit-trail` and then write the removal, or
leave the baseline requirement standing and say so — but not the current silence.

**Also relevant**: this change no longer declares `challenge-history` at all — see the withdrawal
note under *Capabilities* above. The two changes can now be archived in either order; the only
remaining cross-change dependency is documentary, not structural.

## Impact

**New code.** Two tables and their models, an audit writer used by every mutating path, an in-memory
query counter with a periodic flush, a set of aggregate queries, a daily snapshot job, management
API routes, and two console screens' worth of presentation.

**New dependencies.** None. Charting uses what the console already brings.

**Data growth.** One audit row per administrative action, one challenge-history row per
authorization, one snapshot row per project per day. For 500 domains that is a few thousand rows a
year — no partitioning, no pruning, no archival tier.

**Performance.** The write paths already open a transaction; the audit row joins it. The DNS query
path gains an in-memory increment and no database work at all.

**Operational.** The `never_queried` outcome is the first signal this system has that an onboarding
did not actually finish, and it is worth alerting on: a domain whose challenges are published but
never looked up will fail every renewal until somebody fixes the customer's CNAME — discovered
today only when the certificate expires.
