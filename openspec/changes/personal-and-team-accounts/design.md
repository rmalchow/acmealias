# Design

Decisions are numbered `C1`, `C2`, … and referred to by number from `tasks.md`
and from code comments, the way `P`-numbers from `tenancy-and-permissions` and
`U`-numbers from `public-edition` already are.

## C1. Two kinds of account, distinguished by a column, and neither becomes the other

`accounts` gains `kind`, one of `personal` or `team`, fixed at creation.

**The personal account is implicit.** There is no "create a personal account"
flow and no form asking for a name and a slug: a user has one because they are a
user. The row is materialized the first time they sign in, and the picker shows
it as a distinct entry — "Personal" — rather than as a named account sitting
among the team ones.

That is a real simplification and not only a cosmetic one. It removes the
empty-handed state entirely: nobody ever signs in with nowhere to work, so the
first thing a new user sees is somewhere they can already add a project rather
than a form. And it makes C2's limit structural rather than enforced — a user
cannot have two personal accounts because the account is a property of the user,
not a thing they can ask for twice.

The row still exists, with a real id, because everything in the system carries an
`account_id` and every read is confined by `Scope`. Implicit describes how it
comes to be, not whether it is real.

**Naming.** A personal account's name is derived from the user and is not part of
the tenant's vocabulary. It is not slug-addressable the way a team account is —
there is nothing to disambiguate, since a user has exactly one.

**A personal account is never upgraded.** There is no path from personal to team,
and the reason is that the alternative is worse in a way that only shows up
later. An upgradeable personal account has to answer what happens to it when the
balance lapses — does it fall back to free, keeping its projects and losing its
members? Then "free" and "suspended" are the same state wearing two labels, and
the free tier becomes a place accounts land rather than a place they start. Two
kinds that never convert have no such state.

The cost is real and smaller than it first looks. Somebody who outgrows their
personal account builds their inventory again inside a team account — and because
everything here is API-addressable, that is a script they can run against us
rather than data they have to retype. A first-class migration is worth having and
is not MVP; what makes deferring it acceptable is that the workaround is
mechanical rather than manual.

**A personal account's owner never changes.** Not "changes rarely" — the column
is immutable and the service refuses a transfer. The account is the user's own
workspace; handing it to somebody else would make it a team account with the
team parts missing.

## C2. One free account per user, enforced at creation and at transfer

A user may own at most one `personal` account. There is no limit on `team`
accounts.

Because the personal account is implicit (C1), the limit is mostly structural:
there is no request that asks for one, so there is no request to refuse. What
remains are the two ways a second one could still appear, and forgetting the
second is how the rule leaks:

- **Materialization.** The first sign-in creates the row. Two concurrent first
  sign-ins for the same user must produce one account, not two — so the partial
  unique index is the enforcement and the code path is merely the common case.
- **Transfer.** A team account's ownership may be handed over; a personal
  account's may not (C1). So there is no transfer that can produce a second
  personal account — but the check belongs in the transfer path anyway, because
  the moment somebody adds "transfer a personal account for support reasons",
  the limit is silently gone.

A pleasing consequence, and one that falls out rather than needing a rule:
because team accounts are created by paying rather than by upgrading, paying does
not consume the free slot. A user keeps their personal account and their team
accounts alongside each other.

## C3. Funded means `credits > 0`, and nothing else

Not a threshold, not a grace period, not a low-water mark. One comparison, in one
function, so that "is this account usable" has exactly one answer everywhere it
is asked.

The state machine has two states and one door:

```
        ┌──────────────┐   credits reaches 0   ┌──────────────┐
        │    FUNDED    │ ────────────────────▶ │  SUSPENDED   │
        │              │ ◀──────────────────── │              │
        └──────────────┘   checkout credits    └──────────────┘
```

`personal` accounts are outside this machine entirely. They have no balance and
are always usable — being free is what they are.

## C4. Suspension stops everything, and this reverses `metering-and-credits`

A suspended team account:

| Surface | Behaviour |
| --- | --- |
| Account selection, for a member | refused — it cannot be picked |
| Account selection, for the owner | **allowed** |
| What the owner then sees | the balance, and checkout. Nothing else |
| The management API, account-scoped routes | refused |
| The challenge API (`/present`, `/cleanup`) | **refused** |
| Existing DNS answers already published | withdrawn on the normal schedule |

**This is a deliberate reversal of a decision already written down.**
`metering-and-credits` argues for an overdraft on the grounds that *"the
overdraft keeps what you have alive; it does not buy you anything new"* — a
lapsed account keeps answering for the domains it already has and is refused only
new ones. That is kinder, and it is rejected here for two reasons.

First, it needs a third state. "Funded", "overdrawn but serving", and "stopped"
are three behaviours, three predicates and three explanations in the console, and
the middle one is the hardest to describe to somebody who has not paid.

Second, and more decisive: the overdraft's kindness is largely an illusion at
this timescale. Refusing `/present` does not take a customer's site down; it stops
renewals, and the certificate on the wire keeps working until it expires. The
customer has weeks either way. An overdraft buys a softer version of a
consequence that is already soft, at the cost of a state nobody can explain.

**What the overdraft was really protecting against is worth keeping**, and it is
not the refusal — it is the surprise. `metering-and-credits` says so itself:
*"The mitigation is not a different overdraft, it is a different warning."* The
warning path is therefore in scope here (task group 6) even though the ledger is
not.

## C5. The owner of a suspended account sees the balance and the way to fix it, and nothing else

Not the console in read-only. Two reasons.

A read-only console has to answer, screen by screen, what "read-only" means for
every control — and every one of those answers is a second permission model
running alongside the real one, evaluated in a state nobody exercises. The
disabled-with-a-reason treatment that the console now uses for permissions would
have to grow a second reason, and "you cannot do this here" and "you cannot do
this until you pay" are different sentences with different remedies.

And the screen has one job. An owner arriving at a suspended account wants to
know what it costs to make it work again. Everything else on the page is in the
way of that.

## C6. Two suspensions, and they must not be able to lift each other

`staff-plane` has a suspension of its own, for abuse. That one must not be
liftable by paying, and this one must not be liftable by staff forgetting to
re-enable something.

They are therefore separate pieces of state, and usability is the conjunction:

```
usable(account) = kind == personal
                  OR (credits > 0 AND NOT staff_suspended)
```

Written that way, and in one place, so that adding a third reason later is one
clause rather than an audit of every call site.

## C7. The funding check sits where the account is established, not where the request arrives

For the management API this is easy: `scope()` already resolves the account for
every request, and the check goes there.

For the challenge API it is more interesting, and the placement matters.
`/present` does not know which account it belongs to until it has resolved the
alias — that is the deliberately global read that `CrossAccount` exists for, and
it is what establishes the account rather than being filtered by it. So the
funding check goes **immediately after alias resolution and before anything is
published**: the account is known, the key has been authenticated, and no state
has changed yet.

The refusal must be distinguishable from an authentication failure. A `401` sends
an operator to look at their API key, which is not the problem. This answers
**402 Payment Required** with a message naming the account and saying the balance
is zero — one of the few genuinely correct uses of that status code, and it lands
in the logs of a machine whose owner needs to understand it without reading our
documentation.

## C8. Checkout is the whole mechanism minus the processor

`POST /api/v1/accounts/{accountId}/checkout` with an amount of 25, 50 or 100
EUR credits the account by that amount and returns.

Three properties, and the third is the one that makes this worth building now:

- **The amount is a fixed choice, not a number the caller supplies.** A free-text
  amount is an invitation to credit an account with a million by sending a
  different body, and there is no processor in the way to notice.
- **It is a real charge.** The same row, the same balance change and the same
  audit entry a real payment will produce. A fake that writes different state
  proves nothing about the path that replaces it.
- **It is auditable and attributable.** Who credited what, when, to which
  account. That is the record that has to be right on the day money is involved,
  so it is right now, while nothing is.

**Rejected: a staff-only credit grant instead of a customer-facing checkout.**
`metering-and-credits` mentions staff credit-granting, and that mechanism will
exist. It is not a substitute: the thing being exercised here is the flow a
customer walks, and a staff tool exercises none of it.

**Rejected: making it obviously fake in the UI.** A banner reading "no payment
will be taken" would be honest and would also mean the screen being tested is not
the screen that ships. The flow reads as the real one; what makes it safe is that
there is no processor behind it and no way to reach it from outside development.

## C9. Roles exist only in team accounts, but grants still scope API keys

A personal account has no roles, because it has nobody to give one to. The roles
and membership screens are absent there rather than empty.

**The grant model is not conditional, and this distinction matters.** Roles are
how *people* get authority. Grants are also how an *API key* is scoped, and a key
narrower than its holder is good practice in an account of one — the subset rule
of P10 stays live in a personal account, applied to keys.

So: no roles UI, no memberships, no invitations in a personal account; the key
dialog keeps its grant composer everywhere. Anyone deleting the roles machinery
along with the roles screen will break key scoping.

## C11. One affordance carries the message, not a row of disabled ones

Two rules that look contradictory, and the line between them is *where* the thing
sits rather than *why* it is unavailable.

**Inside a screen, nothing is hidden.** A control the caller lacks a grant for is
shown and disabled and says so, because an absent button teaches nothing: someone
who cannot do a thing cannot tell an unavailable feature from one they lack the
rights for. That rule is already in force and does not change.

**In the navigation, a screen the account does not have is not listed.** An
earlier draft of this decision said to list them disabled, for symmetry. That is
wrong, and the reason is that navigation is not a set of controls — it is the map
of the product. A map with four greyed-out destinations does not read as "you
could go here"; it reads as clutter, on every screen, forever, for a customer who
may never want any of them.

What the disabled-control rule is actually protecting is the *remedy*: somebody
must be able to learn that the capability exists and what would unlock it. In
navigation that is served better by **one affordance that names the remedy** than
by four that describe the lack:

```
  PERSONAL                          TEAM (funded)
  ────────                          ─────────────
  Projects                          Projects
  Upgrade  ◀── the whole message    Audit
                                    Settings
                                    (Live challenges — later)
```

`Upgrade` is not a disabled `Settings`. It is the positive form of the same
sentence, pointing at what to do rather than at what is missing, and it costs one
entry instead of several.

The distinction to hold on to: **"forbidden" and "does not exist" still have
different remedies, and must never share wording.** A personal-account owner told
they lack permission goes looking for whoever can grant it, and there is nobody.
What changes here is only that the second sentence is delivered once, by
`Upgrade`, rather than repeated by every screen that is not there.

## C13. Five projects in a personal account. In a team account, credit is the only limit

A personal account holds **five projects**. A team account has **no project
quota at all** — what limits it is the balance.

That asymmetry is the point rather than an inconsistency. A team account is
metered: what it does draws the balance down, and running more projects costs
more. Capping projects on top of that would charge twice for the same growth,
once in money and once in permission, and the second charge buys nothing — a
customer who is paying for what they use has already answered the question a
quota exists to ask.

The free account is the one that needs a number, because nothing else limits it.
Five is that number.

**It is configuration, not a literal in the code.** The number is a pricing
decision wearing an engineering hat, pricing changes on a different clock from
deployments, and the first time somebody wants one customer to have six is the
day a constant becomes a migration.

It is also **a quota and not a permission**, which the console has to keep
straight (C11 has the same shape for a different pair):

| | Permission | Quota |
| --- | --- | --- |
| The question | *may* this caller do it | is there *room* |
| Refused with | 403, until somebody grants it | 409, until you delete one or move to a team account |
| Fixed by | a grant | deleting a project, or paying |

Collapsing them means "upgrade to add a project" and "ask an administrator for a
grant" become the same error, and no single message is right for both.

## C12. The account switcher is a search box with a way to buy the next one

The navbar carries a searchable select of the accounts a user may act in, and an
**Add account** action beside it.

- **Searchable**, because a user who pays for several accounts will have several,
  and a dropdown that requires scrolling is a dropdown that gets the wrong one
  picked.
- **Inert with one account.** A user with only their personal account sees the
  control showing that account and nothing to choose. It is present rather than
  absent so the navbar does not change shape the moment they buy their second —
  and it is inert rather than disabled, because there is nothing wrong.
- **Add account is the buy flow.** A team account is created by paying (C1), so
  "add" and "buy" are the same act. It opens an explanation of what a team account
  is and a **Buy now** that enters checkout (C8).

`Upgrade` in a personal account's navigation and `Add account` beside the switcher
reach the same place. That is deliberate rather than redundant: one is found by a
person looking at what this account cannot do, the other by a person looking at
which account they are in. They are two entrances to one room, and neither is
where the other person would look.

**Live challenges is deferred**, not dropped. It is the only screen that shows
what the service is doing *right now*, which is worth having when something is
wrong and worth nothing the rest of the time. It stays out of the first
navigation and is reconsidered when somebody has actually wanted it.

## C14. A personal account's slug is derived from its owner's email

`accounts.slug` is `not null unique`, and a personal account is never named by
anybody (C1). So one is derived, from the owner's email address.

**The derivation has to be sanitised and disambiguated**, because an email is not
a slug. `dnsname.ValidateLabel` governs the column's shape, and addresses contain
characters it refuses and distinctions it erases: `a.b@x.com` and `a-b@x.com`
both reduce to `a-b-x-com`. A collision is therefore possible between two real
people, and the derivation must carry a discriminator rather than fail the second
person's first sign-in.

**What this costs, and it is worth stating rather than discovering.** A slug that
encodes an email is an email in the database in a second form, and a globally
unique slug is checkable. If a slug is ever exposed on a surface a stranger can
probe, "does `ruben-example-com` exist" answers "does this person have an
account" — the same membership-oracle shape the alias label reasoning already
rejects for a public zone. Two things keep that acceptable today: accounts are
addressed by id in every route, and the slug is not reachable from outside the
account. **Neither is a property to lose casually**, and a future change that
makes slugs public has to revisit this rather than inherit it.

**Rejected: making the column nullable.** It would be honest — a personal account
genuinely has no name — and it would put a null check into every path that reads
a slug, for the benefit of one account kind. A derived value is a smaller lie
than a null that half the code forgets to handle.

## C15. Deleting a user cascades into their personal account and is refused for a team one

A person who leaves is deleted. Their personal account goes with them; a team
account they own stops the deletion.

**The database already says both**, and the application only has to respect the
order:

```
  accounts.owner_user_id   references users    on delete RESTRICT
  projects.account_id      references accounts on delete RESTRICT
  account_members.user_id  references users    on delete CASCADE
```

So deletion is an explicit, ordered operation: remove the personal account's
projects, then the account, then the user. Their memberships of other accounts
vanish on their own. If they own a team account, `RESTRICT` refuses the delete and
the error is the honest one — *this person still owns something other people are
using* — rather than an orphaned row discovered months later.

**The restricts are the backstop, not the mechanism.** A deletion path that gets
the order wrong fails loudly instead of half-completing, which is the property
worth having in an operation nobody performs often enough to remember.

**A team account whose owner leaves has no automatic answer, deliberately.** The
alternatives are all worse: transferring to an arbitrary member picks somebody
who did not agree to be responsible for a bill; deleting the account destroys
other people's work; and leaving it ownerless makes the owner column a lie. What
exists instead is `adminctl`'s transfer verb — somebody decides, and the decision
is recorded. This is the one place in the model where the answer is "a human
handles it", and saying so is better than a default nobody would have chosen.

**That settles a question the migration squash reopened.** `adminctl`'s transfer
verb was originally justified by a state a migration produced, and when the chain
was flattened that state stopped being reachable — which made the verb look as
though it had lost its reason. It has not. Transfer is the ONLY way to make a
departing owner deletable, because the database refuses both to delete a user who
owns a team account and to reassign one on their behalf. The verb is load-bearing
for the account lifecycle, not a repair tool left over from an upgrade.

## C10. Removing the internal edition is part of this change, not a follow-up

It is tempting to split, because a deletion has a different risk profile from a
feature. It is not separable: "every user has their own personal account" and
"everybody shares one account created at migration time" cannot both be true, so
an intermediate state where the first is built and the second still exists is not
a state the service can run in.

What is removed:

| Thing | Where |
| --- | --- |
| `ScopeForBootstrap` | `internal/store/scope.go` |
| Automatic account selection at sign-in | `internal/oidcauth/flow.go` |
| Group-to-role mapping | `internal/authz`, `ACME_*_GROUP` settings |
| Derived membership | P18's second branch |
| The four fixed roles as a global vocabulary | migration seed |
| Constraint 1 | `tenancy-and-permissions` |

**The four roles do not simply vanish.** They become the *default role set a new
team account is seeded with* — the same names, the same grants, but rows in one
account rather than a global vocabulary configured by environment variable. An
account's owner may then edit or delete them, which they never could before.

**Constraint 1's removal deserves a note in the archive rather than a silent
deletion.** It was the property `tenancy-and-permissions` was measured against,
it is what task 11.4 proved, and it did real work: it is why the four seeded roles
reproduce the old permission matrix exactly, and why migration 000009 exists. That
work was not wasted — it is what kept the service running while the model was
rebuilt underneath it — but the constraint has now been discharged and is being
retired, not violated.
