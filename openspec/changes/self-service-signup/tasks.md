# Tasks

> **Read S1 first.** Nothing is refusing anybody today: the blueprint defines one flow and binds no
> enrolment flow, so there is no *Sign up* link to click. There is no policy to reverse — there is a
> flow to write and a gate to build behind it.

## 1. Approval on the account

- [ ] 1.1 Add `accounts.approved_at timestamptz NULL` in a migration, backfilling every existing account to its `created_at` — they predate the gate and worked before it (design S4)
- [ ] 1.2 Add the conjunct to `store.Usable`, ABOVE the kind test, so it gates every account of every kind. Do NOT move `staff_suspended` while doing it: that line is `staff-plane`'s decision and S5 explains why the two behave differently
- [ ] 1.3 Extend `Usable`'s comment where somebody will be standing when they trip over the asymmetry — a suspension is a judgment about conduct, approval is the absence of any judgment yet
- [ ] 1.4 Add the third reason to `store.SuspensionReason`, worded for somebody who registered an hour ago and has done nothing wrong (design S6)
- [ ] 1.5 Regenerate `internal/store/schema.sql` through the golden test and confirm `model_drift_test` passes
- [ ] 1.6 Test the predicate directly in `funding_test.go`'s table: unapproved-and-funded, unapproved-personal, approved-and-unfunded, approved-and-suspended

## 2. The setting

- [ ] 2.1 Add `ACME_REQUIRE_ACCOUNT_APPROVAL` to the layered config schema, defaulting to REQUIRED — a deployment that configures nothing gets the gate, not an open door (design S8)
- [ ] 2.2 Consult it in `store.EnsurePersonalAccount` only, at creation. It writes `approved_at`; it is never read by `Usable` (design S8, C3)
- [ ] 2.3 Add a test that flipping the setting changes NEITHER direction for existing rows — on does not lock out, off does not release
- [ ] 2.4 Document it in the settings table beside `ACME_ALLOW_ACCOUNT_CREATION`, and say plainly how the two differ: one gates buying a team account, this one gates being a customer at all

## 3. Invitation bypasses the queue

- [ ] 3.1 Set `approved_at` on the accepter's own account when an invitation is accepted, if it is not already set (design S7)
- [ ] 3.2 Record it as a vouched admission, distinguishable in the audit trail from a staff decision
- [ ] 3.3 Test the half-state this prevents: an invited person must not end up able to work in the inviting account and not in their own

## 4. The customer's screen

- [ ] 4.1 Add the awaiting-review screen. It is NOT `suspended.component.ts` with different words — S6 is the argument, and the concrete difference is that it has no checkout
- [ ] 4.2 Offer NO way to pay. Every other unusable account here is fixed by money, and taking money from somebody waiting on a human is the worst possible misreading of their situation
- [ ] 4.3 Set an expectation of when they will hear. See the open question — it should probably match `staff-plane`'s existing twelve-hour commitment rather than invent a second number
- [ ] 4.4 Route into it from the same place the suspended screen is reached, on the reason rather than on the balance — the machinery is already there and `suspended.component.ts`'s own comment says why

## 5. The staff queue

- [ ] 5.1 Add the review queue to the staff console: address, when they registered, whether verified, account slug. Nothing else — a reviewer has no other signal and a richer screen invites them to look for some (design S9)
- [ ] 5.2 Approve and refuse. Refusal keeps the row and the audit entry, and leaves the queue, so nobody is re-presented forever
- [ ] 5.3 Do NOT delete the authentik user on refusal; whether somebody may re-register is `staff-plane`'s blocklist question
- [ ] 5.4 Report the backlog when approval is no longer required and accounts still await it, with an approve-together action. This is S8's trap made visible rather than fixed by weakening the predicate
- [ ] 5.5 Put the components in `shared` unless there is a clear reason otherwise

## 6. Audit

- [ ] 6.1 Add the account approval and refusal actions to `internal/authz/vocab.go`, naming the deciding staff member and the account
- [ ] 6.2 Distinguish a vouched admission from a reviewed one — an account let in by an invitation was never looked at, and an audit that says otherwise is wrong about who took responsibility

## 7. The enrolment flow itself

- [ ] 7.1 Add an enrolment flow to `development/authentik/blueprints/acmeservice.yaml`, bound to the authentication flow so a *Sign up* link appears
- [ ] 7.2 Include an email-verification stage. A session must not be issued before control of the address is proved, and the ordering matters: verification first, so the queue a human reads holds only addresses that exist (design S2)
- [ ] 7.3 Place enrolled users in no group. Nothing is derived from groups any more (`personal-and-team-accounts` C10), and that is the property to keep demonstrating
- [ ] 7.4 Do NOT use `create_users_as_inactive`. The gate is ours, and S3 is the argument — the person is told nothing, the reviewer is in the wrong building, and nothing lands in our audit trail
- [ ] 7.5 Point authentik's outbound mail at `mailpit:1025`. The SMTP host is the compose service name, NOT `mail.localhost`: that name is the web interface and is served through the HTTP overlay, which SMTP does not traverse
- [ ] 7.6 Configure the reputation policy bounding repeated attempts by address and by client address. Enrolment sends mail to an arbitrary address on an unauthenticated request, which is a spam vector pointed at third parties, and it is required here rather than deferred (design S10)
- [ ] 7.7 Assert enrolment, recovery and sign-in responses do not reveal whether an address already has a user
- [ ] 7.8 Walk it once by hand, reading the message at `http://mail.localhost`, before it is the only way a stranger gets in
- [ ] 7.9 Review the wording a self-enrolled person meets. It was written for a misconfigured colleague and is now the first sentence of the product for everyone who signs themselves up

## 8. Acceptance

- [ ] 8.1 Register through the provider, verify the address, sign in, and land on the awaiting-review screen rather than in the product
- [ ] 8.2 Approve through the staff surface, and assert the same person's next request works
- [ ] 8.3 Assert an unapproved account is refused with a reason that is NOT the funding `402` and NOT a suspension — the three have different remedies and a customer sent to the wrong one is the failure this gate would otherwise introduce
- [ ] 8.4 Assert an invited person never reaches the queue (design S7)
- [ ] 8.5 Assert a self-registered person holds their personal account and membership of no other (`tenancy-and-permissions` P18)

## 9. Taking enrolment over from two other changes

- [ ] 9.1 Amend `authentik-and-self-signup` §5 to point here for enrolment, keeping branding and the provider swap where they are (design S11)
- [ ] 9.2 Amend `public-edition` §1 the same way, keeping recovery, the production reputation policy and the switches
- [ ] 9.3 Correct `public-edition` U9's premise while there: it presents the closed state as the work of `ACME_ALLOW_ACCOUNT_CREATION`, and that switch gates buying a TEAM account. It has never been what stops a stranger registering
- [ ] 9.4 Add this change to `openspec/changes/SEQUENCE.md`
