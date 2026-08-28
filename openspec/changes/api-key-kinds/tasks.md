# Tasks

> Both kinds are a SHAPE over the grant model, never a second way of deciding what a caller may do
> (design K1). If either grows a check of its own, that is not a feature — it is a second
> authorization path appearing, which is the thing `tenancy-and-permissions` was written to prevent.

## 1. Schema

- [x] 1.1 Add the kind to `api_keys`, as a constrained value rather than a free string
- [x] 1.2 Add `project_id uuid NULL REFERENCES projects (id) ON DELETE CASCADE` — a REAL foreign key, which a grant cannot have (design K2)
- [x] 1.3 Add the role reference for system keys, NOT cascading: deleting a role a key holds is refused, matching `account-invitations` V6
- [x] 1.4 Constrain the two shapes so a row cannot be both or neither: a project key has a project and no role, a system key the reverse
  - LOOSER THAN WORDED, and forced by 1.5 standing next to it. The constraint is `(kind='project' and project_id is not null and role_id is null) or (kind='system' and project_id is null)`: the system branch does NOT also require a role. Every key that exists becomes a system key keeping its own grants and none names a role, so requiring one here would mean inventing a role for each at migration time — the conversion 1.5 forbids — or refusing to migrate a working deployment. The null is a legacy SHAPE and not one anybody can create: `POST /accounts/{id}/keys` requires `roleId`, and the console offers no way without. Written out in the migration and in `model.APIKey`.
- [x] 1.5 Migrate existing keys to SYSTEM keys, keeping their grants exactly as they are. That is what they already are — account-anchored holders of authority — and converting them into roles is a decision nobody asked for (design, migration plan)
- [x] 1.6 Regenerate `internal/store/schema.sql` and confirm `model_drift_test` passes
- [x] 1.7 Test that deleting a project deletes its keys, and that deleting a role held by a system key is refused

## 2. Authorization, which does not change

- [x] 2.1 Resolve a project key to its project's challenge grants, and a system key to its role's grants, before the predicate compiler sees them
- [x] 2.2 Add NO new decision point. Both kinds arrive at `CompilePredicate` as a subject with grants, the way a member does (design K1)
- [x] 2.3 Assert a project key is refused on another project — and that it is refused by the ordinary predicate rather than by a special case

## 3. The address limit

- [x] 3.1 Add the limit as a SWITCH on a project key, not a copied list (design K3)
- [x] 3.2 Resolve the union from `lb_nodes` WHEN A REQUEST ARRIVES. Copying at creation reintroduces the exact staleness the feature removes: the copy is right until a node is added, and then the symptom is a renewal that silently does not happen, surfacing weeks later as an expired certificate
- [x] 3.3 Do NOT share a code path with `addressAllowed`'s empty-list rule. There, no entries means the customer never asked for a limit and everything is allowed; here, the switch IS the customer asking, so no addresses must allow NOTHING. Same emptiness, opposite meanings (design K4)
- [x] 3.4 Compare against `c.RealIP()`, and say in the setting's description that it is the address the request ARRIVES FROM. Behind NAT or a gateway that is not the node's configured address, and the switch would refuse everything while looking correct (design K5)
- [x] 3.5 Name the address actually seen in the refusal, so somebody behind NAT finds out in one attempt instead of by reasoning
  - The refusal ON THE WIRE is unchanged, and deliberately: design D3 requires a caller to be unable to tell a wrong secret from a wrong address, so naming the address there would open the distinction the opacity exists to close. Design K5 says the CONSOLE shows it, so `api_keys.last_refused_ip` / `last_refused_at` are written best-effort on the authentication path (like `last_used_at`) and both key panels render "Last refused from X on DATE — that is the address the request arrived from, which is not always the one on the node."
- [x] 3.6 Test the three transitions: a node added is permitted with no change to the key, a node removed is refused, an unknown address is refused

## 4. Console: the project's keys

- [x] 4.1 Add the key panel to the project screen (design K8)
  - The panel already existed on the project screen. What changed is what it LISTS: project keys bound to this project, rather than "every key whose grants happen to reach it". Its Authority column is gone — every row now has the same authority, and a column repeating one sentence stops being read.
- [x] 4.2 Offer the address limit, and where the project has NO nodes say so instead of rendering a control that would refuse everything (design K4)
- [x] 4.3 Keep the show-once rule for the secret, as the existing key screen does

## 5. Console: the account's system keys

- [x] 5.1 Add the system key panel to a team account's SETTINGS page, beside roles and members — a system key is a member that is not a person (design K8)
- [x] 5.2 Pick a role at creation, from the roles that already exist
- [x] 5.3 Refuse in a personal account with C9's sentence — there CANNOT be one here — not a permission error. In an account of one there is nobody to ask for permission (design K7, C11)
- [x] 5.4 Do not put the two kinds on one screen. A project key dies with its project and a system key outlives every project; one screen makes that invisible, and somebody finds out by having their automation revoked (design K8)

## 6. What this does NOT fix

- [x] 6.1 Record, where the grant model is documented, that grants naming a deleted object are still orphaned. K2 gives PROJECT KEYS a real foreign key so this change adds no new instances, and the underlying gap belongs to `tenancy-and-permissions`
  - Recorded in `internal/authz/doc.go`, which is where the model is described, including what the gap does and does not cost: an orphan names an identifier nothing matches and identifiers are not reused, so it is clutter and a confusing role editor rather than an authority hole.
- [x] 6.2 Do not sweep orphaned grants here. It is a separate decision about a separate table and it would arrive buried in a change about keys

## 7. Documentation

- [x] 7.1 Say which kind to use, in one sentence each, where somebody creating their first key will read it
- [x] 7.2 Update `acme-dns-protocol`'s credentials-file task to hand out a PROJECT key: it is the key that flow wants, and it is the whole reason this change sits before it

## What this change did not anticipate

- [x] `CheckTrustedProxies` was already blind, and this change would have made that worse. The
  startup guard that refuses to run a source-restricted key without `ACME_TRUSTED_PROXY_CIDRS`
  counted rows through `api_keys`, which FORCES row-level security — so with no session settings
  the policy was false for every row and the count was always zero. It passed silently while
  displaying a protection that was not there, which is the exact failure it was written to
  prevent. It now runs under `store.CrossAccount` (this is a question about the whole deployment)
  and counts the new switch as well as `api_key_networks`, because a project key limited to its
  project's nodes is judged on exactly the same forgeable address.
- [x] `store.CreateAPIKey` needed a second parameter. The subset rule is a REQUIRED argument
  precisely so a new write path cannot forget it, and a project key's authority is synthesized
  rather than stored — so it would have walked straight past the check that exists to be
  unforgettable. It now takes the implied grants explicitly, and a test asserts that what the
  subset rule checks equals what `authz.SubjectFromKey` resolves to.
- [x] Listing system keys is refused in a personal account, not answered empty. Task 5.3 only asked
  this of creation; the roster's reasoning applies unchanged to the listing, and an empty list
  reads as "there are none yet" and invites an Add button for something that can never exist there.
- [x] `--project` and `--grant` no longer add up in the CLI. They used to, on the reasoning that
  authority is a union; what changed is that a project key has a LIFETIME as well as an authority,
  and a key that dies with one project while carrying grants over something else is a lifetime
  nobody could reason about. The combination is refused with a sentence saying which flag to use.
