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

### Requirement: A metered event carrying no price produces no deduction

The service SHALL record a metered event whose price is zero without writing a deduction for it. The
deduction record SHALL exist only where a balance moved.

#### Scenario: A zero-priced event writes no deduction

- **WHEN** an event of a type priced at zero is recorded
- **THEN** the event exists, the balance is unchanged, and no deduction is written

### Requirement: Checking is billed per checked node per period, not per probe

The service SHALL derive the billable checking quantity from the nodes an account has configured for
checking, expressed as a count over an interval and emitted by the same periodic job that emits
zone-time. Individual probe results SHALL be metered without price.

The billable quantity SHALL NOT vary with the number of satellites probing, the probe interval, or
the number of address families probed.

#### Scenario: The invoice does not change when the fleet does

- **WHEN** the number of satellites assigned to a node changes
- **THEN** the billable checking quantity for that node is unchanged

#### Scenario: A probe result carries no charge

- **WHEN** a probe result is recorded
- **THEN** it is metered and no balance moves

### Requirement: Probe results are retained outside the ledger

Probe results SHALL be stored separately from events and deductions, SHALL be retained for a bounded
period, and SHALL be removable without affecting any charge.

#### Scenario: Expired probe results leave charges intact

- **WHEN** probe results older than the retention period are removed
- **THEN** every event, deduction and balance is unchanged

### Requirement: Periodic charges are backfilled after an interruption, and late reports are not billed

The periodic job SHALL write the rate events for every period since it last ran, including periods
during which it was not running. A probe result describing a cycle beyond the permitted delivery
delay SHALL produce no billable event.

#### Scenario: An interrupted job charges for the periods it missed

- **WHEN** the periodic job runs after an interruption spanning several periods
- **THEN** one rate event exists for each of those periods, and running it again adds none

#### Scenario: A delivered backlog does not charge

- **WHEN** a satellite delivers results describing cycles beyond the permitted delay
- **THEN** no deduction is written for them
