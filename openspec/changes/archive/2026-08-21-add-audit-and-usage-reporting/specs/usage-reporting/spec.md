## ADDED Requirements

> **Scope, decided 2026-08-21: reporting is per project and per load balancer, never per domain.**
> The requirement *"History is reported per domain"* was removed from this delta because the console
> has no domain route and no domain detail screen, and adding one was declined — see `../../proposal.md`.
> Nothing else here is weakened: project-level aggregation, the daily inventory snapshot, the
> no-data-versus-zero distinction and the metric-cardinality rule all keep every property they had,
> and the metric rule still forbids domain and alias label values. The never-validated signal is
> served at load-balancer scope by the `challenge-history` capability's interactions list, filtered
> to the `unqueried` outcome.

### Requirement: Reported quantities are challenge and issuance attempts, never certificates

The service SHALL report counts of challenge attempts and of issuance attempts, and SHALL NOT
describe any reported quantity as a count of certificates, in the management interface, the API or
the console.

An **issuance attempt** SHALL be the group of challenge attempts at one alias whose live windows
overlap. A **challenge attempt** SHALL be one published value.

#### Scenario: Wildcard and apex count as one issuance attempt

- **WHEN** two values are published at one alias with overlapping lifetimes and both are then cleaned up
- **THEN** the period reports two challenge attempts and one issuance attempt

#### Scenario: Sequential attempts are not grouped

- **WHEN** a value is published and cleaned up, and a second is published afterwards
- **THEN** the period reports two challenge attempts and two issuance attempts

#### Scenario: Naming is explicit

- **WHEN** any reporting response or console figure is read
- **THEN** its quantities are identified as challenge attempts or issuance attempts

### Requirement: Attempts are aggregated per project over time

The service SHALL report issuance and challenge attempts for a project over a requested time range,
bucketed by day or by month, broken down by outcome. The time range SHALL be explicit in the
request.

Aggregates SHALL reflect the project a domain belonged to when the attempt occurred.

#### Scenario: Attempts over a range

- **WHEN** a project's usage is requested for a range with a daily bucket
- **THEN** the response contains one entry per bucket with counts by outcome

#### Scenario: Moved domains do not rewrite history

- **WHEN** a domain is moved to another project after attempts were recorded
- **THEN** earlier attempts continue to be reported under the project they occurred in

### Requirement: Inventory size is recorded daily per project

The service SHALL record, once per day for each project, the number of configured aliases, domains
and load balancers, and SHALL report that series over a requested range.

Recording SHALL be idempotent for a given project and day.

#### Scenario: Daily snapshot is written

- **WHEN** the daily recording runs
- **THEN** each project has one entry for that day with its alias, domain and load balancer counts

#### Scenario: Repeated run does not duplicate

- **WHEN** the daily recording runs twice for the same day
- **THEN** exactly one entry exists for that project and day

#### Scenario: Trend is reported

- **WHEN** a project's inventory history is requested over a range
- **THEN** the recorded counts are returned per day

### Requirement: Absence of data is distinguishable from zero

Reporting responses SHALL distinguish a period for which nothing was recorded from a period in which
the recorded count was zero, and the console SHALL render the two differently.

#### Scenario: Freshly deployed service

- **WHEN** reporting is requested before any attempt has been recorded
- **THEN** the response indicates that no data has been recorded rather than reporting zero attempts

### Requirement: Reporting is readable by every role and mutates nothing

All roles that may read the inventory SHALL be able to read reporting. No reporting operation SHALL
modify inventory, credentials or history.

#### Scenario: Read-only role may read reporting

- **WHEN** a user holding only the read-only role requests project usage
- **THEN** the data is returned

### Requirement: Per-name dimensions are not exposed as metric labels

The service SHALL NOT emit monitoring metrics whose label values identify a domain, alias, project
or API key. History at any finer grain than the metrics expose SHALL be served from the datastore
through the management interface.

#### Scenario: Metric labels are bounded

- **WHEN** the metrics endpoint is scraped
- **THEN** no metric carries a domain, alias, project or key identifier as a label value

#### Scenario: Outcome counters are aggregate

- **WHEN** challenge attempts complete
- **THEN** counters are incremented by outcome only
