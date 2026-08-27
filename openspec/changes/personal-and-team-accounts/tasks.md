# Tasks

> **Order matters here more than usual.** Group 2 removes the edition the service
> currently runs as, and nothing works between the first task of group 2 and the
> last task of group 3. Do not stop in the middle.

## 1. The two kinds of account

- [ ] 1.1 Add `accounts.kind` (`personal` | `team`), `accounts.owner_user_id` and `accounts.credits` to the model and a migration (design C1)
- [ ] 1.2 Make `owner_user_id` `NOT NULL` and backfill it; every account has exactly one owner and always did in practice
- [ ] 1.3 Add the partial unique index that enforces one personal account per user — `UNIQUE (owner_user_id) WHERE kind = 'personal'` — so the limit is the database's rather than a check somebody can route around. With the account implicit, this index is the enforcement and the materialization path is merely the common case: two concurrent first sign-ins must produce one account (C1, C2)
- [ ] 1.4 Refuse a transfer of a `personal` account, and refuse a transfer that would give somebody a second personal account. The second is unreachable while the first holds, and belongs there anyway (C2)
- [ ] 1.5 Refuse any write to `kind` after creation: a personal account never becomes a team account (C1)
- [ ] 1.6 Regenerate `internal/store/schema.sql` via the golden test

## 2. Remove the internal edition — blocked by nothing, blocks everything

- [ ] 2.1 Delete `store.ScopeForBootstrap` and every caller (C10)
- [ ] 2.2 Remove automatic account selection at sign-in from `internal/oidcauth/flow.go`; a session begins with no account selected, which is now the normal first state rather than an unreachable one (P19)
- [ ] 2.3 Remove the group-to-role mapping: `authz.Mapping`, the `ACME_ADMIN_GROUP` family of settings, and their startup validation
- [ ] 2.4 Remove the derived-membership branch of P18, so membership is only ever rows we wrote
- [ ] 2.5 Turn the four roles from a global vocabulary into the default role set seeded into each new **team** account — same names, same grants, now editable by the account's owner (C10)
- [ ] 2.6 Retire Constraint 1 with a note recording that it was discharged rather than violated, and what it bought: the seeded roles reproducing the old matrix, and migration 000009
- [ ] 2.7 Update `development/authentik/blueprints/acmeservice.yaml` — the four `acme-*` groups no longer grant anything. Decide deliberately whether the acceptance suite still needs four distinguishable users, and delete the groups if it does not
- [ ] 2.8 Update `docs/` wherever a Keycloak-or-authentik group is described as the thing that grants a role

## 3. Membership, roles and the personal account

- [ ] 3.1 Materialize a user's personal account on first sign-in, idempotently. There is NO create-personal-account route and no form — a user has one because they are a user (C1)
- [ ] 3.1a Derive its name from the user; it is not slug-addressable and not renameable by the tenant, since there is nothing to disambiguate (C1)
- [ ] 3.1b Creating a team account goes through checkout (group 5) and does not exist until the first credit lands
- [ ] 3.2 Seed a new team account with the default roles and its owner as an unrestricted member
- [ ] 3.3 A personal account has no roles, no memberships and no invitations — the API routes refuse rather than returning empty, so a client learns the difference. The CONSOLE still shows them, disabled: refusing at the API and hiding in the interface are different questions (C9, C11)
- [ ] 3.4 KEEP the grant model live in personal accounts for API keys, including the subset rule. A key narrower than its holder is good practice in an account of one, and this is the task most likely to be deleted by accident (C9)
- [ ] 3.5 Enforce the project quota per account, as a quota and not as a permission: a refusal says there is no room and what to do about it, not that the caller may not
- [ ] 3.6 Transfer of ownership for team accounts, audited, with the recipient's membership established in the same transaction

## 4. Funded, and what suspension means

- [ ] 4.1 Implement `usable(account)` as one function: personal, or `credits > 0` and not staff-suspended (C3, C6)
- [ ] 4.2 Gate account selection on it, with the owner exception — the owner of a suspended account may still select it, or nobody can pay for it (C4)
- [ ] 4.3 Gate every account-scoped management route on it
- [ ] 4.4 Gate the challenge API on it, immediately after alias resolution and before anything is published, answering **402 Payment Required** with a message naming the account and the reason. NOT 401: that sends an operator to look at an API key that is working perfectly (C7)
- [ ] 4.5 Keep staff suspension a separate piece of state that paying cannot lift (C6)
- [ ] 4.6 Test the boundary in both directions: an account at exactly 1 credit works, an account at exactly 0 does not, and the transition takes effect without a restart or a new session

## 5. Checkout, without a processor

- [ ] 5.1 `POST /api/v1/accounts/{accountId}/checkout` accepting an amount from the fixed set 25 / 50 / 100 EUR. A caller-supplied amount is refused — with no processor in the way, a free-text amount is an invitation to credit a million (C8)
- [ ] 5.2 Credit the balance in the same shape a real payment will: same row, same balance change, same audit entry (C8)
- [ ] 5.3 Audit it as a credential-class action, attributed to the user who did it
- [ ] 5.4 Creating a team account is a checkout that also creates the account, in one transaction — an account that exists but was never funded is a state with no purpose
- [ ] 5.5 Do NOT mark the flow as fake in the interface. What makes it safe is that there is no processor behind it, not a banner that will be deleted later (C8)

## 6. The console

- [ ] 6.1 The account picker shows the personal account as a distinct entry — "Personal", not a named row among the team ones — and it is always present (C1)
- [ ] 6.1a The picker shows a suspended team account as suspended rather than omitting it: the owner has to be able to reach it to pay for it (C4)
- [ ] 6.2 The suspended-account screen for an owner: the balance, and checkout. Nothing else (C5)
- [ ] 6.3 A member of a suspended account is told the account is suspended and that its owner can restore it, which is a different sentence from "you do not have permission"
- [ ] 6.4 The checkout flow: enter, choose an amount, return to the account with the balance visible
- [ ] 6.5 Add a **Settings** route holding the three things that administer an account rather than its inventory: members, role assignments, and credits. One route with a consistent shape in both kinds of account (C11)
- [ ] 6.5a Move the existing `/roles` screen into Settings; it stops being a top-level entry
- [ ] 6.5b NOTHING IS HIDDEN. In a personal account, Settings still shows members, roles and credits — present and disabled, each saying it belongs to a team account. A route that appears and disappears as the user switches is the same mistake as an absent button (C11)
- [ ] 6.5c Two reasons, two sentences, and they must not be interchanged: "you are not permitted to do this" is fixed by somebody granting it; "this does not exist in a personal account" is fixed by a different kind of account. Telling a personal-account owner they lack permission sends them looking for whoever can grant it, and there is nobody (C11)
- [ ] 6.5e Add the second reason string alongside NOT_PERMITTED rather than reusing it, so the two cannot drift into one message that is wrong for both
- [ ] 6.5d Re-render the navigation on an account switch without a reload, since the switch changes what is in it
- [ ] 6.6 Keep the grant composer in the API key dialog everywhere, personal included (C9, 3.4)
- [ ] 6.7 Warn before zero, not at it. The refusal is soft — renewals stop and the customer has weeks — so the warning is the thing that actually protects them, and `metering-and-credits` says so itself (C4)

## 7. What the removal breaks, and proving it does not

- [ ] 7.1 Rewrite the acceptance suite's sign-in fixtures: there is no shared account and no group-derived role, so a test user now creates or is invited into an account
- [ ] 7.2 Replace `TestSeededUsersResolveToTheirRoles`, which asserts exactly the mechanism task 2.3 deletes
- [ ] 7.3 Acceptance test the whole arc: sign in, land at the picker with the personal account already offered, select it, add a project, publish a challenge
- [ ] 7.4 Acceptance test the team arc: checkout, account created and funded, invite a member, member selects it, balance to zero, member refused and challenge refused with 402, owner recharges, both work again
- [ ] 7.5 Confirm P19's no-account-selected path is exercised by default now rather than by contrivance — it is the front door, and it was previously unreachable
- [ ] 7.6 Assert the picker is never empty: a brand-new user with no team accounts still has somewhere to work, so "no account selected" means "you have not said which" and never "you have nothing"
