-- Every identifier becomes a UUIDv7 (design P17).
--
-- Two reasons, neither of them fashion. A sequence discloses how many of a thing
-- exist and lets one tenant's object be guessed from another's, which stops being
-- a curiosity the moment accounts are real. And an identifier that is unique
-- across deployments is what lets a satellite, a report and a centre talk about
-- the same object without a translation table.
--
-- v7 rather than v4 because the first 48 bits are a millisecond timestamp, so
-- rows sort roughly by creation and B-tree inserts stay at the right-hand edge
-- instead of scattering across the index.

-- PostgreSQL 18 has uuidv7() built in. On 16 and 17 it does not exist, so this
-- defines one: 48-bit millisecond timestamp, version 7, variant 10, the rest
-- random. Callers in Go use github.com/google/uuid; this exists so that column
-- defaults, backfills and hand-written SQL do not have to.
create or replace function uuidv7() returns uuid as $$
  select encode(
    set_bit(
      set_bit(
        overlay(uuid_send(gen_random_uuid()) placing
          substring(int8send((extract(epoch from clock_timestamp()) * 1000)::bigint) from 3)
          from 1 for 6),
        52, 1),
      53, 1),
    'hex')::uuid;
$$ language sql volatile;

-- Existing rows keep their identity rather than being renumbered: the integer is
-- widened into the low 48 bits of a fixed prefix, so every foreign key converts
-- to exactly the value its parent converts to and referential integrity survives
-- the conversion without a mapping table. New rows get real v7 values.
create or replace function legacy_uuid(n bigint) returns uuid as $$
  select case when n is null then null
    else ('00000000-0000-7000-8000-' || lpad(to_hex(n), 12, '0'))::uuid end;
$$ language sql immutable;

alter table load_balancers        drop constraint load_balancers_project_id_fkey;
alter table lb_nodes              drop constraint lb_nodes_load_balancer_id_fkey;
alter table aliases               drop constraint aliases_load_balancer_id_fkey;
alter table domains               drop constraint domains_load_balancer_id_fkey;
alter table domains               drop constraint domains_alias_id_fkey;
alter table api_keys              drop constraint api_keys_load_balancer_id_fkey;
alter table api_key_networks      drop constraint api_key_networks_api_key_id_fkey;
alter table challenges            drop constraint challenges_alias_id_fkey;

alter table projects
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7();

alter table load_balancers
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column project_id type uuid using legacy_uuid(project_id);

alter table lb_nodes
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column load_balancer_id type uuid using legacy_uuid(load_balancer_id);

alter table aliases
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column load_balancer_id type uuid using legacy_uuid(load_balancer_id);

alter table domains
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column load_balancer_id type uuid using legacy_uuid(load_balancer_id),
  alter column alias_id type uuid using legacy_uuid(alias_id);

alter table api_keys
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column load_balancer_id type uuid using legacy_uuid(load_balancer_id);

alter table api_key_networks
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column api_key_id type uuid using legacy_uuid(api_key_id);

alter table challenges
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column alias_id type uuid using legacy_uuid(alias_id);

-- challenge_interactions carries identifiers without foreign keys on purpose
-- (000002): a history that vanishes when its subject is deleted cannot answer the
-- question it exists for. The columns still become uuid so a caller can join by
-- hand when the row does still exist.
alter table challenge_interactions
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column project_id type uuid using legacy_uuid(project_id),
  alter column load_balancer_id type uuid using legacy_uuid(load_balancer_id),
  alter column alias_id type uuid using legacy_uuid(alias_id);

alter table audit_entries
  alter column id drop default,
  alter column id type uuid using legacy_uuid(id),
  alter column id set default uuidv7(),
  alter column object_id type uuid using legacy_uuid(object_id),
  alter column project_id type uuid using legacy_uuid(project_id);

alter table project_daily_stats
  alter column project_id type uuid using legacy_uuid(project_id);

drop sequence if exists projects_id_seq;
drop sequence if exists load_balancers_id_seq;
drop sequence if exists lb_nodes_id_seq;
drop sequence if exists aliases_id_seq;
drop sequence if exists domains_id_seq;
drop sequence if exists api_keys_id_seq;
drop sequence if exists api_key_networks_id_seq;
drop sequence if exists challenges_id_seq;
drop sequence if exists challenge_interactions_id_seq;
drop sequence if exists audit_entries_id_seq;

alter table load_balancers   add constraint load_balancers_project_id_fkey       foreign key (project_id)        references projects (id)       on delete restrict;
alter table lb_nodes         add constraint lb_nodes_load_balancer_id_fkey       foreign key (load_balancer_id)  references load_balancers (id) on delete cascade;
alter table aliases          add constraint aliases_load_balancer_id_fkey        foreign key (load_balancer_id)  references load_balancers (id) on delete restrict;
alter table domains          add constraint domains_load_balancer_id_fkey        foreign key (load_balancer_id)  references load_balancers (id) on delete restrict;
alter table domains          add constraint domains_alias_id_fkey                foreign key (alias_id)          references aliases (id)        on delete restrict;
alter table api_keys         add constraint api_keys_load_balancer_id_fkey       foreign key (load_balancer_id)  references load_balancers (id) on delete cascade;
alter table api_key_networks add constraint api_key_networks_api_key_id_fkey     foreign key (api_key_id)        references api_keys (id)       on delete cascade;
alter table challenges       add constraint challenges_alias_id_fkey             foreign key (alias_id)          references aliases (id)        on delete cascade;
