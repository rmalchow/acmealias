# Tasks

> **Order matters here more than usual.** Group 2 removes the edition the service
> currently runs as, and nothing works between the first task of group 2 and the
> last task of group 3. Do not stop in the middle.

## 0. Found later: the owner can be demoted out of their own account

> Surfaced while implementing `account-invitations`, which needed to know who may invite. Recorded
> here because it is this change's model, not that one's.

- [x] 0.1 `store.CountUnrestrictedMembers` guards that the unrestricted role keeps at least ONE member. It does not guard that the member it keeps is the OWNER — the person `Account.owner` names, who is the only one who may pay
- [x] 0.2 So a team can demote the person being billed. They then cannot invite anybody, cannot assign a role, and cannot reach the roster of an account that charges their card — while the people who removed them carry on
- [x] 0.3 Decide the rule. "The owner is always in the unrestricted role" is the obvious one and it has a cost: it means the owner cannot be removed even at their own request, so transferring ownership becomes the only way out — which `account-invitations` and the transfer flow already assume. **Decided as written** (design C16): `store.RemoveMember` refuses the owner's unrestricted membership with `ErrOwnerMustBeUnrestricted`
- [x] 0.4 Whatever is decided, the refusal has to say WHICH constraint it hit. "The account must keep an administrator" and "the owner must stay an administrator" send somebody to two different actions. Two errors, both 409, distinct sentences; the owner check runs FIRST because when the owner is the only administrator both hold and only one names an action that works. The console labels the row "account owner" or "last administrator" and carries the remedy
- [x] 0.5 Found while implementing 0.3: both transfer paths asked `store.IsMember`, which answers "is in the account at all", so handing an account to somebody already in it as `READ_ONLY` produced an owner who could not administer it — and after 0.3 that is a violated invariant, not merely awkward. Replaced by `store.EnsureUnrestrictedMember`, called inside the transfer transaction by `httpapi.transferAccount` and `adminctl.AccountTransfer`

## 1. The two kinds of account

- [x] 1.1 Add `accounts.kind` (`personal` | `team`), `accounts.owner_user_id` and `accounts.credits` to the model and a migration (design C1)
- [x] 1.2 Make `owner_user_id` `NOT NULL` and backfill it; every account has exactly one owner and always did in practice
- [x] 1.3 Add the partial unique index that enforces one personal account per user — `UNIQUE (owner_user_id) WHERE kind = 'personal'` — so the limit is the database's rather than a check somebody can route around. With the account implicit, this index is the enforcement and the materialization path is merely the common case: two concurrent first sign-ins must produce one account (C1, C2)
- [x] 1.4 Refuse a transfer of a `personal` account, and refuse a transfer that would give somebody a second personal account. The second is unreachable while the first holds, and belongs there anyway (C2)
- [x] 1.5 Refuse any write to `kind` after creation: a personal account never becomes a team account (C1)
- [x] 1.6 Regenerate `internal/store/schema.sql` via the golden test

## 2. Remove the internal edition — blocked by nothing, blocks everything

- [x] 2.1 Delete `store.ScopeForBootstrap` and every caller (C10)
- [x] 2.2 Remove automatic account selection at sign-in from `internal/oidcauth/flow.go`; a session begins with no account selected, which is now the normal first state rather than an unreachable one (P19)
- [x] 2.3 Remove the group-to-role mapping: `authz.Mapping`, the `ACME_ADMIN_GROUP` family of settings, and their startup validation
- [x] 2.4 Remove the derived-membership branch of P18, so membership is only ever rows we wrote
- [x] 2.5 Turn the four roles from a global vocabulary into the default role set seeded into each new **team** account — same names, same grants, now editable by the account's owner (C10)
- [x] 2.6 Retire Constraint 1 with a note recording that it was discharged rather than violated, and what it bought: the seeded roles reproducing the old matrix, and migration 000009
- [x] 2.7 Update `development/authentik/blueprints/acmeservice.yaml` — the four `acme-*` groups no longer grant anything. Decide deliberately whether the acceptance suite still needs four distinguishable users, and delete the groups if it does not
- [x] 2.8 Update `docs/` wherever a Keycloak-or-authentik group is described as the thing that grants a role

## 3. Membership, roles and the personal account

- [x] 3.1 Materialize a user's personal account on first sign-in, idempotently. There is NO create-personal-account route and no form — a user has one because they are a user (C1)
- [x] 3.1a Derive its name from the user; it is not slug-addressable and not renameable by the tenant, since there is nothing to disambiguate (C1)
- [x] 3.1b Creating a team account goes through checkout (group 5) and does not exist until the first credit lands
- [x] 3.2 Seed a new team account with the default roles and its owner as an unrestricted member
- [x] 3.3 A personal account has no roles, no memberships and no invitations — the API routes refuse rather than returning empty, so a client learns the difference from an account that merely has none yet (C9)
- [x] 3.4 KEEP the grant model live in personal accounts for API keys, including the subset rule. A key narrower than its holder is good practice in an account of one, and this is the task most likely to be deleted by accident (C9)
- [x] 3.5 Enforce a project quota of **five** on personal accounts. Team accounts have NO project quota — credit is their only limit, and capping projects on top of metering charges twice for the same growth (C13)
- [x] 3.5a The limit is CONFIGURATION, not a constant in the code. It is a pricing decision, pricing changes on a different clock from deployments, and the first time one customer needs six is the day a constant becomes a migration (C13)
- [x] 3.5b Refuse over-quota as a QUOTA, not as a permission: 409 and "there is no room, here is what to do", never 403 and "you may not". Collapsing the two makes one message that is wrong for both (C13, C11)
- [x] 3.6 Transfer of ownership for team accounts, audited, with the recipient's membership established in the same transaction

## 4. Funded, and what suspension means

- [x] 4.1 Implement `usable(account)` as one function: personal, or `credits > 0` and not staff-suspended (C3, C6)
- [x] 4.2 Gate account selection on it, with the owner exception — the owner of a suspended account may still select it, or nobody can pay for it (C4)
- [x] 4.3 Gate every account-scoped management route on it
- [x] 4.4 Gate the challenge API on it, immediately after alias resolution and before anything is published, answering **402 Payment Required** with a message naming the account and the reason. NOT 401: that sends an operator to look at an API key that is working perfectly (C7)
- [x] 4.5 Keep staff suspension a separate piece of state that paying cannot lift (C6)
- [x] 4.6 Test the boundary in both directions: an account at exactly 1 credit works, an account at exactly 0 does not, and the transition takes effect without a restart or a new session

## 5. Checkout, without a processor

- [x] 5.1 `POST /api/v1/accounts/{accountId}/checkout` accepting an amount from the fixed set 25 / 50 / 100 EUR. A caller-supplied amount is refused — with no processor in the way, a free-text amount is an invitation to credit a million (C8)
- [x] 5.2 Credit the balance in the same shape a real payment will: same row, same balance change, same audit entry (C8)
- [x] 5.3 Audit it as a credential-class action, attributed to the user who did it
- [x] 5.4 Creating a team account is a checkout that also creates the account, in one transaction — an account that exists but was never funded is a state with no purpose
- [x] 5.5 Do NOT mark the flow as fake in the interface. What makes it safe is that there is no processor behind it, not a banner that will be deleted later (C8)

## 6. The console

- [x] 6.1 The account switcher is a **searchable** select in the navbar, with an **Add account** action beside it (C12)
- [x] 6.1a With one account it is inert — present, showing that account, nothing to choose. Present rather than absent so the navbar does not change shape the moment a second account is bought (C12)
- [x] 6.1b A suspended team account is listed and shown as suspended rather than omitted: the owner has to reach it to pay for it (C4)
- [x] 6.1c **Add account** opens an explanation of what a team account is, with a **Buy now** that enters checkout. Creating and buying are the same act (C1, C12)

- [x] 6.2 Navigation for a **personal** account: `Projects` and `Upgrade`. Nothing else (C11)
- [x] 6.2a Navigation for a **team** account: `Projects`, `Audit`, `Settings` (C11)
- [x] 6.2b `Upgrade` and `Add account` reach the same place. Two entrances to one room — one found by someone looking at what this account cannot do, the other by someone looking at which account they are in (C12)
- [x] 6.2c Re-render the navigation on an account switch without a reload, since the switch changes what is in it
- [x] 6.2d `Live challenges` stays OUT of the first navigation. Deferred rather than dropped, and reconsidered when somebody has wanted it (C12)

- [x] 6.3 THE NAVIGATION IS THE ONE PLACE THINGS ARE LEFT OUT. Inside a screen, a control the caller lacks a grant for stays visible and disabled with a reason — that rule does not change, and this task exists because the two look contradictory and are not (C11)
- [x] 6.4 Keep "forbidden" and "does not exist" as different sentences with different remedies, wherever both can occur. A personal-account owner told they lack permission goes looking for whoever can grant it, and there is nobody (C11)

- [x] 6.5 **Settings**, in a team account only: members, role assignments, and credits. What administers the account, as opposed to the inventory it holds
- [x] 6.5a Move the existing `/roles` screen into Settings; it stops being a top-level entry
- [x] 6.6 The suspended-account screen for an owner: the balance, and checkout. Nothing else (C5)
- [x] 6.7 A member of a suspended account is told the account is suspended and that its owner can restore it — a different sentence from "you do not have permission"
- [x] 6.8 The checkout flow: enter, choose an amount, return to the account with the balance visible
- [x] 6.9 Keep the grant composer in the API key dialog everywhere, personal included (C9, 3.4)
- [x] 6.10 Warn before zero, not at it. The refusal is soft — renewals stop and the customer has weeks — so the warning is what actually protects them, and `metering-and-credits` says so itself (C4)

## 7. What the removal breaks, and proving it does not

- [x] 7.1 Rewrite the acceptance suite's sign-in fixtures: there is no shared account and no group-derived role, so a test user now creates or is invited into an account
- [x] 7.2 Replace `TestSeededUsersResolveToTheirRoles`, which asserts exactly the mechanism task 2.3 deletes
- [x] 7.3 Acceptance test the whole arc: sign in, land at the picker with the personal account already offered, select it, add a project, publish a challenge
- [x] 7.4 Acceptance test the team arc: checkout, account created and funded, invite a member, member selects it, balance to zero, member refused and challenge refused with 402, owner recharges, both work again
- [x] 7.5 Confirm P19's no-account-selected path is exercised by default now rather than by contrivance — it is the front door, and it was previously unreachable
- [x] 7.6 Assert the picker is never empty: a brand-new user with no team accounts still has somewhere to work, so "no account selected" means "you have not said which" and never "you have nothing"
