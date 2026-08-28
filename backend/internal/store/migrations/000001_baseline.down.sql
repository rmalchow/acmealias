-- Reverses the baseline: drop everything it creates, and nothing else.
--
-- Tables go in reverse dependency order rather than with CASCADE, so that a
-- reference this file has not accounted for fails here instead of being silently
-- dropped along with the table it points at. Functions go last, because the
-- policies and triggers that call them must be gone first.

drop table if exists project_daily_stats;
drop table if exists audit_entries;
drop table if exists sessions;
drop table if exists api_key_grants;
drop table if exists role_grants;
drop table if exists account_members;
drop table if exists roles;
drop table if exists challenge_interactions;
drop table if exists challenges;
drop table if exists api_key_networks;
drop table if exists api_keys;
drop table if exists domains;
drop table if exists aliases;
drop table if exists lb_nodes;
drop table if exists load_balancers;
drop table if exists projects;
drop table if exists accounts;
drop table if exists users;

drop function if exists accounts_refuse_reclassification();
drop function if exists audit_entries_append_only();
drop function if exists cross_account_read();
drop function if exists current_account();
drop function if exists legacy_uuid(bigint);
drop function if exists uuidv7();
