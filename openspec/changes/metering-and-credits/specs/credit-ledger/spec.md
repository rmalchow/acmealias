## ADDED Requirements

### Requirement: Activity is charged against the account's existing balance

The service SHALL charge metered activity against the credit balance the account already holds,
which is kept per account above projects, so that an account with several projects has one balance.

This capability SHALL NOT introduce a second balance, a second funding rule, or a second definition
of when an account is usable.

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

- **WHEN** a challenge value is published by a funded account
- **THEN** the balance is reduced

#### Scenario: Queries are not charged

- **WHEN** a published challenge name is queried any number of times
- **THEN** no balance changes

#### Scenario: Withdrawal is not charged

- **WHEN** a published challenge value is withdrawn
- **THEN** no balance changes

### Requirement: Certificate observation is charged over time, per account, without depending on a schedule running on time

The service SHALL charge for the nodes an account has configured for certificate observation over
time, recording one charge per account per period identifying the number of such nodes and the rate
applied.

A charge SHALL be recorded at most once for one account, kind and period. Each run of the accrual
SHALL record charges for every period since the account's last charged period, not only the period
immediately preceding it.

#### Scenario: One charge covers every configured node of an account

- **WHEN** an account has four nodes configured for observation for one period
- **THEN** one charge is recorded for that account and period, identifying the number of nodes

#### Scenario: A missed run catches up

- **WHEN** the accrual does not run for several periods and then runs
- **THEN** a charge is recorded for each period that was missed

#### Scenario: A repeated run charges once

- **WHEN** the accrual runs twice for the same period
- **THEN** one charge exists for that account and period

#### Scenario: An account that configured nothing accrues nothing

- **WHEN** an account has no node configured for observation
- **THEN** the accrual records no charge for it, however many periods pass

### Requirement: The balance floors at zero and no charge is carried as debt

The service SHALL NOT reduce a balance below zero. Where a charge would do so, the excess SHALL be
discarded rather than recorded as an amount owed or reserved in advance.

#### Scenario: A charge is clamped at zero

- **WHEN** a periodic charge exceeds the credits remaining
- **THEN** the balance becomes zero and the excess is discarded

#### Scenario: No debt is recorded

- **WHEN** a charge has been clamped
- **THEN** no record of an amount owed exists and nothing is charged for it later

### Requirement: Observation stops for an account that is not funded, and resumes when it is

The service SHALL cease scheduling certificate observation for an account whose balance is not above
zero, and SHALL resume it without further action when credits are added.

#### Scenario: Observation stops

- **WHEN** an account's balance reaches zero
- **THEN** no further certificate observation is scheduled for it

#### Scenario: Observation resumes

- **WHEN** credits are added to such an account
- **THEN** certificate observation is scheduled for it again

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

### Requirement: An operator may grant credits and thereby restore an account

The service SHALL permit an operator holding the credit-granting permission to add credits to an
account, SHALL record the grant, and SHALL make it visible to that account.

#### Scenario: An operator can restore an account

- **WHEN** an operator holding the credit-granting permission adds credits to a suspended account
- **THEN** the account's balance rises above zero and it is usable again

#### Scenario: A grant is recorded

- **WHEN** an operator adds credits to an account
- **THEN** the action is recorded and is visible to that account
