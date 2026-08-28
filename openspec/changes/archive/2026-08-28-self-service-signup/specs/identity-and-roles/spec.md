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

### Requirement: An invitation confirms an address; it does not admit a person

A person who becomes a user by following an invitation link SHALL have the invited address recorded as
confirmed without a confirmation message being sent.

That person SHALL NOT thereby be exempt from approval: while approval is required, they SHALL NOT be
able to authenticate until approved.

The service SHALL NOT provide a second enrolment path that creates users already approved.

#### Scenario: An invited person's address needs no confirmation

- **WHEN** a person registers by following an invitation link
- **THEN** the invited address is recorded as confirmed and no confirmation message is sent

#### Scenario: An invited person still waits

- **WHEN** a person registers by following an invitation link while approval is required
- **THEN** they cannot authenticate until approved

#### Scenario: There is one enrolment path

- **WHEN** the enrolment paths are enumerated
- **THEN** none of them creates a user that is approved on creation while approval is required
