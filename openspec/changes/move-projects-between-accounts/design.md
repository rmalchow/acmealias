# Design

Decisions are numbered `M1`, `M2`, … and referred to by number from `tasks.md`.

## M1. This is the first ordinary operation that writes across two accounts

Everything else in the product acts inside one account. `store.Scope` carries it, the RLS predicate
compares against it, and `store.CrossAccount` exists as a deliberately greppable escape used by
migrations and the staff plane — places where "no single account" is the honest answer.

A move is different: it has **two** accounts and both are legitimate. That makes it the first caller
of `CrossAccount` that is a customer-facing feature rather than an administrative one, and the review
question for the whole change is whether that escape stays narrow.

**The rule: the escape covers the write, not the decision.** Authority is established in both accounts
first, under ordinary scopes, and only then does a single transaction rewrite the rows. A
`CrossAccount` that also decides who may move what is a hole in the shape of a feature.

## M2. Authority in BOTH accounts, and the refusal has to say which one is missing

Moving a project out of account A into account B requires the right to remove it from A **and** the
right to create it in B.

Neither alone is enough, and the failure modes are not symmetric:

- With only A's authority, somebody could push a project — and its billable domains — into an account
  they do not belong to. That is not a gift; it is an unsolicited liability, since a team account pays
  per challenge.
- With only B's authority, somebody could pull a project out of an account they have no standing in,
  which is theft of configuration.

So the check is a conjunction, and **the refusal must name which side failed.** "You may not do that"
sends somebody to argue with the wrong administrator, and there are two administrators here.

## M3. The alias labels do not change, and this is the whole reason a move is worth building

Every domain in the project already has a published `CNAME` in the customer's own DNS pointing at its
alias label under our zone. Those records are the integration, and they are permanent by design
(`domain-activation` A3: an existing alias is never re-labelled, because its label is published in a
record the service does not control).

A move that re-labelled aliases would require the customer to republish every delegation — which is
the same work as recreating the project by hand, plus a window in which certificates cannot renew.

So the labels are carried across unchanged, and the alias's `account_id` is rewritten under them. This
is only possible because alias labels are globally unique across the deployment rather than unique
per account; if that ever becomes per-account, this change breaks and the note in `resolveLabel` is
where somebody will find out.

## M4. Everything beneath the project moves in one transaction, and RLS is the trap

`lb_nodes`, `aliases`, `domains` and the project's keys all carry a materialized `account_id` —
denormalized so the row-level security predicate needs no join (`tenancy-and-permissions` P2/P3).

A move therefore rewrites that column on every one of them. Two things make this dangerous in a way a
normal update is not:

- **RLS empties rather than fails.** An UPDATE run under the source account's scope will match rows
  while they still belong to it and silently match NOTHING once their `account_id` has changed. A
  half-moved project is a project whose domains belong to one account and whose nodes belong to
  another, with no error anywhere.
- **The order matters and there is no order that is safe.** Whichever table goes first, the rows in
  between are inconsistent. So the whole thing is one transaction, under one escape, and the
  invariant is asserted at the end rather than assumed: **no row beneath the project may name the old
  account.**

## M5. The quota is checked at the destination, and the source does not gain a free slot

A personal account holds five projects. Moving one INTO a personal account is subject to that quota
like any other creation — otherwise the move is a way around it.

Moving one OUT is where the interesting question is, and the answer is that nothing special happens:
the source simply has one fewer project. It is worth stating only because the opposite is tempting to
implement — some notion of the slot being reserved — and it would be a second quota concept for no
reason.

## M6. The audit entry belongs to both accounts

A project leaving an account is a fact about that account, and a project arriving is a fact about the
other. One entry in one account is readable by exactly the people who did not need telling.

Both entries name the same move, the project, and the person, so the two halves can be joined by
somebody reading either side.

## Risks / Trade-offs

- **`CrossAccount` gains a customer-facing caller** (M1). Mitigated by keeping it to the write, and by
  the assertion in M4 — not by trusting the call site.
- **A move is not reversible by symmetry.** Moving back requires authority in both accounts again,
  which the person who moved it may no longer have. This is correct and it will surprise somebody.
- **Billing crosses a boundary.** A domain that was free in a personal account starts costing in a
  team one, from the moment it moves. `metering-and-credits` will need to say what happens to a move
  into an account that cannot pay for it.

## Migration Plan

No schema change. The columns exist; this rewrites them.

1. The store operation, transactional, with the end-state assertion.
2. The two-sided authority check.
3. The audit pair.
4. The console control and destination picker.
5. Delete the sentence's future tense from `upgrade.component.ts`.

Rollback is removing the operation; nothing about the schema changes.

## Open Questions

- **Whether a project can move into a suspended or unfunded account.** It would arrive somewhere that
  cannot answer challenges for it, which reads as the move having broken it.
- **What happens to API keys bound to the project.** `api-key-kinds` gives project keys a real foreign
  key to the project, so they follow it — but their `account_id` moves with them, and whether a key
  minted by one account should keep working in another is a question that change does not answer.
- **Whether the destination's owner must consent.** M2 requires authority in both, which in practice
  means one person holding it on both sides; a stranger cannot push a project at you. Whether a
  colleague should be able to, without asking, is a policy question.
