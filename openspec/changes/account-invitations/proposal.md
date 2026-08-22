## Why

### An account of one is the only account anyone can currently build

`tenancy-and-permissions` establishes that a **user** — a person, global, owned by nobody — may create
an **account** — the billing entity that holds credits, owns projects and anchors every permission
grant — and becomes that account's unrestricted member in the same operation. That is the whole of
membership as it currently stands: you are a member of the accounts you created.

So a company can sign up, and then cannot add a colleague. The `roles` table that
`tenancy-and-permissions` introduces, with custom roles and per-object grants, has exactly one member
to hand them to. Everything built to let an account distribute authority is unreachable until there is
a second person in the account to distribute it to.

The missing piece is small to state and unusually easy to get wrong: a way to put a person you can
name only by email address into an account you control.

### Why it is worth its own change

Inviting somebody is not a permission problem, which is why it does not belong in
`tenancy-and-permissions`. It is an *identity* problem wearing a membership problem's clothes.

The account holder types `alice@corp.com`. At that moment the service knows nothing about Alice: not
whether she has a user, not whether she will sign in with that address, not whether she is even the
person who will read the mail. Everything difficult about this flow comes from that gap between an
address and a user, and none of it is about grants:

- The mail may be forwarded. If holding the link is sufficient to join, an invitation is a
  **transferable grant of access to a billing entity**, and nobody who forwarded a message to a
  colleague intended that.
- The invited address may already have a user, or may not, and the flow has to branch to sign-in or
  sign-up without the *inviter* learning which — the same non-disclosure that registration already
  carries in `add-accounts-and-public-edition`.
- The flow sends mail to an arbitrary address on request from an authenticated user. That is a spam
  vector pointed at third parties, and it needs a limit before it exists rather than after somebody
  finds it.

None of those touch the permission model. All of them touch email, tokens and authentication, which is
what `add-accounts-and-public-edition` already builds.

### Dependencies, and what is assumed from each

**`tenancy-and-permissions`** — assumed complete. Specifically: users exist globally and outlive the
accounts they belong to; an account has exactly one unrestricted role which always has at least one
member; memberships bind a user to an account with a role; decision P14 establishes that **the account
is not an ACL-addressable object**, so operations acting on the account itself are governed by
membership rather than by grants; the member roster is already specified as readable by any member of
the account without a grant, *including outstanding invitations*; and decision P19 establishes that a
session is in exactly one of two states — **no account selected**, or one account selected — where a
session with no selection may do exactly four things, one of which is **accept an invitation**.

This change does not re-specify the roster or the selection rule. It specifies only what an invitation
contributes to the roster, and where its operations sit relative to the selection.

**`public-edition`** — supplies the identity provider a person enrols with, email delivery, and the
token language this change reuses verbatim rather than paraphrasing:
a token that *"SHALL expire, SHALL be usable once, and SHALL NOT be guessable"*, and the rule that a
response *"does not differ from one for an address that does not"* have a user.

## What Changes

### The flow

```
inviter enters email + role
   └─> [mail] ──> accept link (token)
                     └─> authenticated?  ── no ──> login or signup
                                          └─ yes ─┐
                                                  └─> "Join <account> as <role>?" ── accept
```

Worked through with real values. Cloudpowered Ltd holds an account named `cloudpowered`, in which
`bob@cloudpowered.example` is an unrestricted member. He invites `alice@corp.com` as `Platform
Operator`, a custom role in that account granting `[project, <staging>, *]` inheritable.

1. Bob submits the address and the role. An invitation row is written and a mail is sent. Bob's
   response says the invitation was sent, and says nothing else — it is identical whether or not
   `alice@corp.com` already has a user.
2. The invitation appears in the account's roster immediately, as pending, visible to every member.
3. Alice opens the link. Nothing has happened to her account yet; opening the link **does not consume
   the token**.
4. If no user exists for `alice@corp.com`, she is offered sign-up. The user created is given
   `alice@corp.com` **already verified**, because holding a link sent to that address is the same proof
   of control that an email confirmation token provides. No second confirmation mail.
5. If a user exists for `alice@corp.com`, she must authenticate as that user.
6. Either way she is then asked, explicitly: *"Join cloudpowered as Platform Operator?"* Accepting
   creates the membership and consumes the token.

### Only an unrestricted member may invite

Inviting a user, and changing or removing a membership, requires membership of the account's
unrestricted role. This is not a new rule — it is decision P14 of `tenancy-and-permissions` applied:
the account is not an object grants can name, so operations on its membership are governed by
membership.

It has a consequence worth naming rather than discovering: **no subset check is needed on the role an
invitation confers.** The rule that a subject may grant only what it holds exists to stop a partially
privileged member from writing a grant they do not themselves hold. An unrestricted member holds
everything in the account by definition, so there is nothing to check — the invitation can name any
role in the account and it is necessarily within the inviter's authority.

The trade, accepted deliberately: **administrators become a bottleneck as an account grows.** A team
lead who runs a project cannot bring somebody onto it without an administrator. The alternative — a
grantable `invite` permission — would require making account membership an ACL-addressable object,
which is exactly the boundary P14 exists to hold. If the bottleneck becomes real, the answer is
probably a second built-in role rather than dissolving that boundary.

### Invitations are bound to the address, not to whoever holds the link

**Only a user with the invited address may accept.** A forwarded link grants nothing.

This is the decision with the most subtlety in it, because there is a way of implementing "bound" that
quietly is not. Holding a link sent to `alice@corp.com` *is* proof of control of `alice@corp.com` — it
is precisely the proof an email confirmation token provides, and this change relies on that in step 4
above to skip a confirmation mail. If the service also offered *"add `alice@corp.com` to the user I am
currently signed in as"*, then anyone forwarded the mail could attach the address to their own user and
accept — and bound would have collapsed into bearer with two extra clicks.

So that path is not offered, and there are exactly two outcomes:

| State of the invited address | Outcome |
| --- | --- |
| No user exists for it | Sign-up, creating a user with that address already verified, then the accept question |
| A user exists for it | That user must authenticate; authenticating as any other user is refused |

The refusal names the invitation as not being for the authenticated user and shows the invited address
**masked** — `a**e@corp.com` — because somebody holding a forwarded link should not learn the full
address from a message telling them they may not use it.

**The honest cost.** A person invited at `alice@corp.com` who habitually signs in as
`alice@gmail.com` must create a user for the work address. For a billing entity belonging to a company,
that is arguably the right outcome — the membership belongs to the work identity, and survives her
personal address changing. It is nonetheless friction, it will generate support questions, and the
resolution is a later change adding several verified addresses per user rather than anything in this
one.

### Acceptance is reachable without an account selected; issuing one is not

Decision P19 of `tenancy-and-permissions` puts every session in one of two states, and names accepting
an invitation as one of the four operations available with **no account selected**. That is not an
incidental permission — it is what makes the flow work at all. Alice signs up because she was invited;
at the moment she authenticates she belongs to nothing, so requiring a selected account before she can
accept would be a closed loop with no way in.

The two halves of this change therefore sit on opposite sides of that line:

| Operation | Selection | Route |
| --- | --- | --- |
| Accept an invitation | none required | outside the account-scoped tree |
| Issue, revoke, re-send an invitation | account selected, unrestricted role | `/api/v1/accounts/{accountId}/…` |
| Read the roster and its pending invitations | account selected, any member | `/api/v1/accounts/{accountId}/…` |

Reading the invitation the link refers to — enough to render *"Join cloudpowered as Platform
Operator?"* — is part of the accept flow and likewise needs no selection. It is a read of the account's
name and the role's name, addressed by the token rather than by the account, and reveals nothing beyond
what the mail already said.

**After acceptance the newly joined account becomes the session's selection.** Leaving somebody in the
empty state immediately after they joined something is a dead end, and their next intent is obviously
to act in the account they just joined. This is a selection change like any other: it re-evaluates the
membership — trivially satisfied, it was just written — and is recorded like any other selection.

### Acceptance is explicit, authenticated, and the only thing that consumes the token

Opening the link never creates a membership. The token is consumed on acceptance, not on arrival — so a
link followed by a mail-scanning proxy, a link preview generator, or a curious recipient who then
closes the tab, is still usable by Alice afterwards.

A leaked link is therefore not by itself a membership: it is an opportunity to attempt an acceptance
that will be refused unless the attempter can authenticate as the invited address.

### Everything else the flow needs

Invitations expire, are revocable, are re-sendable (which invalidates the previous token), fail closed
if the role they name has been deleted, and are rate limited per account and per invited address. Each
of those is specified, and the reasoning is in the design document rather than here.

### This is the entry path while self-registration is closed

`public-edition` specifies a switch that turns self-service registration off at the server, and the
first months of the product run with it off: the first customers are people we know, invited by name.
That makes this change the *only* way a person becomes a user during that period, which raises its
priority relative to everything downstream of it, and adds one interaction worth naming here.

`staff-plane` keeps a blocklist of email addresses belonging to people who have abused the service.
An invitation to a blocked address is checked when the invitation is **accepted**, not when it is
issued. Issuing succeeds; acceptance is refused; the account's outstanding list says the address
cannot accept.

That ordering is deliberate. Refusing at issue would tell whoever is inviting that a particular
address is on our blocklist — information about a third party that an account administrator has no
business receiving from us. Refusing at acceptance keeps the fact where it belongs, while still
giving the administrator a plain answer about why nobody ever joined, rather than an invitation that
silently disappears.

### Not in scope

- **Several verified addresses per user.** The obvious relief for the friction above, and a change of
  its own — it touches sign-in, password reset and the identity model rather than invitations.
- **Removing a member, and what happens to what they made.** Membership removal is bounded by the
  last-unrestricted-member guard `tenancy-and-permissions` already specifies; the wider question of
  reassigning objects a departing member created is not answered here, because nothing in this model
  attributes ownership to a member — objects belong to the account.
- **Domain-based automatic joining** (anyone with a `@corp.com` address may join the `corp` account).
  A common convenience and a common breach; it requires proving control of the domain, which is a
  different mechanism.
- **Inviting into the internal edition.** Memberships there are derived from identity-provider group
  claims and stored nowhere, so there is nothing for an invitation to write. The flow exists only where
  memberships are rows.

### What we considered and rejected

**Bearer invitations** — whoever holds the link and can authenticate may accept. This is what GitHub and
Slack do, and it handles the common case where somebody is invited at one address and signs in with
another. Rejected because an invitation is access to a **billing entity** — an entity holding prepaid
credits, owning zones that answer authoritatively for the company's domains. A forwarded message should
not carry that, and "I forwarded it to my colleague to deal with" is a thing people do constantly
without imagining they have transferred anything.

**Bound with a re-address step** — bound by default, but a holder may ask the inviter to re-send to an
address they control. It removes the dead end without silently transferring anything. Rejected for this
change as a second flow and a second notification for a case that a later multi-address feature solves
better; the option is recorded because it may return.

**A grantable `invite` permission** — makes inviting delegable to a team lead, bounded by the subset
rule. Rejected because it requires an account-level object for grants to name, which is the boundary
decision P14 draws; see above.

**Inviting a user rather than an address** — pick from a directory of existing users. It removes the
address-to-user gap entirely. Rejected because it requires a globally searchable user directory, which
is both a privacy problem and useless for the case that matters: the colleague who does not have a user
yet.

## Capabilities

### New Capabilities

- `account-invitations`: the invitation entity and its lifecycle — who may issue one, what it names,
  how it is bound to an address, how it is accepted, revoked, re-sent and expired, and the limits on
  issuing them.

### Modified Capabilities

- `identity-and-roles`: an invitation link is added as a means of proving control of an email address,
  so sign-up reached through one produces a user whose address is already verified without a second
  confirmation.
- `audit-trail`: the vocabulary gains the invitation lifecycle — issued, revoked, accepted, expired —
  recorded with the acting user and the invited address.
- `admin-console`: the console gains the invitation flow, on both sides — issuing and managing
  invitations within an account, and the accept screen the invited person reaches.

## Impact

- **Schema.** One new table, `invitations`, holding the account, the role offered, the invited address,
  a hash of the token, the issuing user, and timestamps for issue, expiry, revocation and acceptance.
  Identifiers are UUIDv7 like everything else. No existing table changes shape; a membership created by
  acceptance is an ordinary `account_members` row.
- **Tokens.** The stored form is a hash, never the token, following the same reasoning as
  `api_keys.secret_hash` — a datastore read must not yield a usable credential.
- **Email.** A new template and a new send path, on the delivery mechanism
  `add-accounts-and-public-edition` introduces. This change adds no mail infrastructure of its own.
- **Authentication.** The sign-up path gains an entry point that pre-verifies an address, and the
  sign-in path gains a return-to-invitation redirect. Neither changes how sessions work.
- **Rate limiting.** The first outbound-mail-on-demand surface reachable by an ordinary tenant, so the
  limit is part of this change rather than a later hardening pass.
- **Console.** An invitation panel within the account, and an accept screen that must render for a user
  who may hold no membership at all — the empty state `tenancy-and-permissions` introduces is where
  this screen lives.
- **Nothing on the permission model, the DNS path or the challenge path.**
