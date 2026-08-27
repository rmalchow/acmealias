-- Repair the grants 000009 meant to write and silently did not.
--
-- 000009 adds challenge.publish and challenge.withdraw to CONFIG_ADMIN and
-- KEY_ROTATION, so that a role which may mint an API key still holds what the
-- key is minted with — constraint 1, "the internal edition must not observably
-- change". Its own comment explains why at length.
--
-- It never wrote a row. 000008's last statement enables row-level security on
-- `roles` and `role_grants`, and 000009's insert reads `from roles r`. A
-- migration runs with no session, so `current_account()` is null, so the policy
-- matched no row, so the select returned nothing and the insert wrote nothing —
-- without failing. The symptom is a 403 on key creation for exactly the two
-- roles whose purpose is creating keys:
--
--   [load_balancer, <id>, challenge.publish, inheritable] is not covered by any
--   single grant the writer holds
--
-- 000009 is fixed in place for databases created after this, which is what makes
-- a fresh migration chain correct. This migration is for the ones already past
-- it: version 11 exists in the wild (the development stack, and any internal
-- deployment), and re-running 000009 is not something a forward chain can do.
--
-- Idempotent by the NOT EXISTS guard rather than by a unique constraint, because
-- role_grants deliberately has none: the same authority may legitimately be
-- granted twice by different paths, and it is the subset rule rather than the
-- schema that decides whether a grant is allowed. So a database that somehow did
-- get 000009's rows is left exactly as it is.
set local app.cross_account = 'on';

insert into role_grants (role_id, account_id, object_type, action, inheritable)
select r.id, r.account_id, '*', a.action, true
  from roles r
  cross join (values ('challenge.publish'), ('challenge.withdraw')) as a(action)
 where r.name in ('CONFIG_ADMIN', 'KEY_ROTATION')
   and not exists (
     select 1 from role_grants g
      where g.role_id = r.id
        and g.object_type = '*'
        and g.object_id is null
        and g.action = a.action
   );

reset app.cross_account;
