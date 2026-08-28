# Tasks

> Both kinds are a SHAPE over the grant model, never a second way of deciding what a caller may do
> (design K1). If either grows a check of its own, that is not a feature — it is a second
> authorization path appearing, which is the thing `tenancy-and-permissions` was written to prevent.

## 1. Schema

- [ ] 1.1 Add the kind to `api_keys`, as a constrained value rather than a free string
- [ ] 1.2 Add `project_id uuid NULL REFERENCES projects (id) ON DELETE CASCADE` — a REAL foreign key, which a grant cannot have (design K2)
- [ ] 1.3 Add the role reference for system keys, NOT cascading: deleting a role a key holds is refused, matching `account-invitations` V6
- [ ] 1.4 Constrain the two shapes so a row cannot be both or neither: a project key has a project and no role, a system key the reverse
- [ ] 1.5 Migrate existing keys to SYSTEM keys, keeping their grants exactly as they are. That is what they already are — account-anchored holders of authority — and converting them into roles is a decision nobody asked for (design, migration plan)
- [ ] 1.6 Regenerate `internal/store/schema.sql` and confirm `model_drift_test` passes
- [ ] 1.7 Test that deleting a project deletes its keys, and that deleting a role held by a system key is refused

## 2. Authorization, which does not change

- [ ] 2.1 Resolve a project key to its project's challenge grants, and a system key to its role's grants, before the predicate compiler sees them
- [ ] 2.2 Add NO new decision point. Both kinds arrive at `CompilePredicate` as a subject with grants, the way a member does (design K1)
- [ ] 2.3 Assert a project key is refused on another project — and that it is refused by the ordinary predicate rather than by a special case

## 3. The address limit

- [ ] 3.1 Add the limit as a SWITCH on a project key, not a copied list (design K3)
- [ ] 3.2 Resolve the union from `lb_nodes` WHEN A REQUEST ARRIVES. Copying at creation reintroduces the exact staleness the feature removes: the copy is right until a node is added, and then the symptom is a renewal that silently does not happen, surfacing weeks later as an expired certificate
- [ ] 3.3 Do NOT share a code path with `addressAllowed`'s empty-list rule. There, no entries means the customer never asked for a limit and everything is allowed; here, the switch IS the customer asking, so no addresses must allow NOTHING. Same emptiness, opposite meanings (design K4)
- [ ] 3.4 Compare against `c.RealIP()`, and say in the setting's description that it is the address the request ARRIVES FROM. Behind NAT or a gateway that is not the node's configured address, and the switch would refuse everything while looking correct (design K5)
- [ ] 3.5 Name the address actually seen in the refusal, so somebody behind NAT finds out in one attempt instead of by reasoning
- [ ] 3.6 Test the three transitions: a node added is permitted with no change to the key, a node removed is refused, an unknown address is refused

## 4. Console: the project's keys

- [ ] 4.1 Add the key panel to the project screen (design K8)
- [ ] 4.2 Offer the address limit, and where the project has NO nodes say so instead of rendering a control that would refuse everything (design K4)
- [ ] 4.3 Keep the show-once rule for the secret, as the existing key screen does

## 5. Console: the account's system keys

- [ ] 5.1 Add the system key panel to a team account's SETTINGS page, beside roles and members — a system key is a member that is not a person (design K8)
- [ ] 5.2 Pick a role at creation, from the roles that already exist
- [ ] 5.3 Refuse in a personal account with C9's sentence — there CANNOT be one here — not a permission error. In an account of one there is nobody to ask for permission (design K7, C11)
- [ ] 5.4 Do not put the two kinds on one screen. A project key dies with its project and a system key outlives every project; one screen makes that invisible, and somebody finds out by having their automation revoked (design K8)

## 6. What this does NOT fix

- [ ] 6.1 Record, where the grant model is documented, that grants naming a deleted object are still orphaned. K2 gives PROJECT KEYS a real foreign key so this change adds no new instances, and the underlying gap belongs to `tenancy-and-permissions`
- [ ] 6.2 Do not sweep orphaned grants here. It is a separate decision about a separate table and it would arrive buried in a change about keys

## 7. Documentation

- [ ] 7.1 Say which kind to use, in one sentence each, where somebody creating their first key will read it
- [ ] 7.2 Update `acme-dns-protocol`'s credentials-file task to hand out a PROJECT key: it is the key that flow wants, and it is the whole reason this change sits before it
