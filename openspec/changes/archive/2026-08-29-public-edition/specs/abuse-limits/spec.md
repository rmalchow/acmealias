## ADDED Requirements

### Requirement: Protective limits apply to every account, independently of payment

The service SHALL bound the load one account can place on resources shared with other accounts, and
SHALL apply those bounds to every account whatever its kind, whatever its credit balance, and whether
or not it is charged for anything at all.

A protective limit SHALL NOT be purchasable, and SHALL NOT be raised by holding a larger balance.

#### Scenario: A balance does not raise a limit

- **WHEN** an account with a large credit balance reaches a protective limit
- **THEN** the operation is refused

#### Scenario: An account that is charged nothing is still bounded

- **WHEN** an account that holds no balance and is charged for nothing reaches a protective limit
- **THEN** the operation is refused

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

A refusal for a protective limit SHALL be distinguishable from a refusal for an account that is not
funded and from a failure, and SHALL NOT direct the caller to a balance.

#### Scenario: The client is told what to do

- **WHEN** an operation is refused by a protective limit
- **THEN** the response names the limit and when it clears

#### Scenario: A limit is not mistaken for insolvency

- **WHEN** a publication is refused by a protective limit
- **THEN** the response differs from the one given when the account is not funded, and does not report payment as the remedy

### Requirement: Protective limits are configuration

The values of protective limits SHALL be settings rather than constants, and SHALL NOT be settable
from within an account.

#### Scenario: A limit is changed without a release

- **WHEN** an operator changes a protective limit
- **THEN** the new value takes effect without rebuilding the service

#### Scenario: No tenant surface raises a limit

- **WHEN** the management interface, the console and the command line are searched for an operation that raises a protective limit for an account
- **THEN** no such operation is offered within the account
