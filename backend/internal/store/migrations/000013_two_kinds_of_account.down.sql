-- Undo the two kinds of account.
--
-- The stand-in owners the up migration invented are deleted, and only those: a
-- user row whose subject an identity provider could have issued is somebody's,
-- and going back one migration is not a reason to remove a person. The delete
-- runs after the column is dropped, because the foreign key is what would
-- otherwise refuse it.
set local app.cross_account = 'on';

drop trigger if exists accounts_kind_is_immutable on accounts;
drop function if exists accounts_refuse_reclassification();

drop index if exists accounts_one_personal_per_owner;
drop index if exists accounts_owner_user_id_idx;

alter table accounts
  drop constraint if exists accounts_personal_has_no_balance,
  drop column if exists owner_user_id,
  drop column if exists credits,
  drop column if exists kind;

delete from users where subject like 'system:migrated-owner:%';

reset app.cross_account;
