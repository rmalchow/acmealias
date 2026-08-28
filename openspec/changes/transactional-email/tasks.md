# Tasks

> Ordered by the deadline in design T6: the DNS records are the long-lead item and
> the first stranger to receive mail is `account-invitations`. A domain that starts
> in spam folders stays there.

## 1. The sender identity

- [x] 1.1 Decide the sender address and display name, once, and record them where both senders read them from configuration rather than from a template
- [x] 1.2 Decide the reply address. `no-reply@` is the reflex and T3 argues against it: every message here is something a recipient may reasonably want to answer
- [ ] 1.3 **Half done, and I ticked it too early — correcting that here.** The footer IS written once (`config.yaml:327`, and `mail.go` treats an empty one as a configuration error rather than absent decoration) and the SERVICE carries it. authentik does not: `compose.yaml` shares `AUTHENTIK_EMAIL__FROM` so the two agree on the sender, and there is no equivalent for the footer — authentik renders its own Django templates, which carry none. So "both senders carry the same one" is currently false for the half nobody looked at. Needs custom authentik email templates (a mounted template directory) carrying the same footer as the service's

  > **A placeholder is in place and the task stays open.** `config.yaml` and the
  > built-in defaults carry "AcmeAlias Inc, Example Street 1234, Exampletown,
  > Foobar Province" — every part chosen to LOOK like a placeholder, because a
  > plausible street and a made-up register number would look finished and survive
  > a review.
  >
  > Supplied: the name and the postal address. STILL MISSING from §5 DDG: the
  > legal form, the register court and registration number, the person authorised
  > to represent the company, and the VAT identification number. Nothing in the
  > code can supply them and nothing checks them.

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

**MOVED to `public-edition` (tasks 1a.1–1a.5).** Every task in this group is a
record published at a registrar or a message sent to a real mailbox, and none of
them is development work: this deployment sends to mailpit, which accepts
everything, delivers nothing, and evaluates no authentication at all. There is
nothing here that a green stack could ever have demonstrated, and nothing that
`transactional-email` can finish.

They are not dropped, and the distinction matters, because this is the group the
change's own proposal singles out as failing SILENTLY — a message that does not
authenticate as the domain in its own `From` is filed as spam rather than
bounced, and a domain that starts in spam folders stays there. They belong with
the change that first sends mail to somebody we have never met.

What this change DOES own and has finished: one sender identity, one footer, one
subject grammar, and an acceptance test that reads both senders off the wire and
fails when somebody edits one side and not the other.

- [x] 2.1 Moved to `public-edition` 1a.1 — publish an SPF record authorising EVERY host that sends
- [x] 2.2 Moved to `public-edition` 1a.2 — publish a DKIM key per sender
- [x] 2.3 Moved to `public-edition` 1a.3 — verify ALIGNMENT, not merely authentication
- [x] 2.4 Moved to `public-edition` 1a.4 — publish a DMARC record in a reporting mode first
- [x] 2.5 Moved to `public-edition` 1a.5 — send a real message to a large receiver and read its headers

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
- [x] 4.3 Review the identity provider's own subjects against the same grammar and override them where they disagree

  > **Reviewed, and there is exactly one.** `self-service-signup` added the first
  > and only email stage this deployment has —
  > `acmeservice-enrollment-email-verification` — so the set that was empty when
  > this note was written now has one member.
  >
  > It disagreed and was overridden. authentik's `EmailStage.subject` defaults to
  > the literal `authentik`, which is the correct name of the wrong product as far
  > as the recipient is concerned. It is now `Address confirmation`: the noun the
  > message concerns first, no constant prefix, reading the way
  > `Invitation to <account>` does. Verified by provoking a real registration and
  > reading the message back out of mailpit — subject `Address confirmation`,
  > from `AcmeAlias <no-reply@acmealias.com>`.
  >
  > **Overriding needed no custom template**, which this note previously assumed
  > it would: the subject is a field on the stage and therefore blueprint-managed.
  > The custom TEMPLATE mechanism is still what 1.3's footer needs, and that is
  > still open.

> What exists: `backend/acceptance/mail_test.go` compares the two senders'
> CONFIGURED identities — the service's effective config against authentik's
> environment — which already fails when somebody changes one side.
>
> **UNBLOCKED.** The reason these stayed open was that provoking a message from
> authentik meant driving an enrolment flow nobody had built.
> `self-service-signup` built it, and
> `backend/acceptance/enrollment_test.go` already provokes one and reads it back
> out of mailpit to find the confirmation link — so the machinery both tasks
> wanted is written and only the assertion is missing. The service half is
> provoked by `TestIntegrationInvitationEndToEnd`.

## 5. Proving the two agree

- [ ] 5.1 **Half done.** `TestBothSendersShareOneIdentity` exists, passes, and reads both senders off the wire out of mailpit — but it compares the ADDRESS and the DISPLAY NAME only. The footer half of this task is unasserted, and 1.3 above says why: authentik carries no footer yet, so the assertion would fail today. Add it with 1.3, not before, so the test fails for the real reason rather than being written around the gap. Original task: A test that provokes one message from EACH sender, reads both out of mailpit, and asserts the same sender identity and the same footer (T2)

  > Not written. Both halves can now be provoked in one test — enrolment for the
  > provider's, an invitation for the service's — using
  > `enrollment_test.go`'s helpers. What it must compare is what ARRIVED, not
  > what was configured, which is the whole difference from
  > `TestBothSendersShareOneIdentity`.
  >
  > **The footer half of it cannot pass yet**, and that is 1.3 rather than this
  > task: the footer is not written, and authentik's side of it needs a custom
  > template mounted into the image. So this test lands with the sender-identity
  > assertion and gains the footer one when 1.3 does.
- [x] 5.2 Done by 5.1's test, which is written as exactly this and says so in its own header: it reads what each sender actually put on the wire, because two template sets in two languages converge only as long as somebody remembers. Make that test the thing that fails when somebody edits one template set and not the other — which is the only version of this agreement that survives being forgotten
