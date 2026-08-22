-- An API key stops being "a key belonging to a load balancer" and becomes a
-- subject holding grants.
--
-- The column being dropped WAS the authorization: the whole check was one
-- comparison, `alias.load_balancer_id != key.load_balancer_id`, which worked
-- precisely because there was exactly one thing a key could do. Once a key can
-- be anchored at a project — the customer request that started all this — the
-- foreign key stops being able to express what the key may reach, and keeping it
-- alongside grants would leave two answers to the same question.
--
-- 000008 already wrote each key the grants its column implied, in the same
-- migration chain and before this drop, so the two never coexist at runtime.
--
-- project_id goes with it, for the same reason: design P3 puts an ancestor
-- column on a row for every ancestor type a GRANT may name, and a key no longer
-- has a load balancer or a project as an ancestor. Its only anchor is the
-- account, which stays.

alter table api_keys drop constraint api_keys_lb_project_fkey;
alter table api_keys drop constraint api_keys_lb_account_fkey;
alter table api_keys drop constraint api_keys_load_balancer_id_fkey;
drop index if exists api_keys_load_balancer_id_idx;
drop index if exists api_keys_project_id_idx;
alter table api_keys drop column load_balancer_id;
alter table api_keys drop column project_id;

-- The roles that could mint a key must hold what a key is minted with.
--
-- This is constraint 1 — "the internal edition must not observably change" —
-- and it is only visible once keys carry grants. Under the old model a role
-- holding key.write could create a key, and that key could publish challenge
-- values, because the key's authority was the foreign key rather than anything
-- the creator held. The subset rule now says a subject may grant only what it
-- holds, so a role that can create keys and holds no challenge grant can create
-- only useless ones.
--
-- Reproducing the old behaviour therefore means writing down what the old model
-- left implicit: CONFIG_ADMIN and KEY_ROTATION could confer challenge publishing
-- across the whole account, so they hold it. READ_ONLY could not create keys at
-- all and gains nothing.
--
-- ADMIN needs no row: it is unrestricted.
insert into role_grants (role_id, account_id, object_type, action, inheritable)
select r.id, r.account_id, '*', a.action, true
  from roles r
  cross join (values ('challenge.publish'), ('challenge.withdraw')) as a(action)
 where r.name in ('CONFIG_ADMIN', 'KEY_ROTATION');
