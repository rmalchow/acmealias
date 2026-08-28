## 1. Schema and the invitation entity

- [ ] 1.1 Add `invitations` to `internal/model` and a migration creating it: UUIDv7 identifier, account, role, normalized email, token hash, issuing user, and timestamps for issue, expiry, revocation, DECLINE and acceptance (design V1). Three nullable timestamps rather than a status column — each records when as well as which, and the audit trail wants both
- [ ] 1.2 Add the partial unique index on `(account_id, email)` where the invitation is neither accepted, revoked nor declined, so one-outstanding-per-address is structural rather than checked. `declined_at` belongs in that predicate: a decline is an answer, not a block, and the account may invite the same person again (design V12)
- [ ] 1.2a Add the index on `email` over outstanding invitations. It is what makes V11's second path a lookup rather than a scan across every account, and no other index answers "what is outstanding for this address"
- [ ] 1.3 Reference `roles` without `ON DELETE CASCADE`, and refuse deleting a role while an outstanding invitation names it (design V6)
- [ ] 1.4 Cascade invitations from `accounts`, so deleting an account ends its outstanding invitations
- [ ] 1.5 Regenerate `internal/store/schema.sql` via the golden test and confirm `model_drift_test` passes

## 2. Tokens

- [ ] 2.1 Generate the invitation token from a cryptographically secure source, storing only its hash, following `internal/apikey`'s reasoning that high entropy makes a memory-hard hash pointless
- [ ] 2.2 Resolve a token by hash lookup with a constant-time comparison, returning the same refusal for unknown, expired, revoked and already-accepted tokens
- [ ] 2.3 Evaluate expiry at resolution rather than by a sweep, so an invitation is dead when it is dead regardless of whether a job ran (design V8)
- [ ] 2.4 Set the expiry to FOURTEEN days, as a named constant with the reasoning in its comment. Seven was the first proposal and was rejected: an invitation arriving the day somebody goes away comes back to a dead link, and the person invited is usually the one with the least free attention (design V8)

## 3. Issuing, revoking and re-sending

- [ ] 3.1 Implement issuing in `internal/inventory`, requiring membership of the account's unrestricted role and no subset check on the role offered (design V5)
- [ ] 3.2 Normalize the address on write and on comparison — lower-cased, trimmed
- [ ] 3.3 Refuse issuing to an address whose user is already a member, at issue time rather than at acceptance (design V7)
- [ ] 3.4 Make the response identical whether or not the address already has a user, including timing
- [ ] 3.5 Implement revocation: mark revoked, stop the token resolving, drop out of the outstanding list, retain the row
- [ ] 3.6 Implement re-sending: issue a new token, invalidate the previous one in the same transaction, allow the role offered to change
- [ ] 3.7 Test that a re-sent invitation's earlier token no longer resolves

## 4. Rate limiting

- [ ] 4.1 Limit invitations issued per account per hour
- [ ] 4.2 Limit invitations to one address across all accounts per day — the limit that cannot be omitted, since per-account limits are defeated by creating accounts (design V9)
- [ ] 4.3 Limit re-sends of one invitation per hour
- [ ] 4.4 Refuse with a message stating the limit and when to retry, and record the refusal
- [ ] 4.5 Test that creating several accounts does not multiply the per-address allowance

## 5. Acceptance

- [ ] 5.1 Implement resolution: report the account name and the role name, and nothing else about the account; do not consume the token (design V4)
- [ ] 5.2 Implement acceptance: require an authenticated user whose verified address is the invited address; create the membership and consume the token in one transaction
- [ ] 5.3 Refuse acceptance by any other authenticated user, showing the invited address partially masked (design V2)
- [ ] 5.4 Assert by test that no operation exists adding an invited address to a signed-in user that does not hold it — the path that would turn bound into bearer
- [ ] 5.5 Refuse acceptance when the named role no longer exists, substituting nothing (design V6)
- [ ] 5.6 Refuse acceptance when the accepting user is already a member, leaving the existing membership unchanged
- [ ] 5.7 Make the joined account the session's selected account on acceptance, re-evaluating membership as any selection does (design V5)
- [ ] 5.8 Implement DECLINE: consume the invitation exactly as acceptance does, so a stray click on the link cannot undo it and land somebody in an account they refused (design V12)
- [ ] 5.9 Make a decline distinguishable from an expiry everywhere the account can see it. This is the whole point: fourteen days of silence tells an administrator neither whether the person never saw it nor whether they said no, and the two call for opposite responses
- [ ] 5.10 Do NOT let a decline block the address. "Not now" and "never" are different answers and we cannot tell which was meant

## 6. Routes

- [ ] 6.1 Add issue, revoke and re-send under `/api/v1/accounts/{accountId}/invitations`, requiring a selected account and the unrestricted role
- [ ] 6.2 Add resolve, accept and decline outside the account-scoped tree, addressed by token, reachable with no account selected
- [ ] 6.2a Add the route listing the outstanding invitations for the AUTHENTICATED person's address, across every account, needing no token and no selection (design V11). It grants nothing the token route does not — being signed in as the invited address is strictly stronger evidence than holding a link sent to it
- [ ] 6.3 Add both to the published OpenAPI document, since they are part of the public API
- [ ] 6.4 Test that the accept routes work for a session holding no membership at all

## 7. Identity

- [ ] 7.1 Add the registration entry point reached from an invitation, creating a user with the invited address already confirmed and sending no confirmation message
- [ ] 7.2 Assert that only the invited address is confirmed by this path
- [ ] 7.3 Add the return-to-invitation redirect on the sign-in path
- [ ] 7.4 Test that registration reached any other way still requires confirmation

## 8. Mail

- [ ] 8.1 Add the invitation template on the delivery mechanism `add-accounts-and-public-edition` provides, naming the account, the role offered and the inviter
- [ ] 8.2 Send on issue and on re-send, and not on revocation
- [ ] 8.3 Test that a send failure does not leave an invitation that was never delivered

## 9. Audit

- [ ] 9.1 Add `invitation` as an object type and the create, revoke, resend, accept, DECLINE and expire actions to `internal/authz/vocab.go`, in the credential class (design V10)
- [ ] 9.2 Record the account, invited address and role offered on every entry; record both the invited address and the acting user on acceptance AND on decline — the pair is what lets an administrator tell a refusal from a message that never arrived
- [ ] 9.3 Attribute expiry to the system actor, matching `apikey.expire`
- [ ] 9.4 Exclude `token_hash` from before/after state explicitly rather than relying on the scrubbing heuristic
- [ ] 9.5 Test that no entry describing an invitation contains the token in any form

## 10. Console

- [ ] 10.1 Add the invitation panel to the account's members view, reached through Settings → user management: invite by address and role, revoke, re-send, and the pending list
- [ ] 10.2 Show outstanding invitations to every member alongside members, marking one whose role no longer exists as invalid
- [ ] 10.3 Offer the actions only to members of the unrestricted role
- [ ] 10.4 Build the accept screen inside the no-selection shell: name the account and role, require an explicit action, branch to registration or sign-in
- [ ] 10.4a Offer ACCEPT and DECLINE, not accept and a way out of the page (design V12)
- [ ] 10.4b Put the person's outstanding invitations on their dashboard, so somebody whose mail was eaten by a spam filter or a URL-rewriting gateway still finds the thing they came for. Without it, V5's arrival is an empty product and an instruction to go find an email (design V11)
- [ ] 10.5 Render the wrong-user, expired and revoked states, with the address partially masked in the wrong-user case
- [ ] 10.6 Continue into the joined account after acceptance rather than returning to the empty state

## 11. Documentation

- [ ] 11.1 Document the invitation flow in `docs/`, including why a forwarded link confers nothing
- [ ] 11.2 Document the mismatched-identity case and its resolution, since it will generate support questions

## 12. Blocklist interaction

- [ ] 12.1 Check the invited address against the identity blocklist at acceptance, not at issue (`staff-plane` design S12)
- [ ] 12.2 Refuse acceptance without consuming the token, and report the state in the account's outstanding list
- [ ] 12.3 Test that a blocked address can be invited, cannot accept, and that the invitation remains revocable
