-- One row per TLS handshake this service attempted, successful or not (change
-- domain-diagnostics, design G6 and G11).
--
-- A FAILED HANDSHAKE IS A ROW, NOT A MISSING ROW, and that is the shape decision
-- the whole table rests on. "There is no certificate here" and "we could not
-- look" are different sentences with different audiences, and a gap in this
-- table is indistinguishable from a scheduler that did not run. So `outcome` is
-- never null, the certificate columns are, and every read below can tell a
-- refusal from a silence.
--
-- The grain is (domain, node, family), not (domain) and not (node). A node
-- reached over v4 and over v6 goes through different transit and different
-- firewalls, and a certificate renewed on one listener and not the other is
-- invisible to a dual-stack visitor — including the customer checking in their
-- own browser (G6).


-- ---------------------------------------------------------------------------
-- The keys the composite foreign keys below need to reference
-- ---------------------------------------------------------------------------
--
-- `domains` and `lb_nodes` are the two tables in this schema that never became
-- anybody's parent, so neither carries the `unique (id, account_id)` /
-- `unique (id, project_id)` pair that `projects` and `aliases` have had since
-- the baseline. A composite foreign key can only reference a unique key, so the
-- pair has to exist before the child can name it.
--
-- These are not redundant with the primary key even though id alone is already
-- unique. Postgres requires the referenced columns to be exactly a unique
-- constraint, and the whole value of the composite key is that it names the
-- ownership column too — that is what makes the denormalisation checked rather
-- than hoped for.
alter table domains
  add constraint domains_id_account_id_key unique (id, account_id),
  add constraint domains_id_project_id_key unique (id, project_id);

alter table lb_nodes
  add constraint lb_nodes_id_project_id_key unique (id, project_id);


create table certificate_observations (
  id          uuid primary key default uuidv7(),
  -- The materialized ownership columns every tenant table carries (design P2,
  -- P3): the row-level security predicate below reads account_id off the row
  -- rather than through a join, and project_id is here so a grant naming a
  -- project can reach these without one either.
  account_id  uuid not null,
  project_id  uuid not null,
  domain_id   uuid not null,
  node_id     uuid not null,

  -- What was attempted. The address is dialled and the server name is sent as
  -- SNI; they are deliberately two columns, because dialling the NAME would
  -- observe whatever the customer's DNS points at today, which is the thing G6
  -- rejects. Recording both is what lets a row explain itself without a join.
  family      text not null check (family in ('A', 'AAAA')),
  address     inet not null,
  server_name text not null,

  observed_at timestamptz not null default now(),

  -- The classification, mirroring how internal/resolvercheck spells Status: our
  -- word for what happened, not the underlying error text. The check constraint
  -- is the other half of certobserve's string enum, so a sixth value cannot be
  -- written without both halves being changed together.
  outcome text not null
    check (outcome in ('observed', 'refused', 'timed-out', 'rejected', 'unreadable')),
  -- The underlying text, for the log and never for the customer.
  failure_detail text,

  -- Everything read off the certificate, null unless outcome = 'observed'.
  --
  -- covers_server_name and chain_trusted are recorded rather than derived at
  -- read time because they are facts about the exchange: the chain that was
  -- presented, judged against the pool we held at the time. Recomputing them
  -- later would need the certificate itself, which is not stored, and would
  -- silently change historical rows when a root is added or removed.
  not_before         timestamptz,
  not_after          timestamptz,
  issuer             text,
  serial             text,
  -- `not null default '{}'` rather than nullable, unlike the scalars beside it,
  -- and for a reason outside this file: model.StringArray encodes a nil slice as
  -- an EMPTY array rather than as SQL NULL, so a nullable column here could
  -- never actually hold NULL and would only offer a state nothing can write.
  -- Every other text[] in this schema is spelled this way for the same reason.
  sans               text[] not null default '{}',
  covers_server_name boolean,
  chain_trusted      boolean,

  -- ON DELETE CASCADE, and not the `restrict` this repository otherwise prefers.
  -- An observation is a fact ABOUT a domain and means nothing without it, so
  -- `restrict` would make deleting a domain fail the moment it had ever been
  -- observed — a customer unable to remove a name because we looked at it.
  -- lb_nodes already cascades from its parent for the same reason.
  --
  -- Composite, and only composite: (domain_id, account_id) referencing
  -- domains (id, account_id) is what makes the materialized account_id agree
  -- with the parent row rather than being a denormalisation nothing checks. A
  -- plain `domain_id references domains (id)` beside it is implied by this one
  -- and would only add a second name to the error message — that is 000004's
  -- reasoning about the same pair of choices, and it still holds.
  constraint certificate_observations_domain_account_fkey
    foreign key (domain_id, account_id) references domains (id, account_id) on delete cascade,
  constraint certificate_observations_domain_project_fkey
    foreign key (domain_id, project_id) references domains (id, project_id) on delete cascade,
  -- The node's project must be the domain's project, which the pair above then
  -- ties to the account: an observation cannot name a node from one project
  -- while claiming a domain in another, because the rows it references do not
  -- exist.
  constraint certificate_observations_node_project_fkey
    foreign key (node_id, project_id) references lb_nodes (id, project_id) on delete cascade
);

-- Every read in this change is either "the latest per address for one domain" or
-- "the history for one domain", and both start here. The descending stamp is the
-- half that matters: LatestObservations is a `distinct on (node_id, family)
-- ... order by ... observed_at desc`, which walks this index backwards per
-- domain instead of sorting the domain's whole history.
create index certificate_observations_domain_observed_idx
  on certificate_observations (domain_id, observed_at desc);

-- The three statements the loop at 000001_baseline.up.sql:732 would have written
-- for this table, spelled exactly as it spells them.
--
-- FORCE is not optional and not belt-and-braces: this service owns the tables it
-- migrates, and an owner bypasses row-level security unless the table is forced.
-- Forget it here and the policy is decoration — enabled, visible in the
-- catalogue, enforcing nothing against the only role that connects.
--
-- The table is policied in its own right rather than inheriting from domains the
-- way challenges inherits from aliases, because it carries account_id anyway for
-- the grant model — and a predicate that reads a column beats one that runs a
-- subquery per row on a table whose whole purpose is listing.
alter table certificate_observations enable row level security;
alter table certificate_observations force row level security;

create policy tenant_isolation on certificate_observations
  using (cross_account_read() or account_id = current_account())
  with check (cross_account_read() or account_id = current_account());
