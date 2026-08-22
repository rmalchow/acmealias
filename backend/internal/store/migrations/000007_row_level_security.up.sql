-- Tenant isolation enforced by the database, not only by the query (design P20).
--
-- Scope is a Go parameter, so a store method written without it does not compile.
-- That is half the problem. The other half is a query that HAS a scope and does
-- not use it — whether the WHERE clause includes account_id is a property of each
-- query's text, upheld by review — and a read that never goes through the store
-- package at all. These policies close both: the predicate lives on the table, so
-- a query that forgets it returns nothing.
--
-- FORCE is not optional. A table's owner bypasses row-level security unless the
-- table is forced, and this service owns the tables it migrates, so without FORCE
-- every policy below would be decoration.
--
-- The setting is read with the missing_ok form and NULLIF'd, so an unset
-- app.account_id yields NULL, the comparison yields NULL, and the row is filtered
-- out. Unscoped reads therefore see NOTHING rather than everything: the failure
-- direction matters more than the failure itself.
--
-- DEVIATION FROM DESIGN P20, recorded rather than hidden. P20 says cross-account
-- components connect as a role holding BYPASSRLS. Creating such a role needs
-- superuser, which a deployment that runs its own migrations under an ordinary
-- owner does not have, and splitting the application across two connection pools
-- is a larger change than this one. The escape here is a second session setting,
-- app.cross_account, which a caller sets deliberately through store.CrossAccount.
-- It keeps the property that motivated P20 — the set of cross-tenant readers is
-- explicit, named at each call site and greppable — and gives up the property that
-- an attacker who can already execute arbitrary SQL cannot lift it. Moving to a
-- separate role remains the better answer and is now a small, isolated change:
-- only store.CrossAccount would have to be rewritten.

create or replace function current_account() returns uuid as $$
  select nullif(current_setting('app.account_id', true), '')::uuid;
$$ language sql stable;

create or replace function cross_account_read() returns boolean as $$
  select coalesce(current_setting('app.cross_account', true), '') = 'on';
$$ language sql stable;

do $$
declare t text;
begin
  foreach t in array array['load_balancers', 'lb_nodes', 'aliases', 'domains', 'api_keys', 'projects']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($f$
      create policy tenant_isolation on %I
        using (cross_account_read() or account_id = current_account())
        with check (cross_account_read() or account_id = current_account())
    $f$, t);
  end loop;
end
$$;

-- challenges and challenge_interactions get no policy, deliberately (design P20).
-- They carry no account_id: a challenge row is reachable only through an alias
-- that was itself resolved under a scope, so the fence sits one level up, and
-- POST /present is on a certificate-issuance critical path this change promises
-- not to slow down. Written here so a later reader finds a decision rather than a
-- table somebody missed.
