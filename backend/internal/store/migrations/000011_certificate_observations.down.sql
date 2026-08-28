-- The table goes, and with it the policy and the index — dropping a table takes
-- both, so neither is named here.
drop table certificate_observations;

-- The unique keys the child needed. They go too: they exist only so a composite
-- foreign key had something to reference, and leaving them behind would leave two
-- indexes nothing reads.
alter table lb_nodes drop constraint lb_nodes_id_project_id_key;
alter table domains
  drop constraint domains_id_project_id_key,
  drop constraint domains_id_account_id_key;
