-- Reverses 000002: the index first, then the columns, so that a dependency this
-- file has not accounted for fails here rather than being dropped silently along
-- with the columns.
--
-- The backfilled activations are not recoverable, and nothing tries to recover
-- them: reverting this migration removes the rule those timestamps served.

drop index if exists domains_activation_due_idx;

alter table domains
  drop column if exists last_success_at,
  drop column if exists last_checked_at;
