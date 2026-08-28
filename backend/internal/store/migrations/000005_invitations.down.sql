-- Rolling back invitations, and REFUSING to roll back the roleless member.
--
-- Dropping the table is easy: nothing references it and no other table's rows
-- depend on it, so an outstanding invitation simply stops resolving, which
-- presents to its recipient as an expired one.
--
-- Narrowing account_members.role_id back to `not null` is NOT easy, and this
-- file refuses rather than guessing. By the time anybody runs it, people may
-- have joined accounts and be waiting for a role. There are three ways to
-- proceed and two of them destroy something:
--
--   - Delete the roleless memberships. Silently removes real people from
--     accounts they were invited into and accepted.
--   - Give them a role. Grants authority nobody chose, which is the objection
--     that killed "substitute another role" in the design.
--   - Refuse, and say what to do. This.
--
-- So: if any roleless membership exists, this aborts with a message naming the
-- count. Whoever is rolling back decides what those people should hold, assigns
-- it, and runs this again. That is one manual step in an operation nobody
-- performs often, in exchange for never discovering afterwards that a rollback
-- quietly removed somebody.
do $$
declare n int;
begin
  select count(*) into n from account_members where role_id is null;
  if n > 0 then
    raise exception
      'refusing to roll back: % membership(s) hold no role, and narrowing role_id '
      'back to NOT NULL can only delete them or invent a role for them. Assign each '
      'of them a role first, then run this migration again.', n;
  end if;
end
$$;

drop index if exists users_one_user_per_verified_email;
drop index if exists account_members_one_roleless_per_user;
alter table account_members alter column role_id set not null;

drop table if exists invitations;
