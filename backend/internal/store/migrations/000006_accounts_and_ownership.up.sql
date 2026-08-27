-- The account is the owner of inventory and the anchor of every grant (design P1,
-- P2, P3).
--
-- Two things happen here and they are not the same thing. The account becomes a
-- real row above projects; and every table below it carries the identifier of
-- every ancestor a grant is allowed to name, held true by a composite foreign key
-- rather than by a trigger or by discipline.
--
-- The denormalization is the part worth explaining. An earlier draft rejected
-- putting account_id on every row because a row could then disagree with its
-- parent. That is the right worry and the wrong remedy: a composite foreign key
-- makes the disagreement unrepresentable, because the child references the
-- (id, account_id) pair of its parent and cannot name a pair that does not exist.
-- The column is a denormalization whose correctness is structural.
--
-- Without it, two things this change needs are impossible. Per-account domain
-- uniqueness needs the account on the row, because Postgres cannot build a unique
-- index across a join. And every scoped read would join up to projects, so no
-- index could lead with the tenant.

create table accounts (
  id         uuid primary key default uuidv7(),
  slug       text not null unique,
  name       text not null,
  created_at timestamptz not null default now()
);

-- The account every pre-account row is backfilled into.
--
-- This was an unconditional seed: the internal edition had exactly one account,
-- created here so that no code path had to cope with its absence. Design C10
-- removed that edition, and with it store.ScopeForBootstrap — the producer that
-- resolved "the one account" — so the row is no longer a mechanism. It is only a
-- destination for the backfills below.
--
-- It is therefore seeded only when there is something to backfill. A deployment
-- already past this migration is unaffected: it ran the unconditional form and
-- has the row. A FRESH chain now starts with no accounts at all, which is what a
-- deployment where every account is created by a person actually looks like —
-- rather than with a stray 'internal' account that nobody owns, holds nothing,
-- and (once credits arrive) would not even be usable.
insert into accounts (slug, name)
select 'internal', 'Internal'
 where exists (select 1 from projects) or exists (select 1 from audit_entries);

alter table projects add column account_id uuid;
update projects set account_id = (select id from accounts where slug = 'internal');
alter table projects alter column account_id set not null;
alter table projects add constraint projects_account_id_fkey
  foreign key (account_id) references accounts (id) on delete restrict;
alter table projects add constraint projects_id_account_id_key unique (id, account_id);

alter table load_balancers add column account_id uuid;
update load_balancers lb set account_id = p.account_id from projects p where p.id = lb.project_id;
alter table load_balancers alter column account_id set not null;
alter table load_balancers add constraint load_balancers_project_account_fkey
  foreign key (project_id, account_id) references projects (id, account_id) on delete restrict;
alter table load_balancers add constraint load_balancers_id_account_id_key unique (id, account_id);
alter table load_balancers add constraint load_balancers_id_project_id_key unique (id, project_id);

-- lb_nodes, aliases, domains and api_keys are leaves: they carry account_id and
-- project_id both, because a grant may be anchored at either and a listing has to
-- reduce to a single-column predicate rather than a subquery (design P3).
alter table lb_nodes add column account_id uuid;
alter table lb_nodes add column project_id uuid;
update lb_nodes n set account_id = lb.account_id, project_id = lb.project_id
  from load_balancers lb where lb.id = n.load_balancer_id;
alter table lb_nodes alter column account_id set not null;
alter table lb_nodes alter column project_id set not null;
alter table lb_nodes add constraint lb_nodes_lb_account_fkey
  foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete cascade;
alter table lb_nodes add constraint lb_nodes_lb_project_fkey
  foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete cascade;

alter table aliases add column account_id uuid;
alter table aliases add column project_id uuid;
update aliases a set account_id = lb.account_id, project_id = lb.project_id
  from load_balancers lb where lb.id = a.load_balancer_id;
alter table aliases alter column account_id set not null;
alter table aliases alter column project_id set not null;
alter table aliases add constraint aliases_lb_account_fkey
  foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete restrict;
alter table aliases add constraint aliases_lb_project_fkey
  foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete restrict;
alter table aliases add constraint aliases_id_account_id_key unique (id, account_id);

alter table domains add column account_id uuid;
alter table domains add column project_id uuid;
update domains d set account_id = lb.account_id, project_id = lb.project_id
  from load_balancers lb where lb.id = d.load_balancer_id;
alter table domains alter column account_id set not null;
alter table domains alter column project_id set not null;
alter table domains add constraint domains_lb_account_fkey
  foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete restrict;
alter table domains add constraint domains_lb_project_fkey
  foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete restrict;
alter table domains add constraint domains_alias_account_fkey
  foreign key (alias_id, account_id) references aliases (id, account_id) on delete restrict;

alter table api_keys add column account_id uuid;
alter table api_keys add column project_id uuid;
update api_keys k set account_id = lb.account_id, project_id = lb.project_id
  from load_balancers lb where lb.id = k.load_balancer_id;
alter table api_keys alter column account_id set not null;
alter table api_keys alter column project_id set not null;
alter table api_keys add constraint api_keys_lb_account_fkey
  foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete cascade;
alter table api_keys add constraint api_keys_lb_project_fkey
  foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete cascade;

-- A domain name is unique within an account, not globally (design P8). Two
-- customers may both hold example.com in their inventory; DNS delegation, not this
-- table, arbitrates which of them can actually validate it. A global unique index
-- would let the first account to add a name deny it to every other account, which
-- is a denial of service with a database constraint as the weapon.
alter table domains drop constraint domains_name_key;
alter table domains add constraint domains_account_id_name_key unique (account_id, name);

-- Tenant-leading indexes: every scoped read filters on account_id first.
create index load_balancers_account_id_idx on load_balancers (account_id);
create index lb_nodes_account_id_idx       on lb_nodes (account_id);
create index lb_nodes_project_id_idx       on lb_nodes (project_id);
create index aliases_account_id_idx        on aliases (account_id);
create index aliases_project_id_idx        on aliases (project_id);
create index domains_account_id_idx        on domains (account_id);
create index domains_project_id_idx        on domains (project_id);
create index api_keys_account_id_idx       on api_keys (account_id);
create index api_keys_project_id_idx       on api_keys (project_id);
create index projects_account_id_idx       on projects (account_id);

-- The audit trail records which account an entry belongs to, so that a tenant's
-- reading of their own trail is confined like every other read.
--
-- 000003 makes this table append-only with a statement-level trigger, which means
-- backfilling a new column is refused like any other UPDATE. The trigger is
-- disabled for the length of this statement and re-enabled immediately, which is
-- the one thing a schema migration may legitimately do and application code may
-- not: the trigger exists so that nobody rewrites history through the service,
-- not so that the table can never gain a column. The re-enable is not conditional
-- and not deferred — if it were skipped the trail would silently stop being
-- append-only, which is worse than the migration failing.
-- Two things stand in the way of backfilling this column, and both are the
-- append-only protection working as designed:
--
--   1. The trigger from 000003 refuses UPDATE outright.
--   2. 000003 also REVOKEs update from the connecting role. Its own comment
--      says that revoke is "a statement of intent rather than a lock until
--      ownership is split", on the grounds that a table owner keeps every
--      privilege no matter what is revoked. That is not so — a revoke from the
--      owner does take effect — and the reason nobody noticed is that the
--      service used to connect as a SUPERUSER, which bypasses privilege checks
--      entirely. It no longer does (store.CheckTenantIsolation refuses to
--      start as one), so the lock is real now and this migration is the first
--      thing to meet it.
--
-- Both are lifted for exactly these statements and restored immediately after.
-- A schema migration may legitimately do this and application code may not: the
-- protection exists so nobody rewrites history THROUGH THE SERVICE, not so the
-- table can never gain a column. Neither restore is conditional or deferred —
-- skipping one would leave the trail quietly writable, which is worse than the
-- migration failing.
alter table audit_entries add column account_id uuid;

do $$
begin
  execute format('grant update on audit_entries to %I', current_user);
end
$$;
alter table audit_entries disable trigger audit_entries_no_rewrite;

update audit_entries set account_id = (select id from accounts where slug = 'internal');

alter table audit_entries enable trigger audit_entries_no_rewrite;
do $$
begin
  execute format('revoke update on audit_entries from %I', current_user);
end
$$;
create index audit_entries_account_occurred_idx on audit_entries (account_id, occurred_at desc);
