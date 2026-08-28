## Why

### There is one kind of key, and it is the expert kind

An API key today is a subject that holds grants. `models.ts` records how it got there:

> A key used to belong to one load balancer and its authority WAS that foreign key; it is a subject
> holding grants now, anchored to the account.

That move was right — it is what let a key reach across projects, hold different actions on different
objects, and be reasoned about by the same predicate compiler that reasons about people. The problem
is that it left **only** the expressive shape.

So the overwhelmingly common case — *a load balancer that publishes challenge values for one
project's domains* — is now assembled by hand out of grants. Somebody creating their first key has to
understand the grant grammar to express the one thing almost every key is for, and every one of them
composes the same two grants slightly differently.

### And the network limit goes stale silently

A key can carry a CIDR allowlist. It is typed in by hand.

For the common case those CIDRs are *the addresses of the project's own nodes* — which the service
already stores, in `lb_nodes.ipv4` and `lb_nodes.ipv6`. So a customer copies numbers we already have
into a second place, and the copy is correct until the day they add a node.

**That failure is the bad kind.** The new node publishes, is refused for its address, and the symptom
is a renewal that does not happen — noticed weeks later as an expired certificate, by which time
nobody connects it to the node added last month.

## What Changes

Two named kinds over the machinery that already exists.

### A project key, created on the project's page

Bound to one project. Its authority is that project's challenge answering, and nothing else.

- It carries a **real foreign key** to the project, not a polymorphic grant reference — so deleting
  the project deletes its keys, which the grant model cannot currently manage for anything.
- It may optionally be limited to **the union of that project's node addresses**, evaluated when a
  request arrives rather than copied when the key is made. Add a node and the allowlist follows;
  there is nothing to keep in step.

### A system key, created in a team account's settings

Holds a **role**, and is otherwise a member of the account that happens not to be a person. This is
what today's key already is, expressed through the role machinery instead of raw grants — so the
subset rule, the role editor and the audit vocabulary all apply unchanged.

It exists on **team accounts only**, for the same reason roles do: a personal account has no roster
(`personal-and-team-accounts` C9), and a system key is a role-holder. A project key works in both.

### Where each one lives says what owns it

| | Project key | System key |
| --- | --- | --- |
| Created on | the project's page | the team account's settings |
| Bound to | one project | the account |
| Authority | that project's challenges | whatever its role grants |
| Outlives the project | no | yes |
| Personal accounts | yes | no |

That is not a UI decision. A key made on a project page and deleted with the project is a different
object from one made in account settings that outlives every project — and putting them on the same
screen would be the reason somebody later asks why deleting a project revoked their automation.

## Capabilities

- `identity-and-roles` — a key is of one of two kinds, and a system key holds a role
- `challenge-answering` — a project key answers for its project, optionally only from its nodes
- `admin-console` — keys are made where the thing they belong to lives

## Impact

- **Schema** — a kind, a nullable `project_id` with a real FK, a nullable role reference
- **`internal/apikey`** — the network check gains a computed source
- **The console** — a key panel on the project page, another in team settings
- **No new authorization path.** Both kinds compile to the grants the predicate compiler already
  takes; if either needs its own enforcement, that is the signal the design went wrong
