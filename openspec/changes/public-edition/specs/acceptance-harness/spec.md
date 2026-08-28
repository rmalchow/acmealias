## ADDED Requirements

### Requirement: The commercial path is exercised before release

The acceptance environment SHALL exercise the path a stranger walks: enrolling with an address they
control, reaching a first working domain, buying credits through the payment provider, and being
refused by a protective limit.

Each of those SHALL be exercised against the running stack rather than asserted about configuration,
and the suite SHALL fail if any of them is broken.

#### Scenario: Enrolment is exercised end to end

- **WHEN** the acceptance suite runs
- **THEN** it enrols a new person through the identity provider, verifies the address, and signs in

#### Scenario: A purchase is exercised through the provider

- **WHEN** the acceptance suite runs with payment enabled
- **THEN** it completes a purchase through the provider and the account's balance rises exactly once

#### Scenario: A protective limit is exercised

- **WHEN** the acceptance suite runs
- **THEN** an account reaching a protective limit is refused, and the refusal names the limit
