# Tasks

> **Read S1 first.** Nothing refuses anybody today: the blueprint defines one flow and binds no
> enrolment flow, so there is no *Sign up* link to click. There is no policy to reverse — there is a
> flow to write and a gate to put in front of it.
>
> **And read S3.** An earlier version of this change put the approval gate in our service, with a
> column, a clause in `store.Usable`, a third suspension reason, a customer screen and a staff queue.
> All of it is gone. The gate stands in front of the service, so nothing in our schema, our predicate
> or either console changes — and no `accounts` row is ever written for somebody we have not admitted.

## 1. The enrolment flow

- [ ] 1.1 Add an enrolment flow to `development/authentik/blueprints/acmeservice.yaml`, and link it from the sign-in form so a *Sign up* link actually appears — the absence of that link is the whole of today's closure (design S1)
- [ ] 1.2 Collect a username, an email and a password
- [ ] 1.3 Include an email-verification stage. Verification runs BEFORE approval and the ordering matters: a reviewer's attention is the scarce resource, and a queue full of typos and throwaway addresses is a queue nobody reads carefully (design S2)
- [ ] 1.4 Place enrolled users in no group. Nothing is derived from groups any more (`personal-and-team-accounts` C10), and that is the property worth continuing to demonstrate
- [ ] 1.5 Set `create_users_as_inactive` on the flow's `user_write` stage. This IS the gate (design S3)

## 2. Telling an unapproved person what is happening

> The part that must not be skipped. authentik's default for an inactive user is a sign-in that simply
> fails, which to the person is indistinguishable from a wrong password — so they retry, reset, retry,
> and mail support. Every one of those is work our silence created, at the exact moment we wanted to
> say one sentence.

- [ ] 2.1 Say it at the end of enrolment: the registration was received and is being reviewed
- [ ] 2.2 Say it again on the sign-in attempt. A policy on the authentication flow recognises an unapproved user and a stage renders a real message instead of a generic failure (design S3)
- [ ] 2.3 Set an expectation of when they will hear. See the open question — it should probably match `staff-plane`'s existing twelve-hour commitment rather than invent a second number
- [ ] 2.4 Check the sentence does not become an enumeration oracle: what an unapproved person is told must not let a stranger test an address list and learn which addresses are registered. This is the same property the `password_stage` on the identification stage was set to protect

## 3. An invitation is not a way past the queue

> **This group was reversed.** It used to say the opposite — create invited users
> ACTIVE, in a second enrolment flow whose `user_write` differed by one key. See
> design S7 for why, and `account-invitations` V13 for the argument.

- [ ] 3.1 Create users who arrive by invitation INACTIVE, exactly like everybody else. There is one enrolment flow (design S7)
- [ ] 3.2 Do NOT add a second enrolment flow. The one that used to exist was the exemption's whole mechanism, and deleting it is what makes the exemption unreachable rather than merely unused
- [ ] 3.3 Record the invited address as CONFIRMED without sending a confirmation message — the invitation link already proved control of it (`account-invitations` V3). Confirmed is not admitted, and this is the only thing an invited person skips
- [ ] 3.4 Assert it: a person who follows an invitation link and registers while approval is required cannot authenticate until approved
- [ ] 3.5 Assert the other half: they receive no confirmation message and the address is confirmed anyway

## 4. Abuse, which is one new surface and not the general question

- [ ] 4.1 Configure the reputation policy bounding repeated attempts by address and by client address. Enrolment sends mail to an arbitrary address on an UNAUTHENTICATED request — a spam vector pointed at third parties — and it is required here rather than deferred to `public-edition` (design S10)
- [ ] 4.2 Point authentik's outbound mail at `mailpit:1025`. The SMTP host is the compose service name, NOT `mail.localhost`: that name is the web interface, served through the HTTP overlay, which SMTP does not traverse
- [ ] 4.3 Assert enrolment, recovery and sign-in responses do not reveal whether an address already has a user
- [ ] 4.4 Do NOT re-litigate the rest of the abuse model here. Five projects on a personal account, per-domain billing on a team one, and a domain that answers no challenge until its delegation has been seen already bound it, and an unapproved person reaches none of that machinery

## 5. Turning the gate off

- [ ] 5.1 Document that clearing `create_users_as_inactive` stops the flow creating inactive users and does NOT activate the ones it already created (design S8)
- [ ] 5.2 Make activating whoever is still held an explicit step in that procedure. A config change that silently admits a backlog of real people, attributed to nobody, at a moment when nobody is looking at them, is not an improvement on remembering

## 6. Walking it

- [ ] 6.1 Walk enrolment by hand, reading the message at `http://mail.localhost`, before it is the only way a stranger gets in
- [ ] 6.2 Walk the refused sign-in by hand and read what an unapproved person actually sees. This is the task most likely to reveal that 2.2 does not work as specified
- [ ] 6.3 Approve the user and walk the first sign-in, asserting the personal account is materialized then and not before (design S5)
- [ ] 6.4 Review the wording a self-enrolled person meets throughout. It was written for a misconfigured colleague and is now the first sentence of the product for everyone who signs themselves up

## 7. Acceptance

- [ ] 7.1 Register through the provider, verify the address, and assert the sign-in is refused while unapproved
- [ ] 7.2 Assert the service holds NO account for them at that point — the property S5 keeps, and the one the earlier design gave away
- [ ] 7.3 Approve, sign in, and assert they hold their personal account and membership of no other (`tenancy-and-permissions` P18)
- [ ] 7.4 Assert an invited person does NOT authenticate without approval, and that their address is confirmed regardless (design S7, reversed)
- [ ] 7.5 Assert the interaction `account-invitations` V13 creates: an invitation issued more than fourteen days before its invitee is approved is expired when they finally sign in, and the account can issue a replacement

## 8. Taking enrolment over from two other changes

- [ ] 8.1 Amend `authentik-and-self-signup` §5 to point here for enrolment, keeping branding and the provider swap where they are (design S11)
- [ ] 8.2 Amend `public-edition` §1 the same way, keeping recovery, the production reputation policy and the switches
- [ ] 8.3 Correct `public-edition` U9's premise while there: it presents the closed state as the work of `ACME_ALLOW_ACCOUNT_CREATION`, and that switch gates buying a TEAM account. It has never been what stops a stranger registering
- [ ] 8.4 Add this change to `openspec/changes/SEQUENCE.md`
- [ ] 8.5 Keep S7's reversal cross-referenced in both directions: `account-invitations` V13 argues it and this change implements it, and a reader arriving at either must be sent to the other
