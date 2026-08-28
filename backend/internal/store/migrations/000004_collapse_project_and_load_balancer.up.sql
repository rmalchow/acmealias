-- A project IS the load balancer. The middle level of the inventory goes.
--
-- Before: account -> project -> load_balancer -> (lb_nodes, aliases, domains).
-- After:  account -> project -> (lb_nodes, aliases, domains).
--
-- `projects` survives as the table and takes over what `load_balancers` held.
-- The name survives because "load balancer" names a piece of the CUSTOMER's
-- infrastructure and is the wrong word for a single host, a CDN origin or an
-- appliance; "project" names a unit of ours (design L1).
--
-- THIS IS ALMOST ENTIRELY A DROP, and the reason is worth stating because it is
-- what made the change cheap enough to make now. tenancy-and-permissions P2/P3
-- materialized project_id onto every descendant so the row-level security
-- predicate needed no join, so lb_nodes, aliases and domains ALREADY carry the
-- parent link this migration wants. Nothing is recomputed and nothing is moved:
-- load_balancer_id goes, the foreign keys re-point at projects, and every row
-- keeps the project it already named.
--
-- WHAT A DATA-CARRYING DEPLOYMENT WOULD NEED INSTEAD, recorded because there is
-- none and this migration is therefore written radically: ONE PROJECT PER LOAD
-- BALANCER, taking the load balancer's slug, never two load balancers merged
-- into one project. Merging collides — alias labels and challenge names are
-- unique per load balancer, and two load balancers under one project may
-- legitimately hold the same domain name — so the merge that looks like it
-- preserves the customer's grouping is the one that loses their data (design
-- L2). This file does not do that, because every project here already holds at
-- most the rows it will keep.


-- ---------------------------------------------------------------------------
-- The grant grammar loses an object type
-- ---------------------------------------------------------------------------
--
-- A grant naming a load balancer becomes a grant naming the project that load
-- balancer was in. This runs FIRST, while load_balancers still exists, because
-- the rewrite reads it to find the project.
--
-- THE REWRITE HAS TO LIFT ROW-LEVEL SECURITY, and that is not belt and braces.
-- Both grant tables are FORCE row level security with the predicate
-- `cross_account_read() or account_id = current_account()`. With no session
-- settings the first half is false and the second is NULL, so a bare UPDATE here
-- matches NOTHING, raises no error, and records the migration as applied — and
-- the check constraint below would then abort with a violation whose cause looks
-- like bad data rather than like a scopeless write. set_config's third argument
-- is `true` (is_local), so the lift ends with this block's transaction; it is
-- the same escape store.CrossAccount uses. Migration 000002 made this mistake
-- available to be learned from.
do $$
begin
  perform set_config('app.cross_account', 'on', true);

  update role_grants g
     set object_type = 'project', object_id = lb.project_id
    from load_balancers lb
   where g.object_type = 'load_balancer' and g.object_id = lb.id;

  update api_key_grants g
     set object_type = 'project', object_id = lb.project_id
    from load_balancers lb
   where g.object_type = 'load_balancer' and g.object_id = lb.id;

  -- The wildcard form: "every load balancer in this account" is "every project
  -- in this account" now that there is one object where there were two.
  update role_grants    set object_type = 'project' where object_type = 'load_balancer' and object_id is null;
  update api_key_grants set object_type = 'project' where object_type = 'load_balancer' and object_id is null;

  -- ORPHANS, which the rewrites above cannot reach and which are not a fault.
  --
  -- A grant's object_id is POLYMORPHIC and therefore carries no foreign key —
  -- one column names a project, a domain or a key depending on object_type, and
  -- no single reference can be declared for that. The consequence is that
  -- deleting a load balancer leaves its grants behind, and nothing in the store
  -- removes them (see the note in this migration's change: it is a real gap in
  -- the grant model, not something this migration introduced).
  --
  -- Such a row names an id that resolves to nothing, so it authorises nothing:
  -- the ACL predicate compiles it into a comparison against an object that is
  -- not there. Deleting it is the only honest outcome — it cannot be rewritten,
  -- because there is no project to rewrite it to.
  --
  -- This is why the check constraint below is a check and not an abort in
  -- disguise: everything that CAN be carried across has been by this point, and
  -- everything that cannot is gone, so a violation afterwards means a row nobody
  -- anticipated rather than the ordinary debris of a deletion.
  delete from role_grants    where object_type = 'load_balancer';
  delete from api_key_grants where object_type = 'load_balancer';
end
$$;

-- The check that REFUSES the old word rather than accepting and ignoring it, and
-- it is added BEFORE anything is dropped so that a row the rewrite above could
-- not reach — a grant naming a load balancer that no longer exists — aborts this
-- migration by name instead of surviving as an unsatisfiable grant nothing
-- evaluates (task 3.3).
--
-- This is the shape 000003 used for the selector column: tighten, then drop.
-- Rejected: keeping `load_balancer` as an alias for `project` in the grammar so
-- old grants keep parsing. A grammar with two words for one object type is
-- exactly what 000003 was written to remove, and every stored grant is ours.
alter table role_grants
  add constraint role_grants_object_type_not_load_balancer
  check (object_type <> 'load_balancer');

alter table api_key_grants
  add constraint api_key_grants_object_type_not_load_balancer
  check (object_type <> 'load_balancer');

-- audit_entries is deliberately NOT rewritten. It is append-only — the
-- audit_entries_no_rewrite trigger refuses an UPDATE at statement level — and
-- rewriting it would be the wrong thing even if it were possible: an entry says
-- what happened, and on the day it was written a load balancer is what was
-- created. The vocabulary in internal/authz/vocab.go loses the word for new
-- entries; the old rows keep it, and a filter on object_type=load_balancer
-- returns exactly the history that used it.


-- ---------------------------------------------------------------------------
-- The children re-point at projects
-- ---------------------------------------------------------------------------
--
-- Each child keeps account_id and project_id and loses load_balancer_id. The
-- composite foreign key (project_id, account_id) -> projects (id, account_id)
-- replaces the pair of composite keys that pointed at load_balancers, and it
-- carries the same guarantee for the same reason: a row cannot name a project of
-- one account while claiming another, because the pair it references does not
-- exist.
--
-- One composite key per table rather than a plain project_id reference beside
-- it. The plain one would be implied by the composite and would only add a
-- second index and a second name in the error message.

alter table lb_nodes
  drop constraint lb_nodes_lb_account_fkey,
  drop constraint lb_nodes_lb_project_fkey,
  drop constraint lb_nodes_load_balancer_id_fkey,
  drop constraint lb_nodes_load_balancer_id_label_key,
  drop column load_balancer_id;

-- A node label was unique within its load balancer; it is now unique within its
-- project, which is the same set of rows under a new name.
alter table lb_nodes
  add constraint lb_nodes_project_id_label_key unique (project_id, label),
  add constraint lb_nodes_project_account_fkey
    foreign key (project_id, account_id) references projects (id, account_id) on delete cascade;

alter table aliases
  drop constraint aliases_lb_account_fkey,
  drop constraint aliases_lb_project_fkey,
  drop constraint aliases_load_balancer_id_fkey,
  drop column load_balancer_id;

alter table aliases
  add constraint aliases_project_account_fkey
    foreign key (project_id, account_id) references projects (id, account_id) on delete restrict;

alter table domains
  drop constraint domains_lb_account_fkey,
  drop constraint domains_lb_project_fkey,
  drop constraint domains_load_balancer_id_fkey,
  drop column load_balancer_id;

alter table domains
  add constraint domains_project_account_fkey
    foreign key (project_id, account_id) references projects (id, account_id) on delete restrict;

-- The index on the dropped column went with it; aliases_project_id_idx and
-- domains_project_id_idx were already there, and they are now the parent lookup
-- as well as the tenant one.

-- challenge_interactions holds no foreign keys on purpose — a history that
-- disappears with its subject cannot answer the question it exists for — so this
-- is a column and an index, and project_id is already beside it carrying the
-- same attribution.
drop index challenge_interactions_pair_published_idx;
alter table challenge_interactions drop column load_balancer_id;

-- The reporting snapshot counted three things per project per day; one of them
-- was the level that no longer exists. It is dropped rather than left at zero,
-- because a column reading 0 for every row after a date is a graph that says a
-- customer deleted everything.
alter table project_daily_stats drop column load_balancer_count;

-- Nothing references it any more.
drop table load_balancers;


-- ---------------------------------------------------------------------------
-- Assert the policies, rather than assume them
-- ---------------------------------------------------------------------------
--
-- RLS EMPTIES RATHER THAN FAILS. A policy left naming a dropped column would not
-- error — the DROP COLUMN would have taken the policy with it, and the table
-- would be left enabled, forced and unpoliced, which under FORCE means every
-- read returns nothing and every write is refused with a message about a policy
-- nobody can see. That failure looks like "the account is empty", which is also
-- what a correctly isolated read of somebody else's account looks like.
--
-- So the invariant is asserted here rather than being left to a reviewer's eye:
-- every table that was policied before this migration still has exactly its
-- tenant_isolation policy, and no policy anywhere still mentions the level that
-- is gone. load_balancers is absent from the list because its policy left with
-- the table.
-- NOT pg_policies, and that is the whole reason this is written the long way.
-- That view calls pg_get_expr to render each policy's expression, and
-- pg_get_expr takes an AccessShareLock on the relation the expression belongs
-- to — for EVERY policy in the database, across every schema, before the WHERE
-- clause can save you. In a test run that migrates dozens of schemas at once,
-- this migration then reaches into schemas another migration is holding
-- exclusively and the two deadlock: "deadlock detected ... waits for
-- AccessShareLock", from a statement that reads like a harmless SELECT. The
-- MATERIALIZED CTE below narrows to this schema FIRST, so the only relations
-- locked are the ones this migration already owns.
do $$
declare
  t        text;
  expected text[] := array[
    'projects', 'lb_nodes', 'aliases', 'domains', 'api_keys',
    'roles', 'role_grants', 'api_key_grants', 'account_members', 'challenges'];
  n        int;
begin
  foreach t in array expected loop
    select count(*) into n
      from pg_policy pol
      join pg_class c on c.oid = pol.polrelid
      join pg_namespace ns on ns.oid = c.relnamespace
     where ns.nspname = current_schema() and c.relname = t and pol.polname = 'tenant_isolation';
    if n <> 1 then
      raise exception 'table % has % tenant_isolation policies, expected exactly one', t, n;
    end if;

    select count(*) into n
      from pg_class c join pg_namespace ns on ns.oid = c.relnamespace
     where ns.nspname = current_schema() and c.relname = t
       and c.relrowsecurity and c.relforcerowsecurity;
    if n <> 1 then
      raise exception 'table % is not both enable and FORCE row level security', t;
    end if;
  end loop;

  with mine as materialized (
    select pol.polqual, pol.polwithcheck, pol.polrelid
      from pg_policy pol
      join pg_class c on c.oid = pol.polrelid
      join pg_namespace ns on ns.oid = c.relnamespace
     where ns.nspname = current_schema()
  )
  select count(*) into n
    from mine
   where coalesce(pg_get_expr(polqual, polrelid), '') like '%load_balancer%'
      or coalesce(pg_get_expr(polwithcheck, polrelid), '') like '%load_balancer%';
  if n <> 0 then
    raise exception '% policies still name the load balancer level', n;
  end if;
end
$$;
