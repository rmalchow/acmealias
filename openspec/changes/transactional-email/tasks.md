# Tasks

> Ordered by the deadline in design T6: the DNS records are the long-lead item and
> the first stranger to receive mail is `account-invitations`. A domain that starts
> in spam folders stays there.

## 1. The sender identity

- [ ] 1.1 Decide the sender address and display name, once, and record them where both senders read them from configuration rather than from a template
- [ ] 1.2 Decide the reply address. `no-reply@` is the reflex and T3 argues against it: every message here is something a recipient may reasonably want to answer
- [ ] 1.3 Write the footer, once. Both senders carry the same one (T5)
- [ ] 1.4 Configure the identity provider's sender to match, in `development/authentik/blueprints/acmeservice.yaml`

## 2. Deliverability, which is the part that fails silently

- [ ] 2.1 Publish an SPF record authorising EVERY host that sends — the identity provider's and the service's are different machines (T1)
- [ ] 2.2 Publish a DKIM key per sender and configure each to sign
- [ ] 2.3 Verify ALIGNMENT, not merely authentication: the domain that passes must be the domain in the visible sender. A message can pass both checks and still be filed as spam when neither aligns (T1)
- [ ] 2.4 Publish a DMARC record, initially in a reporting mode, and read what it reports before tightening it
- [ ] 2.5 Send a real message to a mailbox at a large receiver and read its headers. This is the only way to find out what actually happens, and it costs ten minutes

## 3. The service's sender

- [ ] 3.1 Add a mail sender to the service — the shape only. The messages belong to the changes that send them: `account-invitations` the invitation, `metering-and-credits` the credit warning
- [ ] 3.2 Read the sender identity, reply address and footer from configuration, in the layered schema's grouping
- [ ] 3.3 Point it at `mailpit:1025` in development — the compose service name, NOT `mail.localhost`, which is the web interface and is served through the HTTP overlay that SMTP does not traverse

## 4. Subjects

- [ ] 4.1 Adopt the grammar of T4: the noun the message concerns comes first
- [ ] 4.2 Add NO constant subject prefix. It spends the most valuable characters in an inbox on what the sender column already says (T4)
- [ ] 4.3 Review the identity provider's own subjects against the same grammar and override them where they disagree

## 5. Proving the two agree

- [ ] 5.1 A test that provokes one message from EACH sender, reads both out of mailpit, and asserts the same sender identity and the same footer (T2)
- [ ] 5.2 Make that test the thing that fails when somebody edits one template set and not the other — which is the only version of this agreement that survives being forgotten
