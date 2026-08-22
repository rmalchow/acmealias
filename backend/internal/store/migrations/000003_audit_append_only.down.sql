drop trigger if exists audit_entries_no_rewrite on audit_entries;
drop function if exists audit_entries_append_only();

do $$
begin
  execute format('grant insert, select, update, delete, truncate on audit_entries to %I', current_user);
end
$$;
