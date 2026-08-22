> Groups are ordered so that a group's dependencies are earlier in the list. Each heading carries
> a suggested complexity and the directories it writes to; groups whose `touches` sets are
> disjoint can run in parallel. Every task names the files it produces.
>
> **Groups 1–3 are the ones with a deadline.** They change no observable behaviour and are worth
> doing whatever happens commercially, because their cost grows with every row and every query
> written after them. Groups 4 onward should wait for a decision that the public edition is
> actually being built — see design.md, Migration Plan.

## 1. The account entity and the single internal account <!-- complexity: sonnet | touches: backend/internal/store, backend/internal/model -->

- [ ] 1.1 `backend/internal/store/migrations/000003_accounts.up.sql` / `.down.sql`: `accounts` (id, slug unique, name, created_at, suspended_at nullable) and `account_members` (id, account_id, subject, roles text[], created_at, unique on (account_id, subject)); add `projects.account_id` as nullable, insert one account with slug `internal`, backfill every project to it, then set the column `NOT NULL` with a foreign key that refuses deletion of an account still owning projects
- [ ] 1.2 `backend/internal/model/model.go`: `Account` and `AccountMember`; `Project` gains `AccountID`
- [ ] 1.3 `backend/internal/store/account.go`: get by slug, get by id, list memberships for a subject, upsert a membership
- [ ] 1.4 `backend/internal/store/migrate_test.go`: extend the existing round-trip test so the backfill is covered — a database migrated from 000001 with projects in it ends with every project owned by the one account

## 2. Scoping that cannot be forgotten <!-- complexity: opus | touches: backend/internal/store -->

- [ ] 2.1 `backend/internal/store/scope.go`: `Scope` with an unexported account id, `Projects(db)` and `LoadBalancers(db)` query builders (joining `projects` for everything under a load balancer), and constructors that exist only for an authenticated membership and for the internal edition's single account — no constructor from a bare integer
- [ ] 2.2 Thread `Scope` through every inventory read and write in `backend/internal/store/{project,loadbalancer,domain,alias,apikey,interaction}.go` as a required parameter, so the compiler enumerates the call sites rather than a reviewer
- [ ] 2.3 `backend/internal/store/scope_test.go`: two accounts with identical inventory — list, get-by-id, update and delete each see and touch only their own; a get-by-id across accounts is indistinguishable from a missing row
- [ ] 2.4 `backend/internal/httpapi/admin_test.go`: the same isolation asserted through the management API, as two authenticated callers rather than at the store layer

## 3. Authorization takes a scope <!-- complexity: opus | touches: backend/internal/authz, backend/internal/httpapi, backend/internal/oidcauth -->

- [ ] 3.1 `backend/internal/authz`: `Allowed(roles, scope)`; roles resolved from the caller's membership in the account being acted on; role names and the permission matrix unchanged
- [ ] 3.2 `backend/internal/oidcauth/session.go`: the session carries memberships rather than a bare role set; in the internal edition the group-to-role mapping grants those roles on the single account
- [ ] 3.3 `backend/internal/httpapi/admin.go`: each management route resolves the account of the object it addresses and builds the `Scope` from the caller's membership in it; a caller with no membership in that account is refused as though the object did not exist
- [ ] 3.4 `backend/internal/httpapi/admin_test.go`: the existing role matrix re-run unchanged for the internal edition — the proof that step 3 is invisible to operators

## 4. Edition selection <!-- complexity: sonnet | touches: backend/internal/config, backend/cmd/acmeservice -->

- [ ] 4.1 `backend/internal/config/config.go`: `ACME_EDITION` (`internal` | `public`), defaulting to `internal`; an unrecognised value fails startup naming the setting and the accepted values
- [ ] 4.2 `backend/cmd/acmeservice/main.go`: the composition root wires authentication, entitlements and billing per edition; the edition appears in the startup log line and in the readiness payload
- [ ] 4.3 `backend/internal/config/config_test.go`: default, valid values, and the refusal

## 5. The identity seam and local accounts <!-- complexity: opus | touches: backend/internal/localauth, backend/internal/oidcauth -->

- [ ] 5.1 Extract the authentication interface: what both implementations produce is a session with subject, display name and memberships; `oidcauth` becomes one implementation with no behaviour change
- [ ] 5.2 `backend/internal/store/migrations/000004_local_identities.up.sql`: `local_identities` (id, email citext unique, password_hash, confirmed_at, created_at) and `identity_tokens` (id, identity_id, purpose, token_hash, expires_at, used_at)
- [ ] 5.3 `backend/internal/localauth`: registration, argon2id hashing, confirmation tokens (single use, expiring, unguessable), sign-in, password reset that ends every existing session of that subject
- [ ] 5.4 `backend/internal/localauth`: responses that do not disclose whether an address is registered, on both registration and reset; per-account and per-source rate limiting on authentication attempts
- [ ] 5.5 `backend/internal/notify`: the mail seam — confirmation, reset, quota warning, grace-period warning — with a no-op implementation for the internal edition
- [ ] 5.6 `backend/internal/localauth/*_test.go`: token single-use and expiry, reset invalidating sessions, enumeration resistance, rate limiting

## 6. Entitlements <!-- complexity: sonnet | touches: backend/internal/entitlement, backend/internal/httpapi -->

- [ ] 6.1 `backend/internal/store/migrations/000005_plans.up.sql`: `plans` (id, slug, quotas as columns) and `accounts.plan_id`; seed an unlimited plan and make it the internal edition's
- [ ] 6.2 `backend/internal/entitlement`: the create-time check for domains, load balancers and API keys, returning a refusal that names the quota reached
- [ ] 6.3 `backend/internal/entitlement`: the two protective limits — simultaneously published challenge values, and publications per period — applied in every edition, checked on the challenge API path
- [ ] 6.4 `backend/internal/httpapi/challenge.go`: the protective limits enforced on `/present`, refused with a status lego treats as retryable, and recorded
- [ ] 6.5 `backend/internal/entitlement/*_test.go`: a quota refusing the creation that would exceed it; existing objects unaffected when a plan shrinks

## 7. Public alias labels and unverified domains <!-- complexity: sonnet | touches: backend/internal/dnsname, backend/internal/inventory -->

- [ ] 7.1 `backend/internal/dnsname`: label generation becomes a strategy — derived from the challenge name, or a 128-bit random base32 token — selected by edition; the syntactic rules are shared
- [ ] 7.2 `backend/internal/inventory`: challenge-name uniqueness becomes per account; two accounts may add the same domain and neither can answer the other's challenges
- [ ] 7.3 `backend/internal/inventory`: a domain that has never verified holds no claim another account is refused for, and appears in no other account's plan
- [ ] 7.4 `backend/internal/dnsname/*_test.go` and `backend/internal/inventory/*_test.go`: the collision case (two accounts, one domain name) and the non-disclosure of a public label

## 8. Billing <!-- complexity: opus | touches: backend/internal/billing, backend/internal/httpapi -->

- [ ] 8.1 `backend/internal/billing`: the adapter interface — start a checkout, read subscription state, cancel — with a no-op implementation for the internal edition
- [ ] 8.2 `backend/internal/billing/stripe`: checkout sessions, the customer portal, and webhook handling with signature verification; a notification that fails verification changes nothing and is recorded
- [ ] 8.3 `backend/internal/store/migrations/000006_subscriptions.up.sql`: `subscriptions` (account_id, provider, provider_id, status, current_period_end, grace_until)
- [ ] 8.4 Suspension: a state on the account, a configured grace period during which published challenges are still answered, and a warning sent before answering stops
- [ ] 8.5 `backend/internal/billing/*_test.go`: an unverified webhook is inert; suspension inside the grace period still answers `/present`; the warning precedes the cut-off

## 9. The public console <!-- complexity: opus | touches: frontend/src/app -->

- [ ] 9.1 `frontend/src/app/core/session.service.ts`: the session carries memberships; the acting account is explicit in the request rather than inferred from prior state alone
- [ ] 9.2 `frontend/src/app/shared/page-header.component.ts`: the breadcrumb gains an account level, rendered only where a person may hold more than one
- [ ] 9.3 `frontend/src/app/features/account`: sign-up, confirmation, sign-in and password reset screens for the public edition
- [ ] 9.4 `frontend/src/app/features/account`: plan, quota consumption and subscription screens, shown only to members of the account
- [ ] 9.5 `frontend/src/app/features/onboarding`: the first-domain flow — show the delegation record exactly as it must be published, check it, and report what was found against what was expected
- [ ] 9.6 The internal edition renders none of the above: no account in the breadcrumb, no billing route, no sign-up

## 10. The public site <!-- complexity: sonnet | touches: a new repository or directory outside backend/ -->

- [ ] 10.1 A static site, its own pipeline and its own domain, releasable without deploying the service
- [ ] 10.2 "Try now" links into the public edition's registration route; the only coupling is that link and a shared visual language

## 11. Operations and acceptance <!-- complexity: opus | touches: development/, docs/ -->

- [ ] 11.1 `development/compose.yaml`: a second stack, or a profile, running the public edition with its own database and its own zone — never sharing either with the internal one
- [ ] 11.2 `development/acceptance`: the public edition end to end — register, confirm, add a domain, publish the delegation into the fake internet's zone, verify it, and have a quota refuse a creation
- [ ] 11.3 The acceptance suite runs both editions and fails if either is broken, so the edition that is not used daily cannot rot unnoticed
- [ ] 11.4 `docs/installation.md`: operating two editions — the settings that differ, the two databases, the two zones, and what an operator must not share between them
