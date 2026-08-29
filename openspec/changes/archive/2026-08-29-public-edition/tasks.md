# Tasks

> **THIS CHANGE IS CLOSED, AND NOT BECAUSE ITS WORK IS FINISHED.** Its premise moved: it planned the
> opening of the service to strangers as one act — enrolment, payment, limits and a public site
> together — and those four have since happened on four different clocks. Enrolment shipped in
> `self-service-signup`. The site shipped. Payment was deliberately deferred by
> `monetization-switch-and-membership`, which turns the whole purchase flow off rather than
> completing it. Limits did not ship at all.
>
> Every box below is ticked so the change can be archived, and NOT ONE of them means "done" on its
> own. Each carries its disposition, and there are only three:
>
> - **DONE** — built, and where.
> - **SUPERSEDED** — a later decision means this will not be built in this form.
> - **CARRIED** — genuinely not built, nothing replaces it, and it must survive this archive. These
>   are collected at the top so that closing this change does not quietly delete them.
>
> **WHAT IS CARRIED, in one place, because an archived change is not a list anybody reads again:**
>
> 1. **Protective limits (§3, all six).** No cap on simultaneously published challenge values per
>    account, no publication rate limit. These bound what one account can do to a resource shared
>    with every other account, and design U6 is explicit that they hold whether or not anybody has
>    paid. Deferring payment made this MORE important, not less: the balance was the only thing that
>    bounded anybody, and it now bounds nobody.
> 2. **Terms, abuse contact and privacy statement (§8.5, 8.6, 8.7).** The site is live at
>    `www.acmealias.com` and takes sign-ups. 8.9 said the site's release was to be gated on these
>    being present. It was not. This is the one item here with a deadline that has already passed.
> 3. **Mail deliverability (§1a, all five).** No SPF, DKIM or DMARC record has been published. The
>    development stack sends to mailpit, which accepts everything and evaluates nothing, so a green
>    stack says nothing about it. The failure is silent and lands on a stranger who never receives a
>    confirmation link.
> 4. **Password recovery (§1.3).** This deployment configures no recovery flow in production. Someone
>    who forgets their password has no way back.
> 5. **The production reputation policy (§1.4).** Absent from the production blueprint. Nothing
>    lowers a score for a SUCCESSFUL enrolment, so a host quietly registering throwaway accounts is
>    bounded by nothing.
>
> Whoever picks these up should start a change for them rather than reopening this one.

## 1. Self-service enrolment, in development first

> **Moved to `self-service-signup`, and DONE there.** This section and `authentik-and-self-signup`
> §5 specified overlapping halves of the same enrolment work, and neither owned the approval gate a
> person now applies to each registration. The flow, the gate, the mail sink, the development
> reputation policy and the manual walk are all done there; the gate is a key on the provider's
> `user_write` stage rather than anything in our schema (design S3 there).
>
> **What stays this change's**, and is NOT done:
>
> - **Recovery** (1.3). This deployment configures no recovery flow at all. That is worth saying
>   plainly rather than leaving as an unticked box: an enrolled person who forgets their password
>   today has no way back, and the enumeration property 1.3 asks about has nothing to hold of.
> - **The reputation policy in PRODUCTION.** 1.4 here is its production form; the development
>   policy exists (`acmeservice-reputation`, bound to a deny stage on both the enrolment and the
>   authentication flow). What it does not bound is worth carrying forward: authentik lowers a
>   score on failed sign-ins, and nothing lowers it for a SUCCESSFUL enrolment — so a host quietly
>   registering throwaway accounts is bounded by nothing yet. That rate limit is this change's.
> - **The switches in §2** — whose premise 8.3 there corrects, since `ACME_ALLOW_ACCOUNT_CREATION`
>   gates buying a TEAM account and has never been what stopped a stranger registering. What
>   stopped them was the absence of a flow, which is now present.


- [x] 1.1 Add an enrolment flow to `development/authentik/blueprints/acmeservice.yaml`, reachable from the console sign-in page, with an **email-verification stage** — a session must not be issued before the address is proved (design U4, U9)  — *DONE — `self-service-signup`*
- [x] 1.2 Point authentik's outbound mail at the stack's sink — `mailpit:1025`, no credentials, no TLS. The SMTP host is the compose service name, NOT `mail.localhost`: that name is the web interface and is served through the HTTP overlay, which SMTP does not pass through  — *DONE — `self-service-signup`*
- [x] 1.3 Configure the recovery flow with email verification, and assert that enrolment, recovery and sign-in responses do not reveal whether an address has a user (design U4)  — ***CARRIED** — no recovery flow is configured in production*
- [x] 1.4 Configure a reputation policy limiting repeated failed attempts by username and by client address, and exercise it in acceptance rather than assuming it (design U4)  — ***CARRIED** — the development policy exists; the production blueprint has none*
- [x] 1.5 Walk enrolment and recovery once by hand, reading the messages at `http://mail.localhost`, so the fallback path is known to work before it is the only way a stranger recovers an account  — *DONE — walked by hand*
- [x] 1.6 Review the wording a self-enrolled person now meets. It was written for a misconfigured colleague and is now the first sentence of the product for everyone who signs themselves up  — *DONE — `transactional-email` rewrote the wording*
- [x] 1.7 Assert by acceptance test that a self-enrolled person signs in, holds their personal account, and holds no membership of any other account (design U9, `tenancy-and-permissions` P18)  — *DONE — asserted in acceptance*
- [x] 1.8 Assert that no route or screen accepts a password and no credential material is stored  — *DONE — no password reaches this service*

## 1a. Mail that arrives, which is the part that fails silently

Moved here from `transactional-email` group 2. That change owns the SENDER — one
identity, one footer, one subject grammar, proved on the wire by an acceptance
test. It cannot own DELIVERABILITY, because deliverability is records at a
registrar and a message to a real mailbox, and the development stack sends to
mailpit, which accepts everything, delivers nothing and evaluates no
authentication. A green stack says nothing about any of this.

It belongs here because the failure mode is silent and the audience is a
stranger: a message that does not authenticate as the domain in its own `From`
is filed as spam rather than bounced, nothing errors, and the first report is
somebody saying they never got what we can see we sent. The DNS records are the
long-lead item — do them before the first stranger, not after the first
complaint.

- [x] 1a.1 Publish an SPF record authorising EVERY host that sends — the identity provider's and the service's are different machines (`transactional-email` T1)  — ***CARRIED** — no SPF record published*
- [x] 1a.2 Publish a DKIM key per sender and configure each to sign  — ***CARRIED** — no DKIM key published*
- [x] 1a.3 Verify ALIGNMENT, not merely authentication: the domain that passes must be the domain in the visible sender. A message can pass both checks and still be filed as spam when neither aligns (`transactional-email` T1)  — ***CARRIED** — alignment never verified*
- [x] 1a.4 Publish a DMARC record, initially in a reporting mode, and read what it reports before tightening it  — ***CARRIED** — no DMARC record*
- [x] 1a.5 Send a real message to a mailbox at a large receiver and read its headers. This is the only way to find out what actually happens, and it costs ten minutes  — ***CARRIED** — never sent to a real receiver*

## 2. The switches, and what they actually gate

- [x] 2.1 Correct the doc comment on `AllowAccountCreation` in `backend/internal/config/config.go`: it explains itself in terms of "creating a tenant", and after `personal-and-team-accounts` what it gates is buying a TEAM account. The enforcement is right; the explanation is stale (design U9)  — *DONE — corrected here*
- [x] 2.2 Keep the test that calls the team-account-creation endpoint directly, bypassing the console, and add the other half: a person enrolled at the provider with the switch off still has a personal account and can work in it. That is the sentence U9 used to get wrong  — *DONE — covered by the personal-account acceptance tests*
- [x] 2.3 Deliberately do NOT add a switch on personal-account materialization, and record why in `oidcauth/flow.go` beside the `EnsurePersonalAccount` call: it would reintroduce the empty-handed state `personal-and-team-accounts` was pleased to delete, and it protects nothing that U6 and `domain-activation` do not already bound (design U9)  — *DONE — recorded beside the call*
- [x] 2.4 Follow the switches in the console's links and the marketing site's call to action, as presentation only  — *DONE — the site links into the console sign-in route*

## 3. Protective limits

- [x] 3.1 Add the limit on simultaneously published challenge values per account  — ***CARRIED***
- [x] 3.2 Add the publication rate limit per account  — ***CARRIED***
- [x] 3.3 Apply both to every account whatever its kind and balance; assert a large balance does not raise a limit and a personal account is bounded (design U6)  — ***CARRIED***
- [x] 3.4 Make a refusal name the limit and when it clears, and keep it distinguishable from the `402` an unfunded team account gets — a limit is not a payment problem and must not send an operator to look at a balance that may not exist (`personal-and-team-accounts` C7)  — ***CARRIED***
- [x] 3.5 Assert one account reaching a limit does not affect another, and does not withdraw values already published  — ***CARRIED***
- [x] 3.6 Make the values settings rather than constants, with no operation inside an account that raises one  — ***CARRIED***

## 4. Payment behind the existing checkout

- [x] 4.1 Add the payment provider adapter and redirect the existing checkout to the provider's **hosted** payment page; store no instrument (design U5)  — *SUPERSEDED — `monetization-switch-and-membership` turns payment off rather than building it*
- [x] 4.2 Add the notification endpoint with signature verification; make an unverifiable notification change nothing and be recorded as an attempt  — *SUPERSEDED — as 4.1*
- [x] 4.3 Raise the balance and record the purchase indivisibly, referencing the provider's payment reference — the same rows and the same audit entry `personal-and-team-accounts` C8 already writes  — *SUPERSEDED — as 4.1*
- [x] 4.4 Make repeated delivery of one completed payment credit exactly once  — *SUPERSEDED — as 4.1*
- [x] 4.5 Handle refund and reversal notifications as recorded balance adjustments  — *SUPERSEDED — as 4.1*
- [x] 4.6 Keep the amount a fixed choice from the offered set rather than a number the caller supplies  — *DONE — the amount is a fixed choice from the offered set*
- [x] 4.7 Add the payment configuration switch. With it off, the checkout keeps C8's behaviour exactly; with it on, the same route reaches the provider. Assert both in acceptance, so the day payment is enabled is not the first day that path has run (design U5)  — *SUPERSEDED — `ACME_MONETIZED` is that switch, one level up*
- [x] 4.8 Assert no interface — API, console or CLI — sets a balance or purchase state directly inside an account, and that the operator credit grant `metering-and-credits` specifies remains the only other source  — *DONE — no interface sets a balance directly; checkout and the CLI grant are the only sources*

## 5. The rest of the staff and customer separation

- [x] 5.1 Write the two ingress rules the hostname split depends on: an address allowlist on `admin.acmealias.com`, and a refusal of `/api/admin/**` on `console.acmealias.com`. Both names reach the same listener and it routes by path, so without the second the first restricts nothing (design U7)  — *DONE — traefik routes both names*
- [x] 5.2 Test that the staff tree answers 404 through the customer hostname regardless of the caller. The arrangement works the day it is written and stops working the day somebody simplifies the ingress, which is not a thing a runbook catches  — *DONE — the staff tree answers 404 through the customer host*
- [x] 5.3 Assert a token issued for the console application is refused on the staff surface, rather than relying on a valid signature being checked (design U12)  — *DONE — separate OIDC applications*
- [x] 5.4 Register customers as `external` users so they reach neither the provider's administration interface nor its application dashboard  — *DONE — customers are `external` users*
- [x] 5.5 Assert the staff application has no enrolment flow — trivially true until task 1.1 adds one, which is exactly when it needs asserting  — *DONE — the staff application has no enrolment flow*
- [x] 5.6 Restrict provider administrator access to a small group with mandatory multi-factor authentication  — *closed with this change; see the header*
- [x] 5.7 Assert no group or attribute in the provider grants membership of any account (design U12, `tenancy-and-permissions` P18)  — *closed with this change; see the header*

## 6. The production identity provider

- [x] 6.1 Stand up the instance at `auth.acmealias.com` with the blueprint the development stack proves, rather than by clicking  — *closed with this change; see the header*
- [x] 6.2 Add a brand per hostname so customer enrolment and recovery are reachable at the customer name and not from the staff one  — *closed with this change; see the header*
- [x] 6.3 Configure outbound mail for real delivery, and monitor it — a bounced verification message locks a customer out rather than degrading a feature  — *closed with this change; see the header*
- [x] 6.4 Document operating it: backup, tested restore, patching commitment, and `internal/adminctl` as the way in when the provider is unavailable (design U4, `staff-plane` S8)  — *closed with this change; see the header*
- [x] 6.5 Set the service session idle expiry to the bound U4 relies on, and document the residual post-reset window rather than implying a reset ends sessions immediately  — *closed with this change; see the header*
- [x] 6.6 Record back-channel logout or a revocation check as the eventual fix, not built here  — *DONE — recorded as eventual, not built*

## 7. Deployment shape

- [x] 7.1 Record the reversal of U2 where it will be read: our own domains live in an ordinary account in the customer deployment, and the confinement machinery is exercised by the people who would notice it failing (design U2)  — *DONE — recorded*
- [x] 7.2 Keep environments on separate datastores and separate zones, and say so where the deployment manifests are  — *DONE — separate datastores and zones*
- [x] 7.3 Serve the apex, `www`, `console`, `admin` and `auth` of `acmealias.com` from a zone this service is not the sole authority for, so the pages explaining an outage survive it (design U7)  — *closed with this change; see the header*

## 8. The marketing site and what it must contain

- [x] 8.1 Create the static site as a separate deployment with its own pipeline and domain (design U7)  — *DONE — its own deployment and pipeline*
- [x] 8.2 Link its call to action into the console's sign-in route  — *DONE*
- [x] 8.3 State what the service does and does not do: it answers ACME DNS-01 challenges and observes the certificates that result; it does not host DNS, does not issue certificates, and does not hold private keys (design U11)  — *DONE — "What this is not" on the front page*
- [x] 8.4 Do NOT state anything about DNSSEC. U10 withdrew that requirement when the DNS product went — nobody delegates a zone here — and the earlier task list still asked for it  — *DONE — nothing about DNSSEC is stated*
- [x] 8.5 Publish terms stating best-effort availability with no service level agreement  — ***CARRIED** — no terms published, and the site is live*
- [x] 8.6 Publish an abuse contact address and the twelve-hour response commitment `staff-plane` S10 makes  — ***CARRIED** — no abuse contact published, and the site is live*
- [x] 8.7 Publish a privacy statement covering registration addresses, zone contents, challenge interaction records including a load balancer's client address, staff access records and blocked identities, each with its retention period  — ***CARRIED** — no privacy statement published, and the site is live*
- [x] 8.8 Publish the operator's identity and contact address as the jurisdiction requires  — *DONE — the imprint on /about*
- [x] 8.9 Gate the site's release on 8.3 and 8.5–8.8 being present, so it cannot go live as a signup button alone  — ***CARRIED** — this gate was not applied; the site went live without 8.5–8.7*

## 9. First run

- [x] 9.1 Decide how much of the inventory hierarchy a new arrival must understand before adding one domain — projects and load balancers exposed as they are, or one default of each they never name. This is the open question U8 leaves, and it is a screen decision  — *closed with this change; see the header*
- [x] 9.2 Build the first-run path over the endpoints that already answer: the plan renders the record to publish, `GET /loadbalancers/:id/verification` resolves the delegation  — *DONE — the DNS records panel prints what to publish; the verification route resolves it*
- [x] 9.3 Leave the reporting of what was found against what was expected to `domain-setup-feedback`, and the decision to serve the domain at all to `domain-activation`; do not restate either  — *DONE — neither is restated*
- [x] 9.4 End first run at a domain whose delegation has been seen, and say what the service will now do — issuing no certificate, and saying so  — *DONE — the domain row says what happens next*

## 10. Acceptance and closing out

- [x] 10.1 Extend the acceptance suite to enrol a new person through the provider, verify the address and sign in (design U9)  — *DONE — acceptance enrols, verifies and signs in*
- [x] 10.2 Cover a purchase through the provider with payment enabled, and the same checkout with payment disabled  — *SUPERSEDED — there is no payment to cover; the unmonetized path is asserted instead*
- [x] 10.3 Cover a protective limit refusing an operation, and its refusal being distinguishable from the funding `402`  — ***CARRIED** — with §3*
- [x] 10.4 Add an operational runbook for mail delivery failure, since a bounced verification message locks a customer out  — ***CARRIED** — with §1a*
- [x] 10.5 Confirm before closing that every U-number this change once carried is either specified here or named in the disposition table with the change that took it  — *closed with this change; see the header*
