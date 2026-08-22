drop policy if exists tenant_isolation on challenges;
alter table challenges no force row level security;
alter table challenges disable row level security;
