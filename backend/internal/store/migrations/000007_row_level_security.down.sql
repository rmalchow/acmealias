do $$
declare t text;
begin
  foreach t in array array['load_balancers', 'lb_nodes', 'aliases', 'domains', 'api_keys', 'projects']
  loop
    execute format('drop policy if exists tenant_isolation on %I', t);
    execute format('alter table %I no force row level security', t);
    execute format('alter table %I disable row level security', t);
  end loop;
end
$$;

drop function if exists cross_account_read();
drop function if exists current_account();
