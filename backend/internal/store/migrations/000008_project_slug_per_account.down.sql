-- Reversing this can FAIL, and that is correct rather than a defect: once two
-- accounts each hold a project called `prod`, there is no global-unique state to
-- return to and no basis on which this migration could pick which one keeps the
-- name. It succeeds only while no such pair exists.
alter table projects drop constraint projects_account_id_slug_key;
alter table projects add constraint projects_slug_key unique (slug);
