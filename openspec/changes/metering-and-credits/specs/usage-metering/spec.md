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

#### Scenario: A continuing arrangement is a count over an interval

- **WHEN** an account has four nodes configured for certificate observation for one period
- **THEN** an event records four sustained over that period

### Requirement: An event is recorded once for one occurrence or one period

The service SHALL identify each event by what produced it — the request for a discrete action, the
account and period for a continuing arrangement — and SHALL NOT record two events for one of them.

#### Scenario: A repeated periodic run records one event

- **WHEN** the periodic job runs twice covering the same account and period
- **THEN** one event exists for that account, kind and period

#### Scenario: Distinct periods are recorded distinctly

- **WHEN** the periodic job covers two successive periods
- **THEN** an event exists for each

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

### Requirement: A metered event carrying no price produces no deduction

The service SHALL record a metered event whose price is zero without writing a deduction for it. The
deduction record SHALL exist only where a balance moved.

#### Scenario: A zero-priced event writes no deduction

- **WHEN** an event of a type priced at zero is recorded
- **THEN** the event exists, the balance is unchanged, and no deduction is written

### Requirement: Certificate observation is billed per configured node per period, not per observation

The service SHALL derive the billable observation quantity from the nodes an account has configured
for certificate observation, expressed as a count over an interval and emitted by the periodic job.

The billable quantity SHALL NOT vary with how often observation is performed, nor with the number of
address families observed. Individual observations SHALL NOT produce a billable event.

#### Scenario: The invoice does not change when the schedule does

- **WHEN** the observation interval is changed
- **THEN** the billable quantity for a configured node is unchanged

#### Scenario: The invoice does not change when a node gains an address family

- **WHEN** a configured node acquires a second address family and is observed over both
- **THEN** the billable quantity for that node is unchanged

#### Scenario: An observation carries no charge

- **WHEN** a certificate observation is recorded
- **THEN** no balance moves

### Requirement: Observations are retained outside the ledger

Certificate observations SHALL be stored separately from events and deductions, SHALL be retained for
a bounded period, and SHALL be removable without affecting any charge.

#### Scenario: Expired observations leave charges intact

- **WHEN** observations older than the retention period are removed
- **THEN** every event, deduction and balance is unchanged

### Requirement: Periodic charges are backfilled after an interruption

The periodic job SHALL write the rate events for every period since it last ran, including periods
during which it was not running.

A node SHALL be billed as observed for a period only if it was configured for observation in that
period, regardless of whether any observation of it succeeded.

#### Scenario: An interrupted job charges for the periods it missed

- **WHEN** the periodic job runs after an interruption spanning several periods
- **THEN** one rate event exists for each of those periods, and running it again adds none

#### Scenario: A node configured but never reachable is still billed

- **WHEN** a node was configured for observation throughout a period and every handshake against it
  failed
- **THEN** the rate event for that period includes it

#### Scenario: A node not configured in a period is not billed for it

- **WHEN** a node is configured for observation partway through an interruption
- **THEN** the backfilled events for earlier periods do not include it

### Requirement: Events are readable as the evidence for a charge

The service SHALL expose events through the management interface, filterable by kind and time range,
so that a charge can be traced to the activity that produced it.

#### Scenario: A charge is traceable to its events

- **WHEN** a deduction is examined
- **THEN** the event that produced it is identifiable

#### Scenario: Events are filterable

- **WHEN** events are requested for one kind over one period
- **THEN** only those are returned
