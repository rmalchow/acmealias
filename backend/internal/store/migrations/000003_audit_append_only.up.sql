-- The audit trail is append-only IN THE DATABASE, not only in the Go code.
--
-- DO NOT WIDEN THIS WHILE DEBUGGING. If a row is wrong, the fix is another row.
-- A trail that anyone with the service's credentials can rewrite answers the
-- question "what happened" with "whatever the last writer wanted", which is the
-- one answer that makes the whole table pointless.
--
-- It is enforced twice, because the grant alone does not bite here:
--
--   1. REVOKE UPDATE/DELETE/TRUNCATE, leaving INSERT and SELECT. This is what
--      the requirement asks for and what holds when the service is deployed
--      under a role that does NOT own the schema — the arrangement to prefer.
--   2. A trigger that refuses UPDATE and DELETE outright. This is the half that
--      actually holds today: this service owns the tables it migrates, and a
--      table owner keeps every privilege on it no matter what is revoked, so
--      (1) is a statement of intent rather than a lock until ownership is
--      split.

revoke update, delete, truncate on audit_entries from public;

do $$
begin
  execute format('revoke update, delete, truncate on audit_entries from %I', current_user);
  execute format('grant insert, select on audit_entries to %I', current_user);
end
$$;

create function audit_entries_append_only() returns trigger
language plpgsql as $$
begin
  raise exception 'audit_entries is append-only: % is not permitted', tg_op
    using errcode = 'insufficient_privilege';
end
$$;

-- Statement level, so `delete from audit_entries` is refused even when it would
-- have matched no rows: the refusal should not depend on the table's contents.
create trigger audit_entries_no_rewrite
  before update or delete or truncate on audit_entries
  for each statement execute function audit_entries_append_only();
