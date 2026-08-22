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
  role_id       uuid        NOT NULL REFERENCES roles (id),
  email         text        NOT NULL,
  token_hash    bytea       NOT NULL,
  invited_by    uuid        NOT NULL REFERENCES users (id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  revoked_at    timestamptz NULL,
  accepted_at   timestamptz NULL,
  accepted_by   uuid        NULL REFERENCES users (id)
);
CREATE UNIQUE INDEX invitations_pending_idx
    ON invitations (account_id, email)
 WHERE accepted_at IS NULL AND revoked_at IS NULL;
```

`role_id` references `roles` without `ON DELETE CASCADE`, deliberately — see V6.

**Only `token_hash` is stored, never the token**, following the reasoning already applied to
`api_keys.secret_hash`: a datastore read must not yield a usable credential. Verification is a lookup by
hash and a constant-time comparison, exactly as `internal/apikey` does it. The token is high-entropy
random, so a memory-hard hash buys nothing over SHA-256 — the same argument the `apikey` package doc
already makes.

The partial unique index is what makes V7 (one outstanding invitation per address per account)
structural rather than a check somebody has to remember.

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

### V4. Acceptance is the only thing that consumes the token

Opening the link creates nothing. It resolves the invitation, renders *"Join cloudpowered as Platform
Operator?"*, and stops.

This matters for a mundane reason: links in email are opened by things that are not people. Mail
security gateways follow every URL in a message to scan it; clients generate link previews; a recipient
opens the tab and closes it again. If arrival consumed the token, a meaningful proportion of invitations
would be dead before the invitee ever saw them, and the failure would look like a bug in the mail rather
than a design decision.

It also gives the security property its clean statement: **a leaked link is not a membership.** It is an
opportunity to attempt an acceptance that will be refused unless the attempter can authenticate as the
invited address.

Rendering the accept question requires reading the account's display name and the role's display name,
addressed by the token. That reveals nothing the mail did not already say.

### V5. Accepting needs no selection; issuing one requires it

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

### V6. A deleted role fails the invitation closed

Between issuing and accepting, an administrator may delete the role the invitation names. Three options,
and only one is safe.

**Chosen: refuse the acceptance, and tell the account.** The invitation moves to a failed state; the
accept screen says the invitation is no longer valid and to ask for a new one; the roster shows it as
invalid so a member can see why and re-issue. `role_id` therefore references `roles` **without**
`ON DELETE CASCADE` — deleting a role is refused while an invitation names it, or the invitation is
explicitly invalidated first. Either way the outcome is visible rather than silent.

**Rejected: substitute another role**, such as the least privileged one. It sounds forgiving and it means
Alice joins with authority nobody chose for her. Silently altering what a grant of access confers is
never the kind thing to do.

**Rejected: let acceptance succeed with the role that was named**, resurrecting it. The role was deleted
deliberately.

### V7. One outstanding invitation per address per account; re-sending invalidates the previous token

At most one invitation for a given address may be outstanding in a given account, enforced by the partial
unique index in V1.

**Re-sending issues a new token and invalidates the old one.** Anything else means several live tokens
for one offer, so revoking becomes "revoke all of them" and the invitee holds two links that behave
differently. Re-sending is the natural response to "I never got it", and it must not leave the earlier
one working — that earlier message is exactly the one that might have gone somewhere unintended.

Re-sending may also change the role offered; that is an amendment of the offer, and it produces a new
token for the same reason.

**Revoking** marks the invitation revoked and its token stops resolving immediately. A revoked invitation
leaves the roster's pending list and is retained for the audit trail.

**An address that is already a member is refused at issue time**, not at acceptance. The alternative
wastes a mail and produces a confusing dead end for the recipient. This is not a disclosure problem:
membership of *this* account is already visible to the inviter through the roster.

### V8. Expiry is seven days, and an expired link says so

Seven days. Long enough to survive a weekend, a holiday Monday and a full inbox; short enough that a
message sitting in an archive is not a live credential months later.

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
ActionInvitationAccept = "invitation.accept"
ActionInvitationExpire = "invitation.expire"
```

Each entry names the invited address and the role offered. `invitation.accept` names both the accepting
user and the address, which is what makes a bound-acceptance audit trail meaningful — it is the evidence
that the person who joined is the person who was invited. `invitation.expire` is written by the system
actor, matching `apikey.expire`.

The token never appears in an entry, in either the before or after state. `authz.Record` already scrubs
secret-looking fields; `token_hash` is excluded explicitly rather than relying on the field name
matching a heuristic.

## Risks / Trade-offs

- **The mismatched-identity dead end** (invited at work, signs in personally) → named honestly in V2
  rather than smoothed over; the accept screen shows the masked address so the recipient can see which
  identity is wanted; the real fix is multi-address users, recorded as an open question.
- **Administrators are a bottleneck for onboarding** → accepted consequence of V5/P14; the escape, if it
  becomes real, is a second built-in role rather than making account membership ACL-addressable.
- **Mail is a spam vector** → three limits in V9, including a cross-account one, because per-account
  limits are defeated by creating accounts.
- **A leaked link plus a compromised mailbox is a membership** → true, and irreducible: control of the
  address is the whole proof. Bounded by V8's seven days, by V4's explicit acceptance, and by the roster
  making a pending invitation visible to every member of the account, so an unexpected one is noticeable.
- **A mail gateway follows the link before the invitee does** → V4 consumes the token only on acceptance,
  so scanning is harmless.
- **An account deleted between issue and acceptance** → `ON DELETE CASCADE` on `account_id` removes the
  invitation with it, and the link resolves to nothing, which is correct: there is nothing to join.
- **Enumeration of the invitation endpoint** → tokens are unguessable by the inherited requirement, and
  resolution is a hash lookup, so the endpoint offers nothing to enumerate. It is nonetheless rate
  limited by address like the rest of the flow.

## Migration Plan

1. Create `invitations` with its partial unique index.
2. Add the invitation actions and object type to `internal/authz/vocab.go` and to the credential class.
3. Add the mail template and the send path to the delivery mechanism
   `add-accounts-and-public-edition` provides.
4. Add the account-scoped issue, revoke and re-send routes under `/api/v1/accounts/{accountId}/…`.
5. Add the selection-free resolve and accept routes outside the account-scoped tree.
6. Add the sign-up entry point that pre-verifies the invited address.
7. Add the console panels: invitations within the account, and the accept screen in the no-selection
   shell.

Nothing needs backfilling: there are no invitations before this change exists. Rollback is dropping the
table and the routes; a partially rolled-out state leaves outstanding invitations unresolvable, which
presents as expiry.

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
