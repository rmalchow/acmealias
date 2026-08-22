## Why

### What this change is

`add-acme-alias-service` built the service: an inventory of customers, an HTTP API that a load
balancer's ACME client posts challenge values to, an authoritative DNS server that answers them,
and a web console for humans. It is essentially finished — 138 of its 155 tasks are done, the
remainder being the acceptance environment.

This change records a batch of work that landed *on top of* it and was never written down. It is
not one feature. It is four things that arrived together because the first one broke the second,
the second exposed the third, and the third made the fourth worth building:

1. **The console's lists page and filter on the server.** They used to fetch everything.
2. **The console was rebuilt** on a component library, with a fixed layout and dialogs — and two
   screens were deleted.
3. **A load balancer's node addresses became editable**, which they were not.
4. **A new capability: challenge interaction history** — a durable record of every publication and
   of whether the certificate authority ever came to look it up.

Item 4 is the one with lasting value, and it answers a question the service could not answer
before. Items 1–3 are the reason it is reachable.

### 1. The list screens were lying by omission

Every list screen in the console fetched the whole table and filtered it in the browser. With one
seeded project that is invisible. With the two hundred projects this is meant to hold, three things
go wrong at once, and only the third is obvious:

- **The filter searches the wrong set.** A browser-side filter can only match rows the browser
  already has. If the list is cut to the first fifty rows for rendering, typing `shop` searches
  fifty rows and reports "no match" for a project that exists. It is not slow — it is *wrong*, and
  wrong in the direction that makes an operator conclude a customer was never onboarded.
- **The count is meaningless.** "12 results" out of what?
- **The page is enormous.** The project list response carries every project *with its load balancer
  lbs, their nodes and their domains* preloaded. That is the whole inventory in one JSON body.

So `GET /projects`, `GET /challenges` and `GET /audit` now take `limit`, `offset` and — for the
first two — `q`, and `GET /audit` additionally takes `from` and `to`. The service applies all of
it. Concretely, the console's project screen at page 2 with the filter `demo` asks for:

```
GET /api/v1/projects?q=demo&limit=25&offset=25
```

and the answer is still a **plain JSON array** — the same array `adminctl` and any `curl … | jq` in
anyone's runbook already parse. The size of the full filtered result set travels in a response
header instead:

```
HTTP/1.1 200 OK
X-Total-Count: 63
[ { "id": 7, "slug": "demo", "name": "Demo Project", … } ]
```

`63` is what lets the pager say "26–50 of 63". Wrapping the body in `{"items": […], "total": 63}`
was rejected: it would have broken every existing caller to serve one screen. This is written up in
`design.md` D1, together with why there is no cursor.

The date bound has one deliberate subtlety worth stating up front, because getting it wrong is
silent. The console's audit date picker submits bare dates, `from=2026-08-19&to=2026-08-20`. An
operator who names the 20th means *the whole of the 20th*. Parsed as a timestamp, `2026-08-20`
is midnight, and every entry recorded during the day they asked about would be excluded — the
filter would appear to work and would hide the thing being looked for. So a bare `to` date is
pushed to the last instant of that day. A bare `from` date is left at midnight, which is already
the start of the day.

### 2. Paging broke the project screen, which is how `GET /projects/:id` came to exist

The console's project detail screen used to find its project by fetching the project list and
searching it for the id in the URL. That worked precisely as long as the list was complete. The
moment a default limit existed, "find it in the list" quietly became "find it in the *first page*
of the list", and `/projects/9` for the sixtieth project rendered an empty screen with no error —
the fetch succeeded, the search simply found nothing.

A screen addressed by id has to be able to fetch by id. `GET /api/v1/projects/:id` now exists and
the detail screen calls it. This is a small endpoint with a large lesson attached, which is why it
gets a spec requirement of its own rather than a line in a changelog.

### 3. The console rebuild, and two screens that were deleted

The console is now built on **ng-zorro-antd** — the Angular port of Ant Design, a component library
supplying the tables, modals, form controls and icons — in its **dark theme, and only its dark
theme**. There is no toggle and no light stylesheet: shipping one theme means one set of colours to
check contrast on, and the console is an internal operations tool whose users asked for dark.

Three rules came with the rebuild, and each exists to fix something that was actually annoying:

- **Every screen opens with the same header block** — breadcrumb, headline, subheadline — and the
  space for all three is *reserved even when a screen has nothing to put in them*. Without the
  reservation, a screen with no breadcrumb starts its first table 22 pixels higher than the screen
  next to it, so the "Delete" button an operator is reaching for moves under their cursor as they
  navigate. Reserving the space is not decoration; it is why the pointer lands where it was aimed.
- **Every creation is a modal dialog that survives its own failure.** The failure that actually
  happens is a slug collision: an operator types `demo` for a project that already exists, and the
  service answers `409` with a message naming the conflict. If the dialog closed on submit, that
  message would arrive with the form gone and the operator would retype everything. So the dialog
  stays open, keeps what was typed, and renders the service's own sentence — verbatim, because the
  server is the only party that knows which rule was broken.
- **Row actions are icons.** A table of twenty domains with a text "Delete" button on every row is
  twenty repetitions of the word Delete. The trashcan carries its label in a tooltip and an
  `aria-label` instead, so it is still announced to a screen reader and still discoverable on
  hover.

Everything the browser loads is bundled at build time — Roboto arrives as the npm package
`@fontsource/roboto`, icons as ng-zorro's SVG icon components. This is a hard constraint, not a
preference: the compiled bundle is embedded in the Go binary and the binary runs on a host with no
route to the internet. A `<link>` to Google Fonts would render a console in Times New Roman with a
several-second stall on every page load, on exactly the machine nobody can debug from.

There is a sharp edge in ng-zorro here that is worth a requirement rather than a comment. An
`<span nz-icon nzType="delete">` whose icon has not been registered up front does not fail loudly —
it **fetches the SVG from Ant Design's CDN at runtime**. On the production host that request hangs
and the icon never appears. So icons are registered explicitly in `app.config.ts`, and adding an
icon to a template means adding it to that list.

**Two screens were deleted: active sessions, and effective configuration ("settings").** Both were
in `add-acme-alias-service`'s design (D11) and both were built. Both endpoints — `GET`/`DELETE
/sessions` and `GET /config` — still exist and are still role-gated exactly as before; what was
removed is navigation, not authority. The reasoning:

- **Sessions** is a support action, not an operator's work. It is used on the rare day somebody
  leaves the company, by a person who is already at a terminal running the "remove them from the
  Keycloak group *and* revoke their sessions" procedure. A screen for it occupies a permanent slot
  in the navigation of every user who will never open it.
- **Settings** is read while diagnosing — "which group does the *running process* map to
  `CONFIG_ADMIN`?" — and the answer is equally available from the startup log and from `curl`. It
  is not paged through in a browser.

Removing a screen is a real reduction in capability, so it is recorded as a `REMOVED` requirement
and not quietly dropped.

### 4. The new capability: what the load balancer did, and whether anyone came to look

This is the part worth the most.

The service sits between a customer's load balancer and Let's Encrypt, and until now it kept no
record of that. There is a live `challenges` table, but it is *state*, not history: a row exists
while a value is answerable and is deleted on cleanup. Ninety seconds after a successful renewal
there is nothing to show for it.

That matters because of one specific failure, which is both the most common and the most invisible.
The whole design of this service rests on the customer publishing a CNAME in their own zone:

```
_acme-challenge.demo.example.com.  IN  CNAME  demo-example-com.acme.example.net.
```

If they never publish it — or publish it with a typo, or remove it during an unrelated migration —
then everything on our side still looks perfect. Their Traefik authenticates with key `k_ec95dc38`,
posts the challenge value, we store it, our DNS server answers it correctly to anyone who asks. But
**nobody ever asks**, because Let's Encrypt follows the customer's `_acme-challenge` name and there
is nothing there to follow. Traefik reports a generic DNS-01 timeout that does not mention us. Our
logs show a successful `/present` and a successful `/cleanup`. The first symptom anyone notices is
an expired certificate, sixty days later.

The signal that distinguishes this from a working renewal is exactly one bit: **was the value ever
queried in DNS**. So the service now writes one row per publication, opened by `/present` and
closed by `/cleanup`, and folds DNS query counts into it. For the seeded development environment,
one renewal of `demo.example.com` produces a row that reads:

| field | value |
| --- | --- |
| alias label | `demo-example-com` |
| challenge FQDN | `_acme-challenge.demo.example.com` |
| domains at publication | `demo.example.com`, `*.demo.example.com` |
| key id | `k_ec95dc38` |
| client IP | `172.28.0.30` (the `traefik-lb` container) |
| FQDN form | `challenge` |
| value digest | `sha256:9f2c…` |
| published / withdrawn | 14:02:11 / 14:02:31 |
| queried | first 14:02:19, last 14:02:24, count 2 |
| outcome | `validated` |

Four properties of that row are deliberate and each is a decision:

**The challenge value is never stored — only a SHA-256 digest of it.** The value is a secret shared
with the CA for the length of one validation. The only thing the history needed it for was telling
two concurrent authorizations at one name apart, and a digest does that.

**Everything is denormalized and there are no foreign keys to aliases or domains.** `demo.example.com`
and `*.demo.example.com` are copied in as text at publication time. If somebody offboards that
customer next month, the history of what their load balancer did stays readable — which is the
entire reason to keep history. A row that vanishes with the object it describes cannot answer "what
was key `k_ec95dc38` doing in July".

**Writing history is best effort.** If the history write fails, `/present` still returns success and
a warning is logged. The asymmetry is intentional: the certificate is the point of that endpoint,
the record of it is not, and a `500` there would make a client retry publishing a value that is
already published and already answerable.

**Both domains, not one.** A certificate covering `demo.example.com` and `*.demo.example.com` — the
ordinary wildcard setup — produces *two* authorizations validated at the *same* challenge name,
so Traefik publishes two values at `demo-example-com` at once, and a single TXT query returns both.
Two rows; one query; both rows credited. See below.

#### Counting queries without touching the database

The DNS server is the part of this service that must keep answering when PostgreSQL does not — it
serves from an in-memory view precisely so a database outage cannot stop a renewal. A history write
per query would hand it the dependency it was designed not to have, and would couple DNS latency to
database latency for the sake of a statistic.

So the query path does one thing: increment a counter in a map, keyed by alias label, under a
mutex. A background flusher drains that map on the zone-view refresh interval and applies the
totals in one statement per label. If the database is down the counts simply accumulate and land
later. If the process dies, up to one interval of counts is lost — acceptable for a signal that
says "the CA came to look", and written down here so nobody later "fixes" the loss by moving the
write onto the query path.

#### The attribution rule that is the whole point

This is the subtle part, and it was wrong first.

The obvious way to apply a flushed count is: find the rows that are *live right now* at that alias
and add to them. That version credits almost nothing, and the reason is arithmetic. A DNS-01
validation lasts a handful of seconds — publish, get queried, clean up — while the flusher runs on
the zone-view interval, tens of seconds apart. By the time the flush executes, the row it should
have credited was withdrawn ten seconds ago and is no longer live. The count lands on nothing.
Every successful validation is then reported as **never queried** — which is precisely the alarm
this feature exists to raise, fired on exactly the renewals that worked.

The correct rule is **window overlap**. The counter carries the start of the window its counts were
accumulated over; the flush carries the end. A row is credited when it was published before the
window ended *and* was not withdrawn before the window began:

```
window   [------------------------ 30s flush interval ------------------------]
row A       published ●———— queried ————● withdrawn         → overlaps → credited
row B                                            published ●————————…  → overlaps → credited
row C    ●—— withdrawn before the window opened                        → no overlap
```

Both bounds come from the counter, never from `time.Now()` at flush time, so a slow or retried
flush cannot shift the window a query is attributed to.

And when two rows overlap the same window at the same alias — the wildcard case — **both** are
credited. That is not an approximation to apologise for. A TXT query returns every value at the
name; the single query genuinely served both authorizations; there is nothing in the query
identifying which value the asker cared about, and inventing an attribution rule would record
something that did not happen.

#### What the operator sees

A "Recent interactions" panel on the load balancer screen, showing when a value was published
and how long it stood, which domains the alias served at the time, the source address and key that
published it, whether it was ever queried, and an outcome tag. The outcome is **derived on read**,
not stored, so it stays correct for a row that is still open while being looked at:

| outcome | meaning |
| --- | --- |
| `validated` | withdrawn, queried at least once — a normal renewal |
| `unqueried` | withdrawn, never queried — **check the customer's CNAME** |
| `live` | still published, already queried — a validation in flight |
| `pending` | still published, nothing has asked yet |

And one switch: **"Never queried only"**. Turning it on lists exactly the publications nobody came
for, which turns a vague "their renewals keep failing" into a list of domains to go and fix.

### 5. A bug fix that deserves a scenario

`store.ListLoadBalancersForProject` preloaded each load balancer's nodes but not its domains. The project screen
shows a domain count per load balancer, computed from that list, so it displayed **0 for every load balancer that had
domains**. The fix is one added `Preload("Domains")`. It gets a scenario because the failure mode
is a plausible-looking wrong number rather than an error, and a wrong number on the screen an
operator uses to sanity-check an onboarding is worse than a blank one.

## What Changes

### Endpoints

| method | path | change |
| --- | --- | --- |
| `GET` | `/api/v1/projects` | new `q`, `limit`, `offset`; `X-Total-Count` |
| `GET` | `/api/v1/projects/:id` | **new** |
| `GET` | `/api/v1/challenges` | new `q`, `limit`, `offset`; `X-Total-Count` |
| `GET` | `/api/v1/audit` | new `from`, `to`, `limit`, `offset`; `X-Total-Count` |
| `PATCH` | `/api/v1/nodes/:id` | addresses only; label immutable; both-empty refused |
| `GET` | `/api/v1/loadbalancers/:id/interactions` | **new** — `q`, `unqueried`, `limit`, `offset`; `X-Total-Count` |

### Console

Rebuilt on ng-zorro-antd, dark theme only, fixed-width page container, shared page-header block,
modal creation and confirmation dialogs, icon row actions, bundled fonts and icons. The sessions
and settings screens are gone. New: the interactions panel, the node-address editor, and a switch
in the API-key dialog that pins the new key's CIDR restrictions to the load balancer's own node addresses
(for `demo/prod`, `203.0.113.10/32` and `203.0.113.11/32`) — a leaked secret is then useless from
anywhere else.

### Data

One new table, `challenge_interactions`, created by
`backend/internal/store/migrations/000002_challenge_interactions.up.sql`. No foreign keys to
aliases or domains. No changes to any existing table.

## Capabilities

### Modified Capabilities

- `admin-console` — the console's lists page and filter on the server; the fixed header block;
  modal create and confirm; the ng-zorro dark shell and its offline-asset constraint; the
  interactions panel; the node-address editor; the pin-to-node-addresses switch. **Two
  requirements are removed**: the active-sessions screen and the effective-configuration screen.
- `service-inventory` — list endpoints are paged and filtered with a reported total; a project is
  readable by id; node addresses are editable while the label is not; a load balancer's domains are
  loaded wherever its domains are counted.
- `acme-challenge-api` — recording interaction history is best effort and never fails `/present`
  or `/cleanup`.
- `challenge-history` — see the reconciliation below.

### Where `challenge-history` comes from

`challenge-history` is **not a new capability name**. It was introduced by
`add-audit-and-usage-reporting`, a change that is written but entirely unimplemented (0 of 58
tasks). The landed work implements part of it, under a different table name and with two deliberate
differences. Creating a second capability — `challenge-interactions` — for the same table and the
same question would leave two specs describing one thing, which is exactly the outcome the
reconciliation below exists to avoid. So this change writes deltas against `challenge-history`, and
`add-audit-and-usage-reporting` should be revised rather than applied as written.

**What the landed work satisfies, requirement by requirement:**

| requirement in `add-audit-and-usage-reporting` / `challenge-history` | status |
| --- | --- |
| *Every published challenge is recorded durably* | **satisfied, with one change.** The record is written best effort rather than unconditionally. It carries the project **and** the load balancer — both stamped at publication, so history stays attributable after either is deleted — plus three fields that change did not anticipate: the client IP, which FQDN form the client sent, and the alias id. |
| *DNS query observations are attributed without datastore access on the query path* | **satisfied**, and restated here to name the overlap window explicitly, which the original left as "live window overlaps". Its scenario *"unmatched observations … counted only in aggregate metrics"* is **partly** satisfied: an observation whose label no longer resolves is counted and logged at WARN, but there is still no metric, so it cannot be alerted on. |
| *Each record is classified by outcome* | **replaced, not adopted.** That requirement stores one of `validated` / `never queried` / `abandoned`, assigned once and never revised. The implementation derives `validated` / `unqueried` / `live` / `pending` / `abandoned` on read. The two extra values exist because the console shows rows that are still open; `abandoned` is now present too, backed by a flag the expiry sweep stamps — see design D7 for why that one value is stored rather than derived. |
| *Cleanup without a matching record is not an error* | **satisfied.** Closing a value with no live row updates nothing and returns success. |

**What is not satisfied and stays outstanding:** the metric. An observation for a label that no
longer resolves is counted and logged, but nothing exports
`acme_dns_query_observations_total{attributed}`, so "the flush is attributing nothing" and "nothing
is being queried" still look identical to a monitoring system. `tasks.md` 6.9 carries it unchecked.

**What should stay as `add-audit-and-usage-reporting`:** everything else in it, which is the
majority. Its `audit-trail` capability is untouched by this work — no before/after values, no
credential events as first-class actions, no narrowed grants — with the single exception of its
requirement *"Entries are readable and filterable"*, which the `from`/`to`/`limit`/`offset`
parameters landed here now **fully satisfy**. Its `usage-reporting` capability is untouched
entirely: nothing here aggregates per domain or per project, nothing buckets by day or month,
nothing groups overlapping publications into "issuance attempts", and there is no daily inventory
snapshot. Its groups 4 and 5 (challenge history recording, DNS query observation) are the parts
this change overtakes; a note to that effect has been added to those two groups in its `tasks.md`.

### Why this change is not called `add-…`

Every existing change here is named `add-…`. This one is not, because it is not purely additive:
two console screens were removed and the console was re-implemented on a different component
library. `rebuild-console-and-add-interaction-history` says both halves. Naming it
`add-console-paging-and-interaction-history` would have hidden the removal, which is the part a
reader most needs to notice.

### The edits already made to `add-acme-alias-service`

Before this change was written, `add-acme-alias-service/specs/admin-console/spec.md` had been
edited in place: three requirements were added (paged lists, the header block, modal dialogs) and
the two session/settings requirements were rewritten to say the console does not present them.

Those edits are **coherent in content but in the wrong change**, and one is incoherent with its own
design document: `add-acme-alias-service` D11 still lists "Active sessions" and "Settings" among
the console's screens, so that change now specified the absence of screens its design specifies the
presence of. A change describes one decision; a later reversal of that decision belongs to the
change that reversed it, or the record of *why the console once had a sessions screen* is lost.

So the edits have been moved: `add-acme-alias-service` is restored to describing the console it
designed, and this change carries the removals as `REMOVED` requirements and the replacements as
`ADDED` ones. One caveat, stated plainly: `openspec/` is not tracked in git, so the pre-edit text of
the two console-screen requirements was not recoverable and has been reconstructed from D11's screen
list. The reconstruction is faithful in substance; it is not word-for-word what was there.

## Impact

**Code already written.** `backend/internal/httpapi/admin_page.go`,
`backend/internal/httpapi/admin_interactions.go`, `backend/internal/store/interaction.go`,
`backend/internal/queryobs/`, migration `000002`, and the Angular console under `frontend/src/app`.

**New dependencies.** `ng-zorro-antd` 19.3, `@ant-design/icons-angular` and
`@fontsource/roboto` on the frontend. None on the backend. One dead dependency arrived with them,
`material-icons`, declared and referenced nowhere; it has since been removed from
`frontend/package.json`.

**Data growth.** One `challenge_interactions` row per authorization. A few hundred domains renewing
every sixty days is single-digit rows per day. No pruning is specified, and none is needed for
years — but it is also not specified, so it is listed as an open question rather than assumed.

**Performance.** The DNS query path gains one map increment under a mutex and no database work. The
`/present` path gains one `SELECT` and one `INSERT`, both outside the critical transaction. The list
endpoints do strictly less work than before.

**Operational.** The `unqueried` outcome is the first signal this service has ever had that a
customer's onboarding did not actually finish. It is worth watching from the day the table has data
in it, and it cannot be backfilled: every renewal before this deployment left no trace.

**Not covered by tests.** The schema guards have since been fixed — `schema.sql` was regenerated
and `ChallengeInteraction` added to `model.Models()`, so both the golden and the drift test now
cover the new table — and `internal/queryobs` has a full test file including a `-race` concurrency
test, joined by `TestAnsweredQueryIsObserved` in the DNS handler. What still has no test at all is
the datastore layer of the history and the endpoint that reads it: `internal/store/interaction.go`
has no test file, and `admin_interactions.go` is exercised only by the blanket route-role table.
That is the largest outstanding item in this change, and it is where the overlap-attribution rule —
the one that looks like a bug and is the reason this history reports anything at all — lives
unverified.
