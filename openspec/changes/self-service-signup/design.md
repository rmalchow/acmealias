# Design

Decisions are numbered `S1`, `S2`, … and referred to by number from `tasks.md`.

## S1. What is actually there today, since two changes describe it wrongly

`development/authentik/blueprints/acmeservice.yaml` defines one flow,
`default-authentication-flow`, and binds no enrolment flow to it. There is no *Sign up* link on the
sign-in page because nothing creates one.

Two existing changes describe a different situation and both should be read with this in mind:

- **`public-edition` U9** presents the closed state as the work of a switch
  (`ACME_ALLOW_ACCOUNT_CREATION`). That switch is real and it gates buying a **team** account. It has
  never been what stops a stranger registering, because nothing has ever offered a stranger a form.
- **`authentik-and-self-signup` K6** described a self-registered person being *refused* by the
  service. That was true when written and was falsified by `personal-and-team-accounts`, which
  materializes a personal account on first sign-in. It has since been rewritten; this change is what
  makes the rewritten version reachable.

Stated plainly because it changes what needs building: the product is closed **by omission**. There is
no policy to reverse, only a flow to write.

## S2. Verification and approval are different questions and both are asked

| | Verification | Approval |
| --- | --- | --- |
| Asks | is this address yours? | do we want you as a customer? |
| Decided by | a machine | a person |
| Takes | seconds | hours or days |
| Lifetime | permanent | until we remove it |

They are conflated constantly, and the ordering follows from the table: verification runs **first**,
inside the enrolment flow, so that the queue a human reads contains only addresses that exist. A queue
full of typos and throwaway addresses is a queue nobody reads carefully, and the reviewer's attention
is the scarce resource the whole gate depends on.

Neither substitutes for the other. Verification alone admits anybody who owns a mailbox. Approval
alone has staff adjudicating strangers who may never come back.

## S3. The gate is in our service, not in authentik

authentik does this natively: an enrolment flow whose `user_write` stage carries
`create_users_as_inactive`, and an administrator sets `is_active` afterwards. It costs one blueprint
key and no code, and it is still the wrong place.

**The person is told nothing.** An inactive authentik user is not shown an explanation; they are shown
a sign-in that fails. To them that is indistinguishable from a wrong password — so they retry it, run a
password reset, retry again, and mail support. Every one of those is work our silence created, and the
one thing we needed to say ("we have your registration, somebody is looking at it") is the one thing
that channel cannot say.

**The reviewer is in the wrong building.** authentik's admin interface is a separate plane with its own
access, and what it shows is an *identity* — a username, a group list, a login history. What a reviewer
needs is a *registration*: who signed up, when, at what address, and what they have asked for. That is
staff console material, and `staff-plane` already builds the console it belongs in.

**Nothing lands in our audit trail.** `staff-plane` exists to make staff decisions about customers
accountable. "Who let this person in" is squarely that class of decision, and a flag flipped in
authentik's own log is not somewhere our audit answers from.

Underneath all three is one principle: **the identity provider proves somebody is who they say they
are; whether we want them is a decision about our product.** Filing that judgment under identity is
the same category error `tenancy-and-permissions` P18 refuses when it declines to read membership out
of directory groups. The provider can create a person. It cannot decide they are a customer.

**Rejected, and worth naming: doing both.** Inactive in authentik *and* unapproved here would be two
gates that can disagree, with no single answer to "why can this person not get in" — the failure mode
being a customer approved in one place and stuck in the other, and two teams each certain they had
done their part.

## S4. Approval is not a new state; it is the one existing predicate, with a third reason

`store.Usable` is the only place the question "does this account work" is answered (design C3), and
`store.SuspensionReason` is the only place it is explained. C6 wrote the reason down in advance: the
suspensions are "a conjunction, written here once, so that adding a third reason later is one clause
rather than an audit of every call site."

```sql
ALTER TABLE accounts ADD COLUMN approved_at timestamptz NULL;
```

```
usable(account) = approved_at IS NOT NULL
                  AND (kind == personal OR (credits > 0 AND NOT staff_suspended))
```

A nullable timestamp rather than a boolean, for the reason the invitation table takes the same shape:
*when* is audit evidence and *whether* is not, and a boolean throws the first away for nothing.

**Rejected: a `pending` account kind, or an account status enum.** Both introduce a state every
existing `switch` on `Kind` must now handle, and every one that does not becomes a place where a
pending account is quietly treated as a working one. The conjunct cannot be forgotten by a caller
because no caller evaluates it — they call `Usable`.

## S5. The new conjunct sits ABOVE the kind test, and that is the opposite of `staff_suspended`

`Usable` carries a comment about exactly this line:

> because the personal clause comes FIRST and short-circuits, a staff suspension does not stop a
> personal account. That is design C6 as written… When `staff-plane` gives somebody the power to set
> that column it will also be the change that decides what it means for a free account, and this is
> the one line it has to edit.

**This change does not edit that.** `staff_suspended` keeps its position and its meaning, and what a
staff suspension does to a free account remains `staff-plane`'s decision to make. The approval clause
is a separate conjunct placed above the whole expression, so it gates every account of every kind.

That asymmetry is deliberate and it is worth being able to defend, since two adjacent booleans now
behave differently:

- **A suspension is a judgment about conduct**, applied to an account that has been working. Whether
  it should reach into somebody's free personal workspace is a genuinely hard question about
  proportionality, and it is the question `staff-plane` is going to answer.
- **Approval is the absence of any judgment yet.** There is nothing to be proportionate about: the
  account has never worked, nobody has come to rely on it, and the entire purpose of the gate is that
  it applies to a person we know nothing about. A gate that let unapproved people use their personal
  account would gate nothing — the personal account is the whole product for a new arrival.

## S6. "Awaiting review" is not "suspended", to the only audience that matters

One predicate, two sentences, and the console already has the machinery: `SuspensionReason` returns a
sentence the caller acts on, and `suspended.component.ts`'s own comment records that "the reason comes
from the service rather than being inferred from the balance."

Somebody who registered an hour ago and is told their account is **suspended** reads *punished*. They
go looking for what they did wrong; they write to support to appeal a penalty nobody imposed; and
their first impression of the product is an accusation. The word is wrong even though the mechanism is
right.

So the screen is a different screen, and specifically it has **no checkout button**. Every other
unusable account in this product is fixed by paying, and offering to take money from somebody whose
problem is that a human has not looked at them yet is the worst possible misreading of their
situation. The remedy is waiting, so the screen says that, and says roughly how long.

## S7. An invitation is a vouching, so an invited person does not queue

Accepting an invitation sets `approved_at` on the accepter's personal account if it is not already set.

The queue exists to look at **strangers**. Somebody accepting an invitation is not one: a paying
customer named their address and took responsibility for them. Sending them to a review queue inserts
our latency into another customer's onboarding, and asks a reviewer with no context to second-guess
somebody who had plenty.

There is also a half-state to avoid. An invited person holds two things — the account they were
invited into and their own personal account — and approving only the first leaves them able to work in
somebody else's account but not their own, which is not a rule anybody could infer or would expect.

**Rejected: approving the user rather than the account.** Approval hangs off the account because that
is where `Usable` reads, and because a person's relationship to the product is mediated by accounts
everywhere else in this design. A `users.approved_at` would be a second axis to consult.

## S8. The switch decides what is WRITTEN, not what is READ

`ACME_REQUIRE_ACCOUNT_APPROVAL` is consulted once, by `EnsurePersonalAccount`, when the row is
created. On: `approved_at` null. Off: `approved_at = now()`.

It is emphatically **not** read by `Usable`. C3 keeps that predicate a comparison over one row, and a
predicate that consults configuration means the same row is usable or not depending on a setting —
which makes every stored decision provisional and every audit entry ambiguous about what was true at
the time.

The consequence, which is the interesting half:

- **Turning approval ON does not suspend anybody.** Existing accounts keep their `approved_at`. This
  is plainly right; the alternative locks out every customer the day somebody tightens a setting.
- **Turning approval OFF does not release the backlog.** People who registered while it was on stay
  unapproved. This one is a trap, and it is handled by making it visible rather than by weakening the
  predicate: with approval off and accounts still waiting, the staff queue says exactly that and
  offers to approve them together. Somebody decides, once, and the decision is recorded like every
  other approval.

**Rejected: auto-approving the backlog when the switch flips.** It is a bulk decision about real
people taken by a config reload, attributed to nobody, at a moment when nobody is looking at the
queue.

## S9. What the reviewer sees, and what they must not need

The queue is a list of registrations: address, when they registered, whether the address is verified,
and the account slug. That is the whole of it, and the shortness is the point — a reviewer deciding
about a stranger has nothing else to go on, and a screen implying otherwise invites them to look for
signal that is not there.

Two actions: **approve**, and **refuse**. Refusal is not deletion — the row stays, the audit entry
stays, and a refused registration is distinguishable from one nobody has reached yet, so the queue
does not re-present the same person forever. Refusal also does not delete the authentik user; whether
somebody may re-register is `staff-plane`'s blocklist question, not this one.

## S10. Abuse: what genuinely changes and what does not

Most of the abuse question is already answered and does not reopen here — five projects on a personal
account, per-domain billing on a team account, and a domain that answers no challenge until its
delegation has been seen. An **unapproved** account reaches none of that machinery, so the marginal
risk of an open front door is account rows, not service.

One surface is new and is not deferred: the enrolment flow sends mail to an arbitrary address, at the
request of somebody who has not authenticated. That is a spam vector pointed at third parties and it
is bounded by authentik's reputation policy, already specified as `public-edition` task 1.4 and
required by this change rather than left downstream.

## S11. This change takes over enrolment from two others

`authentik-and-self-signup` §5 and `public-edition` §1 each specify a half of self-service enrolment,
and they overlap: both add the enrolment flow to the blueprint, both point outbound mail at
`mailpit:1025`, both require the flow to be walked by hand once. Neither owns approval, because
neither knew it was wanted.

Both are amended to point here rather than being left to be discovered as duplicates. The
non-enrolment parts of each stay where they are: `public-edition` keeps recovery, the reputation
policy's production configuration, and the switches; `authentik-and-self-signup` keeps branding and
the provider swap.

## Risks / Trade-offs

- **A queue nobody reads is worse than no gate**, because the person waiting has been promised a
  decision. Mitigated by the screen setting an expectation and by the queue being in the console staff
  already open — not by anything technical, and worth saying plainly.
- **Approval is friction at exactly the wrong moment**, when somebody has just decided to try the
  product. Accepted deliberately and temporarily: S8 makes removing it a setting and an action rather
  than a change.
- **Two adjacent booleans behave differently** (S5), which is a thing somebody will trip over. Written
  down in `Usable`'s comment where they will be standing when they do.

## Migration Plan

1. Add `approved_at`, backfilling every existing account to `created_at` — they predate the gate and
   were usable before it.
2. Add the clause to `Usable` and the reason to `SuspensionReason`.
3. Add the setting, defaulting to **required**, so a deployment that does nothing gets the gate rather
   than an open door.
4. Blueprint: enrolment flow, verification stage, reputation policy, outbound mail.
5. Console screens, customer then staff.

Rollback is dropping the column and the clause; a partially rolled-out state leaves unapproved
accounts unusable with a reason string nothing renders, which presents as a generic suspension.

## Open Questions

- **How long the expectation on the waiting screen should promise.** It is a commitment to a response
  time and `staff-plane` owns the twelve-hour abuse-contact commitment already; this should probably
  match rather than invent a second number.
- **Whether a refused registration is told.** Silence is unkind and a message is a signal to somebody
  probing what gets through. Deferred, not decided here.
- **Whether approval should ever be automatic for a recognised domain** — an address at a company we
  already bill. Attractive, and it is a rule about identity that S3's principle says to be careful
  with.
