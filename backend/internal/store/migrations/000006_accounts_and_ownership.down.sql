drop index if exists audit_entries_account_occurred_idx;
alter table audit_entries drop column account_id;

drop index if exists projects_account_id_idx;
drop index if exists api_keys_project_id_idx;
drop index if exists api_keys_account_id_idx;
drop index if exists domains_project_id_idx;
drop index if exists domains_account_id_idx;
drop index if exists aliases_project_id_idx;
drop index if exists aliases_account_id_idx;
drop index if exists lb_nodes_project_id_idx;
drop index if exists lb_nodes_account_id_idx;
drop index if exists load_balancers_account_id_idx;

alter table domains drop constraint domains_account_id_name_key;
alter table domains add constraint domains_name_key unique (name);

alter table api_keys drop constraint api_keys_lb_project_fkey;
alter table api_keys drop constraint api_keys_lb_account_fkey;
alter table api_keys drop column project_id;
alter table api_keys drop column account_id;

alter table domains drop constraint domains_alias_account_fkey;
alter table domains drop constraint domains_lb_project_fkey;
alter table domains drop constraint domains_lb_account_fkey;
alter table domains drop column project_id;
alter table domains drop column account_id;

alter table aliases drop constraint aliases_id_account_id_key;
alter table aliases drop constraint aliases_lb_project_fkey;
alter table aliases drop constraint aliases_lb_account_fkey;
alter table aliases drop column project_id;
alter table aliases drop column account_id;

alter table lb_nodes drop constraint lb_nodes_lb_project_fkey;
alter table lb_nodes drop constraint lb_nodes_lb_account_fkey;
alter table lb_nodes drop column project_id;
alter table lb_nodes drop column account_id;

alter table load_balancers drop constraint load_balancers_id_project_id_key;
alter table load_balancers drop constraint load_balancers_id_account_id_key;
alter table load_balancers drop constraint load_balancers_project_account_fkey;
alter table load_balancers drop column account_id;

alter table projects drop constraint projects_id_account_id_key;
alter table projects drop constraint projects_account_id_fkey;
alter table projects drop column account_id;

drop table accounts;
