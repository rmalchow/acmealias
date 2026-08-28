-- Invitations, and the state they create: a member who holds no role.
--
-- An invitation is an offer of membership in one account, addressed to an EMAIL
-- ADDRESS rather than to a user — because at the moment somebody types
-- `alice@corp.com` the service knows nothing about Alice, not whether she has a
-- user, not whether she will sign in with that address (design V1).
--
-- IT NAMES NO ROLE, and that is the newest decision in the change rather than an
-- omission (design V6). An earlier draft carried `role_id` here with a foreign
-- key to `roles`, plus a whole mechanism for what happens when somebody deletes
-- that role during the fourteen days an invitation is alive. All of it guarded a
-- value that is no longer on the row: joining and being given a role are two
-- steps now, so deleting a role has no interaction with invitations at all.
--
-- What that costs is the second half of this migration.


-- ---------------------------------------------------------------------------
-- invitations
-- ---------------------------------------------------------------------------
create table invitations (
  id         uuid primary key default uuidv7(),
  account_id uuid not null references accounts (id) on delete cascade,
  email      text not null,
  -- Only the HASH, never the token, for the reason api_keys.secret_hash already
  -- has: a datastore read must not yield a usable credential. The token is
  -- high-entropy random, so a memory-hard hash buys nothing over SHA-256 — the
  -- same argument internal/apikey's package comment makes at length.
  token_hash bytea not null,
  invited_by uuid not null references users (id) on delete restrict,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,

  -- When a message was last put on the wire for this invitation, which is
  -- created_at until somebody re-sends and then moves.
  --
  -- It is a column rather than a thing derived from expires_at (which also
  -- moves on a re-send) because it is what the re-send rate limit reads, and a
  -- limit computed as `expires_at - the lifetime constant` would silently
  -- change meaning the day somebody changes the lifetime. It is also the honest
  -- answer to "when did we last mail this person", which the roster wants to
  -- show and which created_at stops being after the first re-send.
  last_sent_at timestamptz not null default now(),

  -- Three nullable timestamps rather than a status column, because each records
  -- WHEN as well as WHICH and the audit trail wants both. There is deliberately
  -- no fourth for expiry: outstanding is "none of these three, and expires_at is
  -- in the future", evaluated at read time, so an invitation is dead the moment
  -- it is dead whether or not a sweep has run (design V8).
  revoked_at  timestamptz null,
  declined_at timestamptz null,
  accepted_at timestamptz null,
  accepted_by uuid null references users (id) on delete restrict,

  -- accepted_by exists exactly when accepted_at does. Without this, a row could
  -- record that somebody joined without recording who — which is the one thing
  -- an acceptance entry exists to evidence.
  constraint invitations_accepted_by_whom
    check ((accepted_at is null) = (accepted_by is null))
);

-- One outstanding invitation per address per account, structurally rather than
-- as a check somebody has to remember.
--
-- `declined_at` is in the predicate on purpose: a decline is an ANSWER, not a
-- block. "Not now" and "never" are different answers and we cannot tell which
-- was meant, so the account may invite the same person again — and this index
-- has to let it (design V12).
create unique index invitations_pending_idx
    on invitations (account_id, email)
 where accepted_at is null and revoked_at is null and declined_at is null;

-- The other direction, and the only index that answers it: "what is outstanding
-- for this address", across EVERY account. That is the second way into an
-- invitation (design V11) — a person who signs in as the invited address sees
-- their invitations whether or not the mail ever arrived — and without this it
-- is a full scan on every sign-in.
create index invitations_by_email_idx
    on invitations (email)
 where accepted_at is null and revoked_at is null and declined_at is null;


-- ---------------------------------------------------------------------------
-- One address identifies at most one user
-- ---------------------------------------------------------------------------
--
-- users.email was nullable, unconstrained and — until this change — never
-- written by anything: every caller of store.FindOrCreateUser passed an empty
-- string. Sign-in now records the address the identity provider says it has
-- VERIFIED, because that is what an invitation is bound to (design V2).
--
-- Which makes the binding depend on a property nothing was enforcing: that the
-- address identifies ONE person. Two users both verified as alice@corp.com
-- would both satisfy "the accepting user's address is the invited address", and
-- an invitation meant for one of them could be accepted by the other — with
-- every test still passing, because each of them really does hold the address.
--
-- The index is partial and lower-cased. Partial because the many users with no
-- address at all (created by adminctl, which names a subject and knows no
-- address) must not collide with each other — nulls are distinct, so they do
-- not. Lower-cased because that is the form invitations are stored and compared
-- in, and an index on the raw column would let `Alice@corp.com` in beside
-- `alice@corp.com` and defeat the whole thing.
create unique index users_one_user_per_verified_email
    on users (lower(email))
 where email is not null;


-- ---------------------------------------------------------------------------
-- A member may hold no role
-- ---------------------------------------------------------------------------
--
-- The cost of taking the role off the invitation. Alice accepts, becomes a
-- member, and holds nothing until an administrator gives her a role.
--
-- SUCH A MEMBER MUST AUTHORISE NOTHING. That falls out of the grant model
-- without help — a subject with no role has an empty grant set, so the ACL
-- predicate is false for every row — and the thing to watch is not the
-- authorization but the PRESENTATION: row-level security empties rather than
-- fails, so her project list is zero rows and no error, which looks like an
-- empty account rather than one she cannot see into. That is the console's
-- problem and is not fixable here; it is written down here because this column
-- is where it starts.
alter table account_members alter column role_id drop not null;

-- The composite foreign key still holds and nothing needs relaxing.
--
-- account_members references roles (id, account_id) rather than roles (id) —
-- the PAIR is what stops a membership naming another account's role — and
-- PostgreSQL's default MATCH SIMPLE skips the check when ANY referencing column
-- is null. So the pair is either both present and checked, or absent. This is
-- asserted below rather than trusted, because "the constraint quietly stopped
-- applying" is not a thing that produces an error at any point.

-- What DOES stop applying: unique (account_id, user_id, role_id).
--
-- Nulls are distinct in a unique constraint, so with role_id null that
-- three-column key constrains nothing and the same person could be inserted as a
-- roleless member any number of times — appearing several times in the roster,
-- each row a separate placeholder for one role assignment. The partial index
-- below is what the three-column key stops providing.
create unique index account_members_one_roleless_per_user
    on account_members (account_id, user_id)
 where role_id is null;


-- ---------------------------------------------------------------------------
-- Row-level security, and the assertion that it is really there
-- ---------------------------------------------------------------------------
--
-- invitations carries account_id, so it takes the ordinary tenant predicate.
--
-- TWO READS DELIBERATELY GO ROUND IT, and they are the interesting ones:
-- resolving an invitation by its token, and listing the invitations outstanding
-- for a signed-in person's address. Neither has an account to scope to — that is
-- the whole point of an invitation, since the invitee is not a member yet — so
-- both run through store.CrossAccount. What confines them is not the policy but
-- the lookup: an unguessable token hash in the first case, the authenticated
-- user's own verified address in the second. A reviewer who sees CrossAccount
-- here and assumes it is a mistake should read this paragraph; a reviewer who
-- sees it anywhere ELSE in the invitation code should assume it is.
alter table invitations enable row level security;
alter table invitations force row level security;

create policy tenant_isolation on invitations
  using (cross_account_read() or account_id = current_account())
  with check (cross_account_read() or account_id = current_account());

-- Assert it, and assert that account_members still has its own.
--
-- RLS EMPTIES RATHER THAN FAILS: a table left enabled, forced and unpolicied
-- returns nothing from every read, and that failure looks exactly like a
-- correctly isolated read of somebody else's account. ALTER TABLE ... ALTER
-- COLUMN does not drop a policy today, and the assertion is here because the
-- cost of being wrong about that is a silently empty account rather than an
-- error.
--
-- pg_policy and not pg_policies, for the reason migration 000004 records at
-- length: the view renders every policy expression through pg_get_expr, which
-- takes a lock on every relation in every schema before any WHERE clause can
-- narrow it, and deadlocks against a concurrent migration in a parallel test
-- run.
do $$
declare
  t        text;
  expected text[] := array['invitations', 'account_members'];
  n        int;
begin
  foreach t in array expected loop
    select count(*) into n
      from pg_policy pol
      join pg_class c on c.oid = pol.polrelid
      join pg_namespace ns on ns.oid = c.relnamespace
     where ns.nspname = current_schema() and c.relname = t and pol.polname = 'tenant_isolation';
    if n <> 1 then
      raise exception 'table % has % tenant_isolation policies, expected exactly one', t, n;
    end if;

    select count(*) into n
      from pg_class c join pg_namespace ns on ns.oid = c.relnamespace
     where ns.nspname = current_schema() and c.relname = t
       and c.relrowsecurity and c.relforcerowsecurity;
    if n <> 1 then
      raise exception 'table % is not both enable and FORCE row level security', t;
    end if;
  end loop;

  -- And the composite foreign key is still a composite foreign key. Widening
  -- role_id must not have turned it into anything else.
  select count(*) into n
    from pg_constraint con
    join pg_class c on c.oid = con.conrelid
    join pg_namespace ns on ns.oid = c.relnamespace
   where ns.nspname = current_schema()
     and c.relname = 'account_members'
     and con.contype = 'f'
     and array_length(con.conkey, 1) = 2;
  if n <> 1 then
    raise exception 'account_members has % two-column foreign keys, expected exactly one '
      '(role_id, account_id) -> roles (id, account_id)', n;
  end if;
end
$$;
