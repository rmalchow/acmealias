-- Remove the two challenge grants from the two roles that were repaired.
--
-- This mirrors 000009's own down migration: going back past this point means
-- going back to a chain where 000009 is expected to own those rows, and leaving
-- them here would double them when the chain is re-applied.
set local app.cross_account = 'on';

delete from role_grants g
 using roles r
 where g.role_id = r.id
   and r.name in ('CONFIG_ADMIN', 'KEY_ROTATION')
   and g.object_type = '*'
   and g.object_id is null
   and g.action in ('challenge.publish', 'challenge.withdraw');

reset app.cross_account;
