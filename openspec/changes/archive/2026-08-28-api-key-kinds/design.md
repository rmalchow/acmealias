# Design

Decisions are numbered `K1`, `K2`, … and referred to by number from `tasks.md`.

## K1. Two kinds, one enforcement path

The kinds are a **shape over the grant model**, never a second way of deciding what a caller may do.

Both compile to grants the predicate compiler already takes. A project key resolves to that project's
challenge actions; a system key resolves to its role's grants, which is what a member already does. If
either kind ever needs a check of its own, that is not a feature — it is the signal that a second
authorization path has appeared, which is the thing `tenancy-and-permissions` spent its whole design
avoiding.

**Rejected: making the kinds free-form, so a project key is "any key whose grants happen to name one
project".** That is what exists today, and it is exactly the problem: the shape is not recorded, so
nothing can offer the simple screen, nothing can bound the key to the project's lifetime, and the
network union has no project to compute from. The kind has to be a stored fact.

## K2. The project key gets a REAL foreign key, and that matters more than it looks

```sql
project_id uuid NULL REFERENCES projects (id) ON DELETE CASCADE
```

A grant cannot do this. `role_grants.object_id` and `api_key_grants.object_id` are **polymorphic** —
one column whose referent is decided by the `object_type` beside it, across nine tables — and a SQL
foreign key names exactly one table, so those columns declare none. The consequence was found the
hard way during the project/load-balancer collapse: **nothing deletes a grant when the object it names
is deleted**, so every deleted project has been leaving grants behind, and the collapse migration
aborted on two of them.

A project key is not polymorphic. It names a project and only ever a project, so it can say so in the
schema, and `ON DELETE CASCADE` then does what nobody has to remember to do.

This does **not** fix the underlying gap — grants naming deleted objects are still orphaned, and that
belongs to the grant model rather than here. It does mean this change adds no new instances of it.

## K3. The address limit is COMPUTED, not copied

The union of the project's node addresses is resolved **when a request arrives**, from `lb_nodes`.

Copying it at creation is the obvious implementation and it reintroduces the exact problem the feature
exists to remove. A copied list is correct until a node is added; then the new node publishes, is
refused for its address, and the symptom is a renewal that silently does not happen — surfacing weeks
later as an expired certificate that nobody connects to the node added last month.

So the limit is a **switch**, not a list. On means "only from this project's nodes", and what that
means today is whatever the project's nodes are today.

**Rejected: copying, and re-copying whenever a node changes.** It is a cache with an invalidation
rule, and the invalidation has to fire on node create, node edit, node delete and project restore. The
first missed path is a customer locked out or a limit that silently is not one, and neither announces
itself.

## K4. An empty node set refuses everything, and the screen has to say so

A project with no nodes has an empty union. The switch then permits nothing.

That is the correct reading — "only from my nodes" with no nodes is "from nowhere" — and it is a trap
if it is only correct. Somebody's first act on a new project may well be to create a key before
adding a node, and they would get a refusal that reads as a broken key.

So the empty case is **stated where the switch is**: the console does not offer the switch as an
inert checkbox on a project with no nodes; it says the project has no nodes to allow yet. The refusal,
if one happens anyway, names the reason rather than being the generic address refusal.

**Rejected: treating empty as unrestricted**, the way `addressAllowed` treats an empty CIDR list
today. That rule is right for a hand-typed list — no entries means the customer did not ask for a
limit — and exactly wrong here, where the switch IS the customer asking for one. Same emptiness, two
meanings, so the two must not share a code path.

## K5. The address we compare is the one we can see, not the one they told us

The nodes carry the addresses a customer typed. A request carries the address it arrives from, which
`c.RealIP()` derives via `ACME_TRUSTED_PROXY_CIDRS`.

These are the same only when the node speaks to us directly. Behind NAT, a CDN, or an egress gateway,
a node whose `ipv4` is a private address arrives from something else entirely — and the switch would
refuse every request while looking correctly configured.

This is not solvable here and should not be papered over. The switch is **opt-in**, its description
says it compares the address the request arrives from, and the console shows what that address
actually was on a refusal — so somebody behind NAT finds out in one attempt rather than by reasoning
about it.

## K6. A system key holds a role, not grants

The role machinery already exists: roles are data, the subset rule bounds who may grant what, and the
role editor is built. A system key that holds a role inherits all of it, including the property that
changing a role changes every holder at once — which is what somebody actually wants when they realise
the automation has too much authority.

**Rejected: letting a system key carry its own grants directly.** It is what happens today, and it
means a fleet of keys whose authority can only be audited one key at a time, and adjusted one key at a
time. A role is the noun that already means "this set of authority, named, and shared".

**A deleted role fails the key closed**, matching `account-invitations` V6: the reference is not
`ON DELETE CASCADE`, deleting a role with a key holding it is refused, and a key whose role somehow
vanished authorises nothing rather than falling back to something.

## K7. System keys are a team-account thing, for the reason roles are

A personal account has no roles and no roster — `store.ErrPersonalAccountHasNoRoster`, design C9 — and
a system key is a role-holder. So there is nothing for one to hold.

The refusal is C9's refusal and not a permission error: *there cannot be one here*, which is a
different sentence with a different remedy from *you may not*. In an account of one there is nobody to
ask for permission, which is exactly the confusion C11 warns about.

**Project keys work in both kinds**, because they need no role. That is what keeps a personal account
a working product rather than a demo: the thing keys are mostly for still works.

## K8. Where a key is made is where it belongs

Project keys on the **project page**; system keys on the **team account's settings page**.

This is not a placement decision, it is the lifetime made visible. A project key dies with its project
(K2); a system key outlives every project in the account. Putting both on one screen would leave
somebody asking, later and angrily, why deleting a project revoked their automation — and the answer
would be a rule they had no way to see.

Settings is also where roles and members already are, and a system key is a member that is not a
person. It belongs beside the roster, not beside the inventory.

## Risks / Trade-offs

- **Two kinds is more concept than one.** Accepted: the alternative is one kind that everybody has to
  understand fully in order to use partially, which is the current state and the reason for the
  change.
- **The computed union costs a lookup per request** (K3). The nodes of one project are a handful of
  rows and the key is already being loaded; if it ever matters, it is cacheable with a real
  invalidation story, which K3 declined to write speculatively.
- **The NAT case will generate support questions** (K5). Mitigated by making the switch opt-in and the
  refusal legible, not by pretending the addresses agree.

## Migration Plan

1. Add the kind, the `project_id` foreign key and the role reference.
2. Existing keys become **system keys**, since that is what they are: account-anchored holders of
   authority. Their grants are kept as-is; converting them into roles is a separate decision nobody
   asked for.
3. Add the computed address source beside the stored one, on the switch.
4. Console: the project page's panel, then the settings panel.

Rollback is dropping the columns; every key remains a subject holding grants, which is what they all
were before.

## Open Questions

- **Whether a project key should be able to do anything but answer challenges** — read its project's
  domains, say, so a client can discover what to publish for. Attractive, and it starts the slide back
  toward composing grants.
- **Whether the address switch should apply to system keys too**, computed from something. There is no
  obvious "something", which is the answer for now.
- **What existing keys' grants become** if roles are ever made mandatory for system keys. Not now:
  step 2 keeps them working.
