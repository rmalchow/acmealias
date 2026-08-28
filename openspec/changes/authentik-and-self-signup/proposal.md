# Replace Keycloak with authentik in development, and turn on self-registration

## What this changes, in one paragraph

The development stack signs people in through **Keycloak**, an identity provider from Red Hat. The
production design has already decided on a different one — **authentik** — and has decided it in
detail. This change makes the development stack run the software the production design named, and
switches on the path by which a stranger creates their own account, so that path exists and can be
looked at long before anyone depends on it.

No service code changes. That is not an aspiration, it is an observation: the backend already speaks
plain OpenID Connect and names no provider anywhere outside a comment.

## Terms, before they are used

- **OpenID Connect (OIDC)** is the protocol by which our service hands sign-in off to somebody else.
  Our service never sees a password. It redirects the browser to the provider, the provider
  authenticates the person and redirects back with a signed **identity token**, and our service reads
  that token.
- An **identity provider (IdP)** is the software on the other end of that. Keycloak and authentik are
  both identity providers. They are interchangeable in principle, which is the whole point of the
  protocol, and differ enormously in practice in how they are configured and how their login pages
  are built.
- A **realm** (Keycloak) or a **brand** (authentik) is one isolated configuration: its own users, its
  own groups, its own login page, its own look.
- An **enrolment flow** (authentik) is a sequence of steps a person walks through to create their own
  account — the thing this proposal calls *self-registration* or *self-signup*. Keycloak's equivalent
  is a "user registration" toggle on the realm.
- A **blueprint** (authentik) is a YAML file describing objects that should exist — groups, users,
  applications — which authentik applies at start-up and keeps applied. Keycloak's rough equivalent
  is the realm export JSON at `development/realm/acmeservice-realm.json` that the stack imports
  today.

## Why

### The one path we cannot afford to get wrong is the one path we never exercise

`public-edition`'s design decision **U12** already commits production to authentik, at
`auth.acmealias.com`, and works the consequences out at length: customers are `external` users and
staff are `internal`; two applications each bound to a group; the staff application has no enrolment
flow at all, "not a disabled one — none"; brands separate the hostnames.

None of that is exercised by anything. The development stack runs Keycloak, so every test that
signs a person in — including the whole acceptance suite — proves something about software the
service will not ship with.

The gap is not theoretical, and the sharpest example is the group claim. Roles are derived from a
list of group names in the identity token. Keycloak produces that list through a "Group Membership"
mapper, and the realm at `development/realm/acmeservice-realm.json` sets its `full.path` option to
`false`, which is why the claim carries `acme-admin` rather than `/acme-admin`. authentik builds the
same list a different way. The setting `ACME_OIDC_GROUP_PREFIX` in `development/.env` exists
precisely because providers disagree about this, and `backend/internal/oidcauth/groups.go` says so in
a comment naming both products.

So the service has a knob whose entire purpose is to absorb a difference between two providers, and
it is only ever turned against one of them. A person whose token says `/acme-admin` when we expected
`acme-admin` is not shown a helpful error — they are refused with "you are in none of the configured
groups", which reads as a permissions problem and is actually a claim-shape problem. Finding that out
on the day the first customer signs in is the expensive way.

### Self-registration should be built while nobody is using it

`SEQUENCE.md` already states the intended posture: `public-edition`'s registration switch and payment
integration "wait until strangers arrive. Both are built and inert by then, so opening them is
configuration."

This change is the "built" half for registration. Turning it on in development costs nothing —
there are no strangers — and it makes the path something that can be looked at, styled and corrected
now, rather than written under time pressure later.

It also surfaces one consequence early, and it is a consequence worth staring at. A person who
registers themselves is placed in **no group**. No group means no role. No role means our service places them in their own personal account and nothing else.

**This paragraph used to end differently, and the difference is worth stating.** It said such a person
is REFUSED, reporting the empty list of groups it observed. That was true when it was written and is
not any more: `personal-and-team-accounts` materializes a personal account on first sign-in, so
somebody who signs themselves up arrives in a working product rather than at an explanation of why
they cannot use one. The empty-handed state that paragraph described was deliberately deleted.

What survives is the property that mattered. Account membership is rows we write, never something
derived from a group claim — `tenancy-and-permissions` P18, which `public-edition` U12 restates when
it observes that even a fully compromised identity provider "can create a user, and that user belongs
to nothing." Read precisely, U12 is still exactly right: the provider can create a person and cannot
place that person inside anybody ELSE'S account. The personal account they land in holds nothing but
what they put there. What lets somebody into an account with other people's work in it is
`account-invitations`, which is deliberately not in this change.

### Branding is only real once there is something to brand

The request that started this was for a sign-in and sign-up experience that looks like the product
rather than like somebody else's admin console. In authentik that is a **brand**: a configuration
bound to a hostname carrying the title, logo and stylesheet. It cannot be evaluated in the abstract.
Putting a brand on the development stack's console hostname makes it a thing on a screen that can be
judged and changed.

## What this does not change

- **No service code.** `backend/` keeps every line it has. The Keycloak mentions in it are six
  comments, and they are corrected as prose, not as behaviour.
- **No production deployment.** There is no authentik instance at `auth.acmealias.com` and this change
  does not create one. It changes what `./scripts/dev.sh up` runs and what `docs/` describes.
- **No invitations, no account membership, no payment.** Those are `account-invitations` and
  `public-edition`. A self-registered user in this edition can sign in to the identity provider and
  is then refused by our service, and that is the correct and intended end of the story here.
- **No change to what the four seeded development users can do.** `admin-user`,
  `config-admin-user`, `key-rotation-user` and `read-only-user`, their four groups and the password
  `devpass` are reproduced exactly, because the acceptance suite and
  `backend/internal/authz/roles_test.go` assert them by name.

## What this costs, stated plainly

**The development stack gets heavier.** Keycloak is one container. authentik is a server, a worker
and a Redis, plus a database. `./scripts/dev.sh reset` gets slower for everybody, on every reset.

That deserves more than a shrug, because a slow and fragile reset is not a hypothetical problem in
this repository — it is the reason task 11.4 of `tenancy-and-permissions` sat unproven: the
acceptance suite's `TestMain` resets the stack, the reset was expensive enough that it was run with
`ACCEPTANCE_KEEP_STACK=1` against a shared stack instead, and the run proved nothing in either
direction.

Two things make it acceptable. authentik's supporting services are reused rather than added where
possible — its database is another database inside the Postgres container the stack already runs, not
a second Postgres (design K4). And the alternative cost is worse: an identity provider that is only
ever configured by hand in production, against which nothing has ever run.

## The riskiest part, named up front

The acceptance suite signs in without a browser. It does so by fetching Keycloak's login page and
pulling the `action` URL out of the one HTML `<form>` on it with a regular expression
(`backend/acceptance/signin.go`), then posting the username and password to that URL.

**authentik does not serve such a page.** Its login is a browser application that talks to a JSON
API — the *flow executor* — fetching one step at a time and posting answers back to
`/api/v3/flows/executor/<flow-slug>/`. There is no `<form action=...>` to find.

So `signin.go` is not edited for this change, it is rewritten against a different protocol. That is
the single largest piece of work here and the one most likely to need a debugging pass against a
running stack. It is called out here rather than discovered in the tasks list because it is the part
that could make this change take a day instead of an hour.
