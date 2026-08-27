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
- [The DNS plan and verification](#the-dns-plan-and-verification)
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
more roles, and holding several at once is normal — they are additive, not exclusive. Keycloak
groups do not grant anything. If the account you expect is not in the picker, nobody has added you
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
| DNS plan + run verification | yes | yes | yes | yes |
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
        ├── Domains                what this load balancer answers for
        ├── DNS plan               what the customer must publish + live status
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
those addresses become the address records in the DNS plan, and they are offered as the default
restriction when you create an API key. A wrong address here means a valid certificate on a host
nobody reaches.

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
counterpart is a real decision, not a formality: it puts a wildcard address record into the plan
the customer publishes over their whole zone, and it means a stolen key covers every hostname
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

## The DNS plan and verification

The **DNS plan** panel on a load balancer page is the answer to "what exactly do I ask the
customer for". It is generated from the inventory, so it cannot drift out of date.

```
shop.example.com.                  300  IN  A      203.0.113.10
shop.example.com.                  300  IN  A      203.0.113.11
shop.example.com.                  300  IN  AAAA   2001:db8::10
shop.example.com.                  300  IN  AAAA   2001:db8::11
example.com.                       300  IN  A      203.0.113.10
example.com.                       300  IN  A      203.0.113.11
example.com.                       300  IN  AAAA   2001:db8::10
example.com.                       300  IN  AAAA   2001:db8::11
*.example.com.                     300  IN  A      203.0.113.10
*.example.com.                     300  IN  A      203.0.113.11
*.example.com.                     300  IN  AAAA   2001:db8::10
*.example.com.                     300  IN  AAAA   2001:db8::11
_acme-challenge.shop.example.com.  300  IN  CNAME  k7m3q9x2r4tv8w1p.acmeservice.cloudpowered.services.
_acme-challenge.example.com.       300  IN  CNAME  x4n2vq7btm93kd5r.acmeservice.cloudpowered.services.
```

Note the counts: three domains, but only two CNAMEs, because the wildcard and the apex share one.
Address records are included because a domain whose challenge delegation is perfect but whose
address records point elsewhere produces a valid certificate on a server nobody reaches — the two
failure modes belong in the same table. A node recorded with an IPv6 address gets an
`AAAA` row alongside its `A` row, automatically; a node with IPv4 only gets
just the `A` row.

**Verify** resolves every row live and reports what it found. Results are never stored:
DNS is somebody else's mutable state, and a green tick from yesterday means nothing.

| Status | Meaning | What to do |
| --- | --- | --- |
| **Correct** | The record exists and points where it should. | Nothing. |
| **Missing** | The name does not resolve at all. | The customer has not published it yet, or it has not propagated. Re-check later before escalating. |
| **Incorrect** | Something is there, but not what we need — shown with what was found. | Usually a typo in the target, or a TXT record created directly instead of a CNAME. Send the observed value back to the customer; it is normally self-explanatory to them. |
| **Undetermined** | The lookup itself failed or timed out. | Not a customer problem. Retry; if it persists, the service's own resolver is unreachable. |

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

`ADMIN` shows as *fixed* rather than as a disabled form, and the last member of it has no
**Remove** button. Both are the same rule: an account must always have someone who can administer
it, and neither editing the role's authority nor removing its last member is a thing this screen
will help you do by accident.

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
2. Copy the DNS plan and send it to whoever administers that zone.
3. Click **Verify** until the CNAME row is green.
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
3. The load balancer's **DNS plan → Verify** — did the customer's records change?
4. The key's **last used** — is that load balancer talking to us at all?
5. Still stuck: [troubleshooting](troubleshooting.md).

---

4 · Admin console guide — [← Install & configure](installation.md) · Next: [Troubleshooting →](troubleshooting.md)
