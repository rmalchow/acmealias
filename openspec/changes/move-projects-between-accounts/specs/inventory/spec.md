## ADDED Requirements

### Requirement: A project can move to another account

The service SHALL be able to move a project, with its nodes, domains, aliases and keys, from one
account to another.

Moving SHALL require authority in both the source and the destination account. A refusal SHALL name
which of the two was lacking.

#### Scenario: A move carries the whole project

- **WHEN** a project is moved to another account
- **THEN** its nodes, domains, aliases and keys belong to the destination account

#### Scenario: Authority in one account is not enough

- **WHEN** a caller may act in only one of the two accounts
- **THEN** the move is refused, and the refusal names which account it lacked authority in

#### Scenario: Nothing is left behind

- **WHEN** a move completes
- **THEN** no row beneath the project names the source account

### Requirement: A move does not change what the customer published

Moving a project SHALL NOT change any alias label, and SHALL NOT require the customer to change any
DNS record.

Challenge answering SHALL continue across the move.

#### Scenario: The delegation still works

- **WHEN** a project is moved
- **THEN** the CNAME the customer published still resolves to the same alias, and challenges are
  still answered

### Requirement: A destination's limits apply to what arrives

A project arriving in an account SHALL be subject to that account's project limit as though it had
been created there.

#### Scenario: A move cannot exceed the destination's quota

- **WHEN** a project is moved into an account already holding its maximum
- **THEN** the move is refused
