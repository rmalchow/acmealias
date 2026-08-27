# Personal accounts, team accounts, and the balance that switches one on

## What this changes, in one paragraph

Today every account is the same kind of thing, and the only edition that exists
puts everybody inside one shared account whose roles come from the identity
provider. This replaces that with two kinds of account and one switch. A person
does not create their personal account — they simply have one: free, implicit,
theirs forever, with no colleagues and no roles in it. A **team** account is a different object: it is created by
paying, it holds other people and the roles they are given, and it works only
while its credit balance is above zero. The internal edition goes away entirely.

## Terms, before they are used

- An **account** is the thing everything belongs to. Projects, load balancers,
  domains, aliases and API keys all carry an `account_id`, and every read is
  confined to one account by a `Scope` — that machinery is already built
  (`tenancy-and-permissions` P1–P13) and is not what changes here. What changes
  is what *kinds* of account exist and what makes one work.
- A **project** is one set of load balancers, with their nodes, their domains
  and the API keys that publish for them. It is the unit a person is limited to
  a number of.
- **Credits** are the balance an account is charged with. One number per
  account. This change introduces the balance and a way to add to it; the ledger
  that spends it down is `metering-and-credits`.
- The **owner** is the single user an account belongs to. A personal account's
  owner never changes and was never chosen — it is the user it belongs to. A team
  account's can be handed to someone else.
- **Funded** means `credits > 0`. That is the whole definition, and everything
  below turns on it.
- The **internal edition** is the shape the service runs in today: one account
  created at migration time, membership derived from identity-provider group
  claims, four fixed roles named by configuration. It is being removed.

## Why

### An account of one is the only account that can exist, and it is the wrong one

`account-invitations` opens by observing that an account of one is all anyone can
currently build. This change is the other half of that sentence: it decides what
the *second* kind of account is, and what it costs.

The shape that fell out of the exploration is not a ladder with a personal
account at the bottom. It is two different objects:

| | Personal | Team |
| --- | --- | --- |
| How it comes to exist | **implicit** — every user has one, always | created **by paying** |
| How many per user | exactly one, ever | as many as they pay for |
| Owner | fixed at creation, never transferred | transferable |
| Other people in it | none | invited, and given roles |
| Roles | none — there is nobody to give one to | the whole role and grant model |
| Cost | free | credits |
| Works when | always | only while `credits > 0` |

The personal account is not a trial of the team account and does not become one.
That is a deliberate simplification with a cost, and the cost is stated in
"What this does not solve" below.

### One switch, and it is the balance

`metering-and-credits` already argued for a single mechanism: *"credits are what
run out. One mechanism stops an account, and it is the balance."* This change
takes that literally and early. A team account that is not funded does not
half-work:

- its members **cannot select it**, so it is not reachable in the console;
- the challenge API **refuses to publish** for its domains;
- its **owner** can still select it, and sees exactly two things: the balance and
  a way to add to it.

The owner exception is not a softening. Without it there is no door back in, and
a lapsed account would be unrecoverable by the only person who can pay for it.

### Refusing challenges is the sharpest thing here, and it is worth being precise about

Refusing `/present` does not break a customer's site that day. It stops
renewals, and a certificate already installed keeps working until it expires —
which is where the thirty-odd days of slack in `SEQUENCE.md` come from, and why
that document can say the worst failure in this product is that "renewals stop
until somebody notices".

So the consequence of a lapsed balance is not an outage. It is a **clock**, and
the customer will not feel it for weeks. That makes the warning path more
important than the refusal, and it is why the refusal must say plainly what
happened rather than looking like an authentication failure.

### The internal edition is in the way, and removing it is not tidying

Every user having their own personal account is incompatible with everybody
sharing one account created at migration time. The two cannot both be true, so
this change deletes the second.

What goes with it is load-bearing and should be named rather than discovered:

- **`ScopeForBootstrap`**, the producer that resolves "the one account".
- **Automatic account selection at sign-in**, which is why the "no account
  selected" state of P19 has never actually been reached in practice.
- **The group-to-role mapping** — `ACME_ADMIN_GROUP` and its three siblings, and
  the derived-membership half of P18.
- **The four fixed roles** as a global vocabulary.
- **Constraint 1**, "the internal edition's observable behaviour must not
  change", which is the property `tenancy-and-permissions` was built against and
  which task 11.4 finally proved.

Two consequences worth staring at:

**P19 stops being an edge case and becomes the front door.** Today no one ever
reaches "no account selected", because sign-in selects the only account there is.
Afterwards every sign-in lands there, and the least-exercised path in the console
becomes the most-travelled one.

It is worth being exact about what that state now is, because the implicit
personal account changes it: **the picker is reached on every sign-in and is
never empty.** A user always has at least their personal account to choose. So
"no account selected" means "you have not yet said which" rather than "you have
nothing" — which is the distinction P19 already insisted on when it refused to
answer that state with an empty result. The state it was built for is the state
everybody now starts in.

**P18 gets stronger, not weaker.** Its rule is that membership is rows we write
rather than claims we read, with a derived-membership branch for the internal
edition. Removing that branch turns a rule with an exception into a rule.

### Payment is a no-op, deliberately and visibly

Checkout charges the account without taking any money: enter checkout, choose 25,
50 or 100 EUR, that amount is credited, return to the account.

This is not a placeholder in the sense of being unfinished. It is the whole
funding mechanism minus the processor, so every part that is not the processor —
the balance, the funded predicate, the suspension it drives, the console flow, the
audit entry — gets built and exercised now, against real state, while nobody can
lose money to a mistake. `public-edition` owns the integration that replaces the
middle step.

The one property it must keep is that **the credit is a real charge**: the same
row, the same audit entry, the same balance change that a real payment will
produce. A fake that writes different state teaches nothing.

## What this does not solve, and should be read as a decision

- **There is no first-class way to move a project between accounts**, and this is
  a deferral rather than a refusal. Doing it inside the platform would mean
  rewriting the materialized `account_id` on every load balancer, node, domain,
  alias and key of that project, underneath the composite foreign keys of P2 that
  exist precisely to stop such a write disagreeing with its parent.

  What makes deferring it acceptable is that the workaround is mechanical: every
  object here is API-addressable, so re-creating a project in another account is a
  script a customer runs against us, not data they retype. The one thing it does
  NOT carry across is the alias label — a new domain gets a new alias, so the
  customer-side CNAME has to be republished, which is the same one-time step they
  did when they onboarded. Worth building properly later; not MVP.
- **The credit ledger is not here.** Nothing spends the balance yet. This change
  adds the number, the way to increase it, and what happens when it is zero.
  `metering-and-credits` adds what draws it down.
- **No real payment.** No processor, no invoice, no tax, no currency handling
  beyond a label on three buttons.
- **The overdraft of `metering-and-credits` is reversed here**, explicitly. That
  design keeps a lapsed account's existing work alive and refuses only new
  things; this one stops everything at zero. The reasoning is in design C4, and
  it is a reversal rather than an oversight.
