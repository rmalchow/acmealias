# One row per domain, and everything true about it on that row

## What this changes, in one paragraph

The project screen has two panels answering one question twice. **Domains** lists
each name the customer added and, since `domain-setup-feedback`, tells them
whether the record that name needs is published. **DNS plan** lists the same
answer again, one row per *record* instead of one per *domain*, plus address
records that echo back the IP addresses the customer typed into the Nodes panel
a few centimetres above. This change deletes the second panel and moves what was
diagnostic about it onto the domain's own row, as a short line of coloured status
dots. It then adds a third dot beside the two that exist: an explicitly enabled
**TLS check** that connects to each of the project's node addresses, on both
address families, reads the certificate actually being served for that domain,
and colours the dot by how long that certificate has left.

## Terms, before they are used

Several of these already exist in the codebase; they are restated because this
document is meant to be readable by somebody who has not read the others.

- A **domain** is one name a customer added to a project — `example.com`,
  `shop.example.com`, `*.example.com`. It is a row in the `domains` table and a
  row in the Domains panel.
- A **node** is one of the customer's own servers, identified here by an IPv4
  address, an IPv6 address, or both. We never connect to a node as part of
  answering a challenge; the node is the machine the customer's certificate ends
  up installed on.
- The **delegation** is the single `CNAME` a customer publishes once, from
  `_acme-challenge.example.com` to `k7f2q9.acmealias.com`. It is the whole
  integration. If it is wrong, no certificate is ever issued.
- A **CAA** record set names the certificate authorities permitted to issue for a
  name. Its *absence* means anyone may issue, so an absent CAA record is the
  normal, healthy, overwhelmingly common state — which is the opposite of how an
  absent `CNAME` reads, and is why the two dots below are coloured by different
  rules.
- The **DNS plan** is the structure the service generates listing every record
  the customer must publish outside the service: address records, the `CNAME`,
  and CAA rows when an issuer is configured. It is produced by
  `internal/inventory.Plan` and served at `GET
  /accounts/:a/projects/:p/plan`.
- An **observation** is one completed — or failed — TLS handshake against one
  node address for one domain, and what was read from it. It is a fact about an
  exchange we took part in, so unlike a DNS check it is stored.
- A **SAN** (subject alternative name) is a name listed inside a certificate as
  one the certificate is valid for. A certificate for `example.com` and
  `www.example.com` carries both as SANs.

## Why

### The plan table restates the domain table and disagrees with it

Here is the screen today, for a project with two nodes and two domains, one of
which has a mistyped delegation:

```
Nodes
  a    192.0.2.10    2001:db8::10
  b    192.0.2.11    2001:db8::11

Domains
  shop.example.com   _acme-challenge.shop.example.com   k7f2q9   ● active
                                                        Delegation  configured correctly
                                                        CAA         no restriction
  *.example.com      _acme-challenge.example.com        m3p8xz   ● waiting for delegation
                                                        Delegation  configured incorrectly
                                                        CAA         no restriction

DNS plan                             [Copy records] [Zone snippet] [Run verification]
  shop.example.com                    A      300  192.0.2.10       correct    192.0.2.10
  shop.example.com                    A      300  192.0.2.11       correct    192.0.2.11
  shop.example.com                    AAAA   300  2001:db8::10     correct    2001:db8::10
  shop.example.com                    AAAA   300  2001:db8::11     correct    2001:db8::11
  *.example.com                       A      300  192.0.2.10       correct    192.0.2.10
  …                                                                …
  _acme-challenge.shop.example.com    CNAME  300  k7f2q9.…         correct    k7f2q9.…
  _acme-challenge.example.com         CNAME  300  m3p8xz.…         incorrect  old9kk.…
  example.com                         CAA    300  0 issue "…"      not-published
  example.com                         CAA    300  0 issuewild "…"  not-published
```

Fourteen rows to say two things, both of which the four lines above already said.
Eight of those rows are address records whose "correct" verdict means *the
customer's DNS points at the addresses the customer typed into the panel above* —
a fact we learned from them and are now grading them on.

The two panels are also not always consistent, because they are fetched
separately and one of them can be stale: `Run verification` writes into the same
signal the row chips read from, but the plan itself (`loadPlan()`) is refetched
on a different set of events than the verification is. A customer looking at a
green row above a red row, both about the same `CNAME`, has no way to know which
one to believe.

And the button that would resolve it is in the wrong place. `Run verification`
sits in the DNS plan header — below the table whose rows it explains — which is
the exact complaint `domain-setup-feedback` opened with and only half fixed: that
change moved the *result* onto the domain row and left the *control* down here.
`Check now` exists on the domain row now, but only for domains in two particular
states, so for everything else the only control is still the one at the bottom of
a table that is about to be deleted.

### Nothing on this screen knows whether a certificate was ever installed

This is the larger gap, and it is why a third dot is worth adding rather than
just tidying the two that exist.

The service answers DNS-01 challenges. `challenge-history` records that a
certificate authority came to a challenge name, looked, and was satisfied. Then
it stops, because everything that happens next happens on the customer's machine.

There is a failure mode that is completely invisible from here: validation
passed, a certificate was issued, and it was never deployed. Or it was deployed
to node `a` and not to node `b`. Or it was deployed on the IPv4 listener and not
the IPv6 one. Or it covers `example.com` but not the `shop.example.com` added
last week. The challenge history shows success in every one of those cases,
because in every one of those cases the challenge succeeded. The customer finds
out when a browser tells them, six weeks later.

A single TLS handshake closes the loop, and the result is one more coloured dot
on a row that already has two:

```
Domains                                                      [+ Add domain]
  shop.example.com   ● CNAME  ● CAA  ● TLS      active
  *.example.com      ● CNAME  ● CAA  ● TLS      waiting for delegation
```

### Why the certificate check belongs here rather than in `health-checks`

`health-checks` is change 14 in `SEQUENCE.md` and designs exactly this — a
background worker completing TLS handshakes and recording what it read — but it
designs it as **a screen of its own**: a per-load-balancer certificate view, with
a table of node addresses, per-address history, an uncovered-domains report and
an approaching-expiry report. Four surfaces.

Once the diagnostic dots exist, three of those four collapse into one dot and its
dialog. "This domain is covered by no observed certificate" is not a report; it
is what the TLS dot says when it is red. "This certificate expires in nine days"
is not a report either; it is the same dot in a different colour. Building the
dots and then building the separate view would put the same three facts in two
places and give a customer two screens to reconcile.

So this change **absorbs `health-checks` and that change is archived
unimplemented.** Everything load-bearing in it survives — both address families
probed separately, a failed handshake recorded as an observation rather than
omitted, history kept so "since when" is answerable, and an hours-scale cadence.
What is dropped is its presentation, one of its central cost decisions (see the
next section), and its billing — observation is free here, and only challenges
cost.

### The check is per domain, opt-in, and that inverts one of `health-checks`'s decisions

`health-checks` decided (its H2) to probe **once per node address** and read the
covered names out of the certificate, explicitly rejecting the per-domain matrix
on cost: *"Four nodes and fifty domains is two hundred handshakes to discover
what is almost always four certificates."*

That arithmetic assumed every domain is observed. This change makes observation
**explicitly enabled per domain**, and that changes the sum: the customer names
the handful of domains they care about, and four nodes × two families × three
enabled domains is twenty-four handshakes every few hours, which is nothing.

Having paid that, the per-domain probe answers a question the per-node probe
cannot. A server that selects a certificate by SNI — every reverse proxy, every
ingress controller, every load balancer terminating TLS for more than one site —
presents a *different* certificate depending on the name asked for. Probing the
address without a server name gets whatever the default virtual host serves,
which for a customer running twenty sites behind one address is a certificate for
somebody else's name entirely. `health-checks` listed that as a known accepted
limitation. Under a per-domain opt-in it stops being a limitation, because the
domain **is** the server name.

### History is worth keeping, and the check itself is free

An observation history answers "since when", which is the first question anybody
asks about every problem this change surfaces. It is also, unlike the current
state, a thing a customer cannot reconstruct for themselves after the fact.

Observations are recorded for every enabled domain regardless of account, because
the current state is derived from the latest one and the current state is what
colours the dot. But the **history list** — every observation for this domain,
newest first, with what each handshake found — is shown only on a team account. A
personal account's dialog shows the latest observation and an inline note that
history is part of a team account.

This is the first feature in the product gated on account kind for commercial
rather than structural reasons, and it is worth being honest that it is a
different kind of gate from the ones that exist. `requireTeamAccount` today keeps
a personal account away from the member roster, and it does so because *an
account of one cannot have members* — the screen is meaningless there, and the
service refuses it with a `409` saying so rather than a `403`. Nothing is
meaningless about history on a personal account. It is withheld, and the dialog
says it is withheld rather than pretending the data does not exist.

Because it is a different kind of gate it gets a different refusal: **`418`**. The
three obvious candidates all already mean something a client would act on wrongly
— `403` sends somebody to check permissions that are working perfectly, `402` is
this service's "suspended or unfunded" and drops the console into a funding flow,
and `409` is the structural refusal whose justification this gate specifically
does not have. A status nobody else uses is what lets this one carry its own
meaning without being mistaken for any of them. The cost is that some proxies and
client libraries treat `418` as unknown, so the meaning lives in the error
sentinel and the message body, the console keys on those rather than on the
number, and the response is documented explicitly in `openapi.yaml` because no
generator will infer it. `design.md` G15 carries the full reasoning.

## What Changes

### The DNS plan panel is deleted from the console

- **BREAKING (console only).** The DNS plan section disappears from the project
  screen: the record table, `Copy records`, `Zone snippet` and `Run
  verification`.
- The address records go with it and are not relocated. They restate node
  addresses the customer entered on the same screen.
- The `CNAME` and CAA values a customer actually needs to publish survive, in the
  per-record dialogs, carrying that domain's own values — which is where they
  already are and always were the better place for them, because a dialog can
  print `_acme-challenge.example.com. IN CNAME k7f2q9.acmealias.com.` while a
  table row makes somebody assemble it from four columns.
- **BREAKING (API).** `GET …/plan` and `GET …/plan/zone` are removed, along with
  the `adminctl` subcommand that renders them and their entries in
  `openapi.yaml`. Keeping a machine-readable plan alive with no screen behind it
  would preserve the address records this change just decided not to hand anybody,
  and would leave two shapes — a set of records, and a domain — answering "what
  must I publish". Everything a caller needs is a property of a domain, and the
  domain is where it now lives.
- **The domain carries what it needs published.** A domain read gains its
  challenge name's `CNAME` target and, where an issuer is configured, the CAA
  entries expected at its registrable name. That is what the dialogs print and
  what a client would previously have assembled out of the plan.
- **The verification response loses its per-record half.** It answered twice —
  once per plan record, once per domain — and only the second survives.

### Each domain row carries a line of status dots

Three dots at most, each a small filled circle with the record type beside it,
each with a tooltip and each clickable.

- **`CNAME`** — always present. Green when the delegation is published and points
  at this domain's alias. **Red when a record is published and points somewhere
  else.** **Yellow when nothing is published.** Grey when we have not looked, when
  the lookup failed, or when the domain has no alias to point at yet.
- **`CAA`** — present only when this deployment has a configured certificate
  authority identifier, because with none configured there is no CAA row in the
  plan and nothing about CAA is true or false for this domain. Green when the
  record set authorises our issuer **and also** when no CAA record is published at
  all, since both permit issuance. Red when a set exists and does not name our
  issuer. Grey when the lookup failed or has not run.
- **`TLS`** — present only when the check is enabled for this domain. Grey when
  never checked. Green when the certificate served has more than four weeks left,
  amber between one and four weeks, red below one week, expired, or covering a
  name that is not this domain's.

**BREAKING (behaviour):** the `CNAME` dot's yellow-for-absent inverts what
`domain-setup-feedback` shipped, where absent and incorrect were both red. The
reasoning is in `design.md` G3 and is not obviously right; it is recorded as a
decision with its risk stated rather than as an improvement.

### Every dot opens an explanation

Clicking a dot opens a dialog for that record type alone — not the whole-domain
dialog that exists today — explaining what the state means and, where there is
something to publish, printing the exact record with this domain's own values and
a copy control beside it. The `CNAME` dialog prints the delegation. The CAA
dialog prints the `issue` and `issuewild` lines for every configured issuer. The
TLS dialog prints no record, because there is nothing in DNS to fix; it prints
what was observed, per node address, per family.

### A TLS check is enabled per domain, with a test hostname for wildcards

- A domain carries a switch, off by default. Nothing connects to anybody's node
  until somebody turns it on.
- When the domain is a wildcard, turning the switch on **requires a concrete
  hostname** to test with, because `*.example.com` is not a name a TLS client can
  ask for. The hostname is validated to be covered by the wildcard, stored on the
  domain, and used as the server name for every handshake for that domain.
- Turning it off stops the handshakes and leaves the history in place.

### Observations are recorded, and their history is a team-account feature

- A background worker connects, on an hours-scale cadence, to every node address
  of the project — both families, separately — for every enabled domain, with
  that domain's name (or its test hostname) as the server name.
- Each attempt is stored: the certificate's not-before, not-after, issuer, serial
  and SANs on success; the error on failure. A failed handshake is an
  observation, never an omitted row.
- The dot is coloured from the latest observation per address, taking the worst.
- The dialog lists every address. On a **team account** it also lists the history
  for each. On a personal account it says history is a team-account feature.

### `health-checks` is archived unimplemented

Its proposal, design and specs move to `openspec/changes/archive/` with a note
naming this change as the one that absorbed it. `SEQUENCE.md` entry 14 is
rewritten to point here.

## Capabilities

### New Capabilities

- `certificate-observation`: enabling observation for a domain and the test
  hostname a wildcard needs, the handshake against every node address on both
  families with the domain as the server name, what is recorded from a successful
  and from a failed handshake, how a domain's current certificate state is
  derived from the latest observations, the expiry bands, retention, and the
  visibility of history by account kind.

### Modified Capabilities

- `admin-console`: the DNS plan panel is removed; the required-records
  requirement stops describing a console table; each domain row reports its
  record types as dots with an explanation dialog per type; the console offers
  the TLS switch and collects a wildcard's test hostname.
- `service-inventory`: a domain carries an observation switch and an optional
  test hostname, with the validation rules binding the two to the wildcard rules
  that already exist.

## Impact

### Backend

- **New `internal/certobserve`** — handshake execution, the derived current
  state, the expiry banding. Deliberately separate from `internal/resolvercheck`,
  which stays advisory and stores nothing, for the reason `health-checks` gave and
  which still holds: a DNS answer is somebody else's mutable state, an observation
  is a fact about an exchange we took part in, and conflating them in code or in
  anyone's head is a mistake.
- **New table `certificate_observations`**, with the materialized ownership
  columns and forced row-level security every tenant table carries.
- **`domains` gains two columns** — `observe_tls` and `tls_test_host`.
- **A seventh background worker**, alongside `internal/activation`, on the same
  shape: a ticker, a bounded batch, bounded parallelism, cancelled with the
  process.
- **New configuration** — an observation interval with a one-hour floor refused at
  startup, a handshake timeout, two retention windows, and the check globally
  disableable.
- **`internal/httpapi`** gains the observation read, loses `getPlan` and
  `getPlanZone` with their two routes, and loses the per-record half of the
  verification response.
- **`internal/inventory`** loses the address records, `RenderZoneSnippet`, and
  the plan as a public shape. What survives is the per-domain expectation the
  verification and the dialogs need: a challenge name, a `CNAME` target, and the
  CAA entries for a registrable name.
- **`internal/adminctl/plan.go`** goes, with its subcommand.
- **`openapi.yaml`** loses the two plan paths and gains the observation read.

### Frontend

- `project-detail.component.ts` loses its DNS plan section, `copyPlan`,
  `zoneHref`, `statusFor`, `statusColour` and `foundFor`, and gains the dot line.
- `domain-status.ts` gains the TLS banding and the collapse of CAA's four states
  onto two colours.
- `domain-help.modal.ts` is split into one dialog per record type.
- `domain-editor.component.ts` gains the switch and the conditional hostname
  field.

### Billing

- **Nothing.** Certificate observation is unmetered: no usage event, no rate
  event, no counter, no ledger row, and no change to what an account is charged
  when somebody turns the switch on. Only challenges cost.
- `health-checks` H6 defined `node.observed` and an earlier draft of this change
  carried it forward as `domain.observed`, a count per account per period of
  domains with the switch on. That is **reversed**, and `design.md` G17 records
  why rather than dropping it silently. The short version: the reason for billing
  the switch rather than the handshake was that the customer chose *observe this
  domain* and did not choose the interval or the node count, so our changing
  either must not move their invoice — and charging nothing achieves that same
  end without a rate event, a table, a periodic emitter and a line on a bill
  somebody has to have explained to them. A diagnostic that tells a customer
  their certificate did not deploy should also not have a revenue reason to be
  switched on.
- **What the price was quietly doing, the interval floor now does explicitly.** A
  per-domain fee was the only thing on the customer's side bounding how much of
  this we would ever do. With no fee there is no such brake, so the observation
  interval takes a **floor of one hour**, enforced in configuration validation
  and refused at startup rather than clamped in silence. The six-hour default is
  unchanged.
- `metering-and-credits` needs no task for this. Its existing one naming
  `node.observed` should be dropped rather than retitled.

## What this does not do

- **It does not monitor reachability.** A failed handshake explains why we have no
  observation; it never claims a node is down. `health-checks` H1's reasoning
  survives intact: an hours-apart sample is worse than the monitoring every
  operator already has, and offering it invites somebody to rely on it.
- **It does not alert.** The data supports it; who gets told is a separate
  question.
- **It does not obtain, install or hold a certificate or a private key.** We
  connect as a client and read what is presented.
- **It does not act on an observation.** Nothing is switched off, gated or
  suspended because of what a handshake found. That is what makes the worker safe
  to disable and the table safe to leave in place.
