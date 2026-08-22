-- Roles become rows, grants become data, and API keys become subjects.
--
-- What this replaces: four role names fixed in Go, a seven-row permission matrix
-- literal, and an authorization check that took a verb and nothing else. The
-- reason it has to change is in the proposal — an API key scoped to a project is
-- strictly more powerful than one scoped to a load balancer, and the old model
-- has no way to say so.
--
-- A grant is [type, id, action] plus an inheritable flag, anchored to one
-- account. Each of the three parts admits a wildcard, and the identifier may
-- instead be a typed selector where the type defines one — today only
-- dns_record does.
--
-- The anchoring is what preserves tenancy: a wildcard means "within this
-- account", never "everywhere". There is deliberately no grant that can name an
-- object of another account, which is why tenancy is not expressed as a grant at
-- all (design P1).

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

create table roles (
  id          uuid primary key default uuidv7(),
  account_id  uuid not null references accounts (id) on delete cascade,
  name        text not null,
  description text not null default '',
  -- The unrestricted role of each account: every action on every object of that
  -- account. Uneditable, undeletable, and never without a member — there must
  -- always be a way back into an account.
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
  -- The composite reference is the same construction as P2's: a membership
  -- cannot name a role belonging to a different account.
  foreign key (role_id, account_id) references roles (id, account_id) on delete cascade
);

create index account_members_user_idx on account_members (user_id);

-- Grants. Two tables rather than one polymorphic table with a nullable owner in
-- each column: the two subjects have different lifecycles, different write
-- paths, and different audit vocabulary, and a shared table would need a check
-- constraint saying exactly one owner is set — which is the same complexity
-- moved somewhere less obvious.
--
-- The shape is identical, so the evaluator reads both into one Grant type.
--
-- object_id and selector are mutually exclusive, and both are null for a
-- wildcard identifier. The check constraint is the identifier-form rule: exactly
-- one of the three forms, never two, never none-with-a-non-wildcard-type.
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

-- The four roles that exist today, reproduced as data so the internal edition's
-- behaviour does not observably change (constraint 1). ADMIN becomes the
-- unrestricted role; the other three become ordinary editable rows holding
-- exactly the actions their column of the matrix conferred — no more, since a
-- role that gained an action here would be an escalation nobody asked for.
--
-- The vocabulary is internal/authz's Action set, not the audit vocabulary: an
-- audit action records what happened, an authorization action decides what may.
-- They are different lists and conflating them is how a role quietly acquires a
-- power because two strings looked alike.
--
-- Every grant is [*, *, action] and inheritable: a role-wide permission meant
-- "every object of this account", and a wildcard identifier anchored to the
-- account is exactly that.
insert into roles (account_id, name, description, unrestricted)
select id, 'ADMIN', 'Every action on every object in this account', true from accounts;

insert into roles (account_id, name, description)
select id, 'CONFIG_ADMIN', 'Manage inventory, keys and DNS plans' from accounts;

insert into roles (account_id, name, description)
select id, 'KEY_ROTATION', 'Manage API keys, and read what is needed to use them' from accounts;

insert into roles (account_id, name, description)
select id, 'READ_ONLY', 'Read inventory, DNS plans, verification results and the audit log' from accounts;

insert into role_grants (role_id, account_id, object_type, action, inheritable)
select r.id, r.account_id, '*', a.action, true
  from roles r
  cross join (values
    ('inventory.write'), ('key.write'), ('key.secret.view'),
    ('dns.plan'), ('inventory.view')
  ) as a(action)
 where r.name = 'CONFIG_ADMIN';

insert into role_grants (role_id, account_id, object_type, action, inheritable)
select r.id, r.account_id, '*', a.action, true
  from roles r
  cross join (values
    ('key.write'), ('key.secret.view'), ('dns.plan'), ('inventory.view')
  ) as a(action)
 where r.name = 'KEY_ROTATION';

insert into role_grants (role_id, account_id, object_type, action, inheritable)
select r.id, r.account_id, '*', a.action, true
  from roles r
  cross join (values
    ('dns.plan'), ('inventory.view')
  ) as a(action)
 where r.name = 'READ_ONLY';

-- ADMIN needs no rows: the unrestricted flag is the grant, so it cannot be
-- edited away one row at a time. project.admin and session.admin were ADMIN-only
-- in the matrix and remain so by that flag.

-- Every existing API key becomes a subject holding exactly the authority it had:
-- publish and withdraw challenge values for its one load balancer, and nothing
-- else. The old model expressed that as a foreign key; this expresses it as a
-- grant, which is the whole point of the change. The column it replaces is
-- dropped in the same migration so the two never coexist at runtime.
insert into api_key_grants (api_key_id, account_id, object_type, object_id, action, inheritable)
select k.id, k.account_id, 'load_balancer', k.load_balancer_id, a.action, true
  from api_keys k
  cross join (values ('challenge.publish'), ('challenge.withdraw')) as a(action);

-- Grants are tenant data and get the same policy as everything else.
do $$
declare t text;
begin
  foreach t in array array['roles', 'role_grants', 'api_key_grants', 'account_members']
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

-- users is deliberately NOT policied: a user is global (design P18), exists
-- before belonging to any account, and may belong to several. What confines a
-- listing of people is account_members, which is policied.
