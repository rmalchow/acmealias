## ADDED Requirements

### Requirement: Protective limits apply in every edition, independently of payment

The service SHALL bound the load one account can place on resources shared with other accounts, and
SHALL apply those bounds in every edition to every account, whatever its credit balance and whether or
not it is charged at all.

A protective limit SHALL NOT be purchasable, and SHALL NOT be raised by holding a larger balance.

#### Scenario: A balance does not raise a limit

- **WHEN** an account with a large credit balance reaches a protective limit
- **THEN** the operation is refused

#### Scenario: An unbilled edition is still protected

- **WHEN** an edition that charges nobody is running
- **THEN** protective limits still apply

### Requirement: One account cannot exhaust a shared resource

The service SHALL limit the number of challenge values an account may have published at one time, and
the rate at which it may publish, so that no account can degrade the service for another.

An account reaching a limit SHALL NOT affect the operations of any other account.

#### Scenario: One account cannot fill the shared zone

- **WHEN** an account reaches its limit of simultaneously published challenge values
- **THEN** further publications by that account are refused and other accounts are unaffected

#### Scenario: A runaway client is bounded

- **WHEN** an account exceeds its publication rate limit
- **THEN** further publications are refused until the limit clears

#### Scenario: Existing values are not withdrawn by a limit

- **WHEN** an account reaches a limit
- **THEN** values it has already published continue to be answered

### Requirement: A refusal names the limit reached and when it clears

The service SHALL, when refusing an operation because of a protective limit, report which limit was
reached and when the operation may be retried.

A refusal for a protective limit SHALL be distinguishable from a refusal for an exhausted credit
balance and from a failure.

#### Scenario: The client is told what to do

- **WHEN** an operation is refused by a protective limit
- **THEN** the response names the limit and when it clears

#### Scenario: A limit is not mistaken for insolvency

- **WHEN** an operation is refused by a protective limit
- **THEN** the response is distinguishable from one refused for an exhausted balance
