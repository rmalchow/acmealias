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

A stranger at `console.localhost` clicks *Sign up*, gives `dana@corp.example` and a password, and
receives a verification message. She clicks the link. At that moment:

1. Her authentik user exists and her address is verified. She can authenticate.
2. She signs in. `EnsurePersonalAccount` materializes her personal account, `dana-corp-example`,
   with `approved_at` **null**.
3. The console does not show her the product. It shows her one screen: her registration is being
   reviewed, and roughly how long that takes.
4. A member of staff opens the staff console's review queue, sees `dana@corp.example`, and approves.
5. Dana's next request works. Her personal account is an ordinary personal account and always was —
   nothing about it is special once `approved_at` is set.

With approval switched off, steps 3 and 4 do not exist: `approved_at` is written at creation and Dana
goes from step 2 to step 5.

### The gate lives in our service, not in authentik

authentik can do this natively — an enrolment flow whose `user_write` stage sets
`create_users_as_inactive`, and an administrator flips `is_active` afterwards. It is nearly free. It
is rejected, for three reasons that compound:

- **The person is told nothing.** An inactive user does not get a message; they get a sign-in that
  does not work. That is indistinguishable from a wrong password, so they will retry it, reset their
  password, retry again, and then mail support — every one of which is work created by our silence.
- **The reviewer works in the wrong place.** authentik's administration interface is a different
  plane from the staff console, with its own access, and it shows an identity rather than a
  registration. Whoever is reviewing wants to see who signed up and act on it where they already are.
- **Nothing is recorded where we look.** Approving a customer is exactly the class of decision
  `staff-plane` exists to make accountable, and a flag flipped in authentik's own log is not in our
  audit trail.

The decisive argument is the first principle underneath all three: **authentik's job is proving
somebody is who they say they are. Whether we want them as a customer is our decision, about our
product.** Putting it in the identity provider files a product judgment under identity, which is the
same mistake `tenancy-and-permissions` P18 refuses when it declines to derive membership from
directory groups.

### Approval is not a new state — it is the existing one, with a third reason

The service already has exactly one place that answers "does this account work" (`store.Usable`,
design C3), and one place that says why not (`store.SuspensionReason`), and a console screen that
takes the reason *from the service* rather than inferring it from the balance. C6 anticipated this
precisely: the two suspensions are "a conjunction, written here once, so that adding a third reason
later is one clause rather than an audit of every call site."

This is that third reason. One nullable column, one clause:

```
usable(account) = approved_at IS NOT NULL
                  AND (kind == personal OR (credits > 0 AND NOT staff_suspended))
```

The new conjunct sits **above** the kind test, so it stops a personal account — which `staff_suspended`
deliberately does not. That difference is the subject of a design decision rather than an accident.

### "Awaiting review" and "suspended" are different sentences

They are the same boolean to the database and must never be the same sentence to the reader. Somebody
who signed up an hour ago and is told their account is **suspended** reads that as *punished*: they go
looking for what they did wrong, and they mail support to appeal a penalty that was never imposed.

So the reason distinguishes them, and the console renders a different screen — with no checkout
button, because there is nothing to buy. The remedy for an unfunded account is money; the remedy here
is waiting, and the screen's job is to say so and set an expectation.

### An invited person does not queue

Approval gates people who arrive as strangers. Somebody accepting an invitation was vouched for by a
paying customer who took responsibility for them, and making them wait for staff review inserts a
delay into somebody else's onboarding and puts an entry in the queue that second-guesses a decision
made with more context than the reviewer will ever have.

So accepting an invitation approves the accepter's personal account as a side effect, and the queue
contains exactly the strangers it is for.

### Turning it off is an action, not just a setting

`ACME_REQUIRE_ACCOUNT_APPROVAL` decides what is written when an account is created — not what is read
on every request, which would put configuration inside the one predicate C3 keeps pure.

The consequence is that flipping it off does not approve the people already waiting. That is correct
for the reverse direction (turning it *on* must not lock out existing customers) and a trap in this
one, so the queue states it: with approval off and people still waiting, the staff screen says so and
offers to approve them together. The stranded state is visible rather than silent.

### What this does not open up

Self-signup is a door strangers walk through, and the abuse question is already answered elsewhere:
five projects on a personal account, per-domain billing on a team account, and a domain that answers
no challenge until its delegation has been seen. An **unapproved** account can do none of it.

One surface is genuinely new: the enrolment flow sends mail to an arbitrary address on an
unauthenticated stranger's request. That is authentik's reputation policy to bound, already specified
as `public-edition` task 1.4, and it is required here rather than deferred.

## Capabilities

- `identity-and-roles` — enrolment exists; a self-registered person holds their personal account and
  nothing else; approval gates the account and invitation bypasses it
- `admin-console` — the awaiting-review screen, and the staff review queue
- `audit-trail` — approval and refusal are recorded decisions about a named person

## Impact

- **Schema** — `accounts.approved_at`, and one clause in `store.Usable`
- **Config** — `ACME_REQUIRE_ACCOUNT_APPROVAL`
- **Blueprint** — an enrolment flow, email verification, a reputation policy, outbound mail
- **Both consoles** — a screen in the customer one, a queue in the staff one
- **Takes over** the enrolment tasks currently duplicated between `authentik-and-self-signup` §5 and
  `public-edition` §1, which specify overlapping halves of the same work
