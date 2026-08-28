# Build order

The product is **ACME DNS-01 challenge handling, plus certificate observation.** A customer
publishes one `CNAME` into our challenge zone, once, and keeps their DNS wherever it already is.
We answer the one question a certificate authority asks about that name, and afterwards we look at
what they actually serve.

That is smaller than this repository once planned for, deliberately. What was removed and why is at
the bottom.

## The order

1. **`tenancy-and-permissions`** — the floor, and the only change that is substantially cheaper now
   than later, because it rewrites the shape of every table while the only data is ours.
   *Implemented: 81 of 81 tasks. The acceptance suite runs green on the branch, from a destroyed
   database through a fresh migration chain — which is what finally showed that four cross-account
   reads had been silently emptied by row-level security, one of them stopping certificate issuance
   altogether. See task 11.4.*
2. **`layered-configuration`** — configuration in files that overlay, with `${VAR:default}`
   placeholders. It comes before the account rework for one concrete reason: that change needs the
   project quota to be configuration rather than a constant (C13), and this is where a setting like
   that acquires somewhere to live. It changes no behaviour — every `ACME_*` variable keeps its
   name, its meaning and the highest precedence — which is what makes it safe to do first.
3. **`personal-and-team-accounts`** — what an account IS, and what makes one work. Every user gets
   an implicit personal account; a team account is created by paying and works only while funded.
   It also removes the internal edition, which is why it comes before everything rather than
   alongside it: "every user has their own account" and "everybody shares the one account created
   at migration time" cannot both be true, so there is no order in which this is optional.
4. **`collapse-project-and-loadbalancer`** — delete a level. The inventory is
   `account → project → load balancer → everything`, and the middle object holds a slug, a name and a
   list of children: every actual thing a customer configures hangs off the load balancer. A project
   is a folder, and the ACCOUNT is what folders were for — grouping one customer's inventory was the
   only job it had, and `personal-and-team-accounts` took it. The surviving name is `project`,
   because we do not operate a load balancer: the nodes are the customer's, and the word is wrong for
   anyone running a single host or a CDN origin.

   It is here because it is cheap exactly now and never again. P2/P3 already materialized
   `project_id` onto every descendant for the RLS predicate, so the parent link exists on every row
   and the migration mostly drops a column. And there is no customer: the API path change is a rename
   rather than a deprecation, and the grant-grammar change rewrites rows nobody depends on. Every
   change after this one — a contract with readers, roles written in the grammar, billing against
   this shape — makes it more expensive and none of them makes it less necessary.
5. **`transactional-email`** — one sender identity, one footer, one subject grammar across the
   identity provider's mail and the service's own. It sits immediately before invitations because
   that is the first thing that sends mail to a stranger, and because the part of it that fails
   fails SILENTLY: a message that does not authenticate as the domain in its own `From` is filed as
   spam rather than bounced, and a domain that starts in spam folders stays there. The DNS records
   are the long-lead item. There is no version of this that is cheaper later.
6. **`account-invitations`** — the entry path into a TEAM account. While self-registration is
   closed, an invitation is the only way a person joins one. It now depends on the change above,
   because team accounts do not exist without it.
7. **`self-service-signup`** — the front door. Nothing currently refuses a stranger who wants to sign
   up; there is simply no enrolment flow in the blueprint, so the product is closed BY OMISSION rather
   than by any decision — which is why two changes above describe the closure wrongly. This adds the
   flow, linked from the sign-in form, and holds each new person inactive until somebody approves
   them; later that gate is switched off and the door is simply open. The gate is the IDENTITY
   PROVIDER'S, because "may this person authenticate at all" is a fact about an identity, while
   "suspended" is a fact about a known customer's account and stays ours. Putting it there also means
   no `accounts` row is ever written for somebody we have not admitted, and nothing in our schema or
   in `store.Usable` changes. It sits after invitations because accepting one WAS a vouching that
   bypassed the gate — a position S7 has since reversed: there is one enrolment flow and everybody
   walks through it, invited or not, and what an invited person skips is the confirmation mail and
   only that.

   *Implemented: 29 of 34 tasks, and the shape of what is left is worth reading rather than
   counting.* The flow exists, the gate is one key on its `user_write` stage, and the whole path —
   register, confirm, be refused, be approved, sign in, hold exactly one account that did not exist
   a moment earlier — is walked by `TestSelfEnrolmentWaitsForApproval` in the acceptance suite,
   which passes.

   **Task 2.2 is fixed**, and this entry said otherwise for longer than it was true. The gate moved
   off `is_active` onto a path policy plus a deny stage bound after the password, which is what lets
   a real sentence be rendered where Django's `ModelBackend` had forced "Failed to authenticate."
   before any later stage could run. A wrong password and an unknown identifier still return
   identical bodies, so the enumeration oracle stays shut.

   **What is left is one coherent piece: the invited person's way in.** Group 3 and tasks 7.4–7.5
   all describe somebody who follows an invitation link, registers, and skips the confirmation mail
   because the link already proved control of the address. None of it is built — the blueprint
   defines no invitation stage at all — so an invited person today walks the ordinary front door
   and is held for approval like a stranger. That is safe and it is not what `account-invitations`
   V3 promised.
8. **`domain-setup-feedback`** — telling a customer whether their delegation actually works. It
   sits immediately before the first customers rather than after them, because the very first thing
   any customer does is publish one `CNAME`, and this is the only thing that tells them whether they
   did it right. Without it the failure is silent for weeks and then arrives as an expired
   certificate. It depends on nothing above it — the check is already built and merely hidden — so
   its position here is about when it is NEEDED, not what it needs.
9. **`domain-activation`** — a domain is offline until we have seen its delegation, then stays
   online. This is the third of the three controls that make the abuse model manageable, alongside
   five projects on a personal account and per-domain billing on a team one; it is the one that
   makes the other two mean anything, because without it a domain nobody controls is
   indistinguishable from one somebody does. It pairs with the change above — that one tells a
   customer what is wrong, this one decides whether we do any work for them at all.
10. **`swagger-ui`** — a reader for the contract that is already published. `/api/v1/openapi.yaml`
   serves a hand-written document nobody can comfortably read: 657 lines of YAML in a browser tab is
   a published contract the way a filing cabinet is a published archive. It sits here because the
   audience is the first customer who wants to program against the service rather than click, and
   because it is a shop window — one of the few pages a technical evaluator reads before deciding to
   build against us, which is why it is styled like the product rather than left stock, and why the
   assets are vendored rather than fetched from a CDN that would blank the page on an air-gapped
   deployment.
11. **`api-key-kinds`** — two named kinds where there is one expert kind. A key is a subject holding
    grants, which is the right general model and leaves the COMMON case — a load balancer publishing
    for one project — to be assembled by hand out of the grant grammar. So: a PROJECT key, made on
    the project's page, bound by a real foreign key so it dies with the project; and a SYSTEM key,
    made in a team account's settings, holding a role like a member who is not a person.

    The project key can optionally be limited to the union of its project's node addresses, resolved
    when a request arrives rather than copied — today those CIDRs are typed in by hand from numbers
    we already store, and the copy is right until a node is added, at which point the symptom is a
    renewal that silently does not happen and surfaces weeks later as an expired certificate. It sits
    before the change below because that one hands out a credentials file, and a project key is what
    belongs in it.
12. **`acme-dns-protocol`** — speak the protocol the ecosystem already speaks. The challenge API
    implements lego's `httpreq`, which covers lego and Traefik and leaves every other client needing
    an adapter written by us, one at a time. But acme-dns is a server with THE SAME DESIGN as this
    product — a permanent `CNAME` into a zone somebody else runs, updated over HTTP — and its API has
    become the de facto standard for the pattern. We arrived at their architecture independently and
    then chose a different spelling for the one call it needs.

    The arithmetic decides it: native adapters mean a cert-manager WEBHOOK (a deployable apiserver
    extension), a Caddy libdns module, an acme.sh plugin and a Posh-ACME plugin — four projects with
    four release cycles. Speaking acme-dns's `/update` is ONE endpoint, and acme.sh, Certify The Web,
    cert-manager, lego, Posh-ACME, Sewer, Traefik and WACS all work the day it ships; cert-manager's
    support is built in, needing only a `host:` line. It sits immediately before the first customers
    because which clients work is what decides whether somebody can adopt this at all.
13. **First customers, on challenge answering alone.** Worth pausing at rather than passing through:
   a renewal has about thirty days of slack, so a service interruption of a few hours costs nobody
   anything. Nothing else in this list is that forgiving.
14. **`domain-diagnostics`** — one row per domain, and everything true about it on that row. It
   deletes the DNS plan — the console panel, both endpoints, the zone rendering — because the panel
   restated per RECORD what the domain table above it already said per DOMAIN, and because its
   address rows could only ever find that a customer's DNS disagrees with the node addresses that
   same customer typed in. What a customer must publish becomes a property of a domain.

   It also **absorbs `health-checks`**, which is archived unimplemented. That change designed
   certificate observation correctly and presented it as a screen of its own — a per-node
   certificate view, an uncovered-domains report, an approaching-expiry report — and all three
   collapse into one status dot beside the CNAME and CAA dots on the domain's row. "Covered by no
   certificate" is that dot red; "expiring soon" is the same dot amber. Its position in the list is
   unchanged and its reasoning survives under new numbers: one handshake per address family, hours
   apart, nothing acting on the result.

   The one inversion worth naming here: `health-checks` probed once per node address and read the
   covered names out of whatever certificate came back, rejecting a per-domain probe on cost. The
   TLS check is now **explicitly enabled per domain**, which dissolves that arithmetic and lets the
   probe present the domain as the server name — so a customer running twenty sites behind one
   address is asked about theirs rather than about whatever the default virtual host serves.
15. **`metering-and-credits`** — needs something to meter, and the billable shapes only settle once
   observation exists. Deferred this far because credits are granted by hand while the customers
   are people we know.
16. **`public-edition`** — what stops being avoidable when the next customer is somebody we have
    never spoken to: a self-service enrolment flow at the identity provider, a real payment provider
    behind the checkout that currently takes no money, protective limits that hold whether or not
    anybody has paid, the production identity provider and the rest of the staff/customer separation,
    and a marketing site carrying the commitments other changes describe as "published". It no longer
    has anything to do with editions — `personal-and-team-accounts` removed the second one — and its
    tenancy, balance, ledger and delegation-checking halves have all gone to the changes above it.

`staff-plane` sits outside the numbering. Its suspension button and blocklists are needed **before
the first customer we do not know personally**, wherever that falls; its support-access machinery
can follow.

`authentik-and-self-signup` is **done in the half that matters and deferred in the half that does
not**. The identity provider is swapped, both consoles have their own OIDC client, and the
acceptance suite signs in through authentik's flow executor — all of which had to land before
anything else, because a suite that cannot sign in cannot prove that the next change broke nothing.
Self-registration is a separate concern and waits for `public-edition`. The change originally read: It
replaces Keycloak with authentik in the development stack — the provider `public-edition` U12 already
committed production to — and turns on self-registration so that path is built and inert rather than
unbuilt, which is the posture the triggers below already assume for it. It is placed here for one
reason: `account-invitations` is about turning an authenticated stranger into a member of an account,
and it is worth having the stranger arrive through the software that will actually produce them
before building the machinery that adopts them. It is deliberately **not** a prerequisite for
answering challenges for the first customers, who are people we know and enrol by hand.

## Triggers, not dates

- **`metering-and-credits`** waits until manual credit grants stop being reasonable — a count of
  customers, not a date. Note that the BALANCE arrives earlier, with
  `personal-and-team-accounts`: that change adds the number, the checkout that raises it and what
  happens at zero. What waits is the ledger that draws it down.
- **`public-edition`'s enrolment flow** and payment integration wait until strangers arrive. Both are
  built and inert by then, so opening them is configuration. One correction to what this used to say:
  the switch that gates strangers is **not** `ACME_ALLOW_ACCOUNT_CREATION`. That is built, and after
  `personal-and-team-accounts` it gates buying a TEAM account — every person who can enrol at the
  identity provider gets a free personal account regardless. The front door is the provider's
  enrolment flow, which does not exist yet, so the service is closed by accident rather than by
  decision. `public-edition` U9 works that out.
- **`staff-plane`'s suspension path** must exist before the first stranger. Discovering it is
  missing during a live phishing incident is the wrong time to write it.

## What was removed, and what that bought

`dns-records` and `satellite-sync` are archived unimplemented. `health-checks` was cut down to its
certificate half, and then archived unimplemented in turn — absorbed rather than dropped, by
`domain-diagnostics`, which builds the certificate half as a dot on a domain's row instead of a
screen of its own. Its own note records what survived and under which numbers.

The service was going to host customers' zones: real records, a replicated fleet to answer them,
health checks steering those answers. That is gone.

- **The step change in risk went with it.** The old order had a phase after which "a mistake takes a
  customer's domain off the internet". Nothing here does that. The worst failure now is that
  renewals stop until someone notices, with weeks of slack.
- **DNSSEC stopped being a problem in any form** — no delegation, no delegation signer, no
  `SERVFAIL` for a customer who followed the instructions.
- **The nameservers stopped needing to be diverse.** One domain, `acmealias.com`, rather than two at
  two registrars. The name says what the service does, which the old one did not.
- **Quorum, hysteresis, fail-open and failover answers went** — every one existed because check
  results changed what the internet was told.
- **`zone.active` went**, and with it the only charge that drained a balance while a customer did
  nothing.

Roughly 140 tasks of designed work removed, and the single largest source of irreversible risk for
one person running this alone.

## What this sequence is protecting against

One person can hold one change in their head at a time. The value of the order is that each change
is implemented while its reasoning is still recent — and that the change with the largest
irreversible surface, the data model, is decided first.
