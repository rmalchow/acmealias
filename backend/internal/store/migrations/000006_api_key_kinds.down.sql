-- Rolling back the two kinds.
--
-- This one is genuinely cheap, which is worth saying because the last two
-- rollbacks were not. Every key remains what it was before: a subject anchored
-- to an account, holding grants. A PROJECT key's authority, though, is
-- synthesized from its project_id rather than stored in api_key_grants — that is
-- design K1's "resolve before the compiler sees them" — so dropping the column
-- would leave those keys holding nothing and silently stop the renewals they
-- exist to serve.
--
-- So the grants are written out first, from the project each key names, and only
-- then are the columns dropped. After this runs a project key is exactly what
-- one had to be assembled by hand before: a key holding challenge.publish and
-- challenge.withdraw over one project, inheritable.
--
-- The hand-typed CIDR list in api_key_networks is untouched and keeps working.
-- What is lost is the SWITCH — a key restricted to its project's nodes becomes
-- unrestricted rather than pinned to a snapshot, because writing the snapshot is
-- the staleness the switch exists to remove and a rollback is a poor moment to
-- introduce it. That is a WIDENING of access, so it is announced rather than
-- performed quietly.
-- The lift is not optional, and migration 000004 is where that was learned: both
-- api_keys and api_key_grants force row-level security, so with no session
-- settings `cross_account_read() or account_id = current_account()` is false for
-- every row. A bare read here would count zero and a bare insert would write
-- nothing — silently, recording the migration as applied and leaving every
-- project key holding no authority at all. set_config's third argument is `true`
-- (is_local), so the lift ends with this block's transaction; it is the same
-- escape store.CrossAccount uses.
do $$
declare n int;
begin
  perform set_config('app.cross_account', 'on', true);

  select count(*) into n from api_keys where restrict_to_project_nodes;
  if n > 0 then
    raise notice
      '% key(s) were limited to their project''s node addresses. That limit is being '
      'dropped, not converted: they will accept requests from anywhere. Revoke them, or '
      'add explicit CIDR restrictions in api_key_networks, before relying on this.', n;
  end if;

  insert into api_key_grants (api_key_id, account_id, object_type, object_id, action, inheritable)
  select k.id, k.account_id, 'project', k.project_id, a.action, true
    from api_keys k
   cross join (values ('challenge.publish'), ('challenge.withdraw')) as a (action)
   where k.kind = 'project'
     and k.project_id is not null;
end
$$;

alter table api_keys
  drop constraint if exists api_keys_node_limit_needs_a_project,
  drop constraint if exists api_keys_shape,
  drop constraint if exists api_keys_role_account_fkey,
  drop constraint if exists api_keys_project_account_fkey,
  drop constraint if exists api_keys_kind_check;

drop index if exists api_keys_role_id_idx;
drop index if exists api_keys_project_id_idx;

alter table api_keys
  drop column if exists last_refused_at,
  drop column if exists last_refused_ip,
  drop column if exists restrict_to_project_nodes,
  drop column if exists role_id,
  drop column if exists project_id,
  drop column if exists kind;
