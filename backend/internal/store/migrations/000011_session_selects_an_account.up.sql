-- A session selects one account, and the selection lives on the session row
-- (design P19).
--
-- Nullable, because "no account selected" is a real state and not an error: a
-- person who has just registered belongs to nothing, and a person in five
-- accounts has not yet said which. From that state exactly four things are
-- reachable — their own profile, the accounts they belong to, creating an
-- account, and accepting an invitation — and everything else is refused with a
-- statement that no account is selected rather than with an empty result, which
-- would read as "you have nothing".
--
-- ON DELETE SET NULL rather than CASCADE: deleting an account must not sign its
-- members out of the service. They fall back to having no selection, which is
-- exactly what is true.
--
-- When this migration was written the unselected state was unreachable: the
-- deployment had one account and sign-in selected it, which is what kept
-- constraint 1 true. Design C10 removed both, so this column's null is now the
-- state every session starts in and the picker is the front door.
alter table sessions add column selected_account_id uuid
  references accounts (id) on delete set null;

create index sessions_selected_account_idx on sessions (selected_account_id);
