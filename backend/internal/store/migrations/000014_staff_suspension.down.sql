-- Drop the staff suspension.
--
-- Going back one migration UNSUSPENDS every account staff had stopped, and there
-- is no way for a down migration to avoid that: the state it would have to
-- preserve is the column being dropped. Recorded here rather than worked around,
-- because the alternative — keeping the column and only removing its uses —
-- leaves a schema the previous binary's drift test refuses.
set local app.cross_account = 'on';

alter table accounts drop column if exists staff_suspended;

reset app.cross_account;
