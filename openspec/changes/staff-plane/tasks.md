## 1. The staff plane

- [ ] 1.1 Replace the `Action` constants and the `permissions` matrix in `internal/authz` with the staff vocabulary (`support.access.open`, `account.suspend`, `account.credit.grant`, `account.ownership.reassign`, `account.lifecycle`, `platform.admin`, `staff.audit.read`)
- [ ] 1.2 Repoint the `ACME_*_GROUP` settings at staff permissions; keep the no-permission refusal reporting the observed groups
- [ ] 1.3 Add a test asserting no staff permission takes an object, identifier or pattern (design S2)
- [ ] 1.4 Add a test asserting the staff guard evaluates no tenant grant, and the tenant guard evaluates no staff permission

## 2. Access grants

- [ ] 2.1 Add the `staff_access` model and migration with a non-empty `reason` and a mandatory `expires_at`
- [ ] 2.2 Implement opening, closing and listing access grants behind `support.access.open`
- [ ] 2.3 Implement `store.ScopeForStaffAccess(a *StaffAccess) (Scope, error)`, erroring on a closed, expired or absent grant
- [ ] 2.4 Add the staff request middleware that resolves an access grant per request and never caches it on the session
- [ ] 2.5 Test that expiry and closure take effect on the next request within an open session
- [ ] 2.6 Test that an account identifier supplied in a request confers no access without a grant

## 3. Staff reach into tenant data

- [ ] 3.1 Wire staff read routes to the existing `internal/store` queries through the staff `Scope`, adding no parallel query path
- [ ] 3.2 Test that a staff read returns exactly what a member read of the same account returns
- [ ] 3.3 Test that a staff request under a grant for one account is refused for objects of another

## 4. Audit

- [ ] 4.1 Add `presented_actor`, `presented_reason` and `tenant_visible` to `audit_entries`; backfill existing rows as visible with no presented identity
- [ ] 4.2 Record both identities on every staff and command-line change
- [ ] 4.3 Filter the tenant audit view to `tenant_visible` and select only the presented identity
- [ ] 4.4 Test that no account-facing read, filter or export can disclose an individual staff subject
- [ ] 4.5 Mark `SourceCLI` entries touching tenant inventory as tenant-visible (design S8)

## 5. Staff activity recording

- [ ] 5.1 Add the staff activity store, separate from `audit_entries`, recorded per access grant (design S6)
- [ ] 5.2 Record what was reached under each grant, with a count and a summary
- [ ] 5.3 Expose the fact of an access to the account it was opened against, with reason and window
- [ ] 5.4 Give the store its own retention, independent of the audit trail's

## 6. Staff operations

- [ ] 6.1 Add staff routes on the management listener behind the staff guard, distinguished by path prefix
- [ ] 6.2 Implement account suspension and restoration
- [ ] 6.3 Implement account lifecycle: create, erase, export
- [ ] 6.4 Implement ownership reassignment, bypassing the subset rule and the last-member guard, recorded and tenant-visible (design S7)
- [ ] 6.5 Test that ownership reassignment repairs an account whose unrestricted role has no reachable member

## 7. Break-glass

- [ ] 7.1 Narrow the documented purpose of `internal/adminctl` to bootstrap and emergency in `docs/`
- [ ] 7.2 State the residual risk — possession of the connection string is unlimited tenant access — in the operator documentation
- [ ] 7.3 Confirm the subcommands still operate with the server process stopped

## 8. Documentation

- [ ] 8.1 Document the two planes, what each authorizes, and why staff never call the tenant evaluator
- [ ] 8.2 Document the access grant lifecycle and what an account sees
- [ ] 8.3 Document the staff permission vocabulary and its group settings

## 6. Suspension and blocklists

- [ ] 6.1 Add the `platform.suspend` staff permission and the suspend/restore routes, taking an account and a reason, requiring no access grant (design S10)
- [ ] 6.2 Store suspension as account state carrying reason and acting staff identity; distribute it as the single event `satellite-sync` Y10 specifies
- [ ] 6.3 Block identities whose only membership is the suspended account; refuse new sessions and invalidate existing ones
- [ ] 6.4 Add the account's zone names to the zone blocklist on suspension
- [ ] 6.5 Report collateral before applying: other accounts of affected identities, flagging any where one is the last owner (design S11)
- [ ] 6.6 Create the identity blocklist (canonicalized address entries and suffix-matched domain entries) and the zone blocklist (suffix-matched, may be a top-level domain), both plain text with reason, timestamp and `applies_to` (design S12)
- [ ] 6.7 Check identity entries at registration, sign-in and invitation acceptance; refuse acceptance with a message naming the address
- [ ] 6.8 Make entry addition non-retroactive: flag matching existing accounts and zones for review, with enforcement as a separate action
- [ ] 6.9 Restrict blocklist reads to the staff plane and give entries a stated retention
- [ ] 6.10 Tests: suspend/restore round-trips a customer-disabled zone as disabled; a suspension opens no access grant; a suffix entry catches a subdomain; adding an entry serves an existing matching zone unchanged
- [ ] 6.11 Document the abuse response commitment of twelve hours alongside the operation; it is published on the public site by `public-edition` tasks 7.6 and U11
