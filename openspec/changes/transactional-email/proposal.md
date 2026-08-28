# One voice, from two senders

## What this changes, in one paragraph

Two different programs will send mail to the same person about the same account.
The identity provider sends about signing in — verify this address, reset your
access. The service sends about the product — you have been invited, your credit
is nearly gone, your delegation is not published. They are written in different
languages, rendered by different template engines, and today they would arrive
looking like mail from two companies. This decides what they must agree on, and
gets the one part that fails silently right before anybody receives anything.

## Terms, before they are used

- **Transactional mail** is sent because a person did something, to that person,
  one message at a time. It is not marketing, and the distinction matters
  legally and for deliverability.
- **SPF** and **DKIM** are how a receiving mail server decides whether a message
  claiming to be from our domain really is. SPF authorises sending *hosts*; DKIM
  signs the message with a key published in our DNS.
- **Alignment** is the property that the domain a person sees in `From:` is the
  same one that passed SPF or DKIM. A message can pass both and still be treated
  as suspicious if neither aligns with the visible sender.

## Why

### Two senders that cannot share a line of code

authentik renders Django templates in Python. The service will render Go
templates. There is no template to share, no helper to extract, and no library
that will make one of them produce the other's output.

So what is shared has to be a **specification**, and the only thing that will keep
two implementations honest about a specification is a test that reads what each
one actually sent. The development stack already makes that cheap: every message
from both goes to mailpit, whose API is readable from a test. A test that asserts
the shape of both senders' output is the mechanism; agreement by intention is not.

### The part that fails silently, and the deadline it has

Getting the wording wrong is embarrassing and fixable. Getting the **sender**
wrong is neither.

Mail clients thread by sender, so two spellings of us arrive as two
correspondents in a customer's inbox. Worse: authentik sends from its host and
the service from another, and both must be authorised for the domain. A message
that fails SPF and DKIM alignment does not bounce — it is quietly filed as spam,
and the customer's report is not "your mail looked wrong", it is "I never got the
invitation".

That gives this a deadline the rest of the queue does not have. **A domain that
starts out in spam folders stays there**: receivers weight reputation historically,
so the cost of getting this wrong on the first thousand messages is paid for
months afterwards. It has to be right before the first message a stranger
receives, which is before `account-invitations` sends anything.

### What each sender is for, decided rather than discovered

| Sender | Owns | Examples |
| --- | --- | --- |
| The identity provider | anything about *signing in* | address verification, recovery |
| The service | anything about *the product* | invitations, credit warnings, a delegation that never appeared |

The split is not arbitrary: the identity provider is the only one that can prove
an address, and the service is the only one that knows what an account is doing.
Writing it down stops the invitation mail drifting into authentik because it
happens to have a template engine.

## What Changes

- **One sender identity** — the same `From` address and display name from both,
  with SPF and DKIM authorising each sending host, and alignment verified rather
  than assumed.
- **A reply path that reaches a person**, decided rather than defaulted to
  `no-reply@`.
- **A footer** that identifies the sender, the same in both.
- **A subject grammar** rather than a subject prefix — see design T4, which
  argues against the prefix.
- **A test** that reads what both senders actually put on the wire, because two
  implementations of one specification drift.

## Capabilities

- `service-operations` — what the deployment must be configured to send, and what
  it must prove about it.

## Impact

- The service gains a mail sender it does not have yet; this change owns its
  shape, not the messages, which belong to the changes that send them.
- `development/authentik/blueprints/acmeservice.yaml` gains the sender
  configuration; the dev stack already routes both to mailpit.
- DNS records for the production domain — which is the long-lead item, because a
  DKIM key has to be published and propagated before it can be relied on.

## What this does not do

- **It does not write the messages.** `account-invitations` owns the invitation,
  `metering-and-credits` the credit warning, `domain-setup-feedback` any mail
  about a delegation. This owns what every one of them must look like.
- **It does not add marketing mail.** Nothing here is sent to a list, and nothing
  needs an unsubscribe mechanism, which is a different regime with different law.
- **It does not choose a delivery provider.** That is a deployment decision with
  a cost; what this fixes is what must be true whichever one is chosen.
