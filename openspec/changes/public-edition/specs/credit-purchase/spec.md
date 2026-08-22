## ADDED Requirements

### Requirement: Credits are bought through a payment provider

The service SHALL allow an account to buy a quantity of credits through an external payment provider,
and SHALL increase the account's balance only when that provider reports the payment as completed.

The increase and the record of the purchase SHALL be applied indivisibly, so that no balance rises
without a purchase recording why.

#### Scenario: A completed payment raises the balance

- **WHEN** the provider reports a payment for a quantity of credits as completed
- **THEN** the account's balance rises by that quantity

#### Scenario: An incomplete payment changes nothing

- **WHEN** a purchase is begun and the payment is not completed
- **THEN** the balance is unchanged

#### Scenario: A balance never rises without evidence

- **WHEN** an account's balance has risen
- **THEN** a purchase record exists naming the provider's reference for the payment

### Requirement: Purchase state is derived from the provider and never entered by hand

The service SHALL determine purchase state from what the payment provider reports, and SHALL NOT offer
any interface that sets an account's balance or purchase state directly.

#### Scenario: No interface sets a balance

- **WHEN** the management interface, the console and the command line are searched for an operation that sets a balance
- **THEN** no such operation exists

#### Scenario: Purchase state follows the provider

- **WHEN** the provider reports a payment as refunded or reversed
- **THEN** the account's balance is adjusted accordingly and the adjustment is recorded

### Requirement: A provider notification that cannot be verified changes nothing

The service SHALL verify the authenticity of every notification received from the payment provider
before acting on it, and SHALL make no change when verification fails.

A notification that fails verification SHALL be recorded as an attempt.

#### Scenario: An unverifiable notification is ignored

- **WHEN** a notification fails verification
- **THEN** no balance and no purchase state changes

#### Scenario: A rejected notification is recorded

- **WHEN** a notification fails verification
- **THEN** the attempt is recorded with its source

#### Scenario: A repeated notification credits once

- **WHEN** the provider delivers the same completed payment notification more than once
- **THEN** the balance rises once

### Requirement: The internal edition exposes no payment interface

In an edition that authenticates through an identity provider the service SHALL NOT expose any screen,
route or notification concerning payment, and SHALL NOT require a balance for any operation.

#### Scenario: No billing surface internally

- **WHEN** that edition is running
- **THEN** no screen, route or notification concerns payment

#### Scenario: Operations do not depend on a balance

- **WHEN** an operator performs any operation in that edition
- **THEN** it succeeds without reference to a balance
