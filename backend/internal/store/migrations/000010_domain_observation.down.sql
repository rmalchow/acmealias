-- Both columns go, and nothing is preserved on the way out.
--
-- There is nothing here worth keeping: an observation history is keyed by domain
-- and survives in certificate_observations regardless of whether the switch that
-- produced it is still remembered. What is lost by going down is the customer's
-- opt-in, which is exactly what should be lost — coming back up with the switches
-- still on would start connecting again without anybody having asked twice.
alter table domains
  drop constraint domains_wildcard_observation_needs_a_test_host,
  drop column tls_test_host,
  drop column observe_tls;
