> **This change was revised after it was written.** An invitation used to name a
> ROLE; it no longer does. Everything that followed from that column — a foreign
> key to `roles`, a decision about deleted roles, a role picker on the invite
> form, a role name on the accept screen — is gone, and one thing arrives in its
> place: a member may hold NO role, which is a state the model did not have. The
> other revision is that an invited person is no longer exempt from the approval
> queue. Design V6 and V13 carry both arguments.

## 1. Schema and the invitation entity

- [x] 1.1 Add `invitations` to `internal/model` and a migration creating it: UUIDv7 identifier, account, normalized email, token hash, issuing user, and timestamps for issue, expiry, revocation, DECLINE and acceptance (design V1). Three nullable timestamps rather than a status column — each records when as well as which, and the audit trail wants both
- [x] 1.2 Add the partial unique index on `(account_id, email)` where the invitation is neither accepted, revoked nor declined, so one-outstanding-per-address is structural rather than checked. `declined_at` belongs in that predicate: a decline is an answer, not a block, and the account may invite the same person again (design V12)
- [x] 1.2a Add the index on `email` over outstanding invitations. It is what makes V11's second path a lookup rather than a scan across every account, and no other index answers "what is outstanding for this address"
- [x] 1.3 Do NOT reference `roles` at all. An invitation names no role, so deleting a role has no interaction with invitations — the whole of the old V6 dissolves (design V6)
- [x] 1.4 Cascade invitations from `accounts`, so deleting an account ends its outstanding invitations
- [x] 1.5 Regenerate `internal/store/schema.sql` via the golden test and confirm `model_drift_test` passes

## 1a. A member with no role

> The cost of taking the role off the invitation, and the part of this change most
> likely to be got wrong quietly. RLS EMPTIES RATHER THAN FAILS: a roleless
> member's read returns zero rows and no error, which looks like an empty account
> rather than one they cannot see into.

- [x] 1a.1 Make `account_members.role_id` nullable, in `internal/model` and in a migration
- [x] 1a.2 Add a partial unique index over `(account_id, user_id) where role_id is null`. The existing `unique (account_id, user_id, role_id)` stops constraining anything once nulls are allowed, because nulls are distinct — the same person could be inserted as a roleless member any number of times
- [x] 1a.3 Confirm the composite foreign key `(role_id, account_id) references roles (id, account_id)` still holds with a null role. PostgreSQL's default MATCH SIMPLE skips the check when any referencing column is null, so nothing needs relaxing — verify rather than assume, because the alternative is silently losing the constraint that stops a membership naming another account's role
- [x] 1a.4 Re-assert `account_members`' row-level security policy after the migration. A migration that touches a policied table and does not assert the policy afterwards is how a scopeless read starts returning nothing instead of failing
- [x] 1a.5 Make `authz.Subject` treat a null role as NO grants and NOT unrestricted — an empty grant set, so the ACL predicate is false for every row. Assert it, because "skip the membership" and "the membership grants nothing" produce the same result in every test that only counts rows
- [x] 1a.6 Keep such a member a MEMBER: `IsMember`, `AccountsForUser` and account selection must all still include them (design V6)
- [x] 1a.7 Make assigning a role UPDATE the roleless row rather than inserting beside it. Memberships are per-role — `unique (account_id, user_id, role_id)` permits several — so an insert would leave the person listed twice, once as a member and once as somebody waiting
- [x] 1a.8 Write the down migration honestly: narrowing `role_id` back to `not null` must REFUSE while any roleless membership exists rather than failing halfway or inventing a role for them

## 2. Tokens

- [x] 2.1 Generate the invitation token from a cryptographically secure source, storing only its hash, following `internal/apikey`'s reasoning that high entropy makes a memory-hard hash pointless
- [x] 2.2 Resolve a token by hash lookup with a constant-time comparison, returning the same refusal for unknown, expired, revoked and already-accepted tokens
- [x] 2.3 Evaluate expiry at resolution rather than by a sweep, so an invitation is dead when it is dead regardless of whether a job ran (design V8)
- [x] 2.4 Set the expiry to FOURTEEN days, as a named constant with the reasoning in its comment. Seven was the first proposal and was rejected: an invitation arriving the day somebody goes away comes back to a dead link, and the person invited is usually the one with the least free attention (design V8)

## 3. Issuing, revoking and re-sending

- [x] 3.1 Implement issuing in `internal/inventory`, requiring membership of the account's unrestricted role and no subset check on the role offered (design V5)
- [x] 3.2 Normalize the address on write and on comparison — lower-cased, trimmed
- [x] 3.3 Refuse issuing to an address whose user is already a member, at issue time rather than at acceptance (design V7)
- [x] 3.4 Make the response identical whether or not the address already has a user, including timing
- [x] 3.5 Implement revocation: mark revoked, stop the token resolving, drop out of the outstanding list, retain the row
- [x] 3.6 Implement re-sending: issue a new token and invalidate the previous one in the same transaction
- [x] 3.7 Test that a re-sent invitation's earlier token no longer resolves

## 4. Rate limiting

> There is NO rate-limiting mechanism in the backend today — no limiter, no
> middleware, nothing to hang these off. This group builds one as well as using
> it, which is why it is larger than it reads.

- [x] 4.1 Limit invitations issued per account per hour
- [x] 4.2 Limit invitations to one address across all accounts per day — the limit that cannot be omitted, since per-account limits are defeated by creating accounts (design V9)
- [x] 4.3 Limit re-sends of one invitation per hour
- [x] 4.4 Refuse with a message stating the limit and when to retry, and record the refusal
- [x] 4.5 Test that creating several accounts does not multiply the per-address allowance

## 5. Acceptance

- [x] 5.1 Implement resolution: report the account name and nothing else about the account; do not consume the token (design V4)
- [x] 5.2 Implement acceptance: require an authenticated user whose verified address is the invited address; create the membership WITH NO ROLE and consume the token in one transaction
- [x] 5.3 Refuse acceptance by any other authenticated user, showing the invited address partially masked (design V2)
- [x] 5.4 Assert by test that no operation exists adding an invited address to a signed-in user that does not hold it — the path that would turn bound into bearer
- [x] 5.6 Refuse acceptance when the accepting user is already a member, leaving the existing membership unchanged
- [x] 5.7 Make the joined account the session's selected account on acceptance, re-evaluating membership as any selection does (design V5)
- [x] 5.8 Implement DECLINE: consume the invitation exactly as acceptance does, so a stray click on the link cannot undo it and land somebody in an account they refused (design V12)
- [x] 5.9 Make a decline distinguishable from an expiry everywhere the account can see it. This is the whole point: fourteen days of silence tells an administrator neither whether the person never saw it nor whether they said no, and the two call for opposite responses
- [x] 5.10 Do NOT let a decline block the address. "Not now" and "never" are different answers and we cannot tell which was meant

## 6. Routes

- [x] 6.1 Add issue, revoke and re-send under `/api/v1/accounts/{accountId}/invitations`, requiring a selected account and the unrestricted role
- [x] 6.2 Add resolve, accept and decline outside the account-scoped tree, addressed by token, reachable with no account selected
- [x] 6.2a Add the route listing the outstanding invitations for the AUTHENTICATED person's address, across every account, needing no token and no selection (design V11). It grants nothing the token route does not — being signed in as the invited address is strictly stronger evidence than holding a link sent to it
- [x] 6.3 Add both to the published OpenAPI document, since they are part of the public API
- [x] 6.4 Test that the accept routes work for a session holding no membership at all

## 7. Identity

> **Group 7 is BLOCKED on `self-service-signup`, and blocked in a way that is
> worth stating precisely, because half of it turns out to be already done and
> the other half cannot be started.**
>
> ALREADY DONE, by this change rather than by that one: the service now reads
> the identity provider's `email` claim at sign-in and records it on the user —
> and ONLY when `email_verified` is true. That is what an invitation binds to,
> and until this change every caller of `store.FindOrCreateUser` passed an empty
> string, so the binding had nothing to bind to. A partial unique index on
> `lower(email)` makes one address identify at most one person, which the
> binding silently assumed and nothing enforced.
>
> CANNOT BE STARTED: there is no registration flow to add an entry point TO.
> authentik ships no enrolment flow and this deployment configures none, which is
> `self-service-signup`'s whole subject. 7.1 and 7.4 are edits to a blueprint
> that has nothing to edit yet.

- [ ] 7.1 Add the registration entry point reached from an invitation, creating a user with the invited address already confirmed and sending no confirmation message
- [ ] 7.2 Assert that only the invited address is confirmed by this path
- [ ] 7.3 Add the return-to-invitation redirect on the sign-in path
- [ ] 7.4 Test that registration reached any other way still requires confirmation

## 8. Mail

> `internal/mail` exists and has no callers. `AdminOptions.Mail` is populated in
> `main.go` and read by no handler, so this change is the first thing that ever
> sends. Subjects follow the grammar in `mail.Message`'s comment — the noun
> first, no constant prefix — which closes `transactional-email` task 4.1.

- [x] 8.1 Add the invitation template on the delivery mechanism `add-accounts-and-public-edition` provides, naming the account, the role offered and the inviter
- [x] 8.2 Send on issue and on re-send, and not on revocation
- [x] 8.3 Test that a send failure leaves the invitation STANDING, visible in the pending list, and re-sendable — and that the failure is reported to whoever clicked

  > **This task said the opposite and was wrong.** It asked for the invitation to
  > be rolled back when the message could not be sent. Rewritten rather than left
  > as a standing disagreement, because a task list carrying an argument forever is
  > one nobody trusts. The reasoning is now design V18.

## 9. Audit

- [x] 9.1 Add `invitation` as an object type and the create, revoke, resend, accept, DECLINE and expire actions to `internal/authz/vocab.go`, in the credential class (design V10)
- [x] 9.2 Record the account and the invited address on every entry — and no role, because there is none on the invitation; record both the invited address and the acting user on acceptance AND on decline, since the pair is what lets an administrator tell a refusal from a message that never arrived
- [ ] 9.3 Attribute expiry to the system actor, matching `apikey.expire`

  > Nothing writes `invitation.expire` yet, because nothing OBSERVES an expiry.
  > Expiry is evaluated at read time (V8) — an invitation is dead when
  > `expires_at` has passed, whether or not any job has run — so there is no
  > moment at which the service notices one expiring and could record it. The
  > action constant exists; the writer belongs with a retention sweep, and this
  > change deliberately built no sweep because nothing depends on one having run.
- [x] 9.4 Exclude `token_hash` from before/after state explicitly rather than relying on the scrubbing heuristic
- [x] 9.5 Test that no entry describing an invitation contains the token in any form

## 10. Console

- [x] 10.1 Add the invitation panel to the account's members view, reached through Settings → user management: invite by ADDRESS ALONE — no role picker — plus revoke, re-send, and the pending list
- [x] 10.2 Show outstanding invitations to every member alongside members
- [x] 10.2a Show a member who holds no role as a member, marked as holding none, with a way for an administrator to assign one. This is where the new member lands after accepting, and it is the only screen that says the assignment still has to happen
- [x] 10.3 Offer the actions only to members of the unrestricted role — the account's owner and its administrators, which is the same set (design V5)
- [x] 10.4 Build the accept screen inside the no-selection shell: name the account, require an explicit action, branch to registration or sign-in. It does NOT name a role, because the invitation offers none
- [x] 10.4a Offer ACCEPT and DECLINE, not accept and a way out of the page (design V12)
- [x] 10.4b Put the person's outstanding invitations on their dashboard, so somebody whose mail was eaten by a spam filter or a URL-rewriting gateway still finds the thing they came for. Without it, V5's arrival is an empty product and an instruction to go find an email (design V11)
- [x] 10.5 Render the wrong-user, expired and revoked states, with the address partially masked in the wrong-user case
- [x] 10.6 Continue into the joined account after acceptance rather than returning to the empty state
- [x] 10.7 Build the roleless empty state, and make it reachable: a member with no role selects the account and every account-scoped screen must say they hold no role and an administrator must assign one. Without this the first thing a new colleague sees is an account that appears to be empty, which is what RLS returns and is not what is true (design V6)
- [x] 10.8 Check the roleless member does not meet a wall of refusals instead: selection succeeds, the shell renders, the navigation is present. It is one explanatory screen, not a 403 on every route

## 11. Documentation

- [x] 11.1 Document the invitation flow in `docs/`, including why a forwarded link confers nothing
- [x] 11.2 Document the mismatched-identity case and its resolution, since it will generate support questions
- [x] 11.3 Document that joining and being given a role are two steps, and that a new member can do nothing until the second happens. Support will otherwise field "I accepted and the product is empty" as a bug (design V6)
- [x] 11.4 Document that the fourteen-day clock runs while an invitee waits for approval, and that an approval slower than that expires an invitation belonging to somebody who did everything right (design V13)

## 11a. The approval queue

> `self-service-signup` S7 used to exempt an invited person from approval and no
> longer does (design V13). These tasks are this change's half of that reversal;
> S7's own tasks are that change's half.

- [x] 11a.1 Do not build an exemption. An invited person is created inactive and waits, exactly like a stranger; there is one enrolment flow
- [x] 11a.2 Keep the address confirmation exemption, which is a different thing: the invitation link proved control of the address, so no confirmation mail is sent (design V3)
- [x] 11a.3 Do NOT pause or extend the fourteen days while the invitee waits. Rejected in V13: the service cannot observe when somebody was approved, and lengthening V8's expiry to hide an approval latency problem trades a security parameter for an operational one
- [ ] 11a.4 (blocked with group 7: there is no approval gate to make somebody wait at) Test the unhappy path end to end — invited, registers, approved after the invitation expired — and assert what the invitee sees is an expired invitation naming the account to ask, not an error

## 12. Blocklist interaction

> **Blocked on `staff-plane`, which is not in the numbered sequence and has not
> been implemented.** There is no identity blocklist to check an address against:
> `staff-plane` S12 specifies one and no table, route or list exists. Building a
> blocklist here to satisfy a check would be implementing another change's
> central object as a side effect of this one.
>
> The shape is ready for it. Acceptance already resolves the invitation, then
> checks the caller, then writes — a blocklist check goes between the second and
> the third, refusing WITHOUT consuming the token, which is what 12.2 asks for and
> what the current code would do for free since consumption happens last.

- [ ] 12.1 Check the invited address against the identity blocklist at acceptance, not at issue (`staff-plane` design S12)
- [ ] 12.2 Refuse acceptance without consuming the token, and report the state in the account's outstanding list
- [ ] 12.3 Test that a blocked address can be invited, cannot accept, and that the invitation remains revocable
