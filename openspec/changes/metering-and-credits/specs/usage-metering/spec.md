## ADDED Requirements

### Requirement: Meterable activity is recorded as immutable events

The service SHALL record meterable activity as events, each identifying the owning account, the kind
of activity, a quantity, and the period the quantity covers.

Events SHALL be append-only. The service SHALL contain no operation that modifies or deletes an
event.

#### Scenario: An event is recorded for meterable activity

- **WHEN** an account performs an activity that is metered
- **THEN** an event is recorded identifying the account, the kind, the quantity and the period

#### Scenario: No modification path exists

- **WHEN** the management and command-line surfaces are examined
- **THEN** no operation updates or deletes an event

#### Scenario: Events are confined to their account

- **WHEN** a member of one account reads events
- **THEN** only that account's events are returned

### Requirement: A quantity is a count or a count over an interval

The service SHALL express an event's quantity either as a count of occurrences or as a count
sustained over an interval, and SHALL record which.

#### Scenario: A discrete action is a count

- **WHEN** a challenge value is published
- **THEN** an event records a count of one

#### Scenario: A continuous state is a count over an interval

- **WHEN** an account holds three zones for one period
- **THEN** an event records three sustained over that period

### Requirement: A re-delivered report produces no additional event

Where events originate from a report delivered by another process, the service SHALL identify the
delivery and SHALL NOT record events twice for one delivery.

#### Scenario: A batch delivered twice is recorded once

- **WHEN** a satellite delivers a batch of results, receives no response, and delivers it again
- **THEN** the events recorded for it are those of one delivery

#### Scenario: Distinct deliveries are recorded distinctly

- **WHEN** two different batches are delivered
- **THEN** events are recorded for both

### Requirement: Activity may be metered without being charged for

The service SHALL permit an event kind to be recorded and priced at nothing, so that activity may be
measured without affecting any balance.

DNS queries answered SHALL be metered in this way, aggregated per account and period rather than
recorded individually.

#### Scenario: A metered but unpriced event moves no balance

- **WHEN** an event of a kind priced at nothing is recorded
- **THEN** no balance changes

#### Scenario: Queries are aggregated

- **WHEN** queries are answered for an account during a period
- **THEN** they are recorded as one event for that period, not one event per query

### Requirement: Events are readable as the evidence for a charge

The service SHALL expose events through the management interface, filterable by kind and time range,
so that a charge can be traced to the activity that produced it.

#### Scenario: A charge is traceable to its events

- **WHEN** a deduction is examined
- **THEN** the event that produced it is identifiable

#### Scenario: Events are filterable

- **WHEN** events are requested for one kind over one period
- **THEN** only those are returned
