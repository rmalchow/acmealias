-- Two named kinds of API key, over the grant model that already exists.
--
-- There has been one kind of key, and it is the EXPERT kind: a subject anchored
-- to an account, holding grants. That shape is right and is what let a key reach
-- across projects and be reasoned about by the same predicate compiler that
-- reasons about people. What it left behind is that the overwhelmingly common
-- case — a load balancer publishing challenge values for one project's domains —
-- has to be assembled by hand out of the grammar, and every customer composes
-- the same two grants slightly differently.
--
-- So: a PROJECT key, bound to one project and answering its challenges; and a
-- SYSTEM key, holding a role, which is a member of the account that happens not
-- to be a person.
--
-- THE KINDS ARE A SHAPE, NEVER A SECOND AUTHORIZATION PATH (design K1). Both
-- resolve to grants before the predicate compiler sees them. If either ever
-- needs a check of its own, that is not a feature — it is a second decision
-- point appearing, which is the thing `tenancy-and-permissions` was written to
-- prevent.


-- ---------------------------------------------------------------------------
-- The kind
-- ---------------------------------------------------------------------------
--
-- Constrained rather than free text, for the reason accounts.kind is: a typo in
-- an INSERT should be an error at the write and not a key that matches neither
-- branch of every switch statement downstream.
--
-- The default is 'system' because that is what every existing key already IS —
-- an account-anchored holder of authority — and the backfill below therefore
-- has nothing to do. Converting them into role-holders is a separate decision
-- nobody asked for; their grants are kept exactly as they are.
alter table api_keys
  add column kind text not null default 'system';

alter table api_keys
  add constraint api_keys_kind_check check (kind in ('project', 'system'));


-- ---------------------------------------------------------------------------
-- A project key's project: a REAL foreign key, which a grant cannot have
-- ---------------------------------------------------------------------------
--
-- This is the part that matters more than it looks (design K2).
--
-- role_grants.object_id and api_key_grants.object_id are POLYMORPHIC — one
-- column whose referent is decided by the object_type beside it, across nine
-- tables — and a SQL foreign key names exactly one table, so those columns
-- declare none. The consequence was found the hard way during the
-- project/load-balancer collapse: NOTHING DELETES A GRANT WHEN THE OBJECT IT
-- NAMES IS DELETED, so every deleted project has been leaving grants behind, and
-- the collapse migration aborted on two of them.
--
-- A project key is not polymorphic. It names a project and only ever a project,
-- so it can say so here, and `on delete cascade` then does what nobody has to
-- remember to do.
--
-- This does NOT fix the underlying gap. Grants naming deleted objects are still
-- orphaned; that belongs to the grant model rather than to this change. What it
-- means is that this change adds no new instances of it.
--
-- The reference is composite, (project_id, account_id) -> projects (id,
-- account_id), the same shape lb_nodes uses: it makes "a key's project is in the
-- key's account" a fact the database keeps rather than one the write path
-- remembers.
alter table api_keys
  add column project_id uuid null,
  add constraint api_keys_project_account_fkey
    foreign key (project_id, account_id) references projects (id, account_id)
    on delete cascade;

create index api_keys_project_id_idx on api_keys (project_id);


-- ---------------------------------------------------------------------------
-- A system key's role: NOT cascading
-- ---------------------------------------------------------------------------
--
-- A system key holds a ROLE rather than its own grants (design K6), which buys
-- the whole role machinery: the subset rule, the role editor, the audit
-- vocabulary, and the property that changing a role changes every holder at once
-- — which is what somebody actually wants when they realise the automation has
-- too much authority.
--
-- `on delete restrict`, matching account-invitations V6: deleting a role a key
-- holds is REFUSED. Cascading would silently strip an automation of its
-- authority, and setting null would leave a key whose authority is a mystery.
-- The refusal is also raised in store.DeleteRole with a sentence naming what
-- holds the role, because a foreign key violation is a 500 and not an
-- explanation; this constraint is the floor under that, for the write path
-- somebody adds later and forgets.
alter table api_keys
  add column role_id uuid null,
  add constraint api_keys_role_account_fkey
    foreign key (role_id, account_id) references roles (id, account_id)
    on delete restrict;

create index api_keys_role_id_idx on api_keys (role_id);


-- ---------------------------------------------------------------------------
-- The two shapes, so a row cannot be both
-- ---------------------------------------------------------------------------
--
-- A project key has a project and no role. A system key has no project.
--
-- WHY THE SYSTEM BRANCH DOES NOT ALSO REQUIRE A ROLE, which is a deviation from
-- how task 1.4 is worded and is forced by task 1.5 standing next to it. Every
-- key that exists today becomes a system key keeping its own grants, and none of
-- them names a role. Requiring one here would mean inventing a role for each of
-- them at migration time — precisely the "converting them into roles" that 1.5
-- forbids — or refusing to migrate a working deployment.
--
-- So the null is a LEGACY SHAPE and not a shape anybody can create: the write
-- path requires a role for every new system key, and the console offers no way
-- to make one without. A row with neither project nor role is therefore a key
-- that predates this migration, holding api_key_grants directly, and
-- authz.SubjectFromKey reads it that way on purpose.
alter table api_keys
  add constraint api_keys_shape check (
    (kind = 'project' and project_id is not null and role_id is null)
    or
    (kind = 'system' and project_id is null)
  );


-- ---------------------------------------------------------------------------
-- The address limit: a SWITCH, not a copied list
-- ---------------------------------------------------------------------------
--
-- A key may already carry a hand-typed CIDR allowlist in api_key_networks. For
-- the common case those CIDRs are the addresses of the project's own nodes —
-- which this service already stores, in lb_nodes.ipv4 and lb_nodes.ipv6. So a
-- customer copies numbers we already have into a second place, and the copy is
-- correct until the day they add a node.
--
-- THAT FAILURE IS THE BAD KIND. The new node publishes, is refused for its
-- address, and the symptom is a renewal that does not happen — noticed weeks
-- later as an expired certificate, by which time nobody connects it to the node
-- added last month.
--
-- So this column is a switch and not a list (design K3). On means "only from
-- this project's nodes", and what that means is whatever the project's nodes are
-- AT THE MOMENT A REQUEST ARRIVES. There is nothing to keep in step because
-- nothing was copied.
--
-- Rejected: copying, and re-copying whenever a node changes. That is a cache
-- with an invalidation rule, and the invalidation has to fire on node create,
-- node edit, node delete and project restore. The first missed path is either a
-- customer locked out or a limit that silently is not one, and neither announces
-- itself.
--
-- Only a project key can carry it, because only a project key has a project to
-- compute the union from. Whether a system key should have an equivalent is an
-- open question with no obvious "something" to compute from, which is the answer
-- for now.
alter table api_keys
  add column restrict_to_project_nodes boolean not null default false;

alter table api_keys
  add constraint api_keys_node_limit_needs_a_project check (
    restrict_to_project_nodes = false or kind = 'project'
  );


-- ---------------------------------------------------------------------------
-- Where a refusal is visible
-- ---------------------------------------------------------------------------
--
-- The nodes carry the addresses a customer TYPED. A request carries the address
-- it ARRIVES FROM, which echo derives via ACME_TRUSTED_PROXY_CIDRS. These are
-- the same only when the node speaks to us directly: behind NAT, a CDN or an
-- egress gateway, a node whose ipv4 is a private address arrives from something
-- else entirely, and the switch refuses every request while looking correctly
-- configured (design K5).
--
-- That is not solvable here and must not be papered over. What can be done is
-- make it findable in one attempt instead of by reasoning: the key remembers the
-- address of its most recent address-refusal, and the console shows it beside
-- the switch. The refusal on the wire stays the single opaque 401 design D3
-- requires — a caller must not be able to tell "wrong secret" from "wrong
-- address" — so this is where the answer lives.
--
-- Two columns rather than a table: one address, the most recent, is what answers
-- "why is this not working"; a history of refusals is a different feature nobody
-- has asked for. They are written best-effort on the authentication path, like
-- last_used_at, and losing one costs a display value and nothing else.
alter table api_keys
  add column last_refused_ip inet null,
  add column last_refused_at timestamptz null;
