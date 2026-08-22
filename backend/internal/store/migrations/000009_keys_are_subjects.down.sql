delete from role_grants
 where action in ('challenge.publish', 'challenge.withdraw')
   and role_id in (select id from roles where name in ('CONFIG_ADMIN', 'KEY_ROTATION'));

-- Reconstructs the column from the grants 000008 wrote, which is possible only
-- while every key still holds exactly the shape of grant that migration created:
-- one load balancer, inheritable. A key given a project-anchored grant since —
-- the thing this change exists to allow — has no single load balancer to go
-- back to, and this picks one arbitrarily rather than pretending otherwise.
--
-- It is a rollback for a deployment that has not yet used the new model, which
-- is the window the whole change is sequenced to run in.

alter table api_keys add column load_balancer_id uuid;
alter table api_keys add column project_id uuid;

update api_keys k
   set load_balancer_id = g.object_id
  from (
    select distinct on (api_key_id) api_key_id, object_id
      from api_key_grants
     where object_type = 'load_balancer' and object_id is not null
     order by api_key_id, created_at
  ) g
 where g.api_key_id = k.id;

update api_keys k
   set project_id = lb.project_id
  from load_balancers lb
 where lb.id = k.load_balancer_id;

delete from api_keys where load_balancer_id is null or project_id is null;

alter table api_keys alter column load_balancer_id set not null;
alter table api_keys alter column project_id set not null;

create index api_keys_load_balancer_id_idx on api_keys (load_balancer_id);
create index api_keys_project_id_idx on api_keys (project_id);

alter table api_keys add constraint api_keys_load_balancer_id_fkey
  foreign key (load_balancer_id) references load_balancers (id) on delete cascade;
alter table api_keys add constraint api_keys_lb_account_fkey
  foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete cascade;
alter table api_keys add constraint api_keys_lb_project_fkey
  foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete cascade;
