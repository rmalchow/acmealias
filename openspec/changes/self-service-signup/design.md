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

## S3. The gate is in the identity provider, and the two refusals are different things

**This decision said the opposite, and was wrong.** It argued the approval gate belonged in our
service. The correction is not deference to a preference; it is a distinction the first version
missed, and once it is drawn the rest follows.

There are two refusals in this product and they are about different objects:

| | *Not approved yet* | *Suspended* |
| --- | --- | --- |
| About | an **identity** — may this person authenticate at all | an **account** — this known customer's workspace does not work |
| Known to us | nothing; they are a stranger | everything; they have been using the product |
| Lives in | authentik | our service (`store.Usable`) |
| Reached by | nobody, ever — they cannot get a session | the person, on a screen |

"Not approved yet" is a statement about somebody we have not admitted. There is no account for it to
be a fact about, and inventing one to hold the flag is what the first version of this decision did.

### What that mistake actually cost, which is the argument

If approval lives in our service, then an unapproved stranger **completes enrolment, receives a
session, and gets an `accounts` row written for them**. We would be materializing database rows —
account, slug, personal-account invariants, audit entries — for every person we have explicitly not
yet decided to admit. Anybody who can complete a signup form can make us write rows.

Held inactive at the provider, they never reach us at all. The gate is in front of the thing being
protected instead of inside it, which is where a gate belongs.

### The objection that survives, as a requirement rather than a reason

The first version's best point stands and does not go away: **an inactive user is told nothing.**
authentik's default behaviour for one is a sign-in that fails, which to the person is
indistinguishable from a wrong password — so they retry, reset their password, retry, and mail
support, and every one of those is work our silence created.

But that is a fact about authentik's DEFAULT, not about where the gate belongs. authentik can be made
to say it: a policy on the authentication flow that recognises an unapproved user and a stage that
renders a real message rather than a generic failure. So it becomes a requirement on the flow — task
7.6 — and it is the one part of this that must not be skipped, because skipping it produces exactly
the support load the first version predicted while keeping none of what it was trading for.

The other two objections do not survive contact with the correction. "The reviewer is in the wrong
building" assumed a reviewer working in our staff console on a queue of our rows; there are no rows,
and the provider's own user list is where an identity decision is taken. "Nothing lands in our audit
trail" is right and is no longer a defect: our audit trail records what happens to accounts and the
people in them, and somebody who was never admitted is in neither category. The provider's event log
is the correct home for the decision, because it was a decision about an identity.

## S4. Nothing changes in the account model

The consequence of S3 worth stating on its own, because the first version of this change specified
all of it and none of it is now needed:

- **No `accounts.approved_at`**, no migration, no backfill.
- **No new conjunct in `store.Usable`.** The predicate stays exactly as C3 wrote it, which also means
  the question C6 left open — what a staff suspension means for a free account — stays open and
  `staff-plane`'s to answer, rather than being disturbed by a change that had no business near it.
- **No third reason in `SuspensionReason`**, and no new console screen. Suspension keeps its two
  meanings and its existing screen.

A change that adds a column, a clause in the one predicate the whole permission model funnels
through, a reason string, a customer screen and a staff queue — against one that adds a flow to a
blueprint. The second is not merely smaller; it leaves the load-bearing part of the system untouched.

## S5. The service does not learn about somebody until they are admitted

There is no state in our database for a pending registration, and that is the point rather than an
omission. `store.EnsurePersonalAccount` runs on first sign-in, and a person who has not been approved
never signs in, so it never runs for them.

The property this preserves is worth naming because it is easy to give away: **every row in
`accounts` belongs to somebody we decided to admit.** Once the gate is in front, that is true by
construction rather than by a predicate somebody has to remember to consult.

## S6. Suspension stays exactly where it is

The user's split, kept as written: *"not approved yet"* in authentik, *"suspended"* on the user in the
app.

They are not two implementations of one idea. A suspension is a judgment about **conduct**, applied to
an account that has been working, that somebody has come to rely on, and whose owner is owed both an
explanation and a remedy — which is why `SuspensionReason` returns a sentence and the console renders
a screen with a way out. Approval is the absence of any judgment at all about a stranger, and there is
nobody to explain anything to yet beyond "we have your registration".

## S7. An invitation is a vouching, so an invited person is not held

A person created by accepting an invitation is created **active**. They do not wait for approval.

The queue exists to look at **strangers**. Somebody accepting an invitation is not one: a paying
customer named their address and took responsibility for them. Holding them inserts our latency into
another customer's onboarding and asks a reviewer with no context to second-guess somebody who had
plenty.

Mechanically this is a property of which flow created the user, which is a distinction authentik draws
natively: the enrolment flow reached from the sign-in page creates users inactive, and the flow an
invitation link enters creates them active. Two flows, two `user_write` stages, one differing key —
rather than a rule somebody has to apply.

## S8. Opening the door does not release those already behind it

Approval is required or not according to one key on the enrolment flow's `user_write` stage,
`create_users_as_inactive`. Turning it off means the flow stops creating inactive users. It does
**not** activate the people it created earlier.

That asymmetry is right in one direction and a trap in the other, and both halves are worth stating:

- **Turning it ON cannot lock anybody out.** Existing users keep `is_active`. A setting somebody
  tightens must never suspend the customers already using the product.
- **Turning it OFF leaves the backlog inactive.** The people who registered while the gate was up stay
  where they are, waiting for a gate that no longer exists.

The second is handled by being deliberate about it rather than by making the flow retroactive:
switching the gate off is accompanied by activating whoever is still waiting, as an explicit action
somebody takes and the provider's event log records. A config change that silently admits a backlog of
real people, attributed to nobody, at a moment when nobody is looking at them, is not an improvement
on remembering.

## S9. What the reviewer does

Approving somebody is setting `is_active` on a user in authentik's own interface. There is no queue to
build, because the provider already lists its users and can filter them by exactly this.

Refusal is leaving them inactive, or deleting the user. Neither needs a mechanism from us. Whether
somebody may re-register after a refusal is `staff-plane`'s blocklist question and not this one.

What a reviewer has to go on is short — an address, when it registered, whether it is verified — and
the shortness is honest rather than a limitation of the screen. A reviewer deciding about a stranger
has nothing else, and a richer interface would only invite them to look for signal that is not there.

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

- **A queue nobody reads is worse than no gate**, because the person waiting has been told a decision
  is coming. There is nothing technical to mitigate it with; it is a commitment somebody has to keep,
  and it is named here so that turning the gate on is understood as taking on that commitment.
- **The refusal message is the whole of the person's experience** and it lives in authentik's flow
  rather than in a screen we design. That is the price of S3, and task 7.6 is what stops it becoming
  the dead-login problem the first version of this design was right to worry about.
- **Approval is friction at the worst possible moment**, when somebody has just decided to try the
  product. Accepted deliberately and temporarily — S8 makes removing it one key and one action.

## Migration Plan

There is no migration. Nothing in our schema changes, no account is touched, and nothing needs
backfilling — which is itself the strongest evidence that S3's correction was the right one, since the
first version of this plan had a column, a backfill and a change to the predicate every read in the
product passes through.

1. Blueprint: enrolment flow, verification stage, `create_users_as_inactive`, reputation policy,
   outbound mail.
2. Blueprint: the message an unapproved person sees when they try to sign in (task 7.6).
3. The invitation flow's own `user_write`, creating active users (S7).

Rollback is removing the flow. A partially rolled-out state has an enrolment form that creates users
nobody activates, which presents as registrations that never complete — visible in the provider's user
list rather than silent.

## Open Questions

- **How long the wait should be promised as.** It is a commitment to a response time, and
  `staff-plane` already owns a twelve-hour commitment for its abuse contact; this should probably
  match rather than invent a second number.
- **Whether a refused registration is told.** Silence is unkind; a message is a signal to somebody
  probing what gets through. Not decided here.
- **Whether approval can ever be automatic for a recognised domain** — an address at a company we
  already bill. Attractive, and now unambiguously an identity-side rule, which is where S3 says such a
  rule belongs.
