# Tasks

> **What this list used to contain.** Nine groups, of which four described work that has since been
> done or reassigned: the authentication seam and delegated authentication (`authentik-and-self-signup`
> K1, done), editions and `ACME_EDITION` (`personal-and-team-accounts` C10 removed the second
> edition, so there is nothing to select), the account-creation switch (built —
> `ACME_ALLOW_ACCOUNT_CREATION`, `backend/internal/httpapi/admin_accounts.go`), and most of onboarding
> (`domain-setup-feedback` and `domain-activation`). What remains is below.

## 1. Self-service enrolment, in development first

- [ ] 1.1 Add an enrolment flow to `development/authentik/blueprints/acmeservice.yaml`, reachable from the console sign-in page, with an **email-verification stage** — a session must not be issued before the address is proved (design U4, U9)
- [ ] 1.2 Point authentik's outbound mail at the stack's sink — `mailpit:1025`, no credentials, no TLS. The SMTP host is the compose service name, NOT `mail.localhost`: that name is the web interface and is served through the HTTP overlay, which SMTP does not pass through
- [ ] 1.3 Configure the recovery flow with email verification, and assert that enrolment, recovery and sign-in responses do not reveal whether an address has a user (design U4)
- [ ] 1.4 Configure a reputation policy limiting repeated failed attempts by username and by client address, and exercise it in acceptance rather than assuming it (design U4)
- [ ] 1.5 Walk enrolment and recovery once by hand, reading the messages at `http://mail.localhost`, so the fallback path is known to work before it is the only way a stranger recovers an account
- [ ] 1.6 Review the wording a self-enrolled person now meets. It was written for a misconfigured colleague and is now the first sentence of the product for everyone who signs themselves up
- [ ] 1.7 Assert by acceptance test that a self-enrolled person signs in, holds their personal account, and holds no membership of any other account (design U9, `tenancy-and-permissions` P18)
- [ ] 1.8 Assert that no route or screen accepts a password and no credential material is stored

## 2. The switches, and what they actually gate

- [ ] 2.1 Correct the doc comment on `AllowAccountCreation` in `backend/internal/config/config.go`: it explains itself in terms of "creating a tenant", and after `personal-and-team-accounts` what it gates is buying a TEAM account. The enforcement is right; the explanation is stale (design U9)
- [ ] 2.2 Keep the test that calls the team-account-creation endpoint directly, bypassing the console, and add the other half: a person enrolled at the provider with the switch off still has a personal account and can work in it. That is the sentence U9 used to get wrong
- [ ] 2.3 Deliberately do NOT add a switch on personal-account materialization, and record why in `oidcauth/flow.go` beside the `EnsurePersonalAccount` call: it would reintroduce the empty-handed state `personal-and-team-accounts` was pleased to delete, and it protects nothing that U6 and `domain-activation` do not already bound (design U9)
- [ ] 2.4 Follow the switches in the console's links and the marketing site's call to action, as presentation only

## 3. Protective limits

- [ ] 3.1 Add the limit on simultaneously published challenge values per account
- [ ] 3.2 Add the publication rate limit per account
- [ ] 3.3 Apply both to every account whatever its kind and balance; assert a large balance does not raise a limit and a personal account is bounded (design U6)
- [ ] 3.4 Make a refusal name the limit and when it clears, and keep it distinguishable from the `402` an unfunded team account gets — a limit is not a payment problem and must not send an operator to look at a balance that may not exist (`personal-and-team-accounts` C7)
- [ ] 3.5 Assert one account reaching a limit does not affect another, and does not withdraw values already published
- [ ] 3.6 Make the values settings rather than constants, with no operation inside an account that raises one

## 4. Payment behind the existing checkout

- [ ] 4.1 Add the payment provider adapter and redirect the existing checkout to the provider's **hosted** payment page; store no instrument (design U5)
- [ ] 4.2 Add the notification endpoint with signature verification; make an unverifiable notification change nothing and be recorded as an attempt
- [ ] 4.3 Raise the balance and record the purchase indivisibly, referencing the provider's payment reference — the same rows and the same audit entry `personal-and-team-accounts` C8 already writes
- [ ] 4.4 Make repeated delivery of one completed payment credit exactly once
- [ ] 4.5 Handle refund and reversal notifications as recorded balance adjustments
- [ ] 4.6 Keep the amount a fixed choice from the offered set rather than a number the caller supplies
- [ ] 4.7 Add the payment configuration switch. With it off, the checkout keeps C8's behaviour exactly; with it on, the same route reaches the provider. Assert both in acceptance, so the day payment is enabled is not the first day that path has run (design U5)
- [ ] 4.8 Assert no interface — API, console or CLI — sets a balance or purchase state directly inside an account, and that the operator credit grant `metering-and-credits` specifies remains the only other source

## 5. The rest of the staff and customer separation

- [ ] 5.1 Write the two ingress rules the hostname split depends on: an address allowlist on `admin.acmealias.com`, and a refusal of `/api/admin/**` on `console.acmealias.com`. Both names reach the same listener and it routes by path, so without the second the first restricts nothing (design U7)
- [ ] 5.2 Test that the staff tree answers 404 through the customer hostname regardless of the caller. The arrangement works the day it is written and stops working the day somebody simplifies the ingress, which is not a thing a runbook catches
- [ ] 5.3 Assert a token issued for the console application is refused on the staff surface, rather than relying on a valid signature being checked (design U12)
- [ ] 5.4 Register customers as `external` users so they reach neither the provider's administration interface nor its application dashboard
- [ ] 5.5 Assert the staff application has no enrolment flow — trivially true until task 1.1 adds one, which is exactly when it needs asserting
- [ ] 5.6 Restrict provider administrator access to a small group with mandatory multi-factor authentication
- [ ] 5.7 Assert no group or attribute in the provider grants membership of any account (design U12, `tenancy-and-permissions` P18)

## 6. The production identity provider

- [ ] 6.1 Stand up the instance at `auth.acmealias.com` with the blueprint the development stack proves, rather than by clicking
- [ ] 6.2 Add a brand per hostname so customer enrolment and recovery are reachable at the customer name and not from the staff one
- [ ] 6.3 Configure outbound mail for real delivery, and monitor it — a bounced verification message locks a customer out rather than degrading a feature
- [ ] 6.4 Document operating it: backup, tested restore, patching commitment, and `internal/adminctl` as the way in when the provider is unavailable (design U4, `staff-plane` S8)
- [ ] 6.5 Set the service session idle expiry to the bound U4 relies on, and document the residual post-reset window rather than implying a reset ends sessions immediately
- [ ] 6.6 Record back-channel logout or a revocation check as the eventual fix, not built here

## 7. Deployment shape

- [ ] 7.1 Record the reversal of U2 where it will be read: our own domains live in an ordinary account in the customer deployment, and the confinement machinery is exercised by the people who would notice it failing (design U2)
- [ ] 7.2 Keep environments on separate datastores and separate zones, and say so where the deployment manifests are
- [ ] 7.3 Serve the apex, `www`, `console`, `admin` and `auth` of `acmealias.com` from a zone this service is not the sole authority for, so the pages explaining an outage survive it (design U7)

## 8. The marketing site and what it must contain

- [ ] 8.1 Create the static site as a separate deployment with its own pipeline and domain (design U7)
- [ ] 8.2 Link its call to action into the console's sign-in route
- [ ] 8.3 State what the service does and does not do: it answers ACME DNS-01 challenges and observes the certificates that result; it does not host DNS, does not issue certificates, and does not hold private keys (design U11)
- [ ] 8.4 Do NOT state anything about DNSSEC. U10 withdrew that requirement when the DNS product went — nobody delegates a zone here — and the earlier task list still asked for it
- [ ] 8.5 Publish terms stating best-effort availability with no service level agreement
- [ ] 8.6 Publish an abuse contact address and the twelve-hour response commitment `staff-plane` S10 makes
- [ ] 8.7 Publish a privacy statement covering registration addresses, zone contents, challenge interaction records including a load balancer's client address, staff access records and blocked identities, each with its retention period
- [ ] 8.8 Publish the operator's identity and contact address as the jurisdiction requires
- [ ] 8.9 Gate the site's release on 8.3 and 8.5–8.8 being present, so it cannot go live as a signup button alone

## 9. First run

- [ ] 9.1 Decide how much of the inventory hierarchy a new arrival must understand before adding one domain — projects and load balancers exposed as they are, or one default of each they never name. This is the open question U8 leaves, and it is a screen decision
- [ ] 9.2 Build the first-run path over the endpoints that already answer: the plan renders the record to publish, `GET /loadbalancers/:id/verification` resolves the delegation
- [ ] 9.3 Leave the reporting of what was found against what was expected to `domain-setup-feedback`, and the decision to serve the domain at all to `domain-activation`; do not restate either
- [ ] 9.4 End first run at a domain whose delegation has been seen, and say what the service will now do — issuing no certificate, and saying so

## 10. Acceptance and closing out

- [ ] 10.1 Extend the acceptance suite to enrol a new person through the provider, verify the address and sign in (design U9)
- [ ] 10.2 Cover a purchase through the provider with payment enabled, and the same checkout with payment disabled
- [ ] 10.3 Cover a protective limit refusing an operation, and its refusal being distinguishable from the funding `402`
- [ ] 10.4 Add an operational runbook for mail delivery failure, since a bounced verification message locks a customer out
- [ ] 10.5 Confirm before closing that every U-number this change once carried is either specified here or named in the disposition table with the change that took it
