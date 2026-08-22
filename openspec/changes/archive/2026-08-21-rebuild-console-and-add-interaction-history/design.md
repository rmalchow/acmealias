## Context

This change sits on `add-acme-alias-service`, which is essentially complete. To read what follows
it is enough to know four of its objects:

- a **project** is a customer — `demo`, "Demo Project";
- a **load balancer** is a set of that customer's redundant proxy nodes — `demo/prod`, holding
  two nodes `a` and `b` at `203.0.113.10` and `203.0.113.11`;
- a **domain** is a name that load balancer terminates TLS for — `demo.example.com` and
  `*.demo.example.com`;
- an **alias** is one label in the zone this service is authoritative for, one per distinct
  challenge name. Both domains above derive the same challenge name
  `_acme-challenge.demo.example.com`, so they share one alias, `demo-example-com`. The customer
  publishes a single CNAME from their `_acme-challenge` name to ours; we answer the TXT.

And two of its guarantees, because most of the decisions below are constrained by them:

- **D5**: a value published by `POST /present` is answerable by the time that call returns.
- **D6**: the DNS server answers from an in-memory view and keeps answering while PostgreSQL is
  unavailable.

Neither may be weakened to collect a statistic or to render a screen.

The work recorded here was implemented ad hoc over a short period, in the order the problems
appeared. This document reconstructs the decisions, including the ones that were made twice.

## Goals / Non-Goals

**Goals:**

- List screens whose filter searches the whole collection and whose count means something.
- A console layout that does not move the control the operator is reaching for.
- A durable record of every publication, rich enough to distinguish a renewal that worked from one
  where nobody came looking, that survives deletion of everything it describes.
- Zero datastore work on the DNS query path, unchanged from D6.
- A browser bundle that loads nothing from the internet at runtime.

**Non-Goals:**

- Aggregation, charts, per-project totals over time, "issuance attempts". Those stay in
  `add-audit-and-usage-reporting` and are not started.
- Recording DNS query source addresses. The count answers the diagnostic; the addresses would turn
  a counter into a store of third-party network data.
- Storing the challenge value. A digest is enough for everything the history needs.
- Retention policy. Nothing prunes `challenge_interactions`; at this volume nothing needs to for
  years, but see Open Questions.
- Per-project scoping of roles. Roles remain global and fixed, as D10 of the base change has them.

## Decisions

### D1. Paging is `limit`/`offset` with the total in a response header, and the body stays an array

The list endpoints answer with the same plain JSON array they always did, and put the count of the
full filtered result set — after every filter, before `limit` and `offset` — in `X-Total-Count`.

```
GET /api/v1/projects?q=demo&limit=25&offset=25

HTTP/1.1 200 OK
X-Total-Count: 63
[ {"id":7,"slug":"demo","name":"Demo Project", … } ]
```

**Rejected: an envelope.** `{"items":[…],"total":63}` is the textbook answer and it was the wrong
one here. `adminctl`, the acceptance harness and every `curl … | jq '.[]'` in an operator's runbook
already parse an array. Every one of those callers except the console is indifferent to paging, and
an envelope would break all of them to serve one screen. The header is invisible to a client that
does not look for it. Since the console is served from the same origin as the API there is no CORS
exposure list to add.

**Rejected: cursor paging.** A cursor is the right answer for an infinite feed consumed forwards.
These are inventory tables an operator pages through by hand with a page-number control, ordered by
a stable key (`projects` by slug, `challenges` and `audit` by time descending). A page-number
control needs an offset. The known cost is that a feed growing at the head — the audit trail under
load — can shift a row across a page boundary between two requests; that is the right price for a
screen whose entire job is "show me page 3".

**An over-large `limit` is refused, not clamped.** `limit=10000` against a maximum of 500 returns
`400` naming the bound rather than silently returning 500 rows. A client that asked for ten thousand
and received five hundred without being told has no way to know its list is short — it would
conclude the collection has 500 members. Defaults and maxima: 50/500 for projects, challenges and
interactions; 50/1000 for the audit trail, whose rows are small and whose readers legitimately want
big pages.

**Free text is one parameter, matched across the fields an operator might mean.** `q` on `/projects`
matches slug **or** display name, case-insensitively, because an operator remembers "the shop one"
without remembering which field the word lives in; two parameters would make them guess. `q` on
`/challenges` matches alias label, challenge FQDN or publishing key id — the three things somebody
has in hand while chasing one renewal. `q` on `/interactions` adds the client IP and the recorded
domain names.

The pattern is escaped before it goes into `ILIKE`. This is not hypothetical: slugs and alias labels
routinely contain `_`, which `LIKE` reads as "any single character", so an unescaped filter for
`shop_eu` would also match `shopaeu` — and `%` would match everything. `store.likeEscape` escapes
`\`, `%` and `_` and the queries carry an explicit `ESCAPE '\'`.

**Counting and selecting must not share one query object.** GORM builds onto the statement it is
handed, so counting first and then selecting off the same object leaks the count's clauses into the
select. That produced a real error here — *"for SELECT DISTINCT, ORDER BY expressions must appear in
select list"* — before the filter was factored into a `base()` closure called twice.

### D2. A bare `YYYY-MM-DD` upper bound means the end of that day

`from` and `to` on the audit trail accept either a full RFC 3339 timestamp or a bare date, because a
native `<input type="date">` submits a bare date and the console's range picker is built on one.

A bare `from` is taken as midnight, which is already the start of the day it names. A bare `to` is
pushed to the last instant of that day. The asymmetry is the point: an operator filtering
`from=2026-08-19&to=2026-08-20` means both days inclusive. Parsed symmetrically, `to=2026-08-20`
would be midnight and every entry from the 20th — including the change they are looking for, made
twenty minutes ago — would be excluded. The filter would appear to work and would hide the answer.

The console sends **local** dates, formatted by hand rather than with `toISOString()`. That call
converts to UTC and can name the previous day for anyone east of Greenwich after their evening,
which would drop a day off the range for reasons no operator could possibly diagnose.

### D3. `GET /projects/:id` exists because a detail screen addressed by id must fetch by id

The project detail screen used to fetch `/projects` and search the array for the id in its URL. That
is correct only while the list is complete, and D1 made it incomplete. `/projects/9` for a project
on page 3 then rendered an empty screen with no error at all: the request succeeded, the search
found nothing, the component had nothing to show and no failure to report.

`GET /api/v1/projects/:id` returns one project with its load balancers, nodes and domains. It is a four-line
handler and it is in the spec because the lesson generalises — any screen reachable by a deep link
needs a by-id read, and the failure when it does not have one is silent.

### D4. Node addresses are editable; the node label is not

`PATCH /api/v1/nodes/:id` accepts `ipv4` and `ipv6` and nothing else.

Renumbering happens — a customer moves a load balancer between racks or providers and its addresses
change. Before this, the only way to record that was to delete the node and add it back, which loses
its identity and, more importantly, cannot be done as one act.

The **label is identity and stays fixed**. It names the node in the DNS plan we generate, and that
plan has already been handed to the customer, who has already published records derived from it.
Renaming a node would silently invalidate a document somebody else is holding. Changing its address
does not: the plan changes, the customer republishes, and the edit dialog says so on the form —
*"Records already published by the customer keep pointing at the old address until they republish."*

**Clearing both addresses is refused**, server-side in `inventory.parseAddrs` and again in the
dialog. A node with no address contributes no A or AAAA record, so it is not a node — it is a row
that makes the DNS plan quietly incomplete. Deleting it is the honest way to express "this node is
gone", and deletion is available.

Each address is checked against the family of the column it lands in: an IPv6 literal accepted into
`ipv4` would emit an `A` record with a v6 value, which is the kind of malformed zone data that fails
at the customer's DNS provider hours later.

### D5. `challenge_interactions`: one row per publication, denormalized, no foreign keys

```sql
create table challenge_interactions (
  id             bigserial primary key,
  load_balancer_id     bigint,            -- plain bigint, not a reference
  alias_id       bigint,            -- plain bigint, used only to match live rows
  alias_label    text not null,     -- 'demo-example-com'
  challenge_fqdn text not null,     -- '_acme-challenge.demo.example.com'
  domain_names   text[] not null default '{}',   -- as served AT PUBLICATION
  key_id         text,              -- 'k_ec95dc38'
  client_ip      inet,              -- '172.28.0.30'
  fqdn_form      text not null,     -- 'alias' | 'challenge'
  value_digest   text not null,     -- sha256 of the value, never the value
  published_at   timestamptz not null default now(),
  withdrawn_at   timestamptz,
  first_queried_at timestamptz,
  last_queried_at  timestamptz,
  query_count      bigint not null default 0
);
```

**One row per publication, not per HTTP request.** `/present` opens it, `/cleanup` closes it. That
is what makes "published 14:02:11, queried twice, withdrawn 14:02:31" one line on a screen rather
than three rows an operator correlates by eye.

**No foreign keys, and the names copied in as text.** `domain_names` holds
`{demo.example.com, *.demo.example.com}` as they were at publication. If that customer is offboarded
next month the history stays readable and still says which domains the key was publishing for. A row
that cascades away with the object it describes cannot answer the only question it exists for.
`load_balancer_id` is kept and indexed because the console reads this per load balancer — but as a plain `bigint`,
so deleting a load balancer leaves its history intact and readable by id.

**The value is never stored.** It is a secret shared with the CA for the duration of one validation
and it has no business outliving it. `value_digest` distinguishes two concurrent authorizations at
one name, which is the only thing the value was needed for on this path.

**`fqdn_form` records which name the client sent.** Both spellings are accepted: a client may post
the customer's own `_acme-challenge.demo.example.com` (`challenge`) or our alias
`demo-example-com.acme.example.net` (`alias`), depending on whether its ACME library follows the
CNAME before calling the provider. Recording it says which convention this load balancer's client follows and
nothing stronger — it is a diagnostic, not a policy input.

**Two indexes.** `(load_balancer_id, published_at desc)` is the console's read. A partial index on
`(alias_id, value_digest) where withdrawn_at is null` is the lookup both `/cleanup` and the
observation flush make: the live rows of one alias, which is a handful of rows out of however many
the table has accumulated.

**Rejected: reusing the `challenges` table with a soft delete.** Adding `withdrawn_at` to the live
table and stopping the delete would have avoided a table. It also would have put history rows in the
hot path of the zone view, made the "which values are answerable" query a filtered one on a table
that grows forever, and coupled the retention of history to the correctness of DNS. The live table
should stay small and mean exactly one thing.

**Rejected: `challenge_events` as specified by `add-audit-and-usage-reporting` D3.** That table
carries project and load balancer *slugs* as well as ids, and a stored `outcome` column. The slugs were
dropped as unnecessary duplication at this scale — the load balancer id resolves to a slug while the load balancer
exists, and after it is deleted the alias label and domain names already identify what happened. The
stored outcome was rejected on its own merits; see D7.

### D6. Query counts accumulate in memory and are attributed by window overlap

The DNS handler increments a counter and does nothing else:

```go
// package queryobs — keyed by LABEL, not alias id
func (c *Counter) Observe(label string)   // one map increment under a mutex
```

Keyed by **label** deliberately: the label is what the handler already holds, and turning it into an
alias id is a datastore lookup — exactly what must not happen here. The flusher resolves labels to
ids once per batch instead of once per query. Labels that no longer resolve — an alias deleted
between the query being answered from the in-memory view and the flush running — are dropped.

Counted only on a query that was **answered with at least one value**. An `NXDOMAIN` or an empty
answer says nothing about a validation being checked, and a query arriving after cleanup is not
evidence of anything.

A background flusher drains the map on the zone-view refresh interval and applies it. The interval
is shared with the zone view rather than being its own setting: the counts describe live challenges,
which is exactly the window that refresh already defines, and the number of periodic tasks with an
independent tuning knob stays at zero.

#### The attribution rule, and the version that was wrong

The first implementation credited the rows that were **live at flush time**. It credited almost
nothing, and reported every successful validation as never queried.

The arithmetic is unforgiving. A DNS-01 validation is publish → query → cleanup inside a few
seconds. The flush runs tens of seconds apart. By the time it executes, the row it should credit was
withdrawn long ago, so `withdrawn_at is null` excludes it and the count lands on nothing. The
feature's one alarm — *nobody came to look* — then fires on precisely the renewals that worked,
which is worse than not having the feature.

The rule is **overlap with the window the counts were accumulated over**:

```sql
update challenge_interactions
   set query_count      = query_count + $n,
       last_queried_at  = $to,
       first_queried_at = coalesce(first_queried_at, $to)
 where alias_id = $alias
   and published_at <= $to
   and (withdrawn_at is null or withdrawn_at >= $from);
```

Both bounds come from the counter — `$from` is the moment of the previous drain, `$to` the moment of
this one — never from `time.Now()` inside the flush, so a slow or retried flush cannot shift the
window a query is attributed to.

**A query is credited to every row overlapping the window, not to one of them.** For a certificate
covering `demo.example.com` and `*.demo.example.com`, Traefik publishes two values at
`demo-example-com` and a single TXT query returns both. Both authorizations were genuinely observed
by that query. There is no information in a DNS query identifying which value the asker cared about;
inventing a rule to pick one would record something that did not happen. This looks like a bug to
anyone who assumes one query means one authorization, so it is commented at the call site as well as
specified here.

**A failed flush is merged back**, counts added to whatever accumulated meanwhile, and the window
start goes back with them — the retry must cover the period the queries actually happened in, not
the one that began when they failed to land. So a database outage delays observations instead of
dropping them.

**A crash loses up to one interval.** That is the price of D6's availability property and it is
written down so nobody later "fixes" it by writing on the query path. The one case where it bites is
a value queried once, moments before a crash, whose row is then reported `unqueried` — a false
alarm rather than a missed one, which is the right direction for this error to point.

**A known imprecision, since removed.** The first implementation set both `first_queried_at` and
`last_queried_at` to the window's end, because the counter tracked a count per label and no
timestamps — an error bounded by the flush interval, harmless until someone measured validation
latency from those fields. The counter now carries `{count, first, last}` per label and the flush
writes the real timestamps: `last_queried_at` from the observation, `first_queried_at` through a
`coalesce` so the earliest window to see the row wins and a later flush cannot overwrite it. The
window end survives only as the fallback for a batch that somehow carries no stamps.

### D7. The outcome is derived on read, not stored

Five values, computed from `withdrawn_at`, the `abandoned` flag and `query_count` at the moment the
row is rendered. `abandoned` is tested first, so a swept row is never reported as merely unqueried:

| `abandoned` | `withdrawn_at` | `query_count` | outcome | meaning |
| --- | --- | --- | --- | --- |
| true | set | any | `abandoned` | never cleaned up; the sweep closed it — **look at the client** |
| false | set | > 0 | `validated` | published, queried, withdrawn — a normal renewal |
| false | set | 0 | `unqueried` | **the customer's `_acme-challenge` CNAME is the first thing to check** |
| false | null | > 0 | `live` | still published, already queried — validation in flight |
| false | null | 0 | `pending` | still published, nothing has asked yet |

**Rejected: a stored `outcome` column assigned once at end of life**, which is what
`add-audit-and-usage-reporting` D3 specifies. Three reasons.

First, a stored outcome cannot describe an open row, and the console shows open rows — the live
challenge view exists precisely so an operator can watch a renewal happen. `live` and `pending` have
no stored equivalent; they would have to be `null`, and every reader would then reimplement this
table anyway to render something.

Second, an outcome written by `/cleanup` is written on the best-effort path (D8). A cleanup whose
history write fails would leave a row permanently misclassified, with nothing to correct it. Derived
values self-heal: the flush that lands late still moves the row from `unqueried` to `validated` the
next time anyone looks.

Third, the classification rule is young. Deriving it means changing one function; storing it means a
backfill over rows whose original inputs are still there but whose stored answer is now wrong.

**The `abandoned` gap, and how it was closed.** This document originally described a fifth outcome
that did not exist. A value published and never cleaned up — the publisher died mid-issuance — had
`withdrawn_at` null forever and read `pending` or `live` indefinitely, because the expiry sweep
deleted the live challenge row and left the interaction open. Two ways out were considered: derive
it (withdrawn is null *and* the publication is older than the challenge lifetime), or have the sweep
close the row.

The sweep won, and derivation was rejected here even though derivation wins everywhere else in this
section. The reason is that the two endings are *different facts*, not two renderings of one: a
publication that expired unclaimed and one the client withdrew are distinguishable only at the
moment the sweep runs, and a derivation reading the row afterwards cannot tell them apart — both
would show a stamped `withdrawn_at`. Deriving from an age threshold would also make the outcome
depend on a configured lifetime, so changing that setting would retroactively reclassify history.
So `store.AbandonStaleInteractions` stamps `withdrawn_at` together with a boolean `abandoned`
column, leaving `query_count` untouched, and `outcomeOf` reads the flag. Abandonment and
non-observation stay separately visible, which matters because they point at different culprits —
the client, or the path between the CA and our DNS.

### D8. History is best effort; the challenge API never fails because of it

`POST /present` writes the challenge value, then opens the interaction row. If the second fails, the
request still succeeds and a warning is logged. `POST /cleanup` is the same on the closing side.

The asymmetry is deliberate and is worth stating because it looks like missing error handling. The
certificate is the point of those endpoints; the record of it is not. A `500` from `/present` after
the value was already stored and made answerable would send the client into retrying a publication
that already succeeded, and — if the failure is a database problem — would do it during exactly the
window when the service is least able to absorb the traffic. The observable cost of losing a history
row is one line missing from a diagnostic screen.

`OpenInteraction` is **idempotent in the way `/present` is idempotent**. Publishing is an upsert on
`(alias_id, value)`, so a client retry must not open a second interaction: a live row for the same
`(alias_id, value_digest)` is reused and its `published_at` left alone, because the interaction began
when the CA first asked for it.

`CloseInteraction` stamps **every** live row for the value, not the first one — a value that somehow
opened twice should leave nothing live behind — and closing zero rows is not an error, because
clients retry cleanups and the row may have been lost or never written.

### D9. The console: ng-zorro, dark only, and nothing fetched at runtime

**ng-zorro-antd 19.3**, the Angular implementation of Ant Design, chosen for the three things this
console is almost entirely made of: data tables with server-driven paging, modal dialogs, and form
controls with error states. The alternative considered was hand-rolled components on the existing
CSS, which is what was there; it was rejected once the table requirements (page-size selector,
external pagination, per-column widths, loading state, empty-state templates) made it clear that
half a component library was going to be written badly instead of imported.

**The dark stylesheet, and only it.** `angular.json` lists
`ng-zorro-antd/ng-zorro-antd.dark.min.css` and nothing else; there is no toggle, no Less
compilation, no theme service. One theme is one set of colours to check contrast against and one
set of component overrides to maintain, and it is what the operators asked for. Density comes from
`nzSize="small"` on tables and controls rather than the compact theme, because compact plus dark
would require compiling Less from source and the prebuilt sheets do not combine.

**Everything is bundled; nothing is fetched.** The compiled bundle is embedded in the Go binary and
the binary runs on a host with no route to the internet. Roboto is the npm package
`@fontsource/roboto`, `@import`ed in `styles.css`, so the woff2 files are emitted by `ng build` and
served from the binary. `index.html` contains no external `<link>` at all.

Icons carry a sharp edge that earns a requirement of its own. ng-zorro's `<span nz-icon>` resolves an
unregistered icon by **fetching its SVG from Ant Design's CDN at runtime**. It does not fail loudly;
on the production host the request hangs and the icon silently never appears. So every icon is
registered up front through `provideNzIcons([...])` in `app.config.ts`, and adding an `nz-icon` to a
template means adding it to that list. (An unregistered icon has also thrown outright and taken a
screen down, which is how this was discovered.)

**One page container, fixed width.** `--ac-page-width: 78rem`, centred, with a gutter. Applied to
the shell bar's contents and to `<main>`. The bar itself spans the window so it reads as chrome; its
contents line up with the page beneath it.

### D10. Every screen begins with the same header block, whether or not it has one

A shared `app-page-header` renders breadcrumb → headline → subheadline, always all three, and the
breadcrumb row and the subheadline **reserve their height when empty**.

This is not decoration. Without the reservation, a screen with no breadcrumb starts its first table
a line and a half higher than the screen beside it. An operator moving between screens is reaching
for a control whose position they have learnt, and it moves under the cursor. Reserving the space
means the first row of content sits at the same `y` on every screen, so the pointer lands where it
was aimed. It costs 22 pixels on the two screens that have nothing to put there.

The subheadline is where a list reports what it is showing: *"12 projects matching "demo""*,
*"3 challenges answerable right now"*, *"1,204 entries matching the current filters"*. That is the
`X-Total-Count` value from D1, in a sentence, above the table it describes.

### D11. Creating is a modal that survives its own failure; destroying is a modal that precedes it

**Creation.** Project, load balancer, node, domain and API key are all created in a modal dialog, and all
of them POST from *inside* the dialog. On failure the dialog stays open, keeps every value the
operator typed, and renders the service's own message.

The failure that actually happens is a name collision — an operator types `demo` for a slug that
exists — and the operator's very next act is to edit exactly that field. A dialog that closed on
submit and surfaced the error as a toast would throw away the form and make them retype it, having
told them the one thing they could have guessed.

The server's message reaches the user **verbatim**, through a single helper that reads the JSON
body's `message`, then `error`, then a plain-string body, then falls back to the status line. The
server is the only party that knows which rule was broken or which object conflicted; paraphrasing
it in the client can only lose information.

One dialog deviates on purpose. The domain editor does **not** auto-close on success when the
service returned warnings — a domain can be accepted while still being worth commenting on — so it
flips to a result state with a single Close button, and the warning stays on screen until the
operator dismisses it. A warning-free `201` closes immediately.

**Destruction.** Delete and revoke open a confirmation dialog that names the object —
*Delete domain "demo.example.com"? Renewals for it stop being answerable as soon as this is done.* —
before anything is sent.

**Rejected: `window.confirm`.** It is suppressible per origin: a browser that has been told "don't
show these again" returns `true` without displaying anything, so a user who ticked that box once
deletes a load balancer on the first click. A modal cannot be suppressed.

**Row actions are icons.** A trashcan with a tooltip and a matching `aria-label`, so a table of
twenty rows is not twenty repetitions of the word "Delete" while still being announced to a screen
reader and discoverable on hover. The API-key panel is the exception and keeps text buttons for
"Rotate" and "Revoke" — two verbs with no conventional icon between them, where a wrong guess is
expensive. Rotate is not confirmed, because it mints a successor without revoking the predecessor
and is therefore not destructive.

### D12. The sessions and settings screens were removed; the endpoints were not

`GET`/`DELETE /api/v1/sessions` and `GET /api/v1/config` still exist and are still role-gated
exactly as before — sessions behind `ADMIN`, config behind any signed-in user. What was removed is
navigation, not authority.

**Sessions** is a support action taken on the rare day somebody leaves, by a person already at a
terminal executing the two-step procedure D10 of the base change prescribes: remove them from the
Keycloak group *and* revoke their sessions, because roles are resolved at sign-in and a session
already held is unaffected by a group change. A screen for it takes a permanent slot in the
navigation of every user who will never open it.

**Settings** is read while diagnosing — "which group does the running process map to
`CONFIG_ADMIN`?" — and the same answer is in the startup log and one `curl` away. It is not
something anyone browses.

The base change's D11 lists both as console screens, which is why this reversal is recorded here
rather than by editing that design: it was a reasonable decision then and the record of why the
console once had those screens is worth keeping.

**Loose end, closed.** An earlier draft of the requirement said both capabilities are "offered
through the CLI". They are not, and adding them was never the point: `adminctl` exists for bootstrap
and emergencies — before anyone has console access, and when the console is down — and neither
reading the effective configuration nor revoking a session is reachable only in that state. So the
requirement stopped claiming it. Both now say the capability is *served by the management API*,
which is what `admin.go:167-171` routes and what `admin_test.go` covers. `adminctl` keeps its six
subcommands and gains nothing.

### D13. The interactions panel, and the one filter that earns its place

The panel lives on the load balancer screen and shows, newest first: when a value was published and how
long it stood; which domains the alias served at the time and under which alias label; the source
address and which FQDN form that client sent; the publishing key; the query count with the first
query time; and the derived outcome as a tag.

The filter that matters is a single switch, **"Never queried only"**. An interaction nobody ever
queried is the signature of a customer CNAME that was never published, and being able to list
exactly those turns "their renewals keep failing" into a list of domains to go and fix. It has its
own empty state — *"Every publication was queried. Nothing here points at a missing CNAME."* — which
is a genuinely useful answer rather than an absence.

The panel exists because a renewal that never happened leaves no trace anywhere else in this
console: nothing is live, so the live-challenge view is empty, and nobody changed any inventory, so
the audit trail says nothing.

### D14. The API-key dialog pins restrictions to the load balancer's own node addresses

An API key may carry CIDR restrictions; a key with none is usable from anywhere. Typing
`203.0.113.10/32, 203.0.113.11/32` by hand is the sort of task people skip, so the creation dialog
offers a switch — *"Restrict to this load balancer's node addresses"* — that fills them in from the load balancer's
nodes, `/32` for each IPv4 and `/128` for each IPv6, with the addresses shown underneath so the
operator sees what is being pinned.

Toggling it **off** removes only the entries equal to node addresses, deliberately leaving a CIDR
the operator typed by hand even if it happens to match. The switch owns what it added, nothing else.

When the load balancer has no node addresses the switch is replaced by a sentence saying so, rather than
being shown disabled with no explanation.

This is presentation over an existing API — the `networks` field on key creation is unchanged — so
it is specified in `admin-console` and not in `api-credentials`.

### D15. `ListLoadBalancersForProject` preloads domains

It preloaded `Nodes` and not `Domains`. The project screen renders a domain count per load balancer from that
list, so it showed **0 for every load balancer**, including `demo/prod` with its two domains. One added
`Preload("Domains")`.

It is in the spec because the failure mode is a plausible wrong number rather than an error, on the
screen an operator uses to confirm that an onboarding worked. A count that reads zero when the
answer is two is worse than a count that fails to render.

## Risks / Trade-offs

- **The landed backend work has no tests.** No store test for open/close idempotence or overlap
  attribution, no `-race` test for the counter, no handler test for the interactions endpoint. The
  overlap rule in D6 is the exact kind of logic that regresses invisibly — it was wrong once
  already, and nothing would catch it going wrong again.
- **The golden schema file is stale.** `backend/internal/store/schema.sql` does not contain
  `challenge_interactions`, so `TestIntegrationSchemaGolden` fails against migration `000002` until
  it is regenerated. `model.Models()` also does not list `ChallengeInteraction`, so the model-drift
  test does not cover the new table at all — the one guard against the struct and the migration
  disagreeing is not watching this table.
- **`abandoned` is missing** (D7). A publisher that dies mid-issuance leaves a row reading `pending`
  forever. It is under-reporting a failure, not over-reporting one, which is the safer direction,
  but it is a gap.
- **Unmatched observations vanish silently.** A query answered for an alias with no overlapping row
  is dropped and nothing counts it. `add-audit-and-usage-reporting` expected an aggregate metric
  there; without it, "the flush is attributing nothing" and "nothing is being queried" look
  identical from the outside — which is how the D6 bug survived as long as it did.
- **Offset paging can skip or repeat a row** on the audit trail while entries are being written.
  Accepted per D1.
- **`material-icons` is a declared dependency that nothing references.** Harmless today, misleading
  to the next person reading `package.json`.
- **No retention policy** on `challenge_interactions`, and it holds client IP addresses. At current
  volume the table is trivially small for years; that is a reason not to act, not a reason not to
  decide.
- **One theme means no light theme**, ever, without redoing the colour work. Accepted.

## Migration Plan

Migration `000002_challenge_interactions` creates one table and adds nothing to any existing one, so
it applies to a running system without locking anything an operator would notice, and the down
migration is a `drop table`.

The table starts empty and **cannot be backfilled**: every renewal before deployment left no record.
So the panel is empty for up to one full renewal cycle — sixty days for Let's Encrypt defaults —
before "never queried" is meaningful at all. Do not alert on it until a baseline exists; before
then, "never queried" is indistinguishable from "not deployed yet when it renewed".

Everything else in this change is already deployed with the running binary.

## Open Questions

Four of the five below were open when this document was first written and have since been answered
by the code. They are kept with their answers rather than deleted, because the answer is only
legible next to the question.

- ~~**Does `abandoned` belong to the sweep or to the derivation?**~~ **Both, split the way D7
  leaned.** The sweep stamps state — `withdrawn_at` and a boolean `abandoned` column — because only
  the sweep knows the difference between "expired on our side" and "the client withdrew it"; the
  derivation reads it. `store.AbandonStaleInteractions` does the stamping, `outcomeOf` tests
  `abandoned` before it tests the query count, so a swept row that was never queried reads
  `abandoned` and not `unqueried`. Those are different diagnoses — one points at the client, the
  other at the CA's resolver path — and collapsing them would have lost the more actionable one.
- ~~**Should an interaction row carry the project id?**~~ **Yes, and it does.** It is stamped at
  publication from the load balancer's project, not resolved on read, for the reason the question
  named: `load_balancer_id` resolves to a project only while the load balancer exists. Retrofitting
  it later would have left every row whose load balancer had already been deleted permanently
  unattributable — the exact rows a history exists to keep.
- **Retention and the client IP.** Still open. How long is this kept, and does storing a customer
  load balancer's address for years need a stated policy? Nothing is decided, and nothing prunes the
  table — the sweep closes stale interactions but deletes no rows.
- ~~**Does `adminctl` gain `session` and `config` subcommands**, or does the requirement stop saying
  it has them (D12)?~~ **The requirement stopped saying it.** Both now name the management API. See
  D12.
- ~~**Should the counter track first/last per label**, removing the imprecision noted in D6?~~
  **Yes — it turned out to cost nothing.** The counter already held a struct per label, so carrying
  two timestamps beside the count added no allocation and no lock time to the query path, which was
  the only reason to have hesitated. `ApplyQueryObservations` writes `last_queried_at` from the
  observation and `coalesce`s `first_queried_at`, falling back to the window end only for a batch
  that carries no stamps. D6's *Known imprecision* no longer applies.
