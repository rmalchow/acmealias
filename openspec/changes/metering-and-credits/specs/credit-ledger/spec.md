## ADDED Requirements

### Requirement: Credits are held per account and are prepaid

The service SHALL hold a credit balance per account, above projects, so that an account with several
projects has one balance.

Credits SHALL be added before they are spent. The service SHALL NOT extend credit beyond the
overdraft this capability defines.

#### Scenario: One balance serves every project

- **WHEN** an account holds two projects and activity occurs in both
- **THEN** both are charged against one balance

#### Scenario: A balance is readable subject to permission

- **WHEN** a member holding a grant permitting the balance to be read requests it
- **THEN** it is returned

#### Scenario: A balance is not readable without permission

- **WHEN** a member holding no such grant requests the balance
- **THEN** the request is refused

### Requirement: Prices are data, and the price applied is recorded on the charge

The service SHALL determine the credits an event costs from a price list held as data, separate from
the record of the event.

Each deduction SHALL record the price that was applied to it. A change to a price SHALL affect only
events recorded after the change, and SHALL NOT alter what any past deduction reports.

#### Scenario: A price change does not alter past charges

- **WHEN** a price is changed after a deduction was recorded
- **THEN** that deduction continues to report the price it applied

#### Scenario: A price change applies to later events

- **WHEN** an event is recorded after a price change
- **THEN** it is charged at the new price

#### Scenario: An unpriced kind charges nothing

- **WHEN** an event of a kind absent from the price list is recorded
- **THEN** no balance changes

### Requirement: An event is rated when it is recorded

The service SHALL rate an event and apply its deduction in the same transaction in which the event is
recorded, so that the balance reflects every recorded event.

#### Scenario: The balance reflects a recorded event immediately

- **WHEN** an event is recorded
- **THEN** the balance reflects its cost without waiting for any scheduled process

#### Scenario: A failed deduction fails the event

- **WHEN** the deduction cannot be applied
- **THEN** the event is not recorded

### Requirement: Publishing a challenge value is charged; answering a query is not

The service SHALL charge for the publication of a challenge value, at the time it is published, and
SHALL NOT charge for answering a DNS query.

#### Scenario: A publish is charged

- **WHEN** a challenge value is published
- **THEN** the balance is reduced

#### Scenario: Queries are not charged

- **WHEN** a published challenge name is queried any number of times
- **THEN** no balance changes

#### Scenario: Withdrawal is not charged

- **WHEN** a published challenge value is withdrawn
- **THEN** no balance changes

### Requirement: A check is charged when its result is delivered, and overspend is not carried

The service SHALL charge for a check when its result is delivered, and SHALL NOT charge for a check
that was scheduled but whose result was not delivered.

Where a deduction would reduce a balance below the amount available to it, the excess SHALL be
discarded rather than recorded as an amount owed.

#### Scenario: An undelivered result is not charged

- **WHEN** a check is scheduled and its result is never delivered
- **THEN** no balance changes

#### Scenario: A delivered result is charged

- **WHEN** a check result is delivered
- **THEN** the balance is reduced

#### Scenario: Overspend is discarded

- **WHEN** a delivered result would reduce the balance below what is available
- **THEN** the excess is discarded and no amount is recorded as owed

### Requirement: Active zones are charged over time, per account, without depending on a schedule running on time

The service SHALL charge for zones held by an account over time, recording one charge per account per
period identifying the number of zones and the rate applied.

A charge SHALL be recorded at most once for one account and one period. Each run of the accrual SHALL
record charges for every period since the account's last charged period, not only the period
immediately preceding it.

#### Scenario: One charge covers every zone of an account

- **WHEN** an account holds several zones for one period
- **THEN** one charge is recorded for that account and period, identifying the number of zones

#### Scenario: A missed run catches up

- **WHEN** the accrual does not run for several periods and then runs
- **THEN** a charge is recorded for each period that was missed

#### Scenario: A repeated run charges once

- **WHEN** the accrual runs twice for the same period
- **THEN** one charge exists for that account and period

#### Scenario: Zone-time continues while suspended

- **WHEN** an account's balance is at or below zero and its zones are still served
- **THEN** zone charges continue to be recorded

### Requirement: An overdraft permits continuation and not new activity

The service SHALL permit a balance to fall below zero, to a configured limit, through charges for
zones held over time only.

Charges for publishing and for checks SHALL NOT be permitted to reduce a balance that is not above
zero.

#### Scenario: Zone charges may go negative

- **WHEN** an account's balance reaches zero and it continues to hold zones
- **THEN** the balance continues to fall until the overdraft limit

#### Scenario: New activity is not permitted on the overdraft

- **WHEN** an account's balance is not above zero and a publish is attempted
- **THEN** the request is refused and the balance does not fall further from it

### Requirement: The account is warned by projected time remaining, not by balance alone

The service SHALL report to an account how long its balance is projected to last at its current rate
of consumption, and SHALL warn in advance of the balance reaching zero.

Warning SHALL be delivered by a means that does not require the service to be usable by that account.

#### Scenario: Projected time is reported

- **WHEN** an account reads its balance
- **THEN** the projected time until it reaches zero is reported alongside it

#### Scenario: A warning precedes zero

- **WHEN** an account's projected time remaining falls below the configured threshold
- **THEN** it is warned before its balance reaches zero

#### Scenario: Warning does not depend on service being usable

- **WHEN** an account is suspended
- **THEN** it can still receive the warning
