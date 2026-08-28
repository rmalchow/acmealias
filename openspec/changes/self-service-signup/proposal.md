## Why

### Nobody can sign up, and nothing is refusing them

The obvious reading of the current state is that self-registration is built and switched off. It is
not. `development/authentik/blueprints/acmeservice.yaml` defines exactly **one** flow —
`default-authentication-flow` — and no enrolment flow at all. A person who reaches the sign-in page
sees a username field, a password field, and no way to become somebody who has either.

The door is not locked. **There is no door.**

This matters because the project has been reasoning as though the closure were a decision.
`public-edition` U9 describes a switch that turns registration off at the server and calls the closed
state deliberate; `authentik-and-self-signup` K6 described a self-registered person being *refused* by
the service, with the refusal's wording treated as a deliverable. Neither describes what actually
happens, because neither path can be reached. The first customers arrive by invitation not because a
policy chose that, but because it is the only mechanism that exists.

### And the refusal that was specified is now the wrong outcome anyway

`authentik-and-self-signup` K6 said a self-registered user "belongs to nothing" and is refused, its
groups reported back as an empty list. `personal-and-team-accounts` deleted that state:
`store.EnsurePersonalAccount`, called from `internal/oidcauth/flow.go`, materializes a personal
account on first sign-in. So the two possible outcomes today are *"there is no way in"* and, the
moment a flow exists, *"anybody at all is immediately a working customer"*. Neither is what a
commercial service wants on its first day.

The missing thing between them is a **decision about the person**, made by a human, once.

### Why approval is the interesting half

Adding an enrolment flow is a blueprint edit. The part worth designing is what happens in the gap
between somebody finishing that flow and somebody being able to use the product.

Two things happen in that gap and they are constantly conflated:

- **Email verification** proves the address belongs to the person. It is a machine's judgment, it is
  instant, and it is not optional.
- **Approval** is us deciding we want them as a customer. It is a human's judgment, it is slow, and it
  is temporary — the whole point is to remove it once we trust the volume.

Doing only the first lets anyone in. Doing only the second has staff reviewing addresses that may not
exist. Both, in that order, so the queue a human reads contains only real people.

## What Changes

### The flow, end to end, with real values

A stranger at `console.localhost` clicks *Sign up* — a link on the sign-in form, which is the part
that does not exist today — gives `dana@corp.example` and a password, and receives a verification
message. She clicks the link. At that moment:

1. Her authentik user exists, her address is verified, and she is **inactive**. The flow tells her her
   registration is being reviewed.
2. She tries to sign in. authentik refuses, and says why — not a generic failure, which is what she
   would otherwise get and would read as a wrong password.
3. Nothing has happened in our service. There is no session, no `accounts` row, nothing to clean up if
   she is never approved.
4. Somebody activates her user in authentik.
5. She signs in. `EnsurePersonalAccount` materializes `dana-corp-example`, and she is an ordinary
   customer from her first request — there is no half-state for her to be in.

With the gate off, step 1 creates her active and steps 2–4 do not happen.

### The gate lives in the identity provider, and the two refusals are different things

There are two refusals in this product, and the temptation is to build them out of one mechanism
because they both end in somebody not being able to work. They are about different objects:

- **"Not approved yet"** is about an **identity**. May this person authenticate at all? We know
  nothing about them; there is no account for it to be a fact about. That belongs in authentik.
- **"Suspended"** is about an **account**. A known customer's workspace does not work, and they are
  owed an explanation and a remedy. That belongs in our service, where it already is.

Putting approval in our service means an unapproved stranger completes enrolment, **receives a
session, and gets an `accounts` row written for them** — account, slug, personal-account invariants,
audit entries — for a person we have explicitly not decided to admit. Anybody able to complete a
signup form could make us write rows. Held inactive at the provider, they never reach us at all: the
gate stands in front of the thing it protects rather than inside it.

The consequence is that **nothing in our schema changes**. No column, no migration, no new clause in
`store.Usable` — which also leaves the question C6 deliberately left open, what a staff suspension
means for a free account, undisturbed and still `staff-plane`'s to answer.

### The one thing that must not be skipped

authentik's default treatment of an inactive user is a sign-in that simply fails. To the person that
is indistinguishable from a wrong password, so they retry, reset their password, retry again, and mail
support — all of it work our silence created, at the exact moment we wanted to say one sentence: *we
have your registration, somebody is looking at it.*

That is a fact about a default, not about where the gate belongs, and authentik can be made to say it.
So it is a requirement on the flow rather than an argument against the design, and it is the part of
this change that must not be quietly dropped: skipping it produces the whole support load while
keeping none of the benefit.

### An invited person does not queue

Approval looks at strangers. Somebody accepting an invitation is not one — a paying customer named
their address and took responsibility for them — so holding them inserts our latency into another
customer's onboarding and asks a reviewer with no context to second-guess somebody who had plenty.

This falls out of which flow created them: enrolment from the sign-in page creates users inactive, an
invitation link creates them active. Two flows, one differing key, rather than a rule to remember.

### Turning it off is an action, not just a setting

The gate is one key on the enrolment flow — `create_users_as_inactive`. Turning it off stops the flow
creating inactive users; it does **not** activate the people it created earlier.

That is right in one direction and a trap in the other. Turning it *on* must never lock out existing
customers, and does not. Turning it *off* leaves the backlog waiting for a gate that no longer exists,
so switching it off is accompanied by activating whoever is still held — an explicit action somebody
takes, rather than a config change that silently admits a batch of real people at a moment when nobody
is looking at them.

### What this does not open up

Self-signup is a door strangers walk through, and the abuse question is already answered elsewhere:
five projects on a personal account, per-domain billing on a team account, and a domain that answers
no challenge until its delegation has been seen. An **unapproved** account can do none of it.

One surface is genuinely new: the enrolment flow sends mail to an arbitrary address on an
unauthenticated stranger's request. That is authentik's reputation policy to bound, already specified
as `public-edition` task 1.4, and it is required here rather than deferred.

## Capabilities

- `identity-and-roles` — enrolment exists, an address is verified before it can authenticate, and a
  self-registered person cannot authenticate until approved; an invited one can immediately

## Impact

- **Blueprint** — an enrolment flow linked from the sign-in form, email verification, inactive
  creation, the message an unapproved person meets, a reputation policy, outbound mail
- **Nothing in the schema, the predicate, or either console.** An earlier version of this proposal
  added `accounts.approved_at`, a clause in `store.Usable`, a third suspension reason, a customer
  screen and a staff queue. Putting the gate in front of the service instead of inside it removes all
  of it
- **Takes over** the enrolment tasks currently duplicated between `authentik-and-self-signup` §5 and
  `public-edition` §1, which specify overlapping halves of the same work
