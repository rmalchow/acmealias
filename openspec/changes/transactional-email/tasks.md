# Tasks

> Ordered by the deadline in design T6: the DNS records are the long-lead item and
> the first stranger to receive mail is `account-invitations`. A domain that starts
> in spam folders stays there.

## 1. The sender identity

- [x] 1.1 Decide the sender address and display name, once, and record them where both senders read them from configuration rather than from a template
- [x] 1.2 Decide the reply address. `no-reply@` is the reflex and T3 argues against it: every message here is something a recipient may reasonably want to answer
- [ ] 1.3 Write the footer, once. Both senders carry the same one (T5)

  > **Open, and it is the operator's to close, not this change's.** The mechanism
  > is built — one `mail.footer` setting, read by `mail.Identity`, refused at
  > start-up when empty — and `config.yaml` now names exactly what the value has
  > to contain: §5 DDG makes the company's name and legal form, its postal
  > address, an address for immediate contact, the register court and number, the
  > authorised representative and the VAT identification number compulsory on a
  > commercial message. Those are facts about the operating company, not about
  > this software. The shipped default is a development placeholder and is
  > labelled as one.
  >
  > The second half — the identity provider carrying the SAME footer — has
  > nothing to attach to yet: authentik renders a footer only inside an email
  > stage's template, and this deployment configures no email stage, because no
  > flow here sends mail. It arrives with `self-service-signup`, alongside 4.3
  > and 5.1.

- [x] 1.4 Configure the identity provider's sender to match, in `development/authentik/blueprints/acmeservice.yaml`

  > Done, but NOT in the file this task names, and the difference is worth
  > recording: authentik's sender is `AUTHENTIK_EMAIL__FROM`, which is read from
  > the environment at start-up and is not a blueprint-managed object — there is
  > no `authentik_*` model carrying it, so a blueprint entry for it cannot be
  > written. It is set in `development/compose.yaml` beside the mail host and
  > port, to `AcmeAlias <no-reply@acmealias.com>`, which is what `config.yaml`'s
  > `mail.from` and `mail.sender` default to. `backend/acceptance/mail_test.go`
  > reads both sides back and fails when somebody edits one.

## 2. Deliverability, which is the part that fails silently

- [ ] 2.1 Publish an SPF record authorising EVERY host that sends — the identity provider's and the service's are different machines (T1)
- [ ] 2.2 Publish a DKIM key per sender and configure each to sign
- [ ] 2.3 Verify ALIGNMENT, not merely authentication: the domain that passes must be the domain in the visible sender. A message can pass both checks and still be filed as spam when neither aligns (T1)
- [ ] 2.4 Publish a DMARC record, initially in a reporting mode, and read what it reports before tightening it
- [ ] 2.5 Send a real message to a mailbox at a large receiver and read its headers. This is the only way to find out what actually happens, and it costs ten minutes

## 3. The service's sender

- [x] 3.1 Add a mail sender to the service — the shape only. The messages belong to the changes that send them: `account-invitations` the invitation, `metering-and-credits` the credit warning
- [x] 3.2 Read the sender identity, reply address and footer from configuration, in the layered schema's grouping
- [x] 3.3 Point it at `mailpit:1025` in development — the compose service name, NOT `mail.localhost`, which is the web interface and is served through the HTTP overlay that SMTP does not traverse

> 4.1 was open until there was a subject to judge. `account-invitations` wrote
> one — `Invitation to <account>` — and a test now asserts it against the grammar,
> so this is closed.

## 4. Subjects

- [x] 4.1 Adopt the grammar of T4: the noun the message concerns comes first

  > Closed by `account-invitations`, which wrote the first subject this service
  > has ever sent: `Invitation to <account>`. The noun first, no constant prefix,
  > and `TestIntegrationInvitationEndToEnd` asserts both against the message it
  > reads back out of the sender — so the grammar is now enforced by a test rather
  > than recorded in a comment.
- [x] 4.2 Add NO constant subject prefix. It spends the most valuable characters in an inbox on what the sender column already says (T4)
- [ ] 4.3 Review the identity provider's own subjects against the same grammar and override them where they disagree

  > **Nothing to review yet.** A subject is authentik's only when an email STAGE
  > renders one, and `development/authentik/blueprints/acmeservice.yaml`
  > configures no email stage: authentik ships none by default, the dev stack has
  > no recovery flow and no enrolment flow, so the identity provider currently
  > sends zero messages. Checking this off today would mean reviewing the
  > subjects of an empty set.
  >
  > It unblocks with `self-service-signup`, which is what adds the flow that
  > sends. Overriding is a custom template mounted into authentik's template
  > directory and named on the stage — the same mechanism 1.3's second half
  > needs, so do both at once.

> What exists: `backend/acceptance/mail_test.go` compares the two senders'
> CONFIGURED identities — the service's effective config against authentik's
> environment — which already fails when somebody changes one side. It does not
> yet provoke a message from each and read both out of mailpit, because
> provoking authentik's means driving an enrolment flow `self-service-signup`
> has not built. Both tasks stay open until it does.

## 5. Proving the two agree

- [ ] 5.1 A test that provokes one message from EACH sender, reads both out of mailpit, and asserts the same sender identity and the same footer (T2)
- [ ] 5.2 Make that test the thing that fails when somebody edits one template set and not the other — which is the only version of this agreement that survives being forgotten
