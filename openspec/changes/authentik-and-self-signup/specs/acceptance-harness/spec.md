## MODIFIED Requirements

### Requirement: The acceptance suite exercises sign-in and role enforcement against a real identity provider

The suite SHALL complete the authorization code flow against the stack's identity provider over
that provider's own sign-in mechanism, whatever form that mechanism takes, and SHALL NOT establish a
session by any route a person could not use.

The identity provider the suite runs against SHALL be the one the production design names. A
development stack authenticating through different software than production is deemed to prove
nothing about sign-in.

#### Scenario: Suite signs in as a real user

- **WHEN** the acceptance suite authenticates as a seeded user
- **THEN** it completes the provider's own sign-in exchange and obtains a session the service accepts

#### Scenario: No test-only route to a session

- **WHEN** the service is searched for a route or setting that establishes a session without the identity provider
- **THEN** none exists

#### Scenario: Each seeded user resolves to its configured role

- **WHEN** the suite signs in as each seeded user in turn
- **THEN** each holds exactly the role its group is configured to grant, and role enforcement refuses what that role may not do
