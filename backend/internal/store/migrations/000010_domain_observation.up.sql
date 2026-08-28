-- The switch that decides whether this service connects to a customer's load
-- balancer at all, and the hostname it presents when the domain itself is not a
-- name TLS can be asked for (change domain-diagnostics, design G6 and G8).
--
-- BOTH COLUMNS ARE DEFAULTED SO THAT DEPLOYING THIS MIGRATION CHANGES NOTHING.
-- Every existing row gets observe_tls false, and the observation worker's queue
-- is "domains with the switch on" — so the deploy that adds this feature does
-- not, on the following tick, open a TLS connection to every load balancer this
-- service has ever been told about. Observation is opt-in per domain
-- (G-constraint 3), and the default here is where that starts being true.
--
-- Rejected: backfilling observe_tls true for domains that are already activated,
-- by analogy with 000002, which activated every existing domain. That analogy
-- does not hold. 000002 was preserving behaviour customers already had — their
-- /present calls worked yesterday and had to work today. This would be granting
-- a behaviour nobody asked for, aimed at somebody else's machine.
alter table domains
  add column observe_tls    boolean not null default false,
  add column tls_test_host  text;

comment on column domains.observe_tls is
  'Opt-in: whether this service performs periodic TLS observations of this domain.';
comment on column domains.tls_test_host is
  'For a wildcard domain, the concrete hostname used as the TLS server name. Null means none was ever set — which is different from the empty string, and only one of the two is writable.';

-- The half of design G8's rule that the database can hold.
--
-- `*.example.com` is not a name a TLS client can ask for, so a wildcard with the
-- switch on and no test hostname describes work that can never be performed: the
-- worker would select the row and have no server name to present. That is a row
-- worth refusing at the table rather than only in Go, because the Go check lives
-- on one write path (inventory.UpdateDomain) and a future second path — an
-- import, a fixture, a repair script — would not go through it.
--
-- The database deliberately does NOT check the other half of G8, that the
-- hostname is a name the wildcard actually covers. That test is
-- dnsname.Covers, it shares its rules with ValidateDomain, and restating it as a
-- SQL expression would be a second matcher that can disagree with the first —
-- which G8 names as the failure worth avoiding. Half a constraint enforced
-- exactly beats a whole one enforced twice, differently.
alter table domains
  add constraint domains_wildcard_observation_needs_a_test_host
  check (not observe_tls or name not like '*.%' or tls_test_host is not null);
