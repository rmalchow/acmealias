## ADDED Requirements

### Requirement: An account whose balance is exhausted keeps its existing service

When an account's balance is not above zero, the service SHALL continue to answer DNS for that
account's zones and SHALL continue to answer challenge values already published.

The service SHALL refuse new challenge publications and SHALL cease scheduling checks.

#### Scenario: DNS keeps resolving

- **WHEN** an account's balance reaches zero and a name in its zone is queried
- **THEN** the name resolves as before

#### Scenario: A published challenge is still answered

- **WHEN** an account's balance reaches zero and a previously published challenge name is queried
- **THEN** the value is answered until it expires

#### Scenario: A new publish is refused

- **WHEN** an account's balance is not above zero and a challenge value is published
- **THEN** the request is refused with a status indicating payment is required

#### Scenario: Checks stop

- **WHEN** an account's balance is not above zero
- **THEN** no further checks are scheduled for it

### Requirement: An account that exhausts its overdraft has its zones withdrawn

When an account's balance falls below the overdraft limit, the service SHALL withdraw its zones from
distribution so that they are no longer served.

A withdrawn zone SHALL be answered as a name for which the service is not authoritative. The service
SHALL NOT retain a withdrawn zone in order to deny its names.

#### Scenario: A withdrawn zone is not answered authoritatively

- **WHEN** an account's overdraft is exhausted and a name in its zone is queried
- **THEN** the response indicates the service is not authoritative for that name

#### Scenario: A withdrawn zone does not produce an authoritative denial

- **WHEN** a name in a withdrawn zone is queried
- **THEN** the response is not an authoritative denial of that name's existence

#### Scenario: Withdrawal reaches every satellite

- **WHEN** a zone is withdrawn
- **THEN** every satellite ceases to answer authoritatively for it

### Requirement: Service is restored promptly when credits are added

When credits are added such that an account's balance rises above zero, the service SHALL restore
what was suspended without requiring any further action, and SHALL redistribute any withdrawn zone.

Restoration SHALL NOT be delayed by any period for which a previous response was cached by a
resolver.

#### Scenario: Publishing resumes

- **WHEN** credits are added to a suspended account
- **THEN** the next challenge publication succeeds

#### Scenario: Checks resume

- **WHEN** credits are added to a suspended account
- **THEN** checks are scheduled again

#### Scenario: A withdrawn zone resolves again promptly

- **WHEN** credits are added to an account whose zones were withdrawn
- **THEN** its zones are distributed again and resolve without waiting for a cached denial to expire

### Requirement: Suspension state is visible to the account and adjustable by the operator

The service SHALL report an account's suspension state to that account, and SHALL permit an operator
holding the credit-granting permission to add credits to an account and thereby restore it.

#### Scenario: The account sees that it is suspended

- **WHEN** a member of a suspended account reads its state
- **THEN** the suspension and its cause are reported

#### Scenario: An operator can restore an account

- **WHEN** an operator holding the credit-granting permission adds credits to a suspended account
- **THEN** the account is restored

#### Scenario: Restoration by an operator is recorded

- **WHEN** an operator adds credits to an account
- **THEN** the action is recorded and is visible to that account
