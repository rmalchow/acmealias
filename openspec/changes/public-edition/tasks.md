## 1. The authentication seam

- [ ] 1.1 Extract the interface `internal/oidcauth` already satisfies — login, callback, logout, CSRF — with no behavioral change to the OIDC flow
- [ ] 1.2 Wire the implementation through the composition root in `cmd/acmeservice/main.go` rather than by direct construction
- [ ] 1.3 Assert by test that session storage, idle and absolute expiry, revocation and CSRF are properties of the session layer and not of any implementation
- [ ] 1.4 Confirm the internal edition's observable behavior is unchanged by running the existing acceptance suite

## 2. Delegated authentication

- [ ] 2.1 Point both editions at the identity provider at `auth.edgedns.eu`, differing only in the tenant they authenticate against (design U3)
- [ ] 2.2 Configure separate tenants for staff and for customers; assert a customer-tenant identity is not authenticated on the staff surface
- [ ] 2.3 Configure the customer enrolment flow with an email-verification stage, and assert no usable session is issued without it
- [ ] 2.4 Configure the recovery flow with email verification, and assert enrolment and recovery responses do not reveal whether an address has a user
- [ ] 2.5 Configure a reputation policy limiting repeated failed attempts by username and by client address, and exercise it in acceptance rather than assuming it (design U4)
- [ ] 2.6 Make a credential reset or revocation at the provider end the service's own sessions for that person — back-channel logout, revocation check, or a documented short idle expiry
- [ ] 2.7 Assert no route or screen accepts a password and no credential material is stored
- [ ] 2.8 Create a user belonging to no account on first sign-in, and confirm `tenancy-and-permissions` treats that state as normal
- [ ] 2.9 Confirm enrolment reached through an invitation needs no separate address confirmation, per `account-invitations`, without duplicating that rule here
- [ ] 2.10 Document operating the provider: backup, tested restore, patching commitment, and `internal/adminctl` as the path when the provider is unavailable

## 3. Editions

- [ ] 3.1 Add `ACME_EDITION` to `internal/config` with validation; refuse startup on an unknown value naming the setting and accepted values
- [ ] 3.2 Validate at startup that the configured edition has every setting it requires, and refuse naming what is missing
- [ ] 3.3 Report the edition in the startup log and on the readiness endpoint
- [ ] 3.4 Select the identity provider tenant by edition in the composition root
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

- [ ] 6.1 Hand off to the provider for enrolment, sign-in and recovery; build no credential screens of our own
- [ ] 6.2 Build the account-creation screen for a signed-in person who belongs to no account, subject to the switch in group 9
- [ ] 6.3 Show a signed-in person with no account only account creation, or the invitation they can accept
- [ ] 6.4 Build the onboarding path to a first verified domain over the existing plan and verification endpoints
- [ ] 6.5 Report what was found against what was expected when a delegation is missing or wrong
- [ ] 6.6 Build the billing screen: balance, purchase history, payment state, confined to the account, with no control that sets a balance
- [ ] 6.7 Add credit purchase to the console, ending at the provider's checkout

## 7. Marketing site

- [ ] 7.1 Create the static site as a separate deployment with its own pipeline and domain
- [ ] 7.2 Link "try now" into the public edition's registration route
- [ ] 7.3 State plainly that the service answers challenges and serves DNS, and does not issue certificates
- [ ] 7.4 State that DNSSEC is not supported and that a signed domain must have its delegation signer removed first (design U10, U11)
- [ ] 7.5 Publish terms stating best-effort availability with no service level agreement
- [ ] 7.6 Publish an abuse contact address and the twelve-hour response commitment `staff-plane` S10 makes
- [ ] 7.7 Publish a privacy statement covering registration addresses, zone contents, staff access records and blocked identities, each with its retention period
- [ ] 7.8 Publish the operator's identity and contact address as the jurisdiction requires
- [ ] 7.9 Gate the site's release on 7.4-7.8 being present, so it cannot go live as a signup button alone

## 8. Acceptance and closing out

- [ ] 8.1 Extend the acceptance harness to run both editions and fail if either is broken
- [ ] 8.2 Cover enrolment, address verification, a domain reaching verification, a credit purchase and a protective limit refusal in the public edition
- [ ] 8.3 Add an operational runbook for email delivery failure, since a bounced confirmation locks a customer out
- [x] 8.4 `add-accounts-and-public-edition` is archived unimplemented; confirm before closing that every requirement of it is either here or named in this change's supersession table
- [ ] 8.5 Decide where the unhoused rule "an unverified domain is not charged for" belongs, or record that the credit model makes it moot

## 9. Registration and payment switches

- [ ] 9.1 Add the self-service account-creation switch; refuse the account-creation handler with a stated reason when it is off (design U9)
- [ ] 9.2 Add a test calling the account-creation endpoint directly, bypassing the console, asserting refusal when the switch is off and success when it is on; assert a person enrolled at the provider still reaches nothing
- [ ] 9.3 Follow the switch in the console's links and the marketing site's call to action, as presentation only
- [ ] 9.4 Add the payment configuration switch; refuse the purchase endpoints when it is off, leaving the staff credit grant as the only source of balance
- [ ] 9.5 Confirm invitation acceptance is unaffected by either switch
- [ ] 9.6 State on the marketing site that DNSSEC is not supported, and that a signed domain must have its delegation signer removed before it can be hosted (design U10)
