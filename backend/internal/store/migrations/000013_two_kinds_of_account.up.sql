-- An account is personal or team, has exactly one owner, and carries a balance
-- (design C1, C2, C3).
--
-- Three columns and one index, and the index is the interesting one. A personal
-- account is IMPLICIT: there is no request that asks for one, because a user has
-- one by virtue of being a user, and the row is materialized the first time they
-- sign in. That makes "at most one personal account per user" a property nobody
-- can ask us to violate — except by racing: two concurrent first sign-ins for the
-- same person both find no row and both insert one. A check in Go loses that race
-- by construction, so the partial unique index below IS the enforcement and the
-- materialization path is merely the common case (C1, C2).
--
-- The two mutations that would otherwise unpick the model are refused by a
-- trigger rather than by the service, for the same reason: a rule the service
-- holds is a rule the CLI, a repair script or the next write path can walk past.

set local app.cross_account = 'on';

-- kind defaults to 'team' only for the length of this migration: every account
-- that exists today is a shared workspace with members, which is what a team
-- account is. The default is dropped afterwards so that an insert which forgets
-- to say which kind it is creating fails loudly instead of quietly producing a
-- team account.
alter table accounts
  add column kind text not null default 'team'
    constraint accounts_kind_check check (kind in ('personal', 'team')),
  add column credits bigint not null default 0
    constraint accounts_credits_non_negative check (credits >= 0),
  add column owner_user_id uuid references users (id) on delete restrict;

-- A personal account is free and has no balance at all (C3). Storing a zero it
-- can never leave is not the same as having a balance, and the constraint is
-- what keeps the difference from eroding the first time a credit-granting path
-- forgets to ask which kind of account it is crediting.
alter table accounts
  add constraint accounts_personal_has_no_balance check (kind = 'team' or credits = 0);

-- Backfill: the owner is the account's earliest unrestricted member. Every
-- account created through CreateAccountWithOwner has exactly one such member
-- written in the same transaction as the account, so for those this is not a
-- guess — it is the row that was always the owner and was never named as such.
update accounts a
   set owner_user_id = (
       select m.user_id
         from account_members m
         join roles r on r.id = m.role_id and r.account_id = m.account_id
        where m.account_id = a.id
          and r.unrestricted
        order by m.created_at, m.id
        limit 1)
 where a.owner_user_id is null;

-- Failing that, the earliest member of any role: an account with members but no
-- unrestricted one should not exist (store.RemoveMember refuses to create it),
-- but a backfill that assumes an invariant it can cheaply check instead is a
-- backfill that fails at 3am on the one deployment where it does not hold.
update accounts a
   set owner_user_id = (
       select m.user_id
         from account_members m
        where m.account_id = a.id
        order by m.created_at, m.id
        limit 1)
 where a.owner_user_id is null;

-- What is left is the internal edition's seeded account, which genuinely has no
-- owner to find: its members were DERIVED from an identity-provider group claim
-- rather than written down (P18's second branch, which C10 removes), so
-- account_members can be empty while people were administering it every day. A
-- freshly migrated database is the same case with nobody in it yet.
--
-- It gets a stand-in user, one per ownerless account, whose subject is not a
-- shape any identity provider issues — so nobody can sign in as it, and it is
-- greppable when C10's removal deletes the edition it belongs to.
--
-- Two alternatives were rejected. Leaving owner_user_id nullable would mean the
-- invariant "every account has exactly one owner" is enforced nowhere and the
-- partial unique index binds nothing, which is the whole point of this
-- migration. Deleting the ownerless account is worse still: in an internal
-- deployment it owns every project, load balancer and domain the service has.
insert into users (subject, display_name)
select 'system:migrated-owner:' || a.id, 'Migrated owner'
  from accounts a
 where a.owner_user_id is null;

update accounts a
   set owner_user_id = u.id
  from users u
 where a.owner_user_id is null
   and u.subject = 'system:migrated-owner:' || a.id;

alter table accounts alter column owner_user_id set not null;
alter table accounts alter column kind drop default;

-- Grandfather every account that existed before there was a balance.
--
-- `credits` defaults to 0, and 0 means SUSPENDED for a team account (C3) — so
-- without this line the migration's own act of adding the column stops every
-- deployment it is run on: /present and /cleanup answer 402, which stops
-- renewals silently for about thirty days, and every account-scoped management
-- route answers 402 as well. That is the worst failure this product has, arrived
-- at by an upgrade rather than by anything a customer did.
--
-- These accounts were never sold and cannot retroactively be found unpaid. The
-- opening balance is the smallest amount the service offers (store.CheckoutAmounts[0]);
-- it is written as a literal because a migration cannot read Go, and the number
-- matters far less than the fact that it is above zero.
--
-- Only pre-existing rows are touched. Nothing created after this migration
-- reaches here, and both creation paths refuse a zero opening balance
-- (store.CreateAccountWithOwner), so this cannot become a way to make one.
--
-- It does NOT make an upgraded deployment reachable: the internal edition's
-- account has an owner nobody can authenticate as and, because its membership
-- was derived from a group claim rather than written down, no members at all.
-- Repairing that needs a human decision about who it now belongs to, which is
-- what `adminctl account transfer` and `adminctl account member add` are for.
-- What this line buys is that the certificates keep being renewed while somebody
-- makes it.
update accounts set credits = 25 where kind = 'team' and credits = 0;

-- The enforcement (C1, C2). Partial, because the limit is one PERSONAL account
-- per user and team accounts are deliberately unlimited — paying for one must
-- not consume the free slot.
create unique index accounts_one_personal_per_owner on accounts (owner_user_id) where kind = 'personal';

-- Ownership is asked about across accounts ("which of these does this person
-- own"), never within one, so the index leads with the owner rather than with
-- the tenant.
create index accounts_owner_user_id_idx on accounts (owner_user_id);

-- Kind is fixed at creation and a personal account is never transferred (C1).
--
-- Both refusals live in the database rather than in the service because both are
-- statements about what an account IS, not about who may do what: a personal
-- account that became a team account, or that changed hands, would be a team
-- account with the team parts missing, and no permission check makes that any
-- less true. The service refuses these too, with a sentence a caller can read
-- (store.TransferOwnership) — this is the floor under it.
--
-- The second refusal also closes C2's other door: a transfer cannot produce a
-- second personal account because a personal account cannot be transferred at
-- all. The partial unique index above would catch it anyway, which is the
-- belt-and-braces the design asks for on purpose.
create or replace function accounts_refuse_reclassification() returns trigger as $$
begin
  if new.kind is distinct from old.kind then
    raise exception 'an account is created personal or team and stays that way (design C1)'
      using errcode = 'check_violation';
  end if;
  if old.kind = 'personal' and new.owner_user_id is distinct from old.owner_user_id then
    raise exception 'a personal account belongs to its user and cannot be transferred (design C1, C2)'
      using errcode = 'check_violation';
  end if;
  return new;
end;
$$ language plpgsql;

create trigger accounts_kind_is_immutable
  before update on accounts
  for each row execute function accounts_refuse_reclassification();

reset app.cross_account;
