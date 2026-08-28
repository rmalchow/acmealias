# Tasks

## 1. Reconciling the changes this one lands on top of

- [x] 1.1 Archive the eight completed changes so the main specs are the base this change's deltas apply to — `tenancy-and-permissions`, `layered-configuration`, `personal-and-team-accounts`, `domain-setup-feedback`, `domain-activation`, `swagger-ui`, `api-key-kinds`, `acme-dns-protocol`. Two ordering faults surfaced and were fixed in `personal-and-team-accounts`: it REMOVED a requirement `tenancy-and-permissions` had already removed, and it MODIFIED one that never existed. Both aborted the archive rather than corrupting anything
- [x] 1.2 Archive `health-checks` unimplemented, with `--skip-specs` so its requirements never enter the main specs. Nothing of it was built — there is no `internal/certobserve` and no `certificate_observations` table — so there is no code to reconcile, only a design whose decisions are carried forward under new numbers (G6 inverts H2, G17 replaces H6, G11/G14 keep H3/H1 unchanged)
- [x] 1.3 Rewrite `SEQUENCE.md` entry 14 to point here, and say in "What was removed" that `health-checks` was absorbed rather than cut — the distinction matters, because its certificate half is being built, just not as a screen of its own
- [x] 1.4 Note in the archived `health-checks` which change absorbed it, so somebody reading it later is not left working out why nothing was implemented
- [x] 1.5 Five changes remain open with real work in them and are NOT prerequisites of this one: `collapse-project-and-loadbalancer` (30/35), `account-invitations` (73/78), `self-service-signup` (28/34), `transactional-email` (9/17), `authentik-and-self-signup` (21/38). Two of `collapse`'s five are decisions its own design left open, and the specs still say "load balancer" where this change says "project" — a wording debt this change inherits and does not pay

## 2. The domain gains a switch and a test hostname

- [ ] 2.1 Migration: `domains.observe_tls boolean not null default false` and `domains.tls_test_host text null`. Both defaulted so every existing row is untouched and nothing starts connecting to anybody on deploy (G-constraint 3)
- [ ] 2.2 Model and store: carry both on `model.Domain`, read them wherever a domain is read, so a console row renders without a second request
- [ ] 2.3 Validation, in the store and not only in the API: enabling on a wildcard without a test hostname is refused, and a hostname the wildcard does not cover is refused. Reuse the wildcard rules `service-inventory` already enforces rather than writing a second matcher — a hostname accepted here and rejected by the domain rules is a row nothing can ever observe (G8)
- [ ] 2.4 Never derive a hostname. `www.<domain>` is the obvious guess and it is a guess; a customer whose wildcard serves `a.example.com` and not `www` would get a red dot for a name they never deployed, which is the worst possible first impression of a check (G8)
- [ ] 2.5 Disabling the switch keeps the hostname. Somebody who turns it back on gets their hostname back rather than being asked again (G8)
- [ ] 2.6 Both writable through the permission that already writes a domain — not a new one. This is a property of a domain, not a new object

## 3. `internal/certobserve` — the handshake and what is read from it

- [ ] 3.1 New package, deliberately NOT part of `internal/resolvercheck`. That one is advisory and persists nothing because DNS is somebody else's mutable state; an observation is a fact about an exchange we took part in, it is stored, and it is shown to the customer as history. Same shape of outbound probe, different status, and they must not be conflated in the code or in anyone's head (G10)
- [ ] 3.2 One handshake per (domain, node address, family), port 443, with the domain's name — or its `tls_test_host` — as the server name (G6). Each family is a separate attempt and a separate row: a node reached over v4 and over v6 goes through different transit and different firewalls, and a certificate renewed on one listener and not the other is invisible to a dual-stack visitor, including the customer checking in their own browser
- [ ] 3.3 Complete the handshake with the transport's verification DISABLED, and evaluate expiry, name coverage and chain trust ourselves (G12). A verifying client aborts on an expired or untrusted chain, which would collapse the two most interesting findings in this change into the same "could not look" as a firewall drop. Confine this to this package — it holds no credentials and sends no data
- [ ] 3.4 Record on success: not-before, not-after, issuer, serial, the SANs, whether the server name asked for is among them, and whether the chain was trusted. Record the address, the family and the server name presented alongside, so a row explains itself without a join
- [ ] 3.5 Record on failure: a CLASSIFICATION of ours — refused, timed out, handshake rejected, certificate unreadable — with the underlying text kept for the log and not for the customer. A failed handshake is an observation, never an omitted row: "there is no certificate here" and "we could not look" are different sentences with different audiences, and a gap in a table is indistinguishable from a scheduler that did not run (G11)
- [ ] 3.6 Bounded timeout per attempt, from configuration. A node that has started blackholing must cost one bounded pass, not an unbounded one
- [ ] 3.7 Derive a domain's current state from the latest observation per address, worst-first, with one exception: **grey never outranks a colour**. An address we could not reach beside three we could is a line in the dialog, not a reason to grey out a dot that has real information in it (G10)
- [ ] 3.8 Band the remaining lifetime server-side — >28d, 7–28d, <7d, expired — so no client applies its own thresholds and the console and `adminctl` cannot disagree. A certificate that does not cover the name asked for bands with expired (G9)
- [ ] 3.9 28 rather than 30 days, deliberately: every ACME client renews at 30 by default, so a certificate with 29 days left has just been told to renew and has not yet, which is exactly when the middle band starts being informative (G9)

## 4. The seventh worker

- [ ] 4.1 Schedule it like `internal/activation` — ticker, bounded batch, bounded parallelism inside the pass, an immediate first pass rather than one after the first tick, cancelled with the process (G13). A restart otherwise leaves somebody who just enabled the switch waiting out a full interval for a service that is already running
- [ ] 4.2 Cadence six hours, configurable. Nothing depends on an observation inside a renewal window, a certificate does not change between one hour and the next, and four observations a day against a thirty-day window is ample
- [ ] 4.3 Batch and parallelism are CONSTANTS, not configuration, for the same reason they are in `activation`: they bound the burst this process aims at somebody else's machine and how long one tick may run, not behaviour anybody tunes
- [ ] 4.4 Queue order: a domain whose switch was just turned on before a domain due for routine re-observation, so a batch too small for the backlog delays the settled work and never the person watching the screen (G13)
- [ ] 4.5 Configuration: interval, handshake timeout, and a global off switch. The off switch is the operational form of "nothing acts on an observation" — turning it off costs a stale dialog and nothing else (G14)
- [ ] 4.6 Never on a request path. `/present` is called by lego in the middle of an issuance; nothing in this change may be reachable from it

## 5. Storing and reading observations

- [ ] 5.1 Migration: `certificate_observations`, with the materialized ownership columns and forced row-level security every tenant table carries, so reading a domain's certificate state can be granted without granting the inventory it describes
- [ ] 5.2 A read returning the current state per node address for one domain — available on any account, because it is what colours the dot
- [ ] 5.3 A read returning the history for one domain, refused on a personal account (G15). Refuse it in the SERVICE, not by having the console hide a section it received
- [ ] 5.4 The refusal names the account kind, not a missing permission. `teamAccountGuard` keeps a personal account off the member roster because an account of one CANNOT have members, and the service says `409` rather than `403` precisely so a client can tell "this does not exist here" from "you may not". Nothing is meaningless about history on a personal account — it is withheld, and the refusal has to say so rather than borrow a structural justification it does not have (G15)
- [ ] 5.5 Record history for every account, not only team ones. Upgrading otherwise produces a feature with no data in it for weeks (G15)

## 6. Retention and billing

- [ ] 6.1 Retention in the EXISTING sweeper, not a new worker: full resolution for a recent window, then at most one observation per domain per address per day (G16). Downsampling rather than deletion, because the value of the history is a trend measured in weeks and truncating the period destroys the shape of it
- [ ] 6.2 Emit `domain.observed` from the periodic rate job: one row per account per period per domain with the switch on. Never per handshake, per node or per family (G17). The customer switched on *observe this domain*; they did not choose six hours and they did not choose that four dual-stack nodes is eight handshakes. Both are ours and we will change them, and halving the interval must not double an invoice
- [ ] 6.3 Observations produce no ledger entry. They are operational data with their own table and their own retention

## 7. The console: the line of dots

- [ ] 7.1 Replace the Validation column's labelled chips with a line of dots on the domain row: a filled circle, the record TYPE beside it, a tooltip, and a click (G3)
- [ ] 7.2 Label the record type, never the verdict. A label reading "configured incorrectly" makes a row's width depend on how broken it is, and a table whose columns move as state changes is a table nobody can scan (G3)
- [ ] 7.3 A record type this domain does not have gets NO dot, not a grey one: CAA with no configured issuer, TLS with the switch off. A grey dot would claim we meant to look (G3)
- [ ] 7.4 `CNAME`: green correct, **red incorrect**, **yellow missing**, grey for not-checked / could-not-check / no-alias (G4). This inverts what `domain-setup-feedback` shipped, where absent and incorrect were both red — `missing` is overwhelmingly somebody mid-task who added a domain five minutes ago, and it is the state every new domain passes through; `incorrect` is a record that exists, looks finished, and will never work
- [ ] 7.5 CAA: green for `correct` AND for `not-published`, red for `blocking`, grey for `undetermined` (G5). The two green states are not the same fact and the tooltip does not pretend they are — "authorises us" / "no restriction" — but they are the same instruction, which is *do nothing*, and that is what a dot carries
- [ ] 7.6 TLS: grey never-observed, grey handshake-failed, red uncovered or expired or <7d, amber 7–28d, green >28d (G9). Take the band from the service (3.8) rather than computing thresholds here
- [ ] 7.7 A failed handshake is GREY, not red. It is the one place our own probe's failure meets a colour scale, and red would claim the customer's node is broken when we cannot distinguish that from our transit, their firewall, or a listener that is simply not on 443 (G9)
- [ ] 7.8 Every dot is clickable, including a green one. A control that appears only when something is wrong teaches people to read its presence as an alarm (G3)
- [ ] 7.9 Keep the activation chip and its "last looked / last seen" stamp. It answers a different question from every dot beside it — what the service will DO for this domain, from what it has stored, which is what `/present` actually consults — and it is the one signal that does not soften as a delegation stays absent (G4)
- [ ] 7.10 Offer **Check now** on every row, dropping the "never seen or stopped resolving" condition. An active domain whose certificate expires in nine days is exactly the row somebody re-checks after fixing something, and `Run verification` is about to disappear with the panel (G18)

## 8. The console: one dialog per record type

- [ ] 8.1 Split `domain-help.modal.ts` into three. One dialog covering every record type has to lead with a section the reader did not click on (G7)
- [ ] 8.2 The `CNAME` dialog prints `_acme-challenge.example.com. IN CNAME k7f2q9.acmealias.com.` complete, with a copy control, in every state — including green, where it names the record in effect
- [ ] 8.3 Keep the deleted-and-re-added explanation verbatim for `incorrect`. It is the one cause a customer will not guess: a new alias is issued, and a delegation left pointing at the old one is published, well-formed, and will never validate
- [ ] 8.4 The CAA dialog prints both `issue` and `issuewild` for every configured issuer, with the standing note to leave existing entries alone because CAA is a list. `issue` alone is not the fix — a present `issuewild` governs wildcard issuance INSTEAD of `issue`, so somebody adding only the `issue` line adds a line that is already there and stays blocked (G7)
- [ ] 8.5 The CAA dialog never offers a delegation record as the remedy for `blocking`. That sends somebody to the wrong file with the wrong value while issuance goes on being refused
- [ ] 8.6 The TLS dialog prints no record — there is nothing in DNS to fix — and instead lists every node address, in node order, family within node, so "renewed on v4 and not on v6" is one glance rather than a comparison the reader constructs (G10)
- [ ] 8.7 The TLS dialog names the hostname being tested in every state. A wildcard whose test hostname has gone stale shows red for a name nobody deployed, and this is what makes that dot explain itself the moment it is opened (G8 risk)
- [ ] 8.8 History section: rendered on a team account, and on a personal account replaced by a line saying history is part of a team account — stated, not omitted silently and not dressed as unavailable (G15)
- [ ] 8.9 Never print the diagnostic text of a lookup that failed. On `undetermined` that field holds our resolver's error, addressed to an operator, and showing it reads as a verdict on the customer's zone when it is a verdict on our lookup
- [ ] 8.10 Copy controls on every printed record, with the existing clipboard-refused fallback wording — the zone snippet is no longer there to fall back to, so the message must not name it

## 9. The console: the switch in the domain editor

- [ ] 9.1 A switch in `domain-editor.component.ts`, off by default, worded so it is clear nothing connects to the customer's machines until it is on
- [ ] 9.2 On a wildcard, the switch reveals a required hostname field, validated client-side against the same wildcard rules and never prefilled (G8, task 2.4)
- [ ] 9.3 Turning it off leaves the hostname in the field rather than clearing it

## 10. Moving what the plan carried onto the domain

Order matters through this group and the next: the reader of a value must exist
before the writer of it goes. The domain carries the target before the console
stops reading the plan, and the console stops reading the plan before the plan
stops being served. At no point is a value neither served nor needed.

- [ ] 10.1 Put the `CNAME` target on the domain read — the alias label joined to the served zone, the same string `inventory.Plan` puts in front of a customer today. A domain with no alias reports the absence rather than being omitted: the row still exists on the screen and still deserves an answer (G2)
- [ ] 10.2 Put the expected CAA entries on the domain read, at the registrable name, both tags, per configured issuer, and none at all when no issuer is configured. The rules do not change — only what carries them
- [ ] 10.3 Take the target from the ALIAS rather than recomputing it. `dp.ChallengeName = a.ChallengeFQDN` exists in `buildPlan` for exactly this reason: agreeing by construction beats agreeing by an equality some other test defends, and the failure if the two diverged is silent — every row reading "not checked" for a delegation that is correct
- [ ] 10.4 Reshape `internal/inventory` around the per-domain expectation rather than the plan. The plan builds a full record set and then joins back onto domains, which is the indirection that made a per-domain answer hard to give in the first place (G2)

## 11. Deleting the plan

- [ ] 11.1 Console: delete the DNS plan section from `project-detail.component.ts` — the table, `Copy records`, `Zone snippet`, `Run verification` — and with it `copyPlan`, `zoneHref`, `statusFor`, `statusColour` and `foundFor`
- [ ] 11.2 Console: `answerFor` reads the target from the DOMAIN, not from `plan()`, and `loadPlan()` goes with the rest. This is what lets the `CNAME` dialog print the record to publish on a deployment that never configured a resolver — which record a domain needs is knowable without asking DNS anything, and it stays that way
- [ ] 11.3 Remove the CAA footnote paragraph that lived under the plan table. Its content — absent CAA is not an error — is now carried by the CAA dot being green and by its dialog
- [ ] 11.4 Remove the routes `GET …/plan` and `…/plan/zone`, with `getPlan`, `getPlanZone`, `planView` and `recordView`
- [ ] 11.5 Remove `inventory.RenderZoneSnippet` and the address records from the plan builder. Do NOT relocate the address records: a customer typed those addresses into the Nodes panel on this screen, and grading their DNS against them is a check whose only possible finding is that the customer disagrees with the customer (G2)
- [ ] 11.6 Remove `internal/adminctl/plan.go` and its subcommand. `cnameLine` in `inventory.go` stays — it prints the one record a customer needs when a domain is created, which is the surviving half
- [ ] 11.7 Drop the per-record half of the verification response — the `results` array and `checkResult`. It answered the same question twice, keyed two ways, and only the per-domain keying has a reader (G2)
- [ ] 11.8 Remove the two plan paths from `openapi.yaml` and add the observation read. The contract is served through Swagger UI now, so a stale path is a published lie rather than an unused handler
- [ ] 11.9 Rewrite `backend/acceptance/plan_test.go` around the domain read, and check `delegation_test.go` and `setup_feedback_test.go` for assertions on the removed shape
- [ ] 11.10 This is the one-way step, and it is safe only because there is no customer: `SEQUENCE.md` puts the first customers at step 13, which is the window in which removing a published path is a rename rather than a deprecation. Do it last

## 12. Proving it

- [ ] 12.1 A wildcard and its apex are separate rows with separate switches, separate hostnames and separate TLS dots — and the SAME `CNAME` dot, because they share one alias and one delegation
- [ ] 12.2 Enabling on a wildcard without a hostname is refused, and a hostname outside the wildcard is refused, at the store and not only at the API
- [ ] 12.3 A dual-stack node yields two observations, and a v4-only node yields one with no v6 attempt recorded as a failure
- [ ] 12.4 An expired certificate is an OBSERVATION, not a failure to observe — the case a verifying client would have turned into an error (G12)
- [ ] 12.5 An untrusted chain is recorded with the certificate and flagged, and does not colour the dot in this change
- [ ] 12.6 A certificate not covering the server name asked for bands with expired, whatever its remaining lifetime
- [ ] 12.7 Worst-of across addresses, and the exception: one unreachable address beside three healthy ones does not grey the dot, and still appears in the dialog
- [ ] 12.8 Every band edge: 29 days green, 28 amber, 8 amber, 7 red, 6 red, expired red
- [ ] 12.9 A refused connection and a timeout each produce a stored observation with a classification, and the state reads "could not be examined" rather than "no certificate installed"
- [ ] 12.10 History is refused on a personal account with a refusal naming the account kind, and returned on a team account — and observations are recorded on both
- [ ] 12.11 Observations are confined by row-level security: a caller scoped to one account sees none of another's
- [ ] 12.12 Disabling observation for the deployment stops every handshake and changes no other path (G14)
- [ ] 12.13 A domain with the switch off is never connected to, at all, from any pass
- [ ] 12.14 `domain.observed` is one event per domain per period regardless of node count, family count or interval (G17)
- [ ] 12.15 Acceptance: enable the switch on a domain served by the fake internet, run one pass, and read the banded state back through the API
- [ ] 12.16 The domain read carries its `CNAME` target and its expected CAA entries, and a wildcard and its apex carry the SAME target — the assertion `plan_test.go` used to make about one CNAME row, made about two domains (10.1, 10.2)
- [ ] 12.17 A domain with no alias is still returned, carrying no target, rather than omitted (10.1)
- [ ] 12.18 `GET …/plan` and `…/plan/zone` answer 404, and no domain read carries an address record (11.4, 11.5)
- [ ] 12.19 The verification response carries per-domain answers and no per-record array (11.7)

## 13. Documentation

- [ ] 13.1 `docs/` gains the TLS check: what it connects to, on what port, that it is off until switched on, and that we never hold a certificate or a key
- [ ] 13.2 Say plainly, where a customer will read it, that a failed handshake means we could not look and is not a claim their node is down (G14)
- [ ] 13.3 Document the wildcard test hostname and why it exists — a wildcard is not a name a TLS client can ask for
