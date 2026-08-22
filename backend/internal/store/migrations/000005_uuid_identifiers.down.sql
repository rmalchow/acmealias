-- Reverses 000005 by reading the low 48 bits back out of each identifier.
--
-- This is honest only for rows that were converted by the up migration, whose
-- identifiers were built from the integer they used to have. A row created after
-- the conversion has a real v7 identifier whose low bits are a timestamp and
-- random noise, and reversing it produces a number that was never its key. So
-- this is a rollback for a deployment that has not yet written new rows — which
-- is the situation the whole change is sequenced to run in — and not a general
-- downgrade path.

create or replace function legacy_bigint(u uuid) returns bigint as $$
  select case when u is null then null
    else ('x' || right(replace(u::text, '-', ''), 12))::bit(48)::bigint end;
$$ language sql immutable;

alter table load_balancers        drop constraint load_balancers_project_id_fkey;
alter table lb_nodes              drop constraint lb_nodes_load_balancer_id_fkey;
alter table aliases               drop constraint aliases_load_balancer_id_fkey;
alter table domains               drop constraint domains_load_balancer_id_fkey;
alter table domains               drop constraint domains_alias_id_fkey;
alter table api_keys              drop constraint api_keys_load_balancer_id_fkey;
alter table api_key_networks      drop constraint api_key_networks_api_key_id_fkey;
alter table challenges            drop constraint challenges_alias_id_fkey;

alter table project_daily_stats alter column project_id type bigint using legacy_bigint(project_id);

alter table audit_entries
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column object_id type bigint using legacy_bigint(object_id),
  alter column project_id type bigint using legacy_bigint(project_id);

alter table challenge_interactions
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column project_id type bigint using legacy_bigint(project_id),
  alter column load_balancer_id type bigint using legacy_bigint(load_balancer_id),
  alter column alias_id type bigint using legacy_bigint(alias_id);

alter table challenges
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column alias_id type bigint using legacy_bigint(alias_id);

alter table api_key_networks
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column api_key_id type bigint using legacy_bigint(api_key_id);

alter table api_keys
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column load_balancer_id type bigint using legacy_bigint(load_balancer_id);

alter table domains
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column load_balancer_id type bigint using legacy_bigint(load_balancer_id),
  alter column alias_id type bigint using legacy_bigint(alias_id);

alter table aliases
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column load_balancer_id type bigint using legacy_bigint(load_balancer_id);

alter table lb_nodes
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column load_balancer_id type bigint using legacy_bigint(load_balancer_id);

alter table load_balancers
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id),
  alter column project_id type bigint using legacy_bigint(project_id);

alter table projects
  alter column id drop default,
  alter column id type bigint using legacy_bigint(id);

create sequence projects_id_seq               owned by projects.id;
create sequence load_balancers_id_seq         owned by load_balancers.id;
create sequence lb_nodes_id_seq               owned by lb_nodes.id;
create sequence aliases_id_seq                owned by aliases.id;
create sequence domains_id_seq                owned by domains.id;
create sequence api_keys_id_seq               owned by api_keys.id;
create sequence api_key_networks_id_seq       owned by api_key_networks.id;
create sequence challenges_id_seq             owned by challenges.id;
create sequence challenge_interactions_id_seq owned by challenge_interactions.id;
create sequence audit_entries_id_seq          owned by audit_entries.id;

alter table projects               alter column id set default nextval('projects_id_seq');
alter table load_balancers         alter column id set default nextval('load_balancers_id_seq');
alter table lb_nodes               alter column id set default nextval('lb_nodes_id_seq');
alter table aliases                alter column id set default nextval('aliases_id_seq');
alter table domains                alter column id set default nextval('domains_id_seq');
alter table api_keys               alter column id set default nextval('api_keys_id_seq');
alter table api_key_networks       alter column id set default nextval('api_key_networks_id_seq');
alter table challenges             alter column id set default nextval('challenges_id_seq');
alter table challenge_interactions alter column id set default nextval('challenge_interactions_id_seq');
alter table audit_entries          alter column id set default nextval('audit_entries_id_seq');

select setval('projects_id_seq',               coalesce((select max(id) from projects), 1));
select setval('load_balancers_id_seq',         coalesce((select max(id) from load_balancers), 1));
select setval('lb_nodes_id_seq',               coalesce((select max(id) from lb_nodes), 1));
select setval('aliases_id_seq',                coalesce((select max(id) from aliases), 1));
select setval('domains_id_seq',                coalesce((select max(id) from domains), 1));
select setval('api_keys_id_seq',               coalesce((select max(id) from api_keys), 1));
select setval('api_key_networks_id_seq',       coalesce((select max(id) from api_key_networks), 1));
select setval('challenges_id_seq',             coalesce((select max(id) from challenges), 1));
select setval('challenge_interactions_id_seq', coalesce((select max(id) from challenge_interactions), 1));
select setval('audit_entries_id_seq',          coalesce((select max(id) from audit_entries), 1));

alter table load_balancers   add constraint load_balancers_project_id_fkey   foreign key (project_id)       references projects (id)       on delete restrict;
alter table lb_nodes         add constraint lb_nodes_load_balancer_id_fkey   foreign key (load_balancer_id) references load_balancers (id) on delete cascade;
alter table aliases          add constraint aliases_load_balancer_id_fkey    foreign key (load_balancer_id) references load_balancers (id) on delete restrict;
alter table domains          add constraint domains_load_balancer_id_fkey    foreign key (load_balancer_id) references load_balancers (id) on delete restrict;
alter table domains          add constraint domains_alias_id_fkey            foreign key (alias_id)         references aliases (id)        on delete restrict;
alter table api_keys         add constraint api_keys_load_balancer_id_fkey   foreign key (load_balancer_id) references load_balancers (id) on delete cascade;
alter table api_key_networks add constraint api_key_networks_api_key_id_fkey foreign key (api_key_id)       references api_keys (id)       on delete cascade;
alter table challenges       add constraint challenges_alias_id_fkey         foreign key (alias_id)         references aliases (id)        on delete cascade;

drop function if exists legacy_bigint(uuid);
drop function if exists legacy_uuid(bigint);
drop function if exists uuidv7();
