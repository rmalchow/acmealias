-- Activation state: a domain publishes nothing until a check has SEEN its
-- delegation (change domain-activation, design A1).
--
-- Exactly two nullable columns, and every question is answered by deriving from
-- them rather than by a column of its own:
--
--   is it active?            last_success_at is not null
--   how often to check it?   last_success_at is null -> minutes, else daily (A2)
--   how long has it been broken?  now() - last_success_at
--   did we look recently?    last_checked_at
--
-- Rejected: an `activated` boolean, and a consecutive-failure counter. Both are
-- restatements of last_success_at, and the first bug is the row where they
-- disagree. The counter is the worse of the two: it resets on any success, so it
-- answers "how many times in a row" when the question a human actually asks is
-- "since when" — which the timestamp answers exactly.
--
-- The columns land on domains and not on aliases, even though the delegation is
-- an alias-shaped fact, because example.com and *.example.com collapse onto one
-- alias (dnsname.ChallengeName strips the "*." prefix). One observation
-- activates both rows either way; putting the state on the alias would make them
-- share a fate, which is correct today and silently wrong the moment anything
-- gives a domain its own alias (A1).

alter table domains
  add column last_checked_at timestamptz,
  add column last_success_at timestamptz;

-- The worker's only query is "which domains are due a check", run across the
-- whole deployment on every tick, so it is a scan this table should not have to
-- do sequentially. Ordering by last_checked_at with nulls first is exactly
-- "never looked at, then longest ago"; last_success_at rides along as a second
-- key because it is what chooses between the two cadences, so the predicate is
-- decided from the index entry.
--
-- Not index-only, and the comment this replaced claimed otherwise: the query
-- joins aliases, groups by alias id and orders by aggregates, so it needs
-- alias_id and visits the heap regardless. What the index buys is the
-- selection, not the sort.
create index domains_activation_due_idx
  on domains (last_checked_at nulls first, last_success_at);

-- Every domain that already exists is activated by this migration. Those rows
-- were added under a rule that did not exist yet, and refusing their /present
-- calls the moment this deploys would be a regression dressed as a feature
-- (task 1.3). last_checked_at deliberately stays NULL: we have not looked at
-- them, and stamping it would be a claim that no lookup backs — it also leaves
-- them at the front of the due queue above, so the worker's first pass reports
-- the truth about them.
--
-- The backfill has to lift row-level security, and that is not belt-and-braces.
-- `domains` is FORCE row level security, so the migrating role does not escape
-- its own table's policy, and that policy is
-- `cross_account_read() or account_id = current_account()`. With no session
-- settings the first half is false and the second is NULL, so the predicate is
-- NULL for every row: a bare UPDATE here matches nothing, raises no error, and
-- records the migration as applied. The service refuses to start as a superuser
-- or a BYPASSRLS role (store.CheckTenantIsolation), so the only deployments in
-- which a bare UPDATE would have worked are the ones that cannot boot.
--
-- set_config's third argument is `true` (is_local), so the lift is undone when
-- this block's transaction ends and cannot leak into whatever the migration
-- connection does next. This is the same escape store.CrossAccount uses.
do $$
begin
  perform set_config('app.cross_account', 'on', true);
  update domains set last_success_at = now();
end
$$;
