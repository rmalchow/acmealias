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
-- schema: the trend of a project that has since been deleted is still a
-- readable series rather than a column of bare integers. No foreign key, on
-- purpose.

create table project_daily_stats (
  project_id          bigint not null,
  project_slug        text   not null,
  day                 date   not null,
  alias_count         int    not null,
  domain_count        int    not null,
  load_balancer_count int    not null,
  primary key (project_id, day)
);

-- The report reads one project over a range, which the primary key already
-- serves. This second index is for the other direction — "every project on a
-- given day" — which is what a fleet-wide snapshot listing or a retention
-- sweep would ask for.
create index project_daily_stats_day_idx on project_daily_stats (day);
