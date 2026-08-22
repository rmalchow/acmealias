drop index if exists sessions_selected_account_idx;
alter table sessions drop column selected_account_id;
