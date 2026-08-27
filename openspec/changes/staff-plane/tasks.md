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

## Search and the account view (design S13, S14, S15)

- [ ] Implement cross-account search: projects by project name and by domain name, accounts by account name and by owner. It requires NO access grant — a grant names one account and therefore presupposes the answer to the question being asked (S13)
- [ ] Return identifiers and status ONLY. Never a load balancer's nodes, a credential, an audit entry or a challenge value. The moment search returns contents it is S3's access grant without the reason or the expiry, and a support engineer who can read everything by searching has no incentive to open one (S13)
- [ ] A domain is reachable only by SUPPLYING one. Offer no listing of all domains: a list of our accounts is a fact about our business, a list of our customers' domains is a map of theirs (S13)
- [ ] These are cross-account reads over policied tables, so they go through `store.CrossAccount` and are written to be obvious. RLS empties rather than fails, and a scopeless read here would return nothing and look like "no results" (tenancy-and-permissions 2.12)
- [ ] The account view shows owner, projects and credit status together (S15)
- [ ] Credit status distinguishes an exhausted balance from a staff suspension. Those are identical from inside the customer's console, and only staff can tell the customer which one it is (S15, personal-and-team-accounts C6)
- [ ] A search writes ONE staff-plane read record, never one per matched account. Forty tenants must not each find an entry about a question that was not about them, and the tenant who was the answer must not find theirs indistinguishable from thirty-nine coincidences (S14)
- [ ] Build these in the staff console at `frontend/projects/admin`, which exists with a single overview screen
- [ ] Put every component this change adds in `frontend/projects/shared` unless there is a stated reason it belongs to one surface. The staff account view and the customer's own views should be the SAME table — two implementations drift, and the drift is found during an incident when the two people on the call are describing different screens (S16)
- [ ] Do NOT share the provider lists. `app.config.ts` stays a separate copy per application: a shared one applies to both whether they want it or not and re-couples the dependency graphs the two-application split exists to separate (S16)
- [ ] The account list carries ONE filter box. Split its contents on whitespace; every part must be a SUBSTRING of at least one of the account's owner email, slug, NAME or id, and the parts are ANDed. Which field each part matched need not be the same one (S17)
- [ ] AND, not OR: more typing must mean fewer results, or the filter punishes somebody for knowing more — and a second word making the list longer is what trains people to type one word and scroll (S17)
- [ ] Case-insensitive. Nobody retypes a filter in another case on purpose (S17)
- [ ] Include the account NAME. It is what a customer says on the phone — "we're Acme GmbH", never their slug — and asking somebody for a slug is asking them to know how we store their account. It is the weakest of the four (free text, not unique, two customers may share one) and that is acceptable: the filter promises a shorter list, not a single result (S17)
- [ ] Personal accounts have no slug (C1), so they are reachable by owner email or id — make sure the filter finds them (S17, S18)
- [ ] Accept the scan. `ILIKE '%…%'` is served by no ordinary index, which is correct at a business's worth of accounts and stops being correct at some size — the answer then is a trigram index, not a different interface. Record it so nobody later concludes the design was wrong (S17)
- [ ] Do NOT invent an account number. Support has the address the person is writing from, team accounts have slugs, and what forces a reference is invoicing, which arrives with payment. When one is added it MUST NOT become an authentication factor: knowing it must grant nothing, and that failure arrives by drift rather than by decision (S18)
