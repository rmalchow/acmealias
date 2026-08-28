-- The grant identifier grammar loses its third form.
--
-- The baseline reserved a `selector` column on both grant tables: a typed
-- predicate identifier, for an object type whose members are picked by a rule
-- rather than named one at a time. It existed for dns_record, which went with
-- the DNS product, and was left admitted by the check constraint but refused by
-- authz.Grant.Validate on the theory that re-adding a predicate identifier would
-- then be an evaluator change rather than a migration.
--
-- That theory does not survive contact: a real predicate identifier needs
-- fields, a compiler into a SQL predicate and a containment rule, none of which
-- an empty jsonb column supplied. What was actually being kept was a form no
-- writer could produce, that every reader still had to branch on, and that the
-- constraint advertised as legal. A grammar with a form nothing can mean is a
-- grammar somebody eventually stores a row against.
--
-- No data is dropped by intent: the column is unreachable through every write
-- path, so it is NULL on every row. If some row disagrees, this migration must
-- fail loudly rather than discard what that row meant — which is why the
-- constraint is tightened to "selector is null" BEFORE the column goes. That
-- ALTER validates the existing rows and refuses, naming the table, where
-- dropping the column first would delete the value in silence.
--
-- Dropping the column takes the constraint with it, which is correct and not an
-- oversight: what is left of the identifier-form rule is "a wildcard (object_id
-- null) or one named object (object_id set)", and that is a nullable column
-- stating itself. A CHECK true for every possible row is a comment pretending to
-- be an invariant.
--
-- Rejected: keeping the column and only tightening the constraint. That leaves a
-- dead column in the schema golden and in both ORM structs — the same confusion
-- at a lower price.

alter table role_grants
  drop constraint role_grants_identifier_form,
  add constraint role_grants_identifier_form check (selector is null);

alter table api_key_grants
  drop constraint api_key_grants_identifier_form,
  add constraint api_key_grants_identifier_form check (selector is null);

alter table role_grants drop column selector;
alter table api_key_grants drop column selector;
