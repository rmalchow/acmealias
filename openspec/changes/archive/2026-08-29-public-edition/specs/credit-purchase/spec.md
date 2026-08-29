## ADDED Requirements

### Requirement: Credits are bought through a payment provider

The service SHALL allow an account to buy a quantity of credits through an external payment provider,
and SHALL increase the account's balance only when that provider reports the payment as completed.

The quantity SHALL come from a fixed set offered by the service and SHALL NOT be supplied by the
caller.

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

#### Scenario: An amount that is not offered is refused

- **WHEN** a purchase is begun for a quantity the service does not offer
- **THEN** it is refused and no payment is initiated

### Requirement: Card details are never held by the service

Payment details SHALL be collected by the payment provider, and the service SHALL store no card
number, no equivalent instrument, and no credential of the payer.

#### Scenario: The payer leaves the service to pay

- **WHEN** a purchase is begun
- **THEN** payment details are entered at the provider and not at the service

#### Scenario: No instrument is stored

- **WHEN** the datastore is inspected after a completed purchase
- **THEN** it holds the provider's reference and the amount, and no payment instrument

### Requirement: Purchase state is derived from the provider and never entered by hand

The service SHALL determine purchase state from what the payment provider reports, and SHALL NOT
offer any interface that sets an account's balance or purchase state directly.

#### Scenario: No interface sets a balance

- **WHEN** the management interface, the console and the command line are searched for an operation that sets a balance
- **THEN** no such operation exists within an account

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

### Requirement: Payment is governed by configuration, and the checkout keeps its meaning either way

The service SHALL refuse to initiate a payment while payment is not enabled by configuration, and the
checkout SHALL then behave as it does without a provider: a charge from the offered amounts, recorded
and audited.

Enabling payment SHALL change where the money comes from and SHALL NOT change what the checkout
records.

While payment is disabled, the only other source of a balance SHALL be an audited operator credit
grant, and no interface within an account SHALL set a balance.

#### Scenario: A purchase does not reach the provider while payment is disabled

- **WHEN** a checkout is performed while payment is disabled
- **THEN** no payment is initiated, and the account is credited and audited as it is without a provider

#### Scenario: The recorded charge is the same either way

- **WHEN** a checkout completes with payment enabled and with payment disabled
- **THEN** the balance change and the audit entry are of the same shape in both cases

#### Scenario: An operator grant still funds an account

- **WHEN** an operator grants credits to an account while payment is disabled
- **THEN** the balance rises, the grant is audited, and the account can see it
