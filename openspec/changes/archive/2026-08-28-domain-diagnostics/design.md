# Design

Decisions are numbered `G1`, `G2`, … and referred to by number from `tasks.md`
and from the spec deltas. Where a decision replaces one made in `health-checks`,
that change's number is named — `health-checks` is archived by this change, so
its numbers are the only record of what was decided before.

## Context

### Where the screen is now

`frontend/projects/console/src/app/features/projects/project-detail.component.ts`
renders a project as five stacked sections: Nodes, Domains, DNS plan, API keys,
Interactions, plus a usage panel. Two of those answer the same question.

The **Domains** table has six columns, and two of them are verdicts:
*Challenges* carries the activation chip built from the domain's own
`last_checked_at` / `last_success_at` stamps, and *Validation* carries one chip
per record type built from the live check. Both come from `domain-status.ts`,
which is thorough — five delegation states, four CAA states, each with a
sentence — and which produces, per row, up to three chips and a "What does this
mean?" link. A project with eight domains renders twenty-four chips.

The **DNS plan** table below it renders `internal/inventory.Plan` — one row per
required record — with a *Status* and a *Found* column fed from the same
verification response the chips above are fed from. For a project with two
dual-stack nodes and eight domains that is 32 address rows, up to 8 `CNAME` rows
and 2 CAA rows: forty-two rows restating what the eight rows above already say,
with the address rows grading the customer on addresses the customer typed into
the Nodes panel.

The panel also holds three controls: `Copy records`, `Zone snippet`, and `Run
verification` — the last of which is the one control that acts on the table
*above* it, which is the complaint `domain-setup-feedback` opened with.

### What is not on the screen at all

Nothing tells the customer whether a certificate was ever installed. The service
answers challenges and `challenge-history` records that the authority was
satisfied; after that everything happens on the customer's machine and we are
blind. `health-checks` was designed to close that and is unimplemented — there is
no `internal/certobserve`, no `certificate_observations` table, and its four
console surfaces do not exist.

### Constraints that shaped everything below

1. **Nothing acts on an observation.** It is displayed and retained. No answer
   changes because of it, nothing is gated on it, nothing is suspended for it.
   This is `health-checks` H1 and it survives unchanged — it is what makes the
   worker safe to disable and the table safe to leave in place.
2. **The service never holds a certificate or a private key.** It connects as a
   client and reads what is presented.
3. **We do not connect to a customer's machine without being told to.** Every
   handshake in this change is behind a switch somebody turned on.
4. **Observation is not metered at all.** Only challenges cost. Nothing about
   turning the switch on changes what an account is charged, so how often we
   look is bounded by a configured floor rather than by a price (G17).

## Goals / Non-Goals

**Goals:**

- One place per domain where everything true about it is visible at a glance.
- A per-record explanation carrying that domain's own values, close enough to the
  thing it explains that nobody has to look for it.
- Know whether the certificate actually being served for a domain is valid, on
  every node address, on both families.
- Keep enough history that "since when" is answerable.
- A hard ceiling on how often we connect to somebody else's machine.

**Non-Goals:**

- Charging for any of it. Observation is unmetered and only challenges cost
  (G17).
- Reachability or latency monitoring (`health-checks` H1's reasoning, retained
  verbatim in G14).
- Alerting, notification, on-call integration.
- Steering, failover, or any effect on what is served. There is nothing to steer:
  the service does not host customers' zones.
- Application-level assertions — no HTTP status checks, no body matching.
- Obtaining, renewing, installing or holding certificates or keys.
- Grading a customer's address records against their configured nodes. The plan's
  address rows go and nothing replaces them (G2).

## Decisions

### G1. The domain row is the only place a domain is described

Everything the console knows about a domain appears on that domain's row or in a
dialog opened from it. There is no second table, no summary panel, no report
listing domains by problem.

This is the decision the rest of the change follows from, and it is worth stating
as a rule rather than as a layout, because the pull the other way is constant:
"domains covered by no certificate" is a natural report, "certificates expiring
soon" is a natural report, and `health-checks` H4 designed both as reports. Under
this rule neither is a report. The first is the TLS dot showing red on the rows
it concerns; the second is the same dot showing amber. A report would state the
same fact in a second place, and the two places would be reconciled by the
customer.

The cost is real and is accepted: with fifty domains, finding the three with a
problem means scanning fifty rows. The mitigation is sorting and filtering the
one table rather than building a second one — out of scope here, and cheap when
it is wanted precisely because there is only one table to sort.

**Rejected: keeping a slimmed DNS plan panel with only the `CNAME` and CAA rows.**
It is the smallest change and it keeps `Copy records` working. Rejected because
the rows it keeps are exactly the rows the dots already report, so it preserves
the disagreement described above — two panels, two fetches, one of them stale —
while removing only the rows nobody was confused by.

### G2. The plan goes entirely — the panel, the endpoints, and the shape

The console section is deleted: the table, `Copy records`, `Zone snippet`, `Run
verification`. So are `GET /accounts/:a/projects/:p/plan` and `…/plan/zone`, the
`adminctl` subcommand that renders them, their `openapi.yaml` entries, and
`inventory.RenderZoneSnippet`.

**Everything a caller needs becomes a property of a domain.** That is the whole
rule, and it is the API-side statement of G1. A domain read carries its challenge
name, the `CNAME` target that challenge name must point at, and — where an issuer
is configured — the CAA entries expected at its registrable name. Those three
facts are what the dialogs print, what the verification compares against, and what
anybody would previously have assembled out of the plan.

Keeping the endpoints alive with no screen behind them was the first instinct and
is wrong for two reasons:

- **It preserves exactly what this change decided not to hand anybody.** The plan
  is dominated by address records — one per domain per node address per family —
  and their only possible finding is that a customer's DNS disagrees with the node
  addresses that same customer typed in. A published contract that keeps offering
  them keeps implying they are something to act on.
- **It leaves two shapes answering one question.** "What must I publish for
  `shop.example.com`" would be answerable from a domain and from a set of records,
  and the two would drift — which is the same disagreement between two panels that
  G1 exists to end, moved down a layer where nobody can see it.

The address records are **not relocated**. If grading a customer's address records
against their nodes turns out to be worth doing, it belongs on the node's row, not
the domain's, and it is not in this change.

The **verification response loses its per-record half**. It currently answers
twice — a `results` array keyed by record name and type, and a `domains` array
keyed by domain id — because `domain-setup-feedback` added the second while the
plan table still consumed the first. Only the second has a reader now.

**Rejected: keeping `Zone snippet` alone.** It is the one artefact a customer
cannot reassemble from the dialogs, so keeping it costs one endpoint and one
button. Rejected because what it renders is mostly the address records above, so
what survives the deletion is a download that is mostly noise — and because a zone
snippet containing two `CNAME` lines is a file nobody needs a download for.

**Rejected: keeping the plan as an internal shape and only hiding the routes.**
Tempting, because `inventory.Plan` is what the verification currently iterates.
Rejected because the per-domain expectation is strictly smaller — challenge name,
target, CAA entries — and deriving it by building a whole plan and then joining
back onto domains is the indirection that made a per-domain answer hard to give in
the first place.

### G3. A dot is a colour, a name, a tooltip and a dialog

Each record type this domain has gets one small filled circle, the record type
name beside it, a tooltip stating what is true in one sentence, and a click that
opens the explanation for that record type alone.

```
shop.example.com    ● CNAME   ● CAA   ● TLS      active · last looked 14:02
```

Four things this shape decides:

- **The label is the record type, never the verdict.** The colour carries the
  verdict and the tooltip states it. A label reading "configured incorrectly"
  makes the row's width depend on how broken it is, and a table whose columns
  move as state changes is a table nobody can scan.
- **A record type this domain does not have has no dot** — not a grey one. CAA
  with no configured issuer, TLS with the switch off: nothing about them is true
  or false here, and a grey dot would claim we meant to look.
- **Grey is "we have not looked" and never "this is fine".** It covers not yet
  checked, lookup failed, and nothing to ask about. The tooltip distinguishes
  them; the colour deliberately does not, because every one of them is the same
  instruction to the customer, which is *do nothing on account of this dot*.
- **Every dot is clickable, including a green one.** "What is published now, and
  what should be" is a question with a useful answer in every state, and a
  control that appears only when something is wrong teaches people to read its
  presence as an alarm.

The existing per-domain "What does this mean?" link and the single whole-domain
dialog behind it are replaced by this. One dialog covering all record types has
to lead with a section the reader did not click on.

### G4. Yellow for an absent delegation, red for a wrong one

`CNAME` colours: green `correct`, **red** `incorrect`, **yellow** `missing`, grey
`undetermined`, `not-checked`, and no-alias.

This inverts what `domain-setup-feedback` shipped, where `missing` and `incorrect`
were both red on the explicit grounds that *"both mean no certificate"*. That is
still true. The distinction being drawn now is not severity but **who is in the
middle of what**:

- `missing` is overwhelmingly a customer who added the domain a few minutes ago
  and has not published the record yet. They are mid-task. Nothing is wrong; a
  step has not happened. Red tells somebody who is doing the right thing that
  they have done something wrong, and it is the state every new domain passes
  through.
- `incorrect` is a record that exists, looks finished, and will never work. It
  does not self-heal, nobody is coming back to it, and the commonest cause — a
  domain deleted and re-added, issuing a new alias while the old delegation stays
  published and well-formed — is invisible without being told. It is the state
  that costs a certificate six weeks later.

**The risk is stated rather than mitigated.** A domain that has sat at yellow for
three weeks is genuinely broken and yellow understates it. The check that catches
that is the activation state already on the row — "waiting for delegation" with
"Not looked at yet", which does not soften with time — and, if that proves
insufficient, ageing yellow into red after some interval is a change to one
function. It is not made now because the interval would be invented rather than
measured.

### G5. CAA is two colours over four states, and appears only when we have an issuer to name

`internal/resolvercheck` models four CAA outcomes. The dot shows two colours:

| Outcome | Dot | Why |
| --- | --- | --- |
| `correct` — a set exists and authorises our issuer | green | issuance permitted |
| `not-published` — no CAA record at all | **green** | issuance permitted |
| `blocking` — a set exists and does not authorise us | red | issuance refused outright |
| `undetermined` — the lookup failed | grey | we could not look |

The two green states are not the same fact and the tooltip does not pretend they
are — "authorises us" and "no restriction" — but they are the same **instruction**,
which is *nothing to do*, and the dot's job is to carry the instruction.
`domain-setup-feedback` D3 reached the same conclusion and could only express it
with a colour split (blue for `not-published`, green for `correct`), because a
chip carrying a word has to say something. A dot does not, and blue on a
three-colour scale means "unclear" to everybody who has not read the tooltip.

`blocking` is the one CAA state that matters. It stops issuance however perfect
the delegation is, and it is invisible from every other signal in the product.

**The dot is absent entirely when no certificate authority identifier is
configured** for the deployment, because the plan then emits no CAA rows and there
is no issuer for the dialog to tell anyone to authorise.

### G6. The TLS check is per domain, per node address, per family — and opt-in

For each domain with the switch on, one handshake per node address of its project,
both families separately, with that domain's name (or its wildcard test hostname,
G8) as the server name. Port 443, not configurable.

This **inverts `health-checks` H2**, which probed once per node address with no
server name and read the covered names out of whatever certificate came back,
rejecting the per-domain matrix on cost. The inversion has two legs:

**The cost argument dissolves under an opt-in.** H2's arithmetic — *"four nodes and
fifty domains is two hundred handshakes to discover what is almost always four
certificates"* — assumed every domain is observed. Under constraint 3 nothing is
observed until somebody switches it on, and the domains somebody switches on are
the handful they care about. Four nodes × two families × three enabled domains is
twenty-four handshakes per cycle.

**The per-node probe answers the wrong question on a shared address.** Any server
terminating TLS for more than one name selects its certificate by SNI: nginx
`server_name`, an ingress controller, a reverse proxy, a CDN origin. Connecting
without a server name gets the default virtual host, which for a customer running
twenty sites behind one address is a certificate for an unrelated name. H2
recorded this as *"a known limitation, accepted"* and proposed a later per-name
probe aimed at whatever the SAN comparison flagged. Under a per-domain opt-in the
domain **is** the server name, so the limitation never arises and the later probe
is never needed.

What is kept from H2 without change: **both families are probed separately**. A
node reached over IPv4 and over IPv6 is reached through different transit and
different firewalls, and a certificate renewed on one listener and not the other
is invisible to a dual-stack visitor — including the customer, checking in their
own browser, from their own desk. Only a client that deliberately probes each
family separately sees it.

**Rejected: probing the domain's own resolved addresses rather than the project's
node addresses.** It is what a browser does and it needs no inventory. Rejected
because it observes whatever the customer's DNS currently points at, which may be
a CDN, an old host, or a name that does not resolve — and the question this check
exists to answer is whether the certificate reached *the nodes configured here*.
Per-node is also what makes "deployed to `a` and not to `b`" visible at all.

### G7. Each record type has its own dialog, and prints a record to copy

Three dialogs replacing one:

- **`CNAME`** — what the state means, what is published now when it differs, and
  `_acme-challenge.example.com. IN CNAME k7f2q9.acmealias.com.` with a copy
  control. When the state is `incorrect`, the deleted-and-re-added explanation
  from the existing modal is kept verbatim; it is the one cause a customer will
  not guess.
- **CAA** — what the state means, what the set names now when it is `blocking`,
  and both `issue` and `issuewild` lines for every configured issuer, with the
  standing instruction to leave the existing entries alone because CAA is a list.
  A present `issuewild` governs wildcard issuance *instead of* `issue`, so
  offering only the `issue` line sends somebody to add a line that is already
  there.
- **TLS** — no record, because there is nothing in DNS to fix. It prints what was
  observed: one block per node address, the family, the certificate's not-after
  with its remaining time in words, its issuer, and whether the name asked for is
  among the certificate's names. On a failed handshake it prints the error, in
  our words rather than the transport's (G11).

Two rules the existing modal established and which carry over unchanged:

- **Never print what a failed lookup "found".** On `undetermined` that field holds
  our resolver's error text, addressed to an operator; showing it to a customer
  reads as a verdict on their zone when it is a verdict on our lookup.
- **The fix offered for a blocking CAA is a CAA record, never a delegation one.**

### G8. A wildcard needs a concrete test hostname, and it is a stored field

`*.example.com` cannot be a TLS server name. So enabling the switch on a wildcard
domain requires a hostname, validated at write time to be a name the wildcard
actually covers — exactly one label beneath it, matching the constraints
`service-inventory`'s wildcard requirement already imposes — and stored on the
domain as `tls_test_host`.

It is a stored field rather than a per-run prompt because the check is periodic:
something has to supply the server name at 03:00 with nobody watching.

Three consequences worth naming:

- **A wildcard with the switch off needs no hostname**, and turning the switch off
  does not clear one that was set. Somebody who turns it back on gets their
  hostname back rather than being asked again.
- **The apex and its wildcard are separate rows with separate switches.**
  `example.com` and `*.example.com` share one alias and one delegation — that is
  why both `CNAME` dots read the same state — but they are different names, are
  very often served by different certificates, and each carries its own switch.
- **Nothing derives the hostname automatically.** `www.example.com` is the obvious
  guess and it is a guess; a customer whose wildcard serves `a.example.com` and
  not `www` would get a red dot for a name they never deployed, which is the worst
  possible first impression of a check.

### G9. The expiry bands, and what colours a handshake that did not produce a valid certificate

From the latest observation for one address:

| Condition | Dot |
| --- | --- |
| never observed, or the switch was turned on and no cycle has run | grey |
| handshake failed — refused, timed out, unreadable | grey |
| certificate read, does not cover the name asked for | red |
| certificate read, expired | red |
| certificate read, less than 7 days remaining | red |
| certificate read, 7 to 28 days remaining | amber |
| certificate read, more than 28 days remaining | green |

The band edges are 7 and 28 days. Twenty-eight rather than thirty because the
number people mean when they say "a month out" is four weeks, and because a Let's
Encrypt certificate is renewed at 30 days by every client's default — so a
certificate with 29 days left has just been *told* to renew and has not yet, which
is exactly when amber starts being informative.

**A failed handshake is grey, not red.** This is the one place where the failure
of our own probe meets a colour scale, and colouring it red would claim the
customer's node is broken when we cannot distinguish that from our own transit,
their firewall dropping our source address, or a listener that simply is not on
443. Grey says *we could not look*, which is the only thing we know. The dialog
carries the error for anybody who wants it, and the observation is recorded (G11)
so a run of failures is visible in the history rather than as an absence.

**A certificate that does not cover the name is red**, and it is the most valuable
red in the change: it is the "issued and never deployed" failure, and the
"deployed to two nodes out of four" failure, and the "covers the names you had
last month" failure, all of which the challenge history reports as success.

### G10. The dot is the worst address; the dialog is every address

A domain has one TLS dot and up to eight observations behind it — four nodes, two
families. The dot takes the worst band across every address, in the order red >
amber > green > grey, with one exception: **grey never outranks a colour**. An
address we could not reach beside three we could is not enough to grey out a dot
that has real information in it; it is a line in the dialog.

The dialog lists every address, in node order, family within node, so "renewed on
IPv4 and not on IPv6" is one glance rather than a comparison the reader has to
construct.

### G11. A failed handshake is an observation, not a missing row

`health-checks` H3, retained without change and restated because it is easy to
undo by accident.

A refused connection, a timeout, or a response we cannot read is **written** as an
observation carrying its error. Writing nothing and letting the absence speak does
not work: "there is no certificate here" and "we could not look" are different
sentences with different audiences, and a gap in a table is indistinguishable from
a scheduler that did not run.

The error is stored as our own classification — refused, timed out, TLS handshake
rejected, certificate unreadable — with the underlying text kept beside it for the
log. What the dialog shows is the classification.

### G12. We observe without validating the chain, and evaluate the certificate ourselves

The handshake is completed with certificate verification **disabled at the TLS
layer**, and every judgement — expiry, name coverage, chain validity — is made by
us on the certificate that was presented.

This is not a shortcut. A verifying client aborts the handshake on an untrusted or
expired chain and returns an error, which would collapse the two most interesting
findings in this change — an expired certificate, and one from an authority the
system trust store does not carry — into the same "could not look" grey as a
firewall drop. Observing means reading what is there, including what a browser
would refuse.

Chain validity is therefore evaluated separately and recorded as a field. It does
not colour the dot in this change; the dot is about expiry and coverage. It is
recorded because "the certificate is valid and nobody trusts the issuer" is a real
deployment mistake and the observation is where it would be seen.

This is also why constraint 2 is worth restating here: we are a client reading
what is offered. We never obtain, install or hold a certificate or a key, and
nothing about this puts the service in anybody's trust chain.

### G13. The worker is the seventh, and it looks like the sixth

`internal/certobserve` runs the same shape as `internal/activation`: a ticker, a
pass that selects due rows in a bounded batch, bounded parallelism inside the
pass, an immediate first pass rather than one after the first tick, cancelled with
the process.

- **Cadence: six hours**, configurable **with a floor of one hour**. Nothing
  depends on an observation inside a renewal window, a certificate does not change
  between one hour and the next, and a renewal has about thirty days of slack.
  Four observations a day against a thirty-day window is ample and the number is
  not worth tuning. The floor is refused at startup rather than clamped silently,
  and since G17 makes observation unmetered it is the only remaining bound on how
  often this service connects to somebody else's machine — so it is validated
  rather than trusted to a comment.
- **Handshake timeout: five seconds**, configurable. A node that has started
  blackholing must cost one bounded pass, not an unbounded one.
- **Batch and parallelism are constants, not configuration**, for the same reason
  they are in `activation`: they bound the burst this process aims at somebody
  else's machine and how long one tick may run. They are not behaviour anybody
  tunes.
- **The whole check is globally disableable**, which is the operational form of
  constraint 1: nothing depends on an observation, so switching the worker off
  costs a stale dialog and nothing else.

**Queue order matters and is the same rule `activation` uses**: a domain whose
switch was just turned on is selected before a domain due for a routine
re-observation, so a batch too small for the backlog delays the settled work and
never the person watching the screen.

### G14. Nothing acts on an observation

`health-checks` H1, retained. No answer changes, no request is gated, nothing is
suspended, no domain is deactivated. An observation is written to history and
shown to a human.

This is stated as a decision rather than left as an absence because the absence is
what makes this design short. Anything downstream believing an observation would
need the observation to be trustworthy, and everything that makes a check result
trustworthy — several observers agreeing before a state changes, several
consecutive cycles before it changes back, a rule that does the safe thing when the
conclusion is not credible — is machinery whose entire justification is that
something believes it. Nothing believes this. One observer is enough, two runs
disagreeing are two facts about two moments, and a wholesale failure costs a grey
dot.

The one rule that survives from that machinery applies trivially here: **the check
must not be able to cause a worse problem than the thing it is watching.** A system
that only reads satisfies it by construction.

**Rejected, again and for the record: reporting reachability because the handshake
reveals it anyway.** It is nearly free. It is still rejected: an hours-apart sample
presented as a status invites a customer to rely on it, and every operator running
a node already has monitoring that samples far more often and can page somebody.
What the handshake's failure is good for is explaining a missing observation (G11),
which is a claim about our own visibility rather than about their availability.

### G15. History is recorded for everybody and shown to team accounts

Observations accumulate for every enabled domain regardless of account kind,
because the current state is derived from the latest one and the current state is
what colours the dot. The **history list** in the TLS dialog — every observation
for this domain and address, newest first — is rendered only for a team account.
A personal account sees the latest observation and a line saying history is part
of a team account.

Two things about how this is done:

- **The service enforces it, the console reflects it.** The observation read
  refuses a history range for a personal account; the dialog does not merely hide
  a section it received.
- **It is not dressed up as a structural limit.** `teamAccountGuard` keeps a
  personal account away from the member roster because an account of one *cannot
  have members* — the service refuses those routes with `409`, not `403`,
  precisely so a client can tell "this does not exist here" from "you may not".
  Nothing is meaningless about history on a personal account. It is withheld for
  commercial reasons, the refusal says so, and the dialog says so. Inventing a
  structural justification for a commercial gate is how a product ends up unable
  to explain its own behaviour.

**Rejected: recording history only for team accounts.** It halves the storage and
it is simpler to reason about. Rejected because upgrading an account would then
produce a feature with no data in it for weeks, and because the retention rule
(G16) already makes the volume negligible.

**The refusal is `418`.** Not `403`, not `409`, and not `402`. The reasoning is
that this refusal means something no existing status means, and every candidate
already carries a meaning a client would act on wrongly: `403` sends somebody to
look at permissions that are working perfectly, `402` is this service's "suspended
or unfunded" and routes the console into a funding flow that is the wrong flow,
and `409` is the *structural* refusal — "an account of one cannot have members" —
whose justification G15 exists to say this gate does not have. A status with no
semantics anyone else uses is precisely what lets it carry this one meaning
without being mistaken for any of those three.

**What that costs, stated rather than discovered.** Some proxies and client
libraries treat `418` as unknown, pass it through without a reason phrase, or
collapse it into a generic error. So **the number is not where the meaning
lives**: the sentinel and the message body carry it, the console keys on the
sentinel rather than on the status, and `openapi.yaml` documents the response
explicitly, because no generator will infer a `418` and every one of them will
omit it silently. A client that only sees the number still learns the correct
thing — that this is not about authority and not about payment — which is more
than any of the three alternatives would have told it.

### G16. Retention is stated in weeks, and downsampling keeps a day

Observations arrive at (enabled domains × node addresses × four a day). Full
resolution is kept for a window measured in weeks; beyond it, one observation per
domain per address per day is kept and the rest are dropped.

The value of the history is a trend measured in weeks — this domain has been
renewing on time since March; this one stopped in June — so the retention rule is
stated in the same unit, and downsampling preserves the shape of the trend rather
than truncating it. Deletion happens in the existing sweeper, not in a new worker.

### G17. Observation is not metered, and an hourly floor replaces the price

**Certificate observation is free. Only challenges cost.** No usage event, no rate
event, no counter, no ledger row — nothing about turning the switch on changes
what an account is charged, and the console does not imply that it might.

**This reverses an earlier position in this same document, and the reversal is
worth recording rather than quietly editing out.** `health-checks` H6 defined
`node.observed`, a count per account per period of configured nodes. Because the
opt-in became per domain (G6), the first version of this decision made the event
`domain.observed` — one row per account per period per domain with the switch on,
never one per handshake — and the argument for it was sound as far as it went: the
customer switched on *observe this domain*, they did not choose six hours, and they
did not choose that a dual-stack project of four nodes is eight handshakes rather
than one. Both of those are ours and we will change them, so halving the interval
for our own confidence must not double an invoice.

Not charging at all reaches the same end more simply. Every property that argument
was protecting — an invoice that does not move when we change the cadence, and a
domain that does not cost more to watch because somebody added an IPv6 address —
holds trivially when the price is zero. What the metered version added on top was a
rate event, a table to hold it, an idempotent periodic job to emit it, and a line
on an invoice that a customer would have to be told the meaning of. That is a
significant amount of machinery whose entire output is a number this decision now
sets to zero.

It also removes a bad incentive that was easy to miss. A metered switch is a switch
the product wants people to turn on, and this one exists to tell somebody their
certificate did not deploy. A diagnostic should not have a revenue reason to be
enabled.

**What the price was silently doing, and what now does it instead.** The
per-domain fee was the only thing on the customer's side bounding how much of
this we would ever do; an unmetered check has no such brake, and the honest
consequence is that the brake has to be explicit. So: **a domain is observed at
most once an hour.** The six-hour default is unchanged and remains the right
number for the reasons G13 gives, but the configured interval takes a **floor of
one hour, enforced in configuration validation rather than by a comment**, so a
deployment cannot dial it to five minutes. It is a refusal to start and not a
silent clamp, because a deployment that asked for five minutes and got sixty
should be told rather than left to discover it in a graph.

That floor is now the entire answer to "what stops this service hammering
somebody else's load balancer", which is why it is validated rather than trusted.
The bounded batch, the bounded parallelism and the per-attempt timeout (G13) all
still hold; they bound one pass, and the floor is what bounds how often a pass
comes round.

The observations themselves remain operational data, in their own table with their
own retention (G16), and produce no ledger row — which was true before and is now
true for the simpler reason that there is no ledger row anywhere in this feature.

**Rejected: metering it but pricing it at zero.** It keeps the pipeline for the day
somebody wants to charge, and costs nothing today. Rejected because a rate event
priced at zero is a rate event that has to be maintained, migrated and explained
for years on the chance of a decision nobody has made, and because the day it does
get a price is the day it needs to be designed against real usage rather than
guessed at now. `metering-and-credits` can add it then, and its own task naming
`node.observed` should simply be dropped rather than retitled.

### G18. `Check now` moves onto the row and loses its conditions

`Run verification` disappears with the panel. `Check now` — the recorded check
that also brings a domain online — stays on the domain row, but stops being
offered only for domains that have never been seen or have stopped resolving.
Every row offers it.

The condition existed because an "active" row had nothing a check could tell it.
With a TLS dot on the row that stops being true: an active domain whose
certificate expires in nine days is exactly the row somebody wants to re-check
after fixing something. Pressing it is safe in every state — a check that fails to
find a delegation records only that we looked, and never takes an active domain
offline.

## Risks / Trade-offs

- **Fifty domains and no report means scanning fifty rows** → accepted under G1;
  the answer when it bites is sorting and filtering the one table, which is cheap
  precisely because there is only one.
- **Yellow understates a delegation that has been absent for three weeks** →
  stated, not mitigated (G4). The activation chip beside it does not soften with
  time, and ageing yellow to red is a one-function change once there is a measured
  interval to age it by.
- **A customer reads the TLS dot as uptime monitoring** → the failed handshake is
  grey and the wording is *we could not look* (G9); reachability is deliberately
  not reported (G14).
- **We connect to a customer's machine on a schedule** → only behind a switch they
  turned on (constraint 3), only on 443, one handshake per address per cycle, with
  a bounded timeout and bounded parallelism (G13), and never more often than once
  an hour whatever the configuration says (G17). That last bound used to be
  implicit in the fee; it is explicit now because the fee is gone.
- **An unmetered check has nothing making anyone careful with it** → real, and the
  answer is the hourly floor rather than a price (G17). Worth naming as a risk
  because the pressure it removes was invisible: a metered switch is one somebody
  counts before enlarging, and a free one is not.
- **A verification-disabled TLS client is a bad habit that spreads** → it is
  confined to `internal/certobserve`, it is the only way to observe an expired or
  untrusted certificate rather than fail on it (G12), and the package holds no
  credentials and sends no data.
- **Chain validity is recorded and colours nothing, so it looks forgotten** →
  named as such in G12, with the case it exists for spelled out.
- **The history gate is commercial and could read as arbitrary** → it is stated as
  commercial in the refusal and in the dialog (G15), rather than dressed as a
  structural limit.
- **A wildcard's test hostname goes stale** — the customer stops serving
  `www.example.com` and the dot goes red for a name nobody deployed → the dialog
  names the hostname being tested in every state, so the red dot explains itself
  the moment it is opened.
- **Deleting the plan removes something somebody used** → the values a customer
  actually needed move onto the domain and are printed as complete records in the
  dialogs (G2). What genuinely goes is the address records and the zone rendering,
  and the address records were never a finding.
- **Removing a published endpoint breaks a caller we do not know about** → real,
  and accepted on the grounds that there is no customer yet: `SEQUENCE.md` places
  the first customers at step 13 and this is a pre-customer change, which is the
  only window in which a path removal is a rename rather than a deprecation.

## Migration Plan

1. Add `observe_tls` and `tls_test_host` to `domains`, with the validation binding
   the hostname to the wildcard rules. Both nullable/defaulted off, so every
   existing row is unaffected.
2. Add `certificate_observations` with the materialized ownership columns and
   forced row-level security every tenant table carries.
3. Add `internal/certobserve` — the handshake, the classification, the derived
   current state and banding — with no scheduler.
4. Add the worker and its configuration, defaulting to enabled with a six-hour
   cadence, and the global off switch.
5. Add the observation read to the management API, with the history range refused
   on a personal account.
6. Move onto the domain what the plan used to carry: the `CNAME` target and the
   expected CAA entries, on the domain read (G2).
7. Console: the dot line, the three dialogs, the switch and hostname field in the
   domain editor — reading the domain rather than the plan.
8. Console: delete the DNS plan section and the functions that fed it.
9. Remove the plan routes, the `adminctl` subcommand, `RenderZoneSnippet`, the
   address records and the per-record half of the verification response — last,
   once nothing reads any of them.
10. Retention and downsampling in the existing sweeper.
11. `SEQUENCE.md` entry 14 rewritten to point here.

Nothing here emits a usage or rate event, and there is no step that does: G17
makes observation unmetered, so the migration has one fewer moving part than an
earlier draft of this plan had.

Steps 6 through 9 are ordered so that the reader of a value always exists before
the writer of it goes: the domain carries the target before the console stops
reading the plan, and the console stops reading the plan before the plan stops
being served. At no point is a value neither served nor needed.

**Rollback**: nothing depends on an observation (G14), so the worker can be
switched off and the table left in place with no effect on any other path. Step 9
is the only one-way step, and it is one-way in the sense that any removal is:
restoring it means reverting a commit. It is deliberately last, and it is safe to
take now only because there is no customer yet — `SEQUENCE.md` puts the first
customers at step 13, which is the window in which removing a published path is a
rename rather than a deprecation.

## Open Questions

- **How long is full-resolution history kept before downsampling, and how long
  after?** ~~Weeks and months respectively (G16). The numbers want a look at real
  volume for a real account rather than a guess now.~~ **Settled**: 336h
  (fourteen days) at full resolution, 4320h (six months) thereafter, and both are
  CONFIGURATION rather than constants precisely because they are unmeasured — the
  same reasoning, and the same shape, as `challenge.interaction_retention`, which
  is configurable for the identical reason that nobody has signed off on a
  period. The comment beside each says so, so the next reader knows the number is
  a placeholder with a home rather than a considered value.
- **Does a domain whose every handshake has failed for a week deserve to stop
  being grey?** It is either broken or gone and we cannot tell which. Reporting it
  risks blaming a customer for our own transit; staying grey risks somebody
  believing they are covered. G9 chooses grey for now and the question is left
  open deliberately.
- **Should the TLS dialog offer a check-now?** The DNS side has one. A TLS
  check-now is an outbound connection to a customer's machine on demand, which is
  a different thing to hand an unauthenticated-by-us button, and it is not in this
  change.
- ~~**Does `domain.observed` price per domain or per domain-address?**~~
  **Closed, and the question no longer exists**: observation is unmetered (G17),
  so there is nothing to price either way. What replaced the question is the
  hourly floor on the observation interval, which is what now bounds the volume
  the price was implicitly bounding.
