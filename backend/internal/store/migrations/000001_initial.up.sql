-- Inventory: projects -> load balancers -> domains, with aliases keyed by challenge name (design D4).

create table projects (
  id          bigserial primary key,
  slug        text not null unique,
  name        text not null,
  created_at  timestamptz not null default now()
);

create table load_balancers (
  id          bigserial primary key,
  project_id  bigint not null references projects(id) on delete restrict,
  slug        text not null,
  name        text not null,
  created_at  timestamptz not null default now(),
  unique (project_id, slug)
);

create table lb_nodes (
  id          bigserial primary key,
  load_balancer_id  bigint not null references load_balancers(id) on delete cascade,
  label       text not null,
  ipv4        inet,
  ipv6        inet,
  unique (load_balancer_id, label)
);

create table aliases (
  id              bigserial primary key,
  load_balancer_id      bigint not null references load_balancers(id) on delete restrict,
  challenge_fqdn  text not null unique,
  label           text not null unique,
  created_at      timestamptz not null default now()
);

create table domains (
  id          bigserial primary key,
  load_balancer_id  bigint not null references load_balancers(id) on delete restrict,
  alias_id    bigint not null references aliases(id) on delete restrict,
  name        text not null unique,
  created_at  timestamptz not null default now()
);

create index domains_alias_id_idx on domains (alias_id);
create index aliases_load_balancer_id_idx on aliases (load_balancer_id);

-- API keys (design D3). The secret is never stored; only its SHA-256 digest is.

create table api_keys (
  id              bigserial primary key,
  load_balancer_id      bigint not null references load_balancers(id) on delete cascade,
  identifier      text not null unique,
  secret_hash     bytea not null,
  display_prefix  text not null,
  note            text not null default '',
  created_at      timestamptz not null default now(),
  expires_at      timestamptz,
  last_used_at    timestamptz,
  disabled_at     timestamptz
);

create index api_keys_load_balancer_id_idx on api_keys (load_balancer_id);

create table api_key_networks (
  id          bigserial primary key,
  api_key_id  bigint not null references api_keys(id) on delete cascade,
  cidr        cidr not null,
  unique (api_key_id, cidr)
);

-- Challenge records (design D6). published_by is the key *identifier* as text, not a
-- foreign key, so that deleting a key neither withdraws its in-flight challenges nor
-- loses the attribution.

create table challenges (
  id            bigserial primary key,
  alias_id      bigint not null references aliases(id) on delete cascade,
  value         text not null,
  published_by  text,
  created_at    timestamptz not null default now(),
  expires_at    timestamptz not null,
  unique (alias_id, value)
);

create index challenges_expires_at_idx on challenges (expires_at);

-- Console sessions (design D10). Opaque server-side state; no provider tokens are kept.
-- The groups claim is snapshotted as it was at sign-in.

create table sessions (
  id            text primary key,
  subject       text not null,
  display_name  text not null,
  groups        text[] not null default '{}',
  created_at    timestamptz not null default now(),
  last_seen_at  timestamptz not null default now(),
  expires_at    timestamptz not null,
  revoked_at    timestamptz
);

create index sessions_subject_idx on sessions (subject);
create index sessions_expires_at_idx on sessions (expires_at);

-- Audit trail: append-only, with denormalized labels that survive deletion of the
-- object they describe. project_id and object_id are deliberately not foreign keys.

create table audit_entries (
  id            bigserial primary key,
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
  object_id     bigint,
  object_label  text not null,
  project_id    bigint,
  before        jsonb,
  after         jsonb
);

create index audit_entries_occurred_at_idx on audit_entries (occurred_at desc);
create index audit_entries_project_id_occurred_at_idx on audit_entries (project_id, occurred_at desc);
create index audit_entries_object_idx on audit_entries (object_type, object_id);
