## 1. The authentication seam

- [ ] 1.1 Extract the interface `internal/oidcauth` already satisfies — login, callback, logout, CSRF — with no behavioral change to the OIDC flow
- [ ] 1.2 Wire the implementation through the composition root in `cmd/acmeservice/main.go` rather than by direct construction
- [ ] 1.3 Assert by test that session storage, idle and absolute expiry, revocation and CSRF are properties of the session layer and not of any implementation
- [ ] 1.4 Confirm the internal edition's observable behavior is unchanged by running the existing acceptance suite

## 2. Local authentication

- [ ] 2.1 Add `internal/localauth` implementing the seam: registration, sign-in, sign-out
- [ ] 2.2 Store passwords with argon2id; assert the stored form is unusable for authentication
- [ ] 2.3 Add email confirmation: expiring, single-use, unguessable token; grant nothing but sign-in until confirmed
- [ ] 2.4 Add password reset: expiring, single-use, and ending every existing session of that user on use
- [ ] 2.5 Make registration and reset responses indistinguishable between an address that has a user and one that does not
- [ ] 2.6 Rate limit authentication attempts per user and per source, separately from the protective limits of group 4
- [ ] 2.7 Create a registered user belonging to no account, and confirm `tenancy-and-permissions` treats that state as normal
- [ ] 2.8 Confirm registration reached through an invitation link needs no separate confirmation, per `account-invitations`, without duplicating that rule here

## 3. Editions

- [ ] 3.1 Add `ACME_EDITION` to `internal/config` with validation; refuse startup on an unknown value naming the setting and accepted values
- [ ] 3.2 Validate at startup that the configured edition has every setting it requires, and refuse naming what is missing
- [ ] 3.3 Report the edition in the startup log and on the readiness endpoint
- [ ] 3.4 Select the authentication implementation by edition in the composition root
- [ ] 3.5 Document the operational rule that editions share neither datastore nor zone, and add the deployment manifests for a second zone

## 4. Protective limits

- [ ] 4.1 Add the limit on simultaneously published challenge values per account
- [ ] 4.2 Add the publication rate limit per account
- [ ] 4.3 Apply both in every edition, independently of credit balance; assert a large balance does not raise a limit
- [ ] 4.4 Make a refusal name the limit and when it clears, and distinguishable from an exhausted-balance refusal and from a failure
- [ ] 4.5 Assert one account reaching a limit does not affect another, and does not withdraw values already published

## 5. Credit purchase

- [ ] 5.1 Add the payment provider adapter and the checkout initiation
- [ ] 5.2 Add the notification endpoint with signature verification; make an unverifiable notification change nothing and be recorded as an attempt
- [ ] 5.3 Raise the balance and record the purchase indivisibly, referencing the provider's payment reference
- [ ] 5.4 Make repeated delivery of one completed payment credit exactly once
- [ ] 5.5 Handle refund and reversal notifications as balance adjustments, recorded
- [ ] 5.6 Assert no interface — API, console or CLI — sets a balance or purchase state directly
- [ ] 5.7 Expose no payment route, screen or notification in the identity-provider edition

## 6. Console

- [ ] 6.1 Build registration, confirmation and password reset screens, reachable without a session
- [ ] 6.2 Offer to resend confirmation, and report identically whether or not an address already has a user
- [ ] 6.3 Show an unconfirmed user only confirmation and resend
- [ ] 6.4 Build the onboarding path to a first verified domain over the existing plan and verification endpoints
- [ ] 6.5 Report what was found against what was expected when a delegation is missing or wrong
- [ ] 6.6 Build the billing screen: balance, purchase history, payment state, confined to the account, with no control that sets a balance
- [ ] 6.7 Add credit purchase to the console, ending at the provider's checkout

## 7. Marketing site

- [ ] 7.1 Create the static site as a separate deployment with its own pipeline and domain
- [ ] 7.2 Link "try now" into the public edition's registration route
- [ ] 7.3 State plainly that the service answers challenges and serves DNS, and does not issue certificates

## 8. Acceptance and closing out

- [ ] 8.1 Extend the acceptance harness to run both editions and fail if either is broken
- [ ] 8.2 Cover registration, address confirmation, a domain reaching verification, a credit purchase and a protective limit refusal in the locally authenticated edition
- [ ] 8.3 Add an operational runbook for email delivery failure, since a bounced confirmation locks a customer out
- [ ] 8.4 Remove `add-accounts-and-public-edition` once this change and `tenancy-and-permissions` are accepted, having confirmed every requirement of it is either here or named in this change's supersession table
- [ ] 8.5 Decide where the unhoused rule "an unverified domain is not charged for" belongs, or record that the credit model makes it moot
