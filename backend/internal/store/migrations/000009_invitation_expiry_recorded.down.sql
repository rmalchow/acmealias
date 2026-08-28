drop index if exists invitations_expiry_unrecorded_idx;
alter table invitations drop column expiry_recorded_at;
