do $$
declare t text;
begin
  foreach t in array array['roles', 'role_grants', 'api_key_grants', 'account_members']
  loop
    execute format('drop policy if exists tenant_isolation on %I', t);
    execute format('alter table %I no force row level security', t);
    execute format('alter table %I disable row level security', t);
  end loop;
end
$$;

drop table if exists api_key_grants;
drop table if exists role_grants;
drop table if exists account_members;
drop index if exists roles_one_unrestricted_per_account;
drop table if exists roles;
drop table if exists users;
