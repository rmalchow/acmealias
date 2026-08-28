-- The schema, in one piece.
--
-- Read it as a description of the model rather than as a history: accounts own
-- inventory, every row below an account carries the identifier of every ancestor
-- a grant may name, authorization is data, and tenant isolation is a database
-- policy rather than a convention about WHERE clauses.
--
-- The sections below are ordered so that a table is created after everything it
-- references. Row-level security is turned on at the end, in one place, because
-- it is one decision about the whole schema and not a property of each table
-- that happens to have it.


-- ---------------------------------------------------------------------------
-- Identifiers
-- ---------------------------------------------------------------------------

-- Every identifier is a UUIDv7 (design P17).
--
-- Two reasons, neither of them fashion. A sequence discloses how many of a thing
-- exist and lets one tenant's object be guessed from another's, which stops being
-- a curiosity the moment accounts are real. And an identifier that is unique
-- across deployments is what lets a satellite, a report and a centre talk about
-- the same object without a translation table.
--
-- v7 rather than v4 because the first 48 bits are a millisecond timestamp, so
-- rows sort roughly by creation and B-tree inserts stay at the right-hand edge
-- instead of scattering across the index.
--
-- PostgreSQL 18 has uuidv7() built in. On 16 and 17 it does not exist, so this
-- defines one: 48-bit millisecond timestamp, version 7, variant 10, the rest
-- random. Callers in Go use github.com/google/uuid; this exists so that column
-- defaults and hand-written SQL do not have to.
create or replace function uuidv7() returns uuid as $$
  select encode(
    set_bit(
      set_bit(
        overlay(uuid_send(gen_random_uuid()) placing
          substring(int8send((extract(epoch from clock_timestamp()) * 1000)::bigint) from 3)
          from 1 for 6),
        52, 1),
      53, 1),
    'hex')::uuid;
$$ language sql volatile;

-- A small integer widened into a UUID: a fixed v7-shaped prefix with the number
-- in the low 48 bits. Nothing in the service calls it. It stays because it is the
-- definition of the identifier form that fixtures and hand-written queries use —
-- 00000000-0000-7000-8000-000000000007 is "seven" — so a reader who meets one of
-- those literals has somewhere to look, and a test that wants a well-formed
-- identifier that certainly matches nothing has a way to build one.
create or replace function legacy_uuid(n bigint) returns uuid as $$
  select case when n is null then null
    else ('00000000-0000-7000-8000-' || lpad(to_hex(n), 12, '0'))::uuid end;
$$ language sql immutable;


-- ---------------------------------------------------------------------------
-- The tenant, as the database sees it
-- ---------------------------------------------------------------------------

-- The two functions every row-level security policy in this schema is written in
-- terms of. They are defined here, before any table, because the policies at the
-- end of the file are the reason the columns above them exist.
--
-- current_account reads the session setting with the missing_ok form and NULLIFs
-- it, so an unset app.account_id yields NULL, the comparison yields NULL, and the
-- row is filtered out. An unscoped read therefore sees NOTHING rather than
-- everything: the failure direction matters more than the failure itself.
create or replace function current_account() returns uuid as $$
  select nullif(current_setting('app.account_id', true), '')::uuid;
$$ language sql stable;

-- The deliberate escape hatch, and a recorded deviation from design P20. P20 says
-- cross-account components connect as a role holding BYPASSRLS. Creating such a
-- role needs superuser, which a deployment that runs its own migrations under an
-- ordinary owner does not have, and splitting the application across two
-- connection pools is a larger change. This second session setting is set
-- deliberately through store.CrossAccount, so the set of cross-tenant readers is
-- still explicit, named at each call site and greppable; what it gives up is that
-- an attacker who can already execute arbitrary SQL cannot lift it. Moving to a
-- separate role remains the better answer and is a small, isolated change: only
-- store.CrossAccount would have to be rewritten.
create or replace function cross_account_read() returns boolean as $$
  select coalesce(current_setting('app.cross_account', true), '') = 'on';
$$ language sql stable;


-- ---------------------------------------------------------------------------
-- Identity and accounts
-- ---------------------------------------------------------------------------

-- A user is a global entity (design P18). They exist before they belong to any
-- account, they may belong to several, and they outlive the deletion of any of
-- them. The identity provider owns authentication; this row owns identity.
create table users (
  id           uuid primary key default uuidv7(),
  subject      text not null unique,
  email        text,
  display_name text not null default '',
  created_at   timestamptz not null default now()
);

-- The account is the owner of inventory and the anchor of every grant (design P1,
-- P2, P3). There is no seeded account: every account is created by a person,
-- through store.CreateAccountWithOwner.
--
-- kind has no default on purpose. Every account is personal or team (design C1)
-- and an insert that does not say which is a bug, not a request for the common
-- case — so it fails loudly instead of quietly producing a team account.
--
-- owner_user_id is NOT NULL: "every account has exactly one owner" is an
-- invariant, and an invariant with a nullable column is enforced nowhere.
create table accounts (
  id              uuid primary key default uuidv7(),
  slug            text not null unique,
  name            text not null,
  kind            text not null
                  constraint accounts_kind_check check (kind in ('personal', 'team')),
  -- A team account's balance. 0 means suspended (design C3), which is why
  -- store.CreateAccountWithOwner refuses a zero opening balance.
  credits         bigint not null default 0
                  constraint accounts_credits_non_negative check (credits >= 0),
  -- Suspension by staff, which paying does not lift (design C6). There are two
  -- suspensions and they must not be able to lift each other: running out of
  -- credit is `credits = 0` and is not a column, while abuse needs state of its
  -- own precisely BECAUSE it must survive a checkout. A customer stopped for
  -- abuse who then buys 25 EUR of credit is still stopped.
  --
  -- No `kind = 'team'` clause, unlike credits: abuse is a property of what
  -- somebody did rather than of what they paid, so a personal account can be
  -- flagged. Not indexed either — it is read one account at a time, on the row
  -- the request already has in hand, and never used to select a working set.
  staff_suspended boolean not null default false,
  owner_user_id   uuid not null references users (id) on delete restrict,
  created_at      timestamptz not null default now(),
  -- A personal account is free and has no balance at all (design C3). Storing a
  -- zero it can never leave is not the same as having a balance, and this is what
  -- keeps the difference from eroding the first time a credit-granting path
  -- forgets to ask which kind of account it is crediting. It is structure, not
  -- convention: no code path can be written that violates it.
  constraint accounts_personal_has_no_balance check (kind = 'team' or credits = 0)
);

-- At most one PERSONAL account per user (design C1, C2).
--
-- A personal account is implicit: there is no request that asks for one, because
-- a user has one by virtue of being a user, and the row is materialized the first
-- time they sign in. That makes the limit a property nobody can ask us to violate
-- — except by racing: two concurrent first sign-ins for the same person both find
-- no row and both insert one. A check in Go loses that race by construction, so
-- this index IS the enforcement and the materialization path is merely the common
-- case.
--
-- Partial, because team accounts are deliberately unlimited: paying for one must
-- not consume the free slot.
create unique index accounts_one_personal_per_owner on accounts (owner_user_id) where kind = 'personal';

-- Ownership is asked about across accounts ("which of these does this person
-- own"), never within one, so this index leads with the owner rather than with
-- the tenant.
create index accounts_owner_user_id_idx on accounts (owner_user_id);

-- Kind is fixed at creation and a personal account is never transferred (C1, C2).
--
-- Both refusals live in the database rather than in the service because both are
-- statements about what an account IS, not about who may do what: a personal
-- account that became a team account, or that changed hands, would be a team
-- account with the team parts missing, and no permission check makes that any
-- less true. A rule the service holds is a rule the CLI, a repair script or the
-- next write path can walk past. The service refuses these too, with a sentence a
-- caller can read (store.TransferOwnership) — this is the floor under it.
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


-- ---------------------------------------------------------------------------
-- Inventory: projects -> load balancers -> domains, aliased by challenge name
-- ---------------------------------------------------------------------------
--
-- Every table below an account carries account_id, and every table below a
-- project carries project_id, held true by a COMPOSITE FOREIGN KEY rather than by
-- a trigger or by discipline (design P2, P3).
--
-- The denormalization is the part worth explaining. An earlier draft rejected
-- putting account_id on every row because a row could then disagree with its
-- parent. That is the right worry and the wrong remedy: a composite foreign key
-- makes the disagreement unrepresentable, because the child references the
-- (id, account_id) pair of its parent and cannot name a pair that does not exist.
-- The column is a denormalization whose correctness is structural.
--
-- Without it, two things are impossible. Per-account domain uniqueness needs the
-- account on the row, because Postgres cannot build a unique index across a join.
-- And every scoped read would join up to projects, so no index could lead with
-- the tenant.
--
-- A row carries an ancestor column for every ancestor type a GRANT may name, and
-- that is the rule that decides which columns are here: a listing must reduce to
-- a single-column predicate rather than to a subquery.

create table projects (
  id          uuid primary key default uuidv7(),
  account_id  uuid not null references accounts (id) on delete restrict,
  slug        text not null unique,
  name        text not null,
  created_at  timestamptz not null default now(),
  -- The pair every child of a project references.
  unique (id, account_id)
);

create index projects_account_id_idx on projects (account_id);

create table load_balancers (
  id          uuid primary key default uuidv7(),
  account_id  uuid not null,
  project_id  uuid not null references projects (id) on delete restrict,
  slug        text not null,
  name        text not null,
  created_at  timestamptz not null default now(),
  unique (project_id, slug),
  unique (id, account_id),
  unique (id, project_id),
  constraint load_balancers_project_account_fkey
    foreign key (project_id, account_id) references projects (id, account_id) on delete restrict
);

create index load_balancers_account_id_idx on load_balancers (account_id);

create table lb_nodes (
  id               uuid primary key default uuidv7(),
  account_id       uuid not null,
  project_id       uuid not null,
  load_balancer_id uuid not null references load_balancers (id) on delete cascade,
  label            text not null,
  ipv4             inet,
  ipv6             inet,
  unique (load_balancer_id, label),
  constraint lb_nodes_lb_account_fkey
    foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete cascade,
  constraint lb_nodes_lb_project_fkey
    foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete cascade
);

create index lb_nodes_account_id_idx on lb_nodes (account_id);
create index lb_nodes_project_id_idx on lb_nodes (project_id);

-- An alias is the name a customer CNAMEs their _acme-challenge record at, and the
-- unit a challenge value is published against (design D4). Its label and its
-- challenge FQDN are globally unique because they are DNS names this service
-- serves, not tenant data.
create table aliases (
  id               uuid primary key default uuidv7(),
  account_id       uuid not null,
  project_id       uuid not null,
  load_balancer_id uuid not null references load_balancers (id) on delete restrict,
  challenge_fqdn   text not null unique,
  label            text not null unique,
  created_at       timestamptz not null default now(),
  unique (id, account_id),
  constraint aliases_lb_account_fkey
    foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete restrict,
  constraint aliases_lb_project_fkey
    foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete restrict
);

create index aliases_account_id_idx        on aliases (account_id);
create index aliases_project_id_idx        on aliases (project_id);
create index aliases_load_balancer_id_idx  on aliases (load_balancer_id);

create table domains (
  id               uuid primary key default uuidv7(),
  account_id       uuid not null,
  project_id       uuid not null,
  load_balancer_id uuid not null references load_balancers (id) on delete restrict,
  alias_id         uuid not null references aliases (id) on delete restrict,
  name             text not null,
  created_at       timestamptz not null default now(),
  -- A domain name is unique within an ACCOUNT, not globally (design P8). Two
  -- customers may both hold example.com in their inventory; DNS delegation, not
  -- this table, arbitrates which of them can actually validate it. A global
  -- unique index would let the first account to add a name deny it to every
  -- other account, which is a denial of service with a database constraint as
  -- the weapon.
  unique (account_id, name),
  constraint domains_lb_account_fkey
    foreign key (load_balancer_id, account_id) references load_balancers (id, account_id) on delete restrict,
  constraint domains_lb_project_fkey
    foreign key (load_balancer_id, project_id) references load_balancers (id, project_id) on delete restrict,
  constraint domains_alias_account_fkey
    foreign key (alias_id, account_id) references aliases (id, account_id) on delete restrict
);

create index domains_account_id_idx on domains (account_id);
create index domains_project_id_idx on domains (project_id);
create index domains_alias_id_idx   on domains (alias_id);


-- ---------------------------------------------------------------------------
-- API keys
-- ---------------------------------------------------------------------------

-- The secret is never stored; only its SHA-256 digest is (design D3).
--
-- A key is a SUBJECT that holds grants, not "a key belonging to a load balancer".
-- It therefore has no load_balancer_id and no project_id: the rule that puts an
-- ancestor column on a row is about the ancestors a GRANT may name, and a key's
-- only ancestor is its account. What a key may reach is api_key_grants and
-- nothing else — one authority, one place to read it.
create table api_keys (
  id              uuid primary key default uuidv7(),
  account_id      uuid not null,
  identifier      text not null unique,
  secret_hash     bytea not null,
  display_prefix  text not null,
  note            text not null default '',
  created_at      timestamptz not null default now(),
  expires_at      timestamptz,
  last_used_at    timestamptz,
  disabled_at     timestamptz
);

create index api_keys_account_id_idx on api_keys (account_id);

create table api_key_networks (
  id          uuid primary key default uuidv7(),
  api_key_id  uuid not null references api_keys (id) on delete cascade,
  cidr        cidr not null,
  unique (api_key_id, cidr)
);


-- ---------------------------------------------------------------------------
-- Challenges
-- ---------------------------------------------------------------------------

-- A challenge value published at an alias, in flight until the CA has validated
-- it (design D6).
--
-- published_by is the key IDENTIFIER as text, not a foreign key, so that deleting
-- a key neither withdraws its in-flight challenges nor loses the attribution.
create table challenges (
  id            uuid primary key default uuidv7(),
  alias_id      uuid not null references aliases (id) on delete cascade,
  value         text not null,
  published_by  text,
  created_at    timestamptz not null default now(),
  expires_at    timestamptz not null,
  unique (alias_id, value)
);

create index challenges_expires_at_idx on challenges (expires_at);


-- ---------------------------------------------------------------------------
-- Challenge interactions: the history
-- ---------------------------------------------------------------------------

-- What a load balancer actually did at this service, and whether the CA ever came
-- to look.
--
-- One row per PUBLICATION, not per HTTP request. A /present is the start of an
-- interaction and the matching /cleanup is its end, so a row is inserted by the
-- first and completed by the second — which is what makes "published at 14:02,
-- queried twice, withdrawn at 14:02:31" one line on a screen rather than three
-- rows to correlate by eye.
--
-- Denormalized on purpose: alias_label, challenge_fqdn and domain_names are
-- copied in at publication and there are NO FOREIGN KEYS at all. A record of what
-- happened must survive the deletion of the objects it happened to; a row that
-- vanishes when a domain is removed cannot answer "what was this key doing last
-- month". project_id sits beside load_balancer_id for the same reason: without
-- it, deleting a load balancer would leave its history attributable to nothing.
--
-- The challenge VALUE is never stored. It is a secret shared with the CA for the
-- duration of a validation and it has no business outliving it; value_digest is
-- enough to tell two concurrent authorizations at one name apart, which is the
-- only thing the value was needed for here.
create table challenge_interactions (
  id               uuid primary key default uuidv7(),
  project_id       uuid,
  load_balancer_id uuid,
  alias_id         uuid,
  alias_label      text not null,
  challenge_fqdn   text not null,
  domain_names     text[] not null default '{}',
  key_id           text,
  client_ip        inet,
  -- Which name form the client sent: 'alias' (it followed our CNAME) or
  -- 'challenge' (it left the aliasing to us). Says which convention this load
  -- balancer's client follows, and nothing stronger.
  fqdn_form        text not null,
  value_digest     text not null,
  published_at     timestamptz not null default now(),
  withdrawn_at     timestamptz,
  -- Set when the sweep closes a row whose /cleanup never arrived. Distinct from
  -- a plain withdrawal because the two mean different things to an operator:
  -- "withdrawn, never queried" points at the customer's CNAME, "abandoned"
  -- points at the client that vanished mid-validation.
  abandoned        boolean not null default false,
  -- Set when the DNS server answers a query at this alias while the row is live.
  -- first_queried_at answers "did the CA ever come", query_count answers "how
  -- often" — a validation that was never queried is the signature of a CNAME the
  -- customer has not published, which is the single most common failure here.
  first_queried_at timestamptz,
  last_queried_at  timestamptz,
  query_count      bigint not null default 0
);

create index challenge_interactions_pair_published_idx
  on challenge_interactions (load_balancer_id, published_at desc);

create index challenge_interactions_project_published_idx
  on challenge_interactions (project_id, published_at desc);

-- The lookup the /cleanup path and the query-observation flush both make: the
-- live rows of one alias.
create index challenge_interactions_live_idx
  on challenge_interactions (alias_id, value_digest)
  where withdrawn_at is null;


-- ---------------------------------------------------------------------------
-- Roles, memberships and grants
-- ---------------------------------------------------------------------------

-- Authorization is data. A grant is [type, id, action] plus an inheritable flag,
-- anchored to one account. Each of the three parts admits a wildcard.
--
-- The anchoring is what preserves tenancy: a wildcard means "within this
-- account", never "everywhere". There is deliberately no grant that can name an
-- object of another account, which is why tenancy is not expressed as a grant at
-- all (design P1).

create table roles (
  id          uuid primary key default uuidv7(),
  account_id  uuid not null references accounts (id) on delete cascade,
  name        text not null,
  description text not null default '',
  -- The unrestricted role of each account: every action on every object of that
  -- account. Uneditable, undeletable, and never without a member — there must
  -- always be a way back into an account. It holds no grant rows on purpose, so
  -- its authority cannot be edited away one row at a time.
  unrestricted boolean not null default false,
  created_at  timestamptz not null default now(),
  unique (account_id, name),
  unique (id, account_id)
);

-- At most one unrestricted role per account, enforced by the database rather
-- than by the code that creates accounts.
create unique index roles_one_unrestricted_per_account
  on roles (account_id) where unrestricted;

create table account_members (
  id         uuid primary key default uuidv7(),
  account_id uuid not null references accounts (id) on delete cascade,
  user_id    uuid not null references users (id) on delete cascade,
  role_id    uuid not null,
  created_at timestamptz not null default now(),
  unique (account_id, user_id, role_id),
  -- The same construction as the inventory's composite keys: a membership cannot
  -- name a role belonging to a different account.
  foreign key (role_id, account_id) references roles (id, account_id) on delete cascade
);

create index account_members_user_idx on account_members (user_id);

-- Two grant tables rather than one polymorphic table with a nullable owner in
-- each column: the two subjects have different lifecycles, different write paths,
-- and different audit vocabulary, and a shared table would need a check
-- constraint saying exactly one owner is set — which is the same complexity moved
-- somewhere less obvious.
--
-- The shape is identical, so the evaluator reads both into one authz.Grant.
--
-- The check constraint is the IDENTIFIER-FORM rule, and it is the one place the
-- grammar's three shapes are written down: a wildcard (both null), one named
-- object (object_id), or a typed predicate (selector). Exactly one form, never
-- two, never a non-wildcard type with neither. The selector form is RESERVED and
-- currently unreachable — no object type defines a selector today, and
-- authz.Grant.Validate refuses a grant carrying both forms — but the column and
-- the constraint exist so that adding a type that does define one is a change to
-- the evaluator rather than a migration of every grant already written.
create table role_grants (
  id           uuid primary key default uuidv7(),
  role_id      uuid not null references roles (id) on delete cascade,
  account_id   uuid not null references accounts (id) on delete cascade,
  object_type  text not null,
  object_id    uuid,
  selector     jsonb,
  action       text not null,
  inheritable  boolean not null default false,
  created_at   timestamptz not null default now(),
  constraint role_grants_identifier_form check (
    (object_id is null and selector is null)     -- wildcard
    or (object_id is not null and selector is null)
    or (object_id is null and selector is not null)
  )
);

create index role_grants_role_idx on role_grants (role_id);

create table api_key_grants (
  id           uuid primary key default uuidv7(),
  api_key_id   uuid not null references api_keys (id) on delete cascade,
  account_id   uuid not null references accounts (id) on delete cascade,
  object_type  text not null,
  object_id    uuid,
  selector     jsonb,
  action       text not null,
  inheritable  boolean not null default false,
  created_at   timestamptz not null default now(),
  constraint api_key_grants_identifier_form check (
    (object_id is null and selector is null)
    or (object_id is not null and selector is null)
    or (object_id is null and selector is not null)
  )
);

create index api_key_grants_key_idx on api_key_grants (api_key_id);

-- role_grants deliberately has no unique constraint. The same authority may
-- legitimately be granted twice by different paths, and it is the subset rule
-- (design P10) rather than the schema that decides whether a grant is allowed.

-- The default role set a new team account is seeded with lives in
-- store.DefaultRoles, not here: it is the account-creation path's business, and
-- a fresh database has no accounts to seed.


-- ---------------------------------------------------------------------------
-- Console sessions
-- ---------------------------------------------------------------------------

-- Opaque server-side state; no provider tokens are kept (design D10). The groups
-- claim is snapshotted as it was at sign-in.
--
-- selected_account_id is the one account this session is acting in (design P19).
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
create table sessions (
  id                  text primary key,
  subject             text not null,
  display_name        text not null,
  groups              text[] not null default '{}',
  selected_account_id uuid references accounts (id) on delete set null,
  created_at          timestamptz not null default now(),
  last_seen_at        timestamptz not null default now(),
  expires_at          timestamptz not null,
  revoked_at          timestamptz
);

create index sessions_subject_idx          on sessions (subject);
create index sessions_expires_at_idx       on sessions (expires_at);
create index sessions_selected_account_idx on sessions (selected_account_id);


-- ---------------------------------------------------------------------------
-- Audit trail
-- ---------------------------------------------------------------------------

-- Append-only, with denormalized labels that survive deletion of the object they
-- describe. account_id, project_id and object_id are deliberately not foreign
-- keys, for the same reason challenge_interactions has none.
create table audit_entries (
  id            uuid primary key default uuidv7(),
  occurred_at   timestamptz not null default now(),
  actor_subject text,
  actor_name    text,
  actor_groups  text[],
  actor_roles   text[],
  actor_source  text not null,
  request_id    text,
  client_ip     inet,
  action        text not null,
  object_type   text not null,
  object_id     uuid,
  object_label  text not null,
  account_id    uuid,
  project_id    uuid,
  before        jsonb,
  after         jsonb
);

create index audit_entries_occurred_at_idx            on audit_entries (occurred_at desc);
create index audit_entries_project_id_occurred_at_idx on audit_entries (project_id, occurred_at desc);
create index audit_entries_object_idx                 on audit_entries (object_type, object_id);
-- A tenant's reading of their own trail is confined like every other read.
create index audit_entries_account_occurred_idx       on audit_entries (account_id, occurred_at desc);

-- The trail is append-only IN THE DATABASE, not only in the Go code.
--
-- DO NOT WIDEN THIS WHILE DEBUGGING. If a row is wrong, the fix is another row.
-- A trail that anyone with the service's credentials can rewrite answers the
-- question "what happened" with "whatever the last writer wanted", which is the
-- one answer that makes the whole table pointless.
--
-- It is enforced twice, because either half alone has a gap:
--
--   1. REVOKE UPDATE/DELETE/TRUNCATE, leaving INSERT and SELECT. This holds even
--      against SQL that never goes through this service's code, and it does take
--      effect against the owner — a revoke from the owner is real. What bypasses
--      it is a SUPERUSER connection, which store.CheckTenantIsolation refuses to
--      start under for exactly this reason.
--   2. A trigger that refuses UPDATE and DELETE outright, so the refusal does not
--      depend on which role happens to be connected.
--
-- A schema migration may lift both for the length of one statement — that is how
-- this table would gain a column — and application code may not. The protection
-- exists so nobody rewrites history THROUGH THE SERVICE, not so the table can
-- never change shape.
revoke update, delete, truncate on audit_entries from public;

do $$
begin
  execute format('revoke update, delete, truncate on audit_entries from %I', current_user);
  execute format('grant insert, select on audit_entries to %I', current_user);
end
$$;

create or replace function audit_entries_append_only() returns trigger
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


-- ---------------------------------------------------------------------------
-- Reporting
-- ---------------------------------------------------------------------------

-- Inventory size over time, one row per project per day (design D6).
--
-- This is the ONE place in the reporting capability where something is stored
-- that could otherwise be derived. Everything else — challenge attempts,
-- issuance attempts, outcomes — is aggregated on read from
-- challenge_interactions, because that table is a history and history can be
-- re-read. Inventory is not: `domains` and `aliases` are mutable rows with no
-- deletion record, so "how many labels did this project have in March" is
-- unanswerable in April unless somebody wrote it down in March.
--
-- The row is a LEVEL, not an increment: it says what existed on that day, not
-- what changed. That is what makes the daily job idempotent — see
-- internal/reporting/snapshot.go, which upserts on (project_id, day), so a
-- restart, a second run, or three runs an hour apart all leave exactly one row
-- holding the most recent reading for the day.
--
-- project_slug is denormalized for the same reason as everywhere else in this
-- schema: the trend of a project that has since been deleted is still a readable
-- series rather than a column of bare integers. No foreign key, on purpose.
create table project_daily_stats (
  project_id          uuid   not null,
  project_slug        text   not null,
  day                 date   not null,
  alias_count         int    not null,
  domain_count        int    not null,
  load_balancer_count int    not null,
  primary key (project_id, day)
);

-- The report reads one project over a range, which the primary key already
-- serves. This second index is for the other direction — "every project on a
-- given day" — which is what a fleet-wide snapshot listing or a retention sweep
-- would ask for.
create index project_daily_stats_day_idx on project_daily_stats (day);


-- ---------------------------------------------------------------------------
-- Row-level security
-- ---------------------------------------------------------------------------
--
-- Tenant isolation enforced by the database, not only by the query (design P20).
--
-- Scope is a Go parameter, so a store method written without it does not compile.
-- That is half the problem. The other half is a query that HAS a scope and does
-- not use it — whether the WHERE clause includes account_id is a property of each
-- query's text, upheld by review — and a read that never goes through the store
-- package at all. These policies close both: the predicate lives on the table, so
-- a query that forgets it returns nothing.
--
-- FORCE IS NOT OPTIONAL. A table's owner bypasses row-level security unless the
-- table is forced, and this service owns the tables it migrates. Forget FORCE on
-- one table and its policy becomes decoration — enabled, visible in the catalogue,
-- and enforcing nothing against the only role that connects. That failure is
-- silent in both directions: no error, and a cross-account read that succeeds.
-- The schema golden file records `enabled` and `forced` separately for every
-- table so a missing FORCE is a diff rather than a discovery.

do $$
declare t text;
begin
  foreach t in array array[
    'projects', 'load_balancers', 'lb_nodes', 'aliases', 'domains', 'api_keys',
    'roles', 'role_grants', 'api_key_grants', 'account_members']
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

-- challenges carries no account_id, and gets a policy anyway by INHERITING one.
--
-- A policy does not need a column on the row. It needs a way to decide, and "is
-- my parent visible" is one — because a subquery inside a policy is evaluated
-- under the CALLER's policies, so the aliases policy applies inside this one and
-- the tenant filter propagates down from there. No cycle: challenges references
-- aliases, and nothing aliases references references challenges, so this is a
-- chain rather than the recursion Postgres refuses.
--
-- The cost argument is the reverse of the one that put account_id on the
-- inventory tables. There, a joined policy would have cost a per-row probe on
-- every listing and would have stopped any index leading with the tenant. Here
-- the hot path (POST /present, on a certificate-issuance critical path) touches
-- ONE row, so the policy is one index lookup on the aliases primary key per
-- publish — and the read-count test on that path is what will notice if that ever
-- stops being true.
alter table challenges enable row level security;
alter table challenges force row level security;

create policy tenant_isolation on challenges
  using (
    cross_account_read()
    or exists (select 1 from aliases a where a.id = challenges.alias_id)
  )
  with check (
    cross_account_read()
    or exists (select 1 from aliases a where a.id = challenges.alias_id)
  );

-- challenge_interactions gets NO POLICY, deliberately, and it is the one table
-- where that is a considered decision rather than an oversight.
--
-- It holds no foreign key, because a history that disappears when its subject is
-- deleted cannot answer the question it exists for. A policy of the shape above
-- would therefore make every interaction whose alias has been deleted invisible —
-- destroying exactly the property the table was built for. What confines a read
-- of it is the query: store.ListInteractions resolves the load balancer or the
-- alias under a Scope first, and that resolution IS policied.
--
-- The remaining unpolicied tables are unpolicied because they are not tenant
-- data. users is global (design P18): a user exists before belonging to any
-- account and may belong to several, and what confines a listing of people is
-- account_members, which is policied. accounts, sessions and audit_entries are
-- read through paths that name the account explicitly. api_key_networks and
-- project_daily_stats hang off a row that is itself resolved under a scope.
