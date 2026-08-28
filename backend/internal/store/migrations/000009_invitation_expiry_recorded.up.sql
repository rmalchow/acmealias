-- The marker that makes an invitation's expiry RECORDABLE exactly once
-- (account-invitations task 9.3).
--
-- Read the comment on model.Invitation before this one: it says there is
-- deliberately no `expired_at`, because "expiry is ExpiresAt being in the past,
-- evaluated whenever the row is read, so an invitation is dead the moment it is
-- dead rather than when a sweep last ran". That decision stands and this column
-- does not touch it.
--
-- THIS IS NOT A STATUS COLUMN AND MUST NEVER BE READ AS ONE. Nothing decides
-- whether an invitation is usable by looking here — `store.usableInvitation`
-- compares ExpiresAt to now and always will. What this records is whether the
-- AUDIT ENTRY for that expiry has been written, which is a fact about our own
-- bookkeeping rather than about the invitation.
--
-- It exists because the sweep needs somewhere to remember what it has already
-- accounted for, exactly as `api_keys.disabled_at` does for apikey.expire. The
-- alternative — deriving it — cannot work: "expired and not yet recorded" is not
-- a function of ExpiresAt alone, so without a mark every pass would write the
-- entry again for every invitation that ever lapsed.
alter table invitations add column expiry_recorded_at timestamptz;

comment on column invitations.expiry_recorded_at is
  'When the invitation.expire audit entry was written. Bookkeeping only: it never decides whether an invitation is usable.';

-- Partial, because the query it serves is "expired, never recorded" and that set
-- empties itself: every row the sweep touches leaves it.
create index invitations_expiry_unrecorded_idx
  on invitations (expires_at)
  where expiry_recorded_at is null;
