## Why

### The console now promises this, in the future tense

`upgrade.component.ts` — the screen somebody reads immediately before spending money — says:

> Projects are not permanently tied to the account they were made in either — moving them between
> accounts is not built yet, and when it is, nothing you create now will be stranded.

That sentence is there because it answers the question people actually hesitate on before buying: *if
I start in my personal account, is that work stranded?* The honest answer today is "no in principle,
not yet in practice", and this change is what turns it into "no".

**Until it exists, that screen carries a promise with a date attached to nothing.** That is the
reason to write the change now rather than when somebody asks for it.

### And the five-project quota makes it inevitable

A personal account holds five projects. Somebody who reaches that limit has exactly the profile of a
customer about to pay — and what they will want is for the work they already did to come with them.
Making them recreate it is asking them to do an hour of manual work as the first act of becoming a
paying customer, which is the worst possible moment to ask for anything.

`personal-and-team-accounts` recorded this as out of scope with the note that it "can be done purely
outside the platform via api". That was true and it is not a feature: it means the answer to a paying
customer is a script.

## What Changes

A project moves from one account to another, with everything under it.

- The mover must be able to act in **both** accounts — this is not one permission but two, on
  opposite sides of a boundary the whole permission model exists to hold.
- Every row beneath the project follows: nodes, domains, aliases, keys. All of them carry a
  materialized `account_id` for the row-level security predicate, so **the move is a rewrite of that
  column across several tables and is exactly the kind of operation RLS makes silently wrong if it
  is done without care.**
- The **alias labels do not change**, and therefore neither do the customer's `CNAME` records. A move
  that asked the customer to republish their delegations would not be a move; it would be a migration
  they perform.
- Challenge answering does not stop. The zone view is keyed by alias, not by account.

## Capabilities

- `inventory` — a project can change account, carrying its contents
- `identity-and-roles` — moving requires authority in both accounts
- `audit-trail` — the move is recorded in both accounts, not one

## Impact

- **`internal/store`** — the first operation that legitimately writes across two accounts, which
  makes it the first real user of `store.CrossAccount` outside migrations and staff reads
- **The console** — a control on the project, and a destination picker
- **No schema change** expected: the columns already exist, and this rewrites them
