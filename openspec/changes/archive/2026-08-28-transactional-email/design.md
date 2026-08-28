# Design

Decisions are numbered `T1`, `T2`, … and referred to by number from `tasks.md`.

## T1. The sender identity is one thing, and getting it wrong is invisible

Both senders use the same `From` address and the same display name.

This reads as a cosmetic decision and is the one with real consequences:

- **Mail clients thread by sender.** Two spellings of us — `acmealias` and
  `AcmeAlias`, or `no-reply@` and `noreply@` — arrive as two correspondents, so a
  customer looking for "that invitation" finds a conversation that does not
  contain it.
- **SPF and DKIM must authorise BOTH hosts.** The identity provider sends from
  its own machine and the service from another. Each must be in the domain's SPF
  record and each must sign with a published DKIM key, and the result must
  *align* with the visible `From`.

**Alignment is the part that is easy to half-do.** A message can pass SPF for the
host that sent it and still be filed as spam because the domain that passed is
not the domain the reader sees. Both senders must therefore authenticate as the
same domain as `From`, not merely authenticate.

**None of this bounces when it is wrong.** There is no error to notice: the
message is accepted and filed away from the inbox. The first report is a customer
saying they never received something we can see we sent, which is the hardest
class of bug to believe.

## T2. The specification is shared; the implementation cannot be

authentik renders Django templates in Python; the service will render Go
templates. There is nothing to factor out.

So the agreement is enforced from outside, by a test that reads what each sender
actually put on the wire. The development stack already routes both to mailpit,
whose API is readable — so the test sends one message from each, fetches both,
and asserts the same envelope and the same footer.

**Rejected: agreeing by convention and reviewing it.** Two template sets in two
languages, edited by different changes months apart, converge on identical
headers exactly as long as somebody remembers. The test is the only version of
this that survives being forgotten.

**Rejected: routing authentik's mail through the service.** It would give one
implementation, and it makes the identity provider depend on the service being up
in order to send the message that lets somebody recover access to it — which is
precisely when the service may be what they cannot reach.

## T3. Reply-To goes somewhere a person reads

`no-reply@` is the reflex, and it is wrong for most of what is sent here.

The messages are invitations, warnings that an account is about to stop working,
and notices that a delegation never appeared. Every one of them is something a
recipient may reasonably want to answer, and a reply address that discards mail
teaches them that answering us does not work — which they will remember later,
when we need them to answer.

`From` may remain a no-reply sender; `Reply-To` points at whatever support
address exists. Where none exists yet, that is an argument for creating one, not
for discarding replies.

## T4. A subject grammar, not a subject prefix

**Rejected: `[acmealias] ` on every subject.** The prefix spends the most valuable
characters in an inbox — the ones visible in a notification and in a narrow
list — on information the sender column is already showing. It is a habit
inherited from mailing lists, where the sender genuinely was ambiguous.

Instead the subjects follow a grammar, and the useful noun comes first:

```
  Invitation to <account>            not  [acmealias] You have been invited
  Credit low: <account>              not  [acmealias] Warning about your credits
  Delegation not published: <domain> not  [acmealias] Action required
```

Front-loading the noun is what makes a mailbox search and a notification useful,
and "Action required" is what a subject says when nobody decided what it was
about.

## T5. The footer identifies the sender, and it is not decoration

A transactional message says who sent it. The company is German-facing whatever
the domain's suffix is, so the footer carries the identifying information that
requires, and both senders carry the same one.

It is settled here rather than per-message because the alternative is discovering,
after the fifth template, that three of them say something slightly different —
and because the requirement is about the sender rather than about any one message.

## T6. Deliverability is a deadline, not a task

The DNS records are the long-lead item: a DKIM key must be published and
propagated before it can be relied on, and reputation is accrued rather than
configured.

So this change is ordered by when the first stranger receives mail, not by when it
is convenient. That is `account-invitations`, which is the next numbered change —
and it is the reason this one is worth doing before it rather than alongside it.

**A domain that starts in spam folders stays there.** Receivers weight sender
reputation historically, so the cost of the first thousand badly-authenticated
messages is paid for months. There is no version of this that is cheaper later.
