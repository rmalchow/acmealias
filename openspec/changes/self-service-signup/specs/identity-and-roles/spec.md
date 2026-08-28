## ADDED Requirements

### Requirement: A person may register themselves

The service's identity provider SHALL offer an unauthenticated person a way to create their own user,
reachable from the sign-in form.

The address given SHALL be verified before that person can authenticate, and the verification SHALL be
proof of control of the address rather than a declaration of it.

#### Scenario: A stranger can begin

- **WHEN** an unauthenticated person reaches the sign-in form
- **THEN** they are offered a way to register

#### Scenario: An unverified address does not authenticate

- **WHEN** a person completes registration without proving control of the address
- **THEN** they cannot authenticate

### Requirement: A self-registered person cannot authenticate until they are approved

Where approval is required, a person who registers themselves SHALL NOT be able to authenticate until
somebody approves them.

An unapproved person SHALL be told that their registration is awaiting review. They SHALL NOT be given
a refusal indistinguishable from a wrong password.

#### Scenario: A registered person waits, and knows it

- **WHEN** an unapproved person attempts to sign in
- **THEN** they are refused, and are told their registration is under review

#### Scenario: Approval admits them

- **WHEN** they are approved and sign in
- **THEN** they hold their own account and are an ordinary customer

### Requirement: The service holds nothing for a person who has not been admitted

No account SHALL be created for a person who has not been approved.

#### Scenario: Registration writes nothing to the service

- **WHEN** a person registers themselves while approval is required
- **THEN** the service holds no account for them until they first sign in

### Requirement: Whether approval is required is configurable, and is applied when a person registers

Whether a self-registered person requires approval SHALL be a matter of configuration, applied when
they register.

Changing it SHALL NOT change whether a person who has already registered may authenticate.

#### Scenario: Opening the door does not release those behind it

- **WHEN** approval ceases to be required
- **THEN** people who registered while it was required still cannot authenticate until approved

#### Scenario: Closing the door does not lock out those already admitted

- **WHEN** approval becomes required
- **THEN** everybody who could already authenticate still can

### Requirement: A person who arrives by invitation is not held for approval

A person who becomes a user by accepting an invitation SHALL be able to authenticate immediately.

#### Scenario: An invited person does not wait

- **WHEN** a person accepts an invitation while approval is required for self-registration
- **THEN** they can authenticate without being approved
