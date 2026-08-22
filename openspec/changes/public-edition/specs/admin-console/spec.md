## ADDED Requirements

### Requirement: The console carries registration, confirmation and password reset

The console SHALL, in an edition that authenticates locally, present registration, address confirmation
and password reset, and SHALL reach them without an existing session.

The console SHALL offer to send a confirmation message again, and SHALL NOT report whether an address
already has a user.

#### Scenario: Registration is reachable without a session

- **WHEN** a person with no session opens the console
- **THEN** registration and password reset are reachable

#### Scenario: An unconfirmed user is offered only confirmation

- **WHEN** a person signs in before confirming their address
- **THEN** the console offers confirmation or another confirmation message, and nothing else

#### Scenario: The console does not disclose whether an address is registered

- **WHEN** registration or reset is requested for an address that already has a user
- **THEN** the console reports the same outcome as for an address that does not

### Requirement: A new customer reaches a verified domain without help

The console SHALL lead a person who has just confirmed their address to a first verified domain: it
SHALL show the exact record to publish, SHALL let them check it, and SHALL report the result plainly
enough to act on.

#### Scenario: The record to publish is shown exactly

- **WHEN** a person adds their first domain
- **THEN** the record to publish is shown in the form it must be published in

#### Scenario: The check reports what to do next

- **WHEN** the delegation has not been published or is wrong
- **THEN** the console says what was found and what was expected

#### Scenario: Verification is reported when it succeeds

- **WHEN** the delegation resolves as expected
- **THEN** the console reports the domain as verified

### Requirement: Credit balance and payment state are visible to the account, and only to it

The console SHALL show an account its credit balance, its purchase history and its payment state, and
SHALL show them only to members of that account.

The console SHALL NOT offer any control that sets a balance directly.

#### Scenario: The balance is visible to a member

- **WHEN** a member of an account opens the billing screen
- **THEN** the balance, purchase history and payment state of that account are shown

#### Scenario: Billing state is confined to the account

- **WHEN** a member of one account views balance or payment state
- **THEN** nothing of another account's balance or payment is shown

#### Scenario: No control sets a balance

- **WHEN** the billing screen is inspected for a control that sets a balance
- **THEN** no such control is offered, and buying credits is the only way to raise it
