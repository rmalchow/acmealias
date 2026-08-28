# Tasks

> **Read S1 first.** Nothing refuses anybody today: the blueprint defines one flow and binds no
> enrolment flow, so there is no *Sign up* link to click. There is no policy to reverse — there is a
> flow to write and a gate to put in front of it.
>
> **And read S3.** An earlier version of this change put the approval gate in our service, with a
> column, a clause in `store.Usable`, a third suspension reason, a customer screen and a staff queue.
> All of it is gone. The gate stands in front of the service, so nothing in our schema, our predicate
> or either console changes — and no `accounts` row is ever written for somebody we have not admitted.
>
> **IMPLEMENTED, with one part diagnosed and not fixed.** The flow is in
> `development/authentik/blueprints/acmeservice.yaml`, the operator's half is in
> `development/authentik/README.md`, and `backend/acceptance/enrollment_test.go` walks it. The whole
> path was walked by hand against the running stack through the flow executor — the same API the
> browser drives — and every assertion below marked `[x]` was observed rather than reasoned about.
> **Task 2.2 is the exception and is unchecked with its measurement recorded.**

## 1. The enrolment flow

- [x] 1.1 Add an enrolment flow to `development/authentik/blueprints/acmeservice.yaml`, and link it from the sign-in form so a *Sign up* link actually appears — the absence of that link is the whole of today's closure (design S1)

  > `acmeservice-enrollment`, and the link is `enrollment_flow` on the identification stage. There
  > is no template and no setting: the stage renders the link if and only if that field names a
  > flow, which is exactly why its absence WAS the closure. Verified: the identification challenge
  > now carries `"enroll_url": "/if/flow/acmeservice-enrollment/"`.

- [x] 1.2 Collect a username, an email and a password
- [x] 1.3 Include an email-verification stage. Verification runs BEFORE approval and the ordering matters: a reviewer's attention is the scarce resource, and a queue full of typos and throwaway addresses is a queue nobody reads carefully (design S2)

  > **The flow writes the user TWICE, and that is what makes the ordering real rather than merely
  > chronological.** authentik's email stage hangs its token on a saved user, so the row necessarily
  > exists before the address is proved — meaning "verification first" would otherwise be a claim
  > about time with nothing to show for it. So the first `user_write` puts them on
  > `users/pending-verification` and a second one, after the link is clicked, moves them to
  > `users/pending-approval`. The reviewer's queue is the second folder, and it contains only
  > addresses that exist. authentik renders user paths as a tree, so this is a queue nobody built.
  >
  > The path is carried across the email stage in a HIDDEN prompt field, which is safe for a
  > non-obvious reason worth recording: authentik's prompt stage re-imposes the server's value on
  > every hidden and static field when the form comes back. Verified by posting `"path":"users"`
  > from the client — the user still landed on `users/pending-approval`.

- [x] 1.4 Place enrolled users in no group. Nothing is derived from groups any more (`personal-and-team-accounts` C10), and that is the property worth continuing to demonstrate

  > No `create_users_group`, and no `groups` in the plan context. `enrollment_test.go` asserts the
  > consequence rather than the setting: a self-enrolled person holds their personal account and
  > membership of nothing else.

- [x] 1.5 Set `create_users_as_inactive` on the flow's `user_write` stage. This IS the gate (design S3)

  > And `activate_user_on_success` on the email stage is deliberately FALSE, which the shipped
  > authentik example sets true. The example uses `is_active` to mean "has confirmed their address";
  > here it means "we have admitted this person". Left at the default, the confirmation mail — sent
  > only to prove an address exists — would open the approval gate.

## 2. Telling an unapproved person what is happening

> The part that must not be skipped. authentik's default for an inactive user is a sign-in that simply
> fails, which to the person is indistinguishable from a wrong password — so they retry, reset, retry,
> and mail support. Every one of those is work our silence created, at the exact moment we wanted to
> say one sentence.

- [x] 2.1 Say it at the end of enrolment: the registration was received and is being reviewed

  > A `static` prompt field on the last stage. Verified: after clicking the link in the mail, the
  > flow renders it.

- [ ] 2.2 Say it again on the sign-in attempt. A policy on the authentication flow recognises an unapproved user and a stage renders a real message instead of a generic failure (design S3)

  > **NOT DONE, AND NOT SKIPPED. authentik 2024.10 cannot do this while `is_active` is the gate.**
  > This is the task design S3 said must not be quietly dropped, so here is the measurement instead
  > of a silence, taken against the running stack.
  >
  > **What was observed.** Three sign-in attempts at
  > `/api/v3/flows/executor/default-authentication-flow/` — an unknown identifier, a wrong password
  > for a real user, and the CORRECT password for an inactive one — return byte-identical bodies:
  > `{"response_errors": {"non_field_errors": [{"string": "Failed to authenticate.", "code":
  > "invalid"}]}}`.
  >
  > **Why, structurally.** Because the identification stage carries `password_stage` (which is what
  > closes the enumeration oracle, task 2.4), the password is checked INSIDE that stage's
  > validation, by Django's `ModelBackend`, whose `user_can_authenticate` returns `is_active`. The
  > stage raises a `ValidationError` with that fixed string. No later stage and no policy in the
  > flow is ever reached, so there is nothing for a deny stage to fire on and nowhere to put a
  > message. A deny stage bound at order 15 with a policy on `pending_user.is_active` is dead
  > configuration, and was not added for that reason.
  >
  > **The obvious repair is worse.** Dropping `password_stage` (with `pretend_user_exists` to keep
  > the identification step honest) puts the deny stage before any password is proved — so anybody
  > could type an address and be told whether it has a registration awaiting review. That is
  > precisely the oracle 2.4 forbids, and it leaks the one population we have least right to expose.
  >
  > **The two ways out, both of which cost something the human should choose between.**
  >
  > 1. **Change the gate from `is_active` to the user's PATH**, and deny on `users/pending-approval`
  >    in the authentication flow after the password succeeds. The message becomes possible and the
  >    oracle stays closed. The cost is that the gate stops being a flag Django enforces everywhere
  >    and becomes a policy that fails OPEN if it errors — a gate that fails open is a bad gate, and
  >    `failure_result: true` trades that for an outage for everybody.
  > 2. **Keep `is_active` and put the sentence on the sign-in page for everyone** — "registrations
  >    are reviewed by a person; you can sign in once approved". Oracle-free and cheap, but there is
  >    no field for it: authentik 2024.10's brand has no footer, and the only lever is our own
  >    `custom.css`, which is a poor home for a sentence.
  >
  > Recorded rather than decided. What IS delivered is 2.1 — the sentence at the end of enrolment,
  > which is where most people meet it — and `development/authentik/README.md` names the defect so
  > it is not rediscovered.

- [x] 2.3 Set an expectation of when they will hear. See the open question — it should probably match `staff-plane`'s existing twelve-hour commitment rather than invent a second number

  > Twelve hours, reusing staff-plane's published abuse-contact commitment rather than inventing a
  > second number somebody would have to keep in step. **The number is the human's to confirm** and
  > is load-bearing, not polite: any promise longer than fourteen days is a promise to break
  > invitations (S7).

- [x] 2.4 Check the sentence does not become an enumeration oracle: what an unapproved person is told must not let a stranger test an address list and learn which addresses are registered. This is the same property the `password_stage` on the identification stage was set to protect

  > Held, and it is the constraint that made 2.2 impossible rather than merely hard — see above. The
  > identification stage keeps `password_stage`, and the three attempts above are indistinguishable.
  >
  > **One residual leak, stock and pre-existing.** The enrolment form's username field is
  > authentik's `username` prompt type, which refuses a duplicate — so it reveals whether a USERNAME
  > is taken. It does not reveal anything about addresses (the `email` field type performs no such
  > check), and usernames here are chosen by the registrant rather than being the address list. Left
  > as it is, noted so it is not mistaken for an oversight.

## 3. An invitation is not a way past the queue

> **This group was reversed.** It used to say the opposite — create invited users
> ACTIVE, in a second enrolment flow whose `user_write` differed by one key. See
> design S7 for why, and `account-invitations` V13 for the argument.

- [x] 3.1 Create users who arrive by invitation INACTIVE, exactly like everybody else. There is one enrolment flow (design S7)

  > True by construction: there is one enrolment flow and it creates everybody inactive. Nothing
  > distinguishes an invited registrant at the provider.

- [x] 3.2 Do NOT add a second enrolment flow. The one that used to exist was the exemption's whole mechanism, and deleting it is what makes the exemption unreachable rather than merely unused

  > `acmeservice-enrollment` is the only flow with `designation: enrollment` in the blueprint.

- [ ] 3.3 Record the invited address as CONFIRMED without sending a confirmation message — the invitation link already proved control of it (`account-invitations` V3). Confirmed is not admitted, and this is the only thing an invited person skips

  > **BLOCKED, and the block is `account-invitations` task 7.1, not this flow.** There is nothing in
  > our service that sends somebody INTO enrolment carrying an invitation token: 7.1 there is the
  > entry point that would, and it was blocked on this change existing. It now exists, so 7.1 is
  > unblocked and this task follows it rather than leading it.
  >
  > The mechanism it will use is decided and cheap: authentik's `invitation` stage on the same flow,
  > or a pre-filled `email` prompt whose value is server-imposed, skipping the email stage under a
  > policy. It must NOT be a second flow (3.2).

- [ ] 3.4 Assert it: a person who follows an invitation link and registers while approval is required cannot authenticate until approved

  > Follows 3.3.

- [ ] 3.5 Assert the other half: they receive no confirmation message and the address is confirmed anyway

  > Follows 3.3.

## 4. Abuse, which is one new surface and not the general question

- [x] 4.1 Configure the reputation policy bounding repeated attempts by address and by client address. Enrolment sends mail to an arbitrary address on an UNAUTHENTICATED request — a spam vector pointed at third parties — and it is required here rather than deferred to `public-edition` (design S10)

  > `acmeservice-reputation` (check_ip, check_username, threshold -5) bound through
  > `acmeservice-reputation-deny` to BOTH the enrolment flow (order 5) and the authentication flow
  > (order 15), with `evaluate_on_plan: false` + `re_evaluate_policies: true` — the pair without
  > which a conditional stage is evaluated before anybody has typed anything.
  >
  > **What it does not bound, said plainly rather than left to be discovered.** authentik lowers a
  > reputation score on a failed identification or a failed login, and nothing lowers it for a
  > SUCCESSFUL enrolment. So this bounds an address or a host that has been failing to sign in; it
  > does not bound somebody quietly registering a hundred throwaway accounts from one machine. The
  > rate limit for that is `public-edition`'s, where the production thresholds live too. This is the
  > stock mechanism the task names, configured; it is not the whole of the bound.

- [x] 4.2 Point authentik's outbound mail at `mailpit:1025`. The SMTP host is the compose service name, NOT `mail.localhost`: that name is the web interface, served through the HTTP overlay, which SMTP does not traverse

  > Already true — `AUTHENTIK_EMAIL__HOST: mailpit` in `development/compose.yaml`, set by
  > `transactional-email`. Verified end to end for the first time here: an enrolment produced a real
  > message in mailpit, `Address confirmation`, from `AcmeAlias <no-reply@acmealias.com>`, to the
  > address that was typed. That closes `transactional-email` 4.3 as well.

- [x] 4.3 Assert enrolment, recovery and sign-in responses do not reveal whether an address already has a user

  > Sign-in: asserted by measurement (2.2's note) — three attempts, identical bodies. Enrolment: the
  > `email` prompt performs no duplicate check. **Recovery: nothing to assert — this deployment
  > configures no recovery flow at all**, which is `public-edition` task 1.3's, and is recorded here
  > so the absence is not read as a passing check.

- [x] 4.4 Do NOT re-litigate the rest of the abuse model here. Five projects on a personal account, per-domain billing on a team one, and a domain that answers no challenge until its delegation has been seen already bound it, and an unapproved person reaches none of that machinery

## 5. Turning the gate off

- [x] 5.1 Document that clearing `create_users_as_inactive` stops the flow creating inactive users and does NOT activate the ones it already created (design S8)
- [x] 5.2 Make activating whoever is still held an explicit step in that procedure. A config change that silently admits a backlog of real people, attributed to nobody, at a moment when nobody is looking at them, is not an improvement on remembering

  > Both in `development/authentik/README.md`, beside the approval procedure — a two-step
  > procedure of which the second step is named as not optional, and the rejected alternative (a
  > retroactive flow) recorded with it.

## 6. Walking it

- [x] 6.1 Walk enrolment by hand, reading the message at `http://mail.localhost`, before it is the only way a stranger gets in

  > Walked against the running stack through the flow executor — the same API `mail.localhost`'s
  > browser drives, and the same one `backend/acceptance/signin.go` uses. Registered
  > `dana@corp.example`; the user appeared inactive on `users/pending-verification`; the message
  > arrived in mailpit; the link resumed the flow; the notice rendered; finishing it moved her to
  > `users/pending-approval`. **Not walked in a rendered browser** — the browser extension was not
  > connected — so what is unverified is CSS and layout, not behaviour.

- [x] 6.2 Walk the refused sign-in by hand and read what an unapproved person actually sees. This is the task most likely to reveal that 2.2 does not work as specified

  > It did exactly that. See 2.2.

- [x] 6.3 Approve the user and walk the first sign-in, asserting the personal account is materialized then and not before (design S5)

  > Before approval: `select slug, name from accounts` had no row for her, and sign-in was refused.
  > After ticking Active: sign-in completed the OIDC exchange and `GET /api/v1/accounts` answered
  > exactly one account — `personal-01a048117d577c9fbf642a0767a1d974`, "Dana Corp", kind personal,
  > owner true, created at the moment of that sign-in. The property S5 keeps, observed rather than
  > argued.
  >
  > **A trap for whoever repeats this by hand:** `curl` will not send the console's `acme_oidc_flow`
  > cookie over plain http because it is marked `Secure`, and the callback then fails with
  > `oidcauth: sign-in failed: no flow cookie`, which reads like our bug. The acceptance suite has a
  > `laxJar` for exactly this (`signin.go`); by hand, strip the Secure column from the cookie jar.

- [x] 6.4 Review the wording a self-enrolled person meets throughout. It was written for a misconfigured colleague and is now the first sentence of the product for everyone who signs themselves up

  > Reviewed and rewritten: the flow's title is "Create your AcmeAlias account" rather than
  > authentik's, the confirmation subject is `Address confirmation` rather than the provider's
  > default of "authentik" (the correct name of the wrong product), and the closing notice names the
  > twelve hours and says there is nothing else to do. **The one piece of wording still wrong is the
  > refusal**, and that is 2.2 rather than a sentence somebody can edit.

## 7. Acceptance

> `backend/acceptance/enrollment_test.go`. **Written and compiling (`go vet -tags acceptance`), not
> run**: the acceptance suite must run alone and the human runs it. Everything it asserts was first
> observed by hand against the stack (group 6), so these are regression pins for facts already
> established rather than claims resting on an unrun test.

- [x] 7.1 Register through the provider, verify the address, and assert the sign-in is refused while unapproved
- [x] 7.2 Assert the service holds NO account for them at that point — the property S5 keeps, and the one the earlier design gave away

  > Asserted as `createdAt` being AFTER the moment of approval, which is the strongest form
  > available and deliberately not a query for a flag: there is nothing to query, and that is the
  > design. A row created before the approval instant is the failure this pins.

- [x] 7.3 Approve, sign in, and assert they hold their personal account and membership of no other (`tenancy-and-permissions` P18)
- [ ] 7.4 Assert an invited person does NOT authenticate without approval, and that their address is confirmed regardless (design S7, reversed)

  > Follows 3.3 / `account-invitations` 7.1.

- [ ] 7.5 Assert the interaction `account-invitations` V13 creates: an invitation issued more than fourteen days before its invitee is approved is expired when they finally sign in, and the account can issue a replacement

  > Follows 7.4. This is `account-invitations` 11a.4 seen from the other side, and it is the same
  > test; whichever change writes it should delete the other's copy rather than write two.

## 8. Taking enrolment over from two other changes

- [x] 8.1 Amend `authentik-and-self-signup` §5 to point here for enrolment, keeping branding and the provider swap where they are (design S11)

  > The pointer was already there and said the gate is "in OUR service", which S3 reversed. Corrected
  > rather than merely left.

- [x] 8.2 Amend `public-edition` §1 the same way, keeping recovery, the production reputation policy and the switches
- [x] 8.3 Correct `public-edition` U9's premise while there: it presents the closed state as the work of `ACME_ALLOW_ACCOUNT_CREATION`, and that switch gates buying a TEAM account. It has never been what stops a stranger registering
- [x] 8.4 Add this change to `openspec/changes/SEQUENCE.md`

  > Already at position 7, with S3's correction described. Amended to record that it is implemented
  > and where the one gap is.

- [x] 8.5 Keep S7's reversal cross-referenced in both directions: `account-invitations` V13 argues it and this change implements it, and a reader arriving at either must be sent to the other
