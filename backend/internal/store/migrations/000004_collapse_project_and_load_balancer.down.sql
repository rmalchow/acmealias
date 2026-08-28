-- Reverses 000004: the middle level comes back.
--
-- It is reversible in SHAPE and it is reversible in ROWS, which is more than a
-- radical migration usually manages, and the trick is that the collapse merged
-- nothing. Every child kept the project it already named, so putting the level
-- back means giving each project exactly one load balancer and pointing its
-- children at that. The load balancer TAKES THE PROJECT'S OWN IDENTIFIER, so
-- load_balancer_id = project_id on every row and no lookup table is needed —
-- which is also the honest reading of what the up migration did to those rows.
--
-- The one thing that does not come back is the count of load balancers per
-- project on days already recorded in project_daily_stats. Those numbers were
-- deleted, and a rollback cannot invent them; the restored column is zero for
-- history and correct from the next snapshot.
--
-- Grants are NOT rewritten back to naming load balancers. A grant on a project
-- is a legal grant before and after this change, and it means the same thing —
-- the whole project — so turning it back into a load-balancer grant would be a
-- rollback that CHANGES what somebody may do rather than restoring it.

alter table role_grants drop constraint role_grants_object_type_not_load_balancer;
alter table api_key_grants drop constraint api_key_grants_object_type_not_load_balancer;

create table load_balancers (
  id          uuid primary key default uuidv7(),
  account_id  uuid not null,
  project_id  uuid not null references projects (id) on delete restrict,
  slug        text not null,
  name        text not null,
  created_at  timestamptz not null default now(),
  unique (project_id, slug),
  unique (id, account_id),
  unique (id, project_id),
  constraint load_balancers_project_account_fkey
    foreign key (project_id, account_id) references projects (id, account_id) on delete restrict
);

create index load_balancers_account_id_idx on load_balancers (account_id);

alter table load_balancers enable row level security;
alter table load_balancers force row level security;

create policy tenant_isolation on load_balancers
  using (cross_account_read() or account_id = current_account())
  with check (cross_account_read() or account_id = current_account());

alter table lb_nodes add column load_balancer_id uuid;
alter table aliases  add column load_balancer_id uuid;
alter table domains  add column load_balancer_id uuid;
alter table challenge_interactions add column load_balancer_id uuid;

-- One load balancer per project, then the children pointed back at it. This
-- block lifts row-level security for the same reason the up migration's rewrite
-- does: every table it touches is FORCE row level security with a predicate that
-- is NULL when no account is selected, so a bare INSERT or UPDATE here would
-- write nothing, raise nothing, and leave the SET NOT NULL below to fail with a
-- message about null values that says nothing about why they are null.
do $$
begin
  perform set_config('app.cross_account', 'on', true);

  insert into load_balancers (id, account_id, project_id, slug, name, created_at)
    select id, account_id, id, slug, name, created_at from projects;

  update lb_nodes set load_balancer_id = project_id;
  update aliases  set load_balancer_id = project_id;
  update domains  set load_balancer_id = project_id;
  update challenge_interactions set load_balancer_id = project_id;
end
$$;

alter table lb_nodes
  alter column load_balancer_id set not null,
  drop constraint lb_nodes_project_id_label_key,
  drop constraint lb_nodes_project_account_fkey,
  add constraint lb_nodes_load_balancer_id_fkey
    foreign key (load_balancer_id) references load_balancers (id) on delete cascade,
  add constraint lb_nodes_load_balancer_id_label_key unique (load_balancer_id, label),
  add constraint lb_nodes_lb_account_fkey
    foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete cascade,
  add constraint lb_nodes_lb_project_fkey
    foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete cascade;

alter table aliases
  alter column load_balancer_id set not null,
  drop constraint aliases_project_account_fkey,
  add constraint aliases_load_balancer_id_fkey
    foreign key (load_balancer_id) references load_balancers (id) on delete restrict,
  add constraint aliases_lb_account_fkey
    foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete restrict,
  add constraint aliases_lb_project_fkey
    foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete restrict;

create index aliases_load_balancer_id_idx on aliases (load_balancer_id);

alter table domains
  alter column load_balancer_id set not null,
  drop constraint domains_project_account_fkey,
  add constraint domains_load_balancer_id_fkey
    foreign key (load_balancer_id) references load_balancers (id) on delete restrict,
  add constraint domains_lb_account_fkey
    foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete restrict,
  add constraint domains_lb_project_fkey
    foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete restrict;

create index challenge_interactions_pair_published_idx
  on challenge_interactions (load_balancer_id, published_at desc);

-- Zero for history, and the comment above says why. `not null default 0` then
-- dropping the default reproduces the baseline's `not null` with no default,
-- which is what the golden file records.
alter table project_daily_stats add column load_balancer_count int not null default 0;
alter table project_daily_stats alter column load_balancer_count drop default;
