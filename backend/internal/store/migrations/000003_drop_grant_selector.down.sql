-- Reverses 000003, restoring the reserved predicate identifier exactly as the
-- baseline wrote it: the column, then the three-form check constraint.
--
-- Nothing is restored INTO the column, because nothing was ever in it — the form
-- was unreachable through every write path, so re-adding it as NULL everywhere
-- is the whole of the old state.

alter table role_grants add column selector jsonb;
alter table api_key_grants add column selector jsonb;

alter table role_grants
  add constraint role_grants_identifier_form check (
    (object_id is null and selector is null)     -- wildcard
    or (object_id is not null and selector is null)
    or (object_id is null and selector is not null)
  );

alter table api_key_grants
  add constraint api_key_grants_identifier_form check (
    (object_id is null and selector is null)
    or (object_id is not null and selector is null)
    or (object_id is null and selector is not null)
  );
