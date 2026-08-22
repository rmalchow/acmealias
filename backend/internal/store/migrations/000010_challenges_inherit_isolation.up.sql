-- challenges gets a policy after all, by inheriting one.
--
-- Design P20 left this table out, and the reason given was sound as far as it
-- went: challenges carries no account_id, a challenge row is only ever reached
-- through an alias that was itself fetched under a scope, and POST /present is
-- on a certificate-issuance critical path that must not get slower.
--
-- What that reasoning missed is that a policy does not need a column on the row.
-- It needs a way to decide, and "is my parent visible" is one — because a
-- subquery inside a policy is evaluated under the CALLER's policies, so the
-- aliases policy applies inside this one and the tenant filter propagates down
-- from there. No cycle: challenges references aliases, aliases references
-- nothing that references challenges, so this is a chain rather than the
-- recursion Postgres refuses.
--
-- The cost argument is the reverse of the one that kept account_id on the
-- inventory tables. There, a joined policy would have cost a per-row probe on
-- every listing and would have stopped any index leading with the tenant, which
-- is why P2 materialized the column. Here the hot path touches ONE row, so the
-- policy is one index lookup on the aliases primary key per publish — and the
-- read-count test on that path is what will notice if that ever stops being
-- true.
--
-- challenge_interactions still gets nothing, and now for a reason of its own
-- rather than by association: it deliberately holds NO foreign key, because a
-- history that disappears when its subject is deleted cannot answer the question
-- it exists for. A policy of this shape would make every interaction whose alias
-- has been deleted invisible — destroying exactly the property that table was
-- built for.

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
