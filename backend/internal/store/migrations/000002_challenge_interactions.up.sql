-- Challenge interactions: what a load balancer actually did at this service, and
-- whether the CA ever came to look.
--
-- One row per PUBLICATION, not per HTTP request. A /present is the start of an
-- interaction and the matching /cleanup is its end, so a row is inserted by the
-- first and completed by the second — which is what makes "published at 14:02,
-- queried twice, withdrawn at 14:02:31" one line on a screen rather than three
-- rows to correlate by eye.
--
-- Denormalized on purpose, exactly like audit_entries above: alias_label,
-- challenge_fqdn and domain_names are copied in at publication and there are no
-- foreign keys to aliases or domains. A record of what happened must survive the
-- deletion of the objects it happened to; a row that vanishes when a domain is
-- removed cannot answer "what was this key doing last month".
--
-- load_balancer_id IS kept (and indexed) because the console reads this per load balancer, and
-- project_id beside it, both as plain bigints: deleting a load balancer leaves its history
-- readable, and without the project copied in, that history could no longer be
-- attributed to anything at all once its load balancer was gone.
--
-- The challenge VALUE is never stored. It is a secret shared with the CA for the
-- duration of a validation and it has no business outliving it; value_digest is
-- enough to tell two concurrent authorizations at one name apart, which is the
-- only thing the value was needed for here.

create table challenge_interactions (
  id             bigserial primary key,
  project_id     bigint,
  load_balancer_id     bigint,
  alias_id       bigint,
  alias_label    text not null,
  challenge_fqdn text not null,
  domain_names   text[] not null default '{}',
  key_id         text,
  client_ip      inet,
  -- Which name form the client sent: 'alias' (it followed our CNAME) or
  -- 'challenge' (it left the aliasing to us). Says which convention this load balancer's
  -- client follows, and nothing stronger.
  fqdn_form      text not null,
  value_digest   text not null,
  published_at   timestamptz not null default now(),
  withdrawn_at   timestamptz,
  -- Set when the sweep closes a row whose /cleanup never arrived. Distinct from
  -- a plain withdrawal because the two mean different things to an operator:
  -- "withdrawn, never queried" points at the customer's CNAME, "abandoned"
  -- points at the client that vanished mid-validation.
  abandoned      boolean not null default false,
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
