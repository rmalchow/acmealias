## Context

`tenancy-and-permissions` gives an account exactly one member — whoever created it. This change adds
the second.

The whole difficulty is that an account holder can name the person they want to add only by **email
address**, and an address is not a user. At the moment Bob types `alice@corp.com` the service knows
nothing: whether a user exists for that address, whether Alice will sign in with it, whether Alice is
even the person who will read the message. Every decision below is about closing that gap without
turning a forwarded email into a transferable grant of access to a billing entity.

Terms, defined once and used consistently with `tenancy-and-permissions`:

- **User** — a person. Global, owned by nobody, outlives the accounts they belong to.
- **Account** — the billing entity holding credits, owning projects, anchoring grants. Never used here
  to mean a person's login.
- **Membership** — the row binding one user to one account with a role.
- **Unrestricted role** — the built-in role conferring everything within its account, which always has
  at least one member.
- **Invitation** — an offer of a membership, addressed to an email address rather than to a user.
- **Selection** — which account a session is currently acting in, or none (decision P19).

Constraints inherited rather than chosen:

1. **Only the unrestricted role may invite** (P14: the account is not an ACL-addressable object).
2. **Accepting must work with no account selected** (P19), because the invitee may belong to nothing.
3. **Token language is already written** in `add-accounts-and-public-edition` — expiring, single-use,
   unguessable — and is reused rather than paraphrased.
4. **The roster is already specified** as readable by every member including pending invitations, so
   this change adds an entity to it rather than a view of it.

## Goals / Non-Goals

**Goals:**

- A person can be added to an account by email address, whether or not they already have a user.
- A forwarded invitation confers nothing.
- The inviter learns nothing about whether the address already has a user.
- Sending mail to arbitrary third-party addresses on request is bounded before the feature exists.

**Non-Goals:**

- Several verified addresses per user. It is the right relief for the friction V2 creates, and it is a
  change to the identity model rather than to invitations.
- Removing members, and reassigning what a departing member created. Objects belong to the account, not
  to the member who made them, so there is nothing to reassign.
- Domain-based automatic joining. It requires proving control of a domain.
- Invitations in an identity-provider edition. Memberships there are derived from group claims and
  stored nowhere, so there is nothing for an invitation to write.

## Decisions

### V1. An invitation is a row addressed to an address, and only a hash of its token is stored

```sql
CREATE TABLE invitations (
  id            uuid        PRIMARY KEY DEFAULT uuidv7(),
  account_id    uuid        NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  email         text        NOT NULL,
  token_hash    bytea       NOT NULL,
  invited_by    uuid        NOT NULL REFERENCES users (id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  revoked_at    timestamptz NULL,
  declined_at   timestamptz NULL,
  accepted_at   timestamptz NULL,
  accepted_by   uuid        NULL REFERENCES users (id)
);
CREATE UNIQUE INDEX invitations_pending_idx
    ON invitations (account_id, email)
 WHERE accepted_at IS NULL AND revoked_at IS NULL AND declined_at IS NULL;

CREATE INDEX invitations_by_email_idx
    ON invitations (email)
 WHERE accepted_at IS NULL AND revoked_at IS NULL AND declined_at IS NULL;
```

The state is carried as three nullable timestamps rather than a status column, because each of them
records *when* as well as *which*, and the audit trail wants both. Outstanding is the absence of all
three plus an `expires_at` in the future — expiry is evaluated at read time (V8), so it is not a
fourth column that some job has to write.

`invitations_by_email_idx` is what makes V11's second path a lookup rather than a scan: signing in
asks "what is outstanding for this address" across every account, which no other index answers.

**There is no `role_id`, and its absence is the newest decision here** — see V6. An earlier draft
of this table carried one, and every consequence of removing it (a nullable membership role, a
shorter accept screen, a whole decision about deleted roles that stops existing) follows from that
one dropped column.

**Only `token_hash` is stored, never the token**, following the reasoning already applied to
`api_keys.secret_hash`: a datastore read must not yield a usable credential. Verification is a lookup by
hash and a constant-time comparison, exactly as `internal/apikey` does it. The token is high-entropy
random, so a memory-hard hash buys nothing over SHA-256 — the same argument the `apikey` package doc
already makes.

The partial unique index is what makes V7 (one outstanding invitation per address per account)
structural rather than a check somebody has to remember. `declined_at` sits in its predicate for the
reason V12 gives: a decline is an answer, not a block, so the account may invite the same person
again — and the index must let it.

`email` is stored normalized — lower-cased, whitespace trimmed — and compared in that form. Local-part
case sensitivity is technically permitted by RFC 5321 and observed by essentially no mail system; treating
`Alice@corp.com` and `alice@corp.com` as different people would produce two invitations and one confused
customer.

### V2. Bound to the address — and the path that would make "bound" a fiction is not offered

Only a user whose verified address is the invited address may accept.

The subtlety, stated here because it is the single easiest thing to get wrong in this change: **holding
a link sent to `alice@corp.com` is proof of control of `alice@corp.com`.** It is precisely the proof an
email confirmation token provides, and V3 relies on exactly that to skip a confirmation mail on the
sign-up path.

Which means that if the service also offered *"add this invited address to the user I am currently
signed in as"*, then anybody forwarded the message could attach the address to their own user and then
accept as its owner. Bound would have become bearer with two extra clicks, and it would look correct in
every test that checked "the accepting user's address matches the invitation".

So there are exactly two paths, and no third:

| Invited address | Path |
| --- | --- |
| No user exists for it | Sign up (V3); the created user has it verified; then accept |
| A user exists for it | Authenticate as that user; any other authenticated user is refused |

The refusal shows the address **masked** — `a**e@corp.com` — because somebody holding a forwarded link
should not learn the full address from the message telling them they may not use it. The mask preserves
enough for the legitimate recipient signed in as the wrong identity to recognize which of their addresses
is wanted.

**The cost, stated rather than hidden.** Alice, invited at `alice@corp.com`, who habitually signs in as
`alice@gmail.com`, must create a user for the work address. For a membership of a company's billing
entity that is arguably correct — the membership belongs to the work identity and survives her personal
address changing — but it is friction, it will produce support questions, and the real answer is several
verified addresses per user, which is a different change.

**Rejected: bearer invitations.** What GitHub and Slack do, and it handles the mismatched-identity case
gracefully. Rejected because an invitation here is access to an entity holding prepaid credits and
serving authoritative DNS for the company's domains, and forwarding a message is something people do
dozens of times a day without imagining they have transferred anything.

**Rejected: bound with a re-address step** — the holder may ask the inviter to re-send to an address they
control, which notifies rather than silently transfers. Genuinely good, and rejected only as a second
flow and a second notification for a case the multi-address change solves better. Recorded because it may
return.

### V3. Sign-up reached through an invitation produces an already-verified address

The invitation link *is* the confirmation token, so requiring a second one would mean sending a second
mail to an address we have just proven the recipient controls, to prove it again.

A user created through this path therefore has the invited address verified at creation, and goes
straight to the accept question. This is the one place this change touches
`add-accounts-and-public-edition`'s registration flow, and it touches it by adding an entry point rather
than by altering the existing one: registration reached any other way is unchanged, and still requires
confirmation.

The invitation token is **not** consumed by the sign-up — only by the acceptance (V4).

**Verified is not the same as admitted, and V13 turns on that distinction.** This decision says the
invited address needs no confirmation mail, because the link already proved control of it. It says
nothing about whether the person may authenticate: that is the approval gate, a different question
with a different answer, and V13 answers it.

### V4. Acceptance is the only thing that consumes the token

Opening the link creates nothing. It resolves the invitation, renders *"Join cloudpowered?"*, and
stops.

This matters for a mundane reason: links in email are opened by things that are not people. Mail
security gateways follow every URL in a message to scan it; clients generate link previews; a recipient
opens the tab and closes it again. If arrival consumed the token, a meaningful proportion of invitations
would be dead before the invitee ever saw them, and the failure would look like a bug in the mail rather
than a design decision.

It also gives the security property its clean statement: **a leaked link is not a membership.** It is an
opportunity to attempt an acceptance that will be refused unless the attempter can authenticate as the
invited address.

Rendering the accept question requires reading the account's display name, addressed by the token. That
reveals nothing the mail did not already say — and since V6 removed the role from the offer, it is now
strictly less than the mail said.

### V5. Accepting needs no selection; issuing one requires it

**"The owner and the admins may invite" and "members of the unrestricted role may invite" are the
same sentence, and this is why.** They sound like two rules because the model has two words that both
translate to "in charge", so it is worth walking the actual rows rather than trusting the vocabulary.

A team account is created by `store.CreateAccountWithOwner`, which writes four roles from
`store.DefaultRoles()` — `ADMIN`, `CONFIG_ADMIN`, `KEY_ROTATION`, `READ_ONLY` — and exactly one of
them, `ADMIN`, carries `Unrestricted: true`. It holds no grant rows at all; its authority *is* the
flag, which is what stops somebody editing their way out of an account one grant at a time. The same
function then writes the first `account_members` row binding the creating user to `ADMIN`. So:

- **"admin"** is a member of the `ADMIN` role, and there may be several. Nothing limits it to one.
- **"owner"** is `accounts.owner_user_id`, exactly one person, and its only job is billing: it is what
  `RESTRICT` refuses to delete while the account exists, and what `adminctl transfer` moves.
- **The owner is an `ADMIN` member from the account's first instant**, because the same transaction
  writes both.

So the rule that was already written — membership of the unrestricted role — admits precisely the
owner and the admins. **This is a wording change and not a model change**, and no second built-in role
is being invented. What follows is only that the spec and the console must say "an administrator"
where they currently say "a member of the unrestricted role", because the second phrase is our word
for it and the first is the customer's.

**One gap this exposed, since closed.** Nothing guaranteed the owner *stayed* an `ADMIN` member.
`store.CountUnrestrictedMembers` guards that the role keeps at least one member; it did not guard that
the owner is one of them, so a team could demote the person who pays the bill, who would then be
unable to invite anybody into the account they are billed for. That is a property of
`personal-and-team-accounts` rather than of invitations, and it was fixed there: its design C16 and
tasks 0.1–0.5 make the owner's unrestricted membership an invariant, refused on removal and
established on transfer.

Decision P19 puts every session in one of two states and names accepting an invitation among the four
operations reachable with **no account selected**. That is not incidental — it is what makes the flow
terminate. Alice authenticates because she was invited; at that instant she belongs to nothing, so
requiring a selected account to accept would be a closed loop.

| Operation | Selection | Route |
| --- | --- | --- |
| Resolve an invitation by token | none | outside the account-scoped tree |
| Accept an invitation | none | outside the account-scoped tree |
| Issue, revoke, re-send | selected, unrestricted role | `/api/v1/accounts/{accountId}/…` |
| Read pending invitations | selected, any member | `/api/v1/accounts/{accountId}/…` (the roster) |

The accept routes are addressed **by token**, not by account. Putting them under
`/api/v1/accounts/{accountId}/…` would require the caller to name an account the selection rule would
then refuse, since they are not yet a member.

**After acceptance the newly joined account becomes the session's selection.** Leaving somebody in the
empty state immediately after joining is a dead end, and their next intent is obviously to act in what
they just joined. It is a selection change like any other — it re-evaluates the membership, which was
written moments earlier in the same transaction, and is recorded as a selection like any other.

### V6. An invitation offers membership, not a role — and a member may hold none

An invitation names an account and an address. It does not name a role. Alice is asked *"Join
cloudpowered?"*, she joins, she appears in the roster **with no role**, and somebody assigns her one
afterwards.

**What this replaces.** The earlier design put a role on the invitation, and spent a whole decision on
what happens when that role is deleted between issuing and accepting: fail the invitation closed, show
it as invalid in the roster, refuse to delete a role an invitation names. All of that machinery existed
to protect a value that no longer sits on the row. There is nothing to delete, nothing to fail closed,
nothing to substitute. `roles` is not referenced from `invitations` at all, so deleting a role has no
interaction with invitations whatsoever.

**Why it is better, and it is not only simplicity.** Bob invites Alice on Monday knowing he wants her
in the account; he does not necessarily know yet which role, and under the old model he had to decide
at the moment he had the least information. Worse, he had to decide it fourteen days before it took
effect, against a role list that he might edit in the meantime. Separating "you are in" from "here is
what you may do" puts the second decision at the moment somebody actually has to make it, with the
person present.

**The cost, and it is a real one: a member with no role authorises nothing.** This is a state the
model did not previously have. `account_members.role_id` becomes nullable, and every path that reads
authority out of a membership has to treat a null role as *no grants*, not as *skip the check*:

- The ACL predicate compiler builds a `WHERE` clause from the subject's grants. A subject with no role
  has an empty grant set, so the predicate is false for every row.
- **RLS empties rather than fails.** A roleless member's read of the project list returns zero rows and
  no error. That is arithmetically correct and, presented raw, it is a lie: it looks like an account
  with nothing in it rather than an account she cannot see into.

So the roleless state has to be **legible**, and that is a requirement of this change rather than a
detail of it. A roleless member can select the account — selection is governed by membership, and she
is a member — and every account-scoped screen must say *you have no role in this account yet; ask an
administrator* rather than rendering an empty table or a wall of refusals. The empty state is the
feature; without it, the first thing a newly joined colleague sees is a product that appears to be
broken.

**Rejected: give the invitation a default role instead, such as the least privileged one.** It keeps
`role_id` non-null and keeps the member always authorised for something. Rejected because it puts
authority nobody chose onto a new member, which is the same objection that killed "substitute another
role" in the earlier draft — and because "least privileged" is not a fixed thing here: roles are rows
in the account, editable and deletable, so there is no role the code can name as the safe one.

**Rejected: refuse to complete the join until a role is picked.** It avoids the roleless state by
making acceptance block on an administrator. It reintroduces the bottleneck at the worst moment — the
invitee is sitting there, having just signed up — and it makes the pending list ambiguous again, which
is exactly what V12 went to trouble to avoid.

**Two details of the existing schema that the null role lands on, because both would otherwise be
found by a failing test rather than by reading.**

*The composite foreign key still holds.* `account_members` does not reference `roles (id)`; it
references `roles (id, account_id)`, which is what stops a membership pointing at a role belonging to
some other account. A null `role_id` does not weaken it: PostgreSQL's default `MATCH SIMPLE` skips the
check when any referencing column is null, so the pair is either both present and checked, or absent.
Nothing needs relaxing and nothing silently stops being enforced.

*The uniqueness constraint stops covering the case.* Today `unique (account_id, user_id, role_id)`
prevents the same person being given the same role twice — and note what that shape also says: **a
person may hold SEVERAL memberships of one account, one per role.** With `role_id` null, that unique
constraint no longer constrains anything, because nulls are distinct: the same person could be
inserted as a roleless member any number of times. So a partial unique index over
`(account_id, user_id) where role_id is null` comes with the nullable column, in the same migration.

*And assigning a role consumes the roleless row rather than adding beside it.* Because memberships are
per-role, "give Alice the CONFIG_ADMIN role" could plausibly mean inserting a second row and leaving
the null one in place — which would leave her listed twice, once as a real member and once as somebody
waiting for a role. The roleless row is a placeholder for exactly one role assignment, so the first
assignment updates it in place. Later ones, if the account wants her in two roles, insert normally.

### V7. One outstanding invitation per address per account; re-sending invalidates the previous token

At most one invitation for a given address may be outstanding in a given account, enforced by the partial
unique index in V1.

**Re-sending issues a new token and invalidates the old one.** Anything else means several live tokens
for one offer, so revoking becomes "revoke all of them" and the invitee holds two links that behave
differently. Re-sending is the natural response to "I never got it", and it must not leave the earlier
one working — that earlier message is exactly the one that might have gone somewhere unintended.

**Revoking** marks the invitation revoked and its token stops resolving immediately. A revoked invitation
leaves the roster's pending list and is retained for the audit trail.

**An address that is already a member is refused at issue time**, not at acceptance. The alternative
wastes a mail and produces a confusing dead end for the recipient. This is not a disclosure problem:
membership of *this* account is already visible to the inviter through the roster.

### V8. Expiry is fourteen days, and an expired link says so

Fourteen days. Long enough to survive a holiday, a conference week and a full inbox, which is the
realistic failure — the person invited is usually the person with the least free attention. Short
enough that a message sitting in an archive is not a live credential months later.

Seven was the first proposal and was rejected as too short for exactly the case that matters: an
invitation that arrives the day somebody goes away comes back to a dead link, and the recovery
costs an administrator's time as well as the invitee's.

An expired link renders a page that says the invitation has expired and to ask the account for a new one.
It does **not** offer self-service renewal — renewal is the account's decision, since the account is what
is being joined. It names the account so the recipient knows who to ask, which reveals nothing beyond
what the original mail said.

Expiry is evaluated at resolution, not by a sweep, so an invitation is dead the moment it is dead
regardless of whether a job has run. A sweep may reclaim expired rows on the retention schedule, but
nothing depends on it having run — the same reasoning `metering-and-credits` applies to its ticks.

### V9. Rate limiting is part of the feature, not a later hardening pass

This is the first surface where an ordinary tenant can cause the service to send mail to an address of
their choosing. That is a spam vector aimed at third parties, and the reputational damage lands on the
sending domain — ours.

Three limits, each answering a different abuse:

| Limit | Bounds |
| --- | --- |
| Invitations issued per account per hour | bulk sending from one account |
| Invitations to one address across all accounts per day | using many accounts to harass one person |
| Re-sends of one invitation per hour | using the re-send button as a send button |

Exceeding a limit refuses with a message saying so and when to retry. Refusals are recorded, because a
tenant hitting a mail-sending limit repeatedly is the signal that they are doing something worth looking
at.

The cross-account limit is the one that cannot be omitted: the per-account limits are trivially defeated
by creating accounts, which any authenticated user may do freely.

### V10. Invitation events join the credential class of the audit vocabulary

`internal/authz/vocab.go` names actions `<object>.<verb>` and sorts them into two classes: `config`
("who changed the setup") and `credential` ("who touched the credentials"). An invitation confers access
to an account, so it belongs with `session.revoke` and the key actions rather than with domain and node
changes.

```go
ObjectInvitation = "invitation"

ActionInvitationCreate = "invitation.create"
ActionInvitationRevoke = "invitation.revoke"
ActionInvitationResend = "invitation.resend"
ActionInvitationAccept  = "invitation.accept"
ActionInvitationDecline = "invitation.decline"
ActionInvitationExpire  = "invitation.expire"
```

Each entry names the invited address. It does not name a role, because V6 took the role off the
invitation; the role a member ends up with is assigned separately and is recorded by whatever action
assigns it. `invitation.accept` names both the accepting user and the address, which is what makes a bound-acceptance audit trail meaningful — it is the evidence
that the person who joined is the person who was invited. `invitation.decline` names the same pair, and
is what lets an administrator tell a refusal from a message that never arrived (V12).
`invitation.expire` is written by the system actor, matching `apikey.expire`.

The token never appears in an entry, in either the before or after state. `authz.Record` already scrubs
secret-looking fields; `token_hash` is excluded explicitly rather than relying on the field name
matching a heuristic.

### V11. The link is a convenience, not the only way in — an invitee sees their invitations on arrival

Everything above is written around the token in the mail, and a flow that depends on a link is a flow
that depends on mail delivery being perfect. It is not. The message lands in spam, the corporate
gateway rewrites the URL and breaks it, the recipient reads it on a phone and signs in on a laptop,
or somebody deletes it and asks to be invited again — which produces a second invitation and a
support conversation instead of a click.

So the invited address is what an invitation is bound to (V2), and that binding works in **both**
directions:

- **From the link.** The token resolves the invitation, and the person authenticates as the invited
  address to accept it. This is the path the mail takes.
- **From the session.** A person who signs in as `alice@corp.com` is shown every outstanding
  invitation issued to `alice@corp.com`, on their dashboard, whether or not they ever opened a mail.

The second path grants nothing the first does not. Both require authenticating as the invited
address, which is the whole of V2's binding; the token only ever addressed the row, and a session
already addressed by the same address addresses it just as well. **Being signed in as the invited
address is strictly stronger evidence than holding a link sent to it**, so a flow that accepts the
link must accept the session.

This is also what makes the empty state useful rather than a wall. V5 already has a person arriving
with no account selected and nothing to select. Without this they see an empty product and are told
to go find an email; with it, the thing they came for is the first thing on the screen.

### V12. Accept and DECLINE, and declining is not the same as ignoring

The invitation is offered with two answers, not one and a silence.

Declining marks the invitation declined, removes it from the account's pending list, and is written
to the audit trail like any other resolution. It does **not** block the address: the account may
invite the same person again, because "not now" and "never" are different answers and we are not in
a position to tell which one was meant.

**Why bother, when ignoring an invitation already expires it.** Because the two outcomes look
identical to the account and mean opposite things. An administrator watching a pending invitation sit
for fourteen days cannot tell whether the person never saw it — the spam folder, the rewritten URL,
the wrong address — or saw it and does not want to join. The first calls for re-sending or a phone
call; the second makes re-sending a nuisance aimed at somebody who already said no. Fourteen days of
silence answers neither question, and the administrator's most likely reaction to silence is to
invite again.

Declining does not reveal anything the account does not already have: the account chose the address
and can already see whether the invitation was accepted.

**A declined invitation is not resurrectable by its own link.** The token is consumed by the decline
exactly as it is by the acceptance (V4) — otherwise "declined" would be a state a stray click could
undo, and the person who declined would find themselves a member of an account they refused.

### V13. An invited person is NOT exempt from approval, and the invitation clock runs while they wait

`self-service-signup` S7 said the opposite: a person created by accepting an invitation was created
**active**, skipping the approval queue, on the argument that a paying customer had vouched for them.
That is reversed. **Being logged in may include being approved**, for the invited person exactly as
for the stranger who found the sign-in page.

**Why the reversal, since S7's argument was not silly.** It was an argument about *who deserves* to
wait, and the queue is not sorted by desert. While self-registration is closed, the approval queue is
the only thing standing between the outside world and a user row, and an exemption reachable by anyone
who receives an invitation is a hole in it whose width is decided by whichever customer is least
careful with their invitations. It also cost a whole second enrolment flow in authentik — two flows,
two `user_write` stages, one differing key — to be maintained, tested and kept in step. One door,
which everybody walks through, is both safer and less to keep working.

**Approval and address verification are separate gates and stay separate.** V3 still holds: the
invitation link proved control of the address, so no confirmation mail is sent and the address is
verified at creation. What the invited person does not skip is the human looking at them. Verified and
admitted are different facts, and it is worth saying plainly because "already verified" reads like
"already through" and is not.

**The consequence, which is unpleasant and has to be stated rather than discovered.** The invitation
expires fourteen days after it is issued (V8), and that clock does not pause. It runs while the
invitee waits for a member of staff. So:

> Bob invites Alice on the 1st. Alice signs up on the 1st, and waits. Nobody approves her until the
> 16th. She signs in for the first time to find that the invitation she signed up in order to accept
> has expired, and she must ask Bob to send another — having done everything right, and having waited
> because of us.

That is a real failure mode created by this decision, and the honest answer is that **the invitation's
expiry is now partly a service-level commitment about approval latency**, not only a security
parameter. `self-service-signup` has an open question about what response time to promise; this makes
that question load-bearing rather than cosmetic, because any promise longer than fourteen days is a
promise to break invitations.

Three responses were considered and none is adopted here:

- **Pause the clock while the invitee is unapproved.** Correct-feeling and it makes `expires_at` a
  derived value that depends on an authentik-side fact the service cannot see. Rejected: the service
  does not know when somebody was approved, and inventing a way for it to find out is a bigger change
  than the problem.
- **Extend expiry to something longer than any plausible approval wait.** Rejected because V8 chose
  fourteen days against a real argument, and lengthening it to hide a latency problem trades a
  security parameter for an operational one.
- **Approve invited people automatically.** This is S7 wearing a different hat, and it fails for the
  same reason.

What is adopted instead is that the failure is made **legible**: an invitation that expired while its
invitee was waiting is still just an expired invitation to Bob, who re-sends it. The re-send is one
click and the new fourteen days begin. That is a poor consolation and it is the true one.

### V18. Write, then send — and a send failure keeps the invitation

The row is committed first and the message goes afterwards. Neither order is safe, so the question is
not which one can fail but **which failure a person can recover from.**

- **Send first, write second.** A failure leaves a message in somebody's inbox pointing at an
  invitation that does not exist. The recipient can do nothing at all, and what they see is our link
  being broken — which is also what they will report.
- **Write first, send second.** A failure leaves an invitation nobody was told about. The account can
  SEE it, sitting in the pending list, and fix it with the re-send button that exists for exactly
  this.

The second failure is visible to the person who can act on it, and the first is not.

**Rejected: rolling the invitation back when the send fails.** It is the intuitive tidy-up and it
removes the one thing that makes recovery possible — the row the re-send button acts on. It converts a
recoverable state into a lost one in the name of consistency.

What the recovery needs is that nobody is misled in the meantime, so the send failure is reported to
whoever clicked. They know the message did not go; the pending entry tells them it still can.

## Risks / Trade-offs

- **The mismatched-identity dead end** (invited at work, signs in personally) → named honestly in V2
  rather than smoothed over; the accept screen shows the masked address so the recipient can see which
  identity is wanted; the real fix is multi-address users, recorded as an open question.
- **Administrators are a bottleneck for onboarding** → accepted consequence of V5/P14; the escape, if it
  becomes real, is a second built-in role rather than making account membership ACL-addressable.
- **Mail is a spam vector** → three limits in V9, including a cross-account one, because per-account
  limits are defeated by creating accounts.
- **A leaked link plus a compromised mailbox is a membership** → true, and irreducible: control of the
  address is the whole proof. Bounded by V8's fourteen days, by V4's explicit acceptance, and by the roster
  making a pending invitation visible to every member of the account, so an unexpected one is noticeable.
- **A mail gateway follows the link before the invitee does** → V4 consumes the token only on acceptance,
  so scanning is harmless.
- **An account deleted between issue and acceptance** → `ON DELETE CASCADE` on `account_id` removes the
  invitation with it, and the link resolves to nothing, which is correct: there is nothing to join.
- **Enumeration of the invitation endpoint** → tokens are unguessable by the inherited requirement, and
  resolution is a hash lookup, so the endpoint offers nothing to enumerate. It is nonetheless rate
  limited by address like the rest of the flow.
- **A new member sees an account that looks broken** → the consequence of V6's roleless state, and the
  one thing in this change that RLS makes silent rather than loud: a member with no role reads zero
  rows and no error. Answered by making the empty state say what it is, which is a requirement rather
  than a nicety.
- **An invitation expires while its invitee waits for approval** → created by V13, irreducible without
  either pausing the clock against a fact the service cannot observe or lengthening V8's fourteen days
  to hide a latency problem. Bounded by treating approval latency as a commitment, and by re-sending
  being one click.

## Migration Plan

1. Create `invitations` with its two partial indexes.
2. Make `account_members.role_id` nullable, add the partial unique index over
   `(account_id, user_id) where role_id is null`, and assert afterwards that the table's row-level
   security policy still holds — a migration that touches a policied table and does not re-assert the
   policy is how a scopeless read starts returning nothing instead of failing.
3. Add the invitation actions and object type to `internal/authz/vocab.go` and to the credential class.
4. Add the mail template and the send path on `internal/mail`, which exists and which nothing sends
   through yet: `AdminOptions.Mail` is populated in `main.go` and read by no handler. This change is
   its first caller.
5. Add the account-scoped issue, revoke and re-send routes under `/api/v1/accounts/{accountId}/…`.
6. Add the selection-free resolve, accept and decline routes outside the account-scoped tree.
7. Add the sign-up entry point that pre-verifies the invited address, still creating an inactive user
   (V13).
8. Add the console panels: invitations within the account's settings, the roleless empty state, the
   accept screen in the no-selection shell, and the invitation list on arrival.

Nothing needs backfilling: there are no invitations before this change exists, and every existing
`account_members` row has a role, so widening the column changes no data.

Rollback is dropping the table and the routes. Narrowing `role_id` back to `not null` is NOT safe once
anybody has joined without one — the down migration has to say what happens to those rows rather than
failing on them, and the honest answer is that it refuses while any exist.

## Open Questions

- **Several verified addresses per user.** The right relief for V2's friction. It touches sign-in,
  password reset and the identity model, so it is deferred rather than sketched here.
- **Should an invitation be re-issuable to a different address?** Currently no — that is a revoke and a
  new invitation. It may be worth it if support finds people are commonly invited at the wrong address.
- **How long revoked and accepted invitations are retained.** They are audit evidence, so retention
  should probably follow the audit trail's policy rather than having one of its own, but the audit
  retention question is itself open in `staff-plane`.
- **Whether an account may cap its own membership.** Some customers will want "no more than N members"
  as a control of their own. Entirely additive; not needed to make the flow work.
- ~~**Whether the owner must remain an unrestricted member.**~~ Raised by V5, answered in
  `personal-and-team-accounts` C16: yes, always, and the only way out is to transfer the account.
  Recorded here because inviting is where it first bit.
- **How long a member may stay roleless.** V6 makes it a legal state with a legible screen, and puts no
  bound on it. A member who is never given a role sits in the roster indefinitely, seeing an account
  they cannot act in. Whether that wants a nudge, a reminder or nothing at all is a question for
  whoever first watches it happen to a real customer.
