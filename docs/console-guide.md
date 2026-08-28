> **docs/ is for humans, not for AI.** Authoritative specification: [`../openspec/`](../openspec/).

# Admin console guide

Using the web UI at `https://acme-console.cloudpowered.services/` — what each screen is
for, and the handful of recipes that cover almost everything you will actually do.

**On this page**

- [Signing in, and what you can do](#signing-in-and-what-you-can-do)
- [Screen map](#screen-map)
- [Projects and load balancers](#projects-and-load-balancers)
- [Domains](#domains)
- [Wildcards](#wildcards)
- [What each domain's row says](#what-each-domains-row-says)
- [Certificate observation](#certificate-observation)
- [API keys](#api-keys)
- [Recent interactions](#recent-interactions)
- [Live challenges](#live-challenges)
- [Audit log](#audit-log)
- [Roles](#roles)
- [Settings](#settings)
- [Recipes](#recipes)

## Signing in, and what you can do

There is no password to remember. Opening the console redirects you to the identity provider and
back; a deep link you followed is preserved, so a link to a specific load balancer still lands you there
after signing in.

Signing in lands you on the **account picker** rather than inside an account. A session begins with
nothing selected, and choosing which account you are working in is the first thing you do; the
switcher in the navigation bar is how you change it later.

Your permissions come from your **membership of that account**: somebody in it put you in one or
more roles, and holding several at once is normal — they are additive, not exclusive. Groups at
the identity provider do not grant anything. If the account you expect is not in the picker, nobody has added you
to it yet, and that is the whole of the diagnosis.

Roles are **rows in your account**, not fixed names in the software. The four below are seeded for
you and confer exactly what they always did, but they can be edited and you can add others — see
**Roles** in the navigation.

| | `ADMIN` | `CONFIG_ADMIN` | `KEY_ROTATION` | `READ_ONLY` |
| --- | --- | --- | --- | --- |
| create / delete projects | yes | no | no | no |
| load balancers, nodes, domains | yes | yes | no | no |
| API keys create/revoke | yes | yes | yes | no |
| see a key secret (once) | yes | yes | yes | no |
| check now / verify records | yes | yes | yes | yes |
| view inventory + audit log | yes | yes | yes | yes |

`ADMIN` is your account's **unrestricted** role: every action on every object, uneditable, and
always with at least one member. The console shows it as fixed rather than as a form you cannot
submit, because there is nothing in it to edit — its authority is a property of the account, not a
list someone could shorten. For the same reason the last member's **Remove** button is absent: an
account with nobody in that role is one nobody can administer.

Any other role is a set of **grants**. A grant names an object type, an object, and an action, plus
whether it reaches the things that object contains — so "read this one project" and "manage keys for
this load balancer" are both sayable, and a contractor given the first sees only that project in
every listing. Grants are anchored to your account: a wildcard means "everything here", and there is
no way to write one that names anything outside it.

You may only grant what you hold. The composer offers no more than your own authority, and the
service refuses anything wider — checked against a single grant you hold rather than against your
grants combined, so holding two load balancers does not let you issue authority over the project
containing them. Today those are the only two; tomorrow there might be a third you never had.

Controls you may not use are not shown. That is a convenience, not the enforcement — every action is
checked again on the server, so nothing is reachable by poking at the API directly.

## Screen map

```
Projects
└── Project
    └── Load balancer              ← where you will spend most of your time
        ├── Nodes                  addresses of its Traefik hosts
        ├── Domains                what this load balancer answers for, checked live
        ├── API keys               credentials, and what each one may reach
        └── Recent interactions    every publish, and whether the CA came to look

Live challenges                    what is being validated right now
Audit log                          who changed what
Roles                              who may do what, and to which objects
Settings                           effective configuration, active sessions
```

## Projects and load balancers

A **project** groups everything belonging to one customer engagement or one of our own
products. Permission *can* be granted per project now: a role holding a grant over one project, with
"reaches contained objects" ticked, sees and manages everything inside it and nothing outside. So
organising projects to match teams is no longer only tidiness — it is the shape access control will
follow.

A **load balancer** is one environment's Traefik nodes. Record every node's public address:
those addresses are what the customer's own DNS must point at, they are offered as the default
restriction when you create an API key, and — for any domain whose certificate observation
switch is on — they are the addresses this service connects to on port 443 to read what
certificate is actually being served there. A wrong address here means a valid certificate on
a host nobody reaches, or an observation reading somebody else's machine.

A project or load balancer that still owns children cannot be deleted. Remove the domains first — which is
deliberate, because deleting a load balancer silently would revoke certificate renewals for everything under
it.

## Domains

Add the exact name the certificate will cover. The editor validates as you type and explains any
rejection.

| Input | Result |
| --- | --- |
| `shop.example.com` | Accepted — a specific host. |
| `example.com` | Accepted — the zone apex. |
| `*.example.com` | Accepted — a wildcard; shares an alias with the apex if that is also present. |
| `*.*.example.com` | Rejected — ACME permits one wildcard label. |
| `foo.*.example.com` | Rejected — the wildcard must be leftmost. |
| `*.co.uk` | Rejected — that would be a certificate covering a public suffix. Always a typo. |
| `shop.example.com` when `*.example.com` exists | Accepted **with a warning** — the wildcard already covers it. Occasionally intentional, usually not. |

Below the name field sits an unticked checkbox that adds the typed name's **counterpart** in the
same action: for `example.com` it offers to add the wildcard `*.example.com`, for `*.example.com`
it offers to add the apex `example.com`, and for `shop.example.com` it offers `*.shop.example.com`.
The label always names the exact name it would add. Both rows are created together against the one
alias they share, or neither is: if the counterpart breaks a rule the typed name does not —
`co.uk` is a valid name, `*.co.uk` is not — the dialog stays open with the service's reason and
nothing has been created.

It is unticked every time the dialog opens and the console never remembers your last choice. The
counterpart is a real decision, not a formality: it means the customer must publish a wildcard
address record covering their whole zone, and it means a stolen key covers every hostname
beneath the domain. Some customers forbid wildcard certificates by policy. Tick it when the
customer asked for a wildcard; leave it alone otherwise.

Adding a domain creates or reuses an **alias**: the single label inside our zone that
the customer's CNAME will point at. The label is assigned by the service and cannot be edited, here
or anywhere else: by default it is randomly generated (`k7m3q9x2r4tv8w1p`), so that
nobody outside can compute a domain's label and ask our nameservers whether it answers. A
deployment configured with `ACME_ALIAS_USE_DOMAIN=true` gets a readable
`demo-example-com` instead. Either way the label of an alias that already exists never
changes — it is published in a zone we do not control.

> **One challenge name belongs to one load balancer**
>
> Adding `*.example.com` to a load balancer while `example.com` lives on a different
> one is refused, and the error names the load balancer that owns it. Both would need the same CNAME pointing
> at the same alias, which would make "who may publish here" ambiguous. Move the domain, or put both
> on the same load balancer.

Removing the last domain that uses an alias removes the alias and any live challenge values with it,
and that name stops resolving. Removing one of several domains that share an alias leaves the alias
in place.

## Wildcards

Wildcards are worth pushing for, because they collapse the slow part of onboarding — the part that
depends on a human at the customer — almost to nothing.

The mechanism is a quirk of the ACME protocol: the challenge name for `*.example.com` is
`_acme-challenge.example.com`. The `*.` is stripped, not included. So
**one CNAME covers the wildcard, the apex, and every host under the domain**, instead of
one CNAME per hostname.

```
Without a wildcard                       With a wildcard
─────────────────────────────────────    ─────────────────────────────────────
shop.example.com     → 1 CNAME           *.example.com  ┐
www.example.com      → 1 CNAME           example.com    ┴→ 1 CNAME, total
api.example.com      → 1 CNAME
example.com          → 1 CNAME
                    = 4 requests to
                      the customer
```

Two limits to mention when proposing this to a customer, because both routinely surprise people:

- `*.example.com` does **not** cover `example.com` itself — add
  the apex as a second domain if it is served.
- `*.example.com` does **not** cover `a.b.example.com` — a
  wildcard matches exactly one label.

On the Traefik side a wildcard must be requested explicitly with `tls.domains`; a routing
rule alone will not produce one. See [per-load-balancer setup](load-balancer-setup.md#wildcards).

## What each domain's row says

Every domain in the table checks itself when you open the load balancer, with no button to
press. Nothing is stored: the row is what a resolver answered a moment ago, and reopening the
screen asks again.

There are two columns, and the difference between them is the difference between what DNS says
right now and what the service has actually seen.

### Challenges — will we publish for this domain at all

A domain is **offline until we have seen its delegation**. Adding it is not enough: something has
to observe the customer's `CNAME` before we will answer a `/present` for that name. A background
check looks for it every few minutes and never gives up, so this resolves itself once the record
is published — usually within minutes of it propagating.

| Chip | What is true | Who fixes it |
| --- | --- | --- |
| **waiting for delegation** | We have never seen the `CNAME`. Certificate requests for this domain are refused with a `409` naming the record to publish. | The customer publishes it. This is the normal state of a domain added five minutes ago, not a fault. |
| **active** | We have seen it. Challenges for this domain are published. | Nobody. |
| **not resolving** | We have seen it before, and the last time we looked it was gone. | The customer, when they can — their next renewal will fail until it resolves again. |

**A domain is never switched off.** Once we have seen the delegation the domain stays active, no
matter how many later checks fail. A zone being rebuilt, a provider migration or a TTL change
makes a delegation blip, and deactivating on that would stop renewals silently for weeks. "not
resolving" is information, never a countdown.

Underneath the chip the row says when we last looked, or — where a delegation has stopped
resolving — when we last saw it. That is the question anybody actually asks: not how many checks
have failed, but since when.

**Check now** appears on any domain a check could change: one that is still waiting, and one that
is **not resolving**. Press it after publishing — or republishing — the record rather than waiting
for the poller; it runs the lookup immediately and, if the delegation is there, the domain is
active before the page finishes reloading. It cannot do harm — a check that does not find the
record only records that we looked.

It is the same lookup as the **CNAME** dot below and **Check now** beside it: either brings a
domain online, and both re-check every domain sharing the delegation at once, because the
delegation is one record per alias rather than one per domain.

### The dots — what a live lookup says about the records

Every domain row carries a line of dots: a filled circle per record type it actually has, the
type named beside it, and a click that opens what that colour means and, where there is one, the
record to publish. A record type this domain does not have gets **no dot at all** — CAA with no
configured issuer, TLS with the switch off — never a grey one, because a grey dot would claim we
meant to look and did not.

Each dot is a fresh answer, asked when the screen opened (TLS is the exception — it shows the
latest scheduled observation, see [Certificate observation](#certificate-observation) below), and
it can legitimately disagree with the Challenges column beside it for the few minutes between a
customer publishing a record and our next check noticing.

**`CNAME`** — is the one delegation record the customer publishes actually there?

| Dot | What is true | Who fixes it |
| --- | --- | --- |
| green | It resolves to this domain's alias. | Nobody. |
| yellow | Nothing resolves at the challenge name. | The customer publishes the record. This is the normal state of a domain added five minutes ago, not a fault. |
| red | Something resolves, but not the alias we need. | The customer changes a record they already have — and the commonest cause is a domain deleted and added again, which issues a *new* alias while the old delegation stays published, well-formed, and permanently invalid. |
| grey | Could not be checked, or has not been checked yet — no verification resolver is configured, or the answer has not arrived. | Nobody; it is not a statement about their DNS. |

Missing and incorrect are deliberately different colours, never both red: missing is
overwhelmingly somebody mid-onboarding, and every domain passes through it on the way to green;
incorrect is a record that looks finished to whoever published it and will never work.

**CAA** — who may issue for this name. This is *not* the same vocabulary, and the difference
matters: a CAA record is optional, and its absence means anyone may issue.

| Dot | What is true | Who fixes it |
| --- | --- | --- |
| green | Either a CAA set exists and names our issuer, or no CAA record is published at all — the commonest state, and entirely correct. | Nobody. |
| red | A CAA set exists and does not name our issuer. Issuance is refused however perfect the delegation is. | The customer adds our issuer to their CAA set. |
| grey | The lookup failed or has not run. | Us, or the resolver. |

The two green states are not the same fact — "authorises us" and "no restriction" read
differently in the dot's tooltip — but they carry the same instruction, *do nothing*, which is
what a single colour is for.

Clicking a dot opens its record, with *that domain's* own challenge name and alias in it, ready
to copy — in every state, including green, where it names the record in effect. For a blocking
CAA it offers both `issue` and `issuewild` lines: `issuewild` governs wildcard issuance *instead
of* `issue` when it is present, so adding only the `issue` line adds a line that is already there
and changes nothing.

Never printed, in any dialog: the diagnostic text of a lookup that failed. That field holds our
own resolver's error, addressed to an operator, and showing it to a customer would read as a
verdict on their zone when it is a verdict on our lookup.

## Certificate observation

The **TLS** dot is not a DNS lookup. It is this service acting as a TLS client against the load
balancer's own node addresses — the ones recorded on the [load balancer](#projects-and-load-balancers)
— and reading what certificate is actually presented on port 443, one handshake per node
address, per address family, for every domain where the switch is on.

**It is off until you turn it on, per domain.** Nothing is dialled for a domain until its own
switch — in the domain editor, beside its delegation — is switched on; the connections this makes
are to machines the customer told us about, for a check the customer opted into. **It costs
nothing.** Observation is not metered and is not going to be: only a published challenge is
billed. The switch says so where you flip it, because a switch on a product that bills for other
things and says nothing about price reads as a new charge nobody agreed to.

We only ever read. No certificate and no private key belonging to the customer, or to anyone
else, is ever obtained, held or installed by this service — this is the same kind of connection a
browser visiting the site would make, and nothing about it puts us in anybody's trust chain.

**A wildcard needs a hostname to ask for.** `*.example.com` is not a name a TLS client can
request — the handshake needs one concrete label to put in the request. So switching TLS on for
a wildcard domain asks for a **test hostname**: a real name the wildcard covers, exactly one
label beneath it (`shop.example.com` — not `a.b.example.com`, which a wildcard does not cover
either), stored on the domain and reused every cycle rather than asked for again. Nothing
guesses one for you: `www` is the obvious default and it is only a guess, and a customer whose
wildcard serves `a.example.com` and never `www` would get a red dot for a name they never
deployed — the worst possible first impression of a check. Turning the switch off keeps the
stored hostname; turning it back on does not ask again.

Every enabled domain is re-observed on a fixed cadence — six hours by default, configurable by
whoever runs the service, see [Installation](installation.md#configuration-reference) — against
every node address of its load balancer, `A` and `AAAA` checked separately wherever a node has
both, because a certificate renewed on one address family and not the other is invisible to a
dual-stack visitor, including the customer checking from their own desk.

| Dot | What is true |
| --- | --- |
| grey | Never observed yet, or the last attempt did not complete — refused, timed out, or an unreadable response. |
| green | A certificate was read, covers the name asked for, and has more than 28 days left. |
| amber | Same, with 7 to 28 days left. |
| red | Expired, fewer than 7 days left, or a certificate that does not cover the name asked for at all — whatever its remaining lifetime. |

**Grey does not mean the customer's node is down.** It is the one place our own probe's failure
meets a colour, and it stays grey rather than turning red because a refused connection, a
timeout and a listener that simply is not answering on 443 all look identical from here, and none
of them tell us apart from our own transit or a firewall dropping our source address —
colouring any of that red would be a claim about the customer's infrastructure we are not in a
position to make. Click the dot: it lists every node address, family within node, so "renewed on
`v4` and not on `v6`" is one glance rather than a comparison you construct by hand, and a failed
attempt reads as **could not be examined** rather than as a blank row — the check ran and has
nothing to report, which is a different fact from the check never having run.

History — every past observation for this domain, not only the latest — is part of a team
account. Opened on a personal account, the dialog says so plainly instead of showing an empty
section.

## API keys

A key is a credential with **grants**: it reaches whatever they name, and nothing else. Create one
from a load balancer page and it gets the obvious pair — publish and withdraw challenge values for
that load balancer — which is what a key used to be, and still the right answer for most
deployments. All of a load balancer's Traefik nodes normally share one.

Keys are no longer *owned* by a load balancer, though, and the listing shows what each one can
actually do. A key granted a whole project appears on every load balancer inside it, because it can
genuinely act on all of them; one key for a project instead of four keys for four load balancers is
now a thing you can ask for. The **Authority** column is where that difference is visible, and it is
worth reading before revoking anything.

### Creating one

1. **API keys → Create key** on the load balancer page. Name it after the load balancer.
2. Optionally add source-address restrictions. The load balancer's node addresses are offered as one-click
   defaults — take them unless the nodes egress from a different address than they ingress on.
3. **The secret is shown exactly once.** The dialog will not close until you confirm
   you have stored it. There is no recovery: if it is lost, revoke the key and create another.

> **Rotating without downtime**
>
> A load balancer may hold several live keys. Create the second, deploy it to every node, confirm traffic on
> it via `last used`, then revoke the first. No coordinated cutover, no window where
> renewals fail.

The list shows the identifier, a short display prefix, its grants, restrictions, expiry and
**last used** — that last column is the useful one. A key that has never been used
points at a deployment that never picked it up; a key nobody has used in months is a candidate for
revocation.

## Recent interactions

The last section on the load balancer page, after **API keys**. Where [Live
challenges](#live-challenges) shows what is answerable right now, this is the history behind it: one row per
challenge this load balancer has published, going back as far as retention keeps the record. Each
row carries when it was published and how long it stayed up, the domains it covered, the source
address it was published from, the API key that published it, how many times it was queried, and
an outcome.

This is the screen that answers the question the rest of the console cannot. A renewal that never
happens leaves no trace in Live challenges — nothing is live — and only a thin one in the
[audit log](#audit-log), because nobody changed any inventory. Here it shows up as a
publication with a query count of zero.

| Outcome | Meaning |
| --- | --- |
| **validated** | Published, queried by the CA, then withdrawn. The normal life of a renewal. |
| **unqueried** | Published and withdrawn, but **never queried**. This is the one that matters: it almost always means the customer's `_acme-challenge` CNAME is missing or points somewhere else, so the CA never came to look. |
| **abandoned** | Never cleaned up — the sweep closed it after 24 hours. Points at the client that vanished mid-validation, not at the customer's DNS. |
| **live** | Still published, already queried. A validation in progress. |
| **pending** | Still published, nothing has asked yet. |

The **"Never queried only"** switch filters straight to the outcome above that
matters. It turns "renewals are failing somewhere" into a list of domains to go and fix, without
reading a single row by eye.

The **From** column's second line, `alias` or `challenge`,
records which name form the client sent: `alias` means it followed the customer's CNAME
and sent our alias name; `challenge` means it sent the customer's own
`_acme-challenge` name and left the aliasing to us. Both are correct and supported — it
tells you which convention that client follows, and nothing stronger.

The filter box searches domain, alias, key id and source address. Paging is server-side, so a
filter searches every interaction on record, not just the page currently on screen.

## Live challenges

What is answerable in DNS right now, with its alias, its age and the key that published it. This is
the screen that makes an otherwise invisible process visible, and it answers two questions quickly:

- **Is a renewal happening at this moment?** A value appearing and disappearing within
  a minute or two is a healthy cycle.
- **Did something fail after publishing?** A value sitting there for many minutes
  means the challenge was published but never cleaned up — the validation failed, and the
  interesting logs are on the Traefik side.

Two values at one alias is normal, not a bug: that is a wildcard and its apex being validated
together.

## Audit log

Every change to inventory and keys, with who did it and when, filterable by project, actor and
action. Secrets never appear in it. Each entry also records the roles the person held at the time,
which is what lets it answer "how was this allowed" after the role has since been edited.

Besides the obvious compliance use, it is the fastest way to answer "this used to work" — a domain
removed, a key revoked, or a restriction added usually shows up right before the failure started.

## Roles

Who may do what in this account, and to which objects.

Reading the roster needs no permission at all — knowing who else is in the room is not a privilege,
and an account whose membership nobody can see is how it ends up with a member nobody remembers
adding. Changing anything on this screen needs `ADMIN`.

A role is a name and a list of grants. Each grant is three choices and a flag:

| | |
| --- | --- |
| **Applies to** | a kind of object, or anything in the account |
| **Object** | one identifier, or blank for every object of that kind |
| **Action** | what may be done to it |
| **Reaches contained objects** | whether it extends to what that object contains |

The flag is the one worth pausing on. Without it, a grant on a project is a grant on the project row
itself and nothing beneath it — you can rename the project and touch none of its load balancers.
With it, the grant reaches every load balancer, node, domain and alias inside. Inheritance never
runs the other way: a grant on a load balancer says nothing about the project containing it.

The composer offers only actions you already hold, and the service refuses anything wider. It checks
each proposed grant against a **single** grant you hold rather than against all of them together, so
holding two load balancers separately does not let you issue authority over the project containing
them. That refusal is deliberate: those two may be the only load balancers in the project today, and
a third added tomorrow would silently fall under a grant you were never entitled to make.

A member can hold **no role at all**. That is somebody who accepted an invitation and has not been
given one yet — see [Inviting people](#inviting-people). They are in the account and authorised for
nothing in it, and their row carries the picker that fixes that.

`ADMIN` shows as *fixed* rather than as a disabled form, and the last member of it has no
**Remove** button. Both are the same rule: an account must always have someone who can administer
it, and neither editing the role's authority nor removing its last member is a thing this screen
will help you do by accident.

## Inviting people

An account of one cannot do much. **Settings → Invitations** is how a second person gets in, and it
asks for one thing: an email address.

> **It does not ask for a role, and that is deliberate.**
>
> An invitation offers *membership*. What the new person may then do is a separate decision, made
> after they have accepted, by somebody with the account in front of them. Under the earlier design
> you picked a role at the moment you knew least about what the person would be doing — a fortnight
> before it took effect, against a list of roles you might have edited in the meantime.

So joining is two steps, and the second one is yours:

1. You send an invitation. It appears in the pending list immediately, visible to every member.
2. They accept. They appear in **Members** with **no role yet**.
3. You assign a role, on that row.

**Between steps 2 and 3 they can do nothing.** They can pick the account and see its name; every
screen inside it says they hold no role and that an administrator must give them one. That is worth
knowing before somebody messages you to say the product is empty — it is empty *for them*, and
correctly so.

### What the invited person sees

They get a message with a link. Opening it shows *Join &lt;account&gt;?* with two buttons: **Join**
and **Decline**. Opening the link does nothing by itself — it can be followed by a mail-scanning
gateway, previewed by a client, or opened and closed, and it still works afterwards.

If the message never arrives — a spam filter, a corporate gateway that rewrites URLs and breaks
them, or a colleague who deleted it — **they do not need it.** Signing in as the invited address
shows the invitation on the account picker, with the same two buttons. Being signed in as the
address is stronger evidence than holding a message sent to it, so nothing is being relaxed.

### A forwarded link gives nobody anything

**Only somebody signed in as the invited address can accept.** Forwarding the message to a colleague
transfers nothing; they are told the invitation is not for them, and shown the address only partly
masked — `a***e@corp.example` — so the right person can recognise which of their own addresses is
wanted and a stranger learns nothing they did not already have.

This has an honest cost. Somebody invited at `alice@corp.example` who habitually signs in as
`alice@gmail.com` **must create an account for the work address.** There is no way to attach the
invited address to the account they already have, and there is not going to be one in this design:
holding a link sent to an address is itself proof of controlling that address, so an "add this
address to my account" button would let anybody who was forwarded the message do exactly what the
paragraph above says they cannot.

For a membership of a company's billing account that is arguably the right outcome — the membership
belongs to the work identity and survives a personal address changing — but it will generate
support questions, and the answer is "create the work account", not "we can move it for you".

### Declining, expiring, revoking and re-sending

| | |
| --- | --- |
| **Decline** | Their answer. It leaves your pending list and cannot be undone by a later click on the same link. It is **not** a block: you may invite them again. |
| **Expire** | Fourteen days of silence. The link stops working and says so, naming the account so they know who to ask. |
| **Revoke** | Yours. The link stops working immediately. No message is sent — telling somebody an offer they may never have seen has been withdrawn is noise. |
| **Re-send** | Issues a **new** link and kills the old one, and restarts the fourteen days. |

**A decline and an expiry look the same in a list and mean opposite things**, which is why declining
exists at all. Silence tells you neither that the message was never seen nor that the person said
no, and those call for opposite responses — a phone call, or leaving them alone.

Re-send kills the previous link on purpose. Re-sending is what you do when told "I never got it",
and the message that was never got is exactly the one that might have gone somewhere unintended.

### Limits

Inviting makes this service send mail to an address you chose, which is a thing that can be abused,
so it is bounded three ways: how many invitations one account may issue in an hour, how often one
address may be invited in a day **across every account**, and how often one invitation may be
re-sent. A refusal says which limit you reached and when you may try again.

The middle one is the one that will occasionally surprise a legitimate user: if a colleague is being
invited into three accounts on the same day, the third is refused. It counts across accounts because
anybody may create accounts freely, so a per-account limit would be no limit at all.

### Waiting for approval

While registration is closed, a person who has never signed in before is held until a member of
staff approves them — **including somebody you invited.** An invitation confirms an *address*; it
does not admit a *person*.

The clock does not stop while they wait. An approval that takes longer than fourteen days will
expire the invitation of somebody who did everything right, and they will sign in for the first time
to find a dead link. If that happens, re-send it: that is one click and a fresh fortnight.

## Settings

`ADMIN` only. Roles and who holds them live on the [Roles](#roles) screen — they are data in this
account, and editable there.

What remains on this screen is an **effective configuration** view, with secrets redacted, showing
the zone, the resolver and the trusted proxies the running process actually loaded. It no longer
reports a group-to-role mapping, because there is none: nothing outside this account decides who is
in it.

> **Revoking access is not instant**
>
> Membership is re-checked when somebody SELECTS an account, not on every request. Removing a
> member takes effect the next time they switch accounts — bounded by
> `ACME_SESSION_MAX_LIFETIME`, up to that long a still-live session keeps what it already had.
>
> **Active sessions** shows your OWN sessions and lets you end them, which is what you want after
> losing a laptop. Ending somebody else's is deliberately not here: a session belongs to a person
> rather than to an account, so no role in this account carries authority over one. If access must
> stop immediately, remove the membership and ask them to sign out.

## Recipes

### Onboard a new domain

1. Open the load balancer → **Add domain**. Consider a wildcard if the customer has several
   hostnames — and if they want one, tick the counterpart checkbox so the apex and the wildcard
   are added in the one action. It is off by default; see **Domains** above for what the customer
   is agreeing to publish.
2. Click the domain's **`CNAME`** dot and copy the record it prints; send it to whoever
   administers that zone. It names the exact target for this domain, so there is nothing else to
   gather first.
3. Wait for the dot to turn green — the row checks itself each time you open the load balancer.
   Red means they published something, at the wrong target; the dot's own dialog shows both
   values side by side.
4. Only then point the Traefik router at the `acmesvc` resolver.
5. Watch **Live challenges** for one full publish-and-withdraw cycle.

### Rotate a credential

1. Create a second key on the load balancer; store the secret.
2. Deploy it to every node.
3. Wait for **last used** on the new key to update.
4. Revoke the old key.

### Give someone access

1. Decide what they actually need. One of the seeded roles usually fits — `CONFIG_ADMIN` for a team
   that manages its own load balancers and domains, `READ_ONLY` for someone who only needs
   visibility.
2. If neither does, make a role that does. **Roles → New role**, then add grants: naming one project
   and ticking "reaches contained objects" gives someone everything inside it and nothing outside.
   This is the answer to "that is too broad" — it no longer has to be.
3. Ask them to sign in to the console once, so the service learns their identity-provider subject.
4. **Roles → Members → Add**, naming that subject and the role. They have access as soon as they
   select the account. Removing the membership takes effect the next time they switch accounts.

### Investigate a renewal that is not happening

1. **Live challenges** — is anything being published at all?
2. **Audit log**, filtered to the project — did somebody change something?
3. The domain's dots — did the `CNAME` or CAA record change? Click **Check now** to re-check
   immediately rather than waiting for the poller. A red CAA dot stops issuance no matter how
   right everything else looks.
4. The key's **last used** — is that load balancer talking to us at all?
5. Still stuck: [troubleshooting](troubleshooting.md).

---

4 · Admin console guide — [← Install & configure](installation.md) · Next: [Troubleshooting →](troubleshooting.md)
