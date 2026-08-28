## ADDED Requirements

### Requirement: A person may register themselves

The service SHALL offer an unauthenticated person a way to create their own user, reachable from the
sign-in page.

The address given SHALL be verified before the person can authenticate, and the verification SHALL be
proof of control of that address rather than a declaration of it.

#### Scenario: A stranger can begin

- **WHEN** an unauthenticated person reaches the sign-in page
- **THEN** they are offered a way to register

#### Scenario: An unverified address does not authenticate

- **WHEN** a person completes registration and has not proved control of the address
- **THEN** they cannot authenticate

### Requirement: A self-registered account does not work until it is approved

Where approval is required, an account created for a person who registered themselves SHALL NOT be
usable until it has been approved, whatever its kind and whatever its balance.

An account that has never been approved SHALL be distinguishable, in the reason given for its being
unusable, from an account that is suspended and from one that has no credit.

Approval SHALL be recorded with the time it happened.

#### Scenario: A registered person waits

- **WHEN** a person registers themselves and signs in while approval is required
- **THEN** their account is not usable, and the reason names review rather than payment or suspension

#### Scenario: Approval admits them

- **WHEN** their account is approved
- **THEN** it is usable, and behaves as any other account of its kind

#### Scenario: Approval is not payment

- **WHEN** an account is unusable for want of approval
- **THEN** adding credit does not make it usable

### Requirement: Approval is required by configuration, and the requirement is evaluated once

Whether approval is required SHALL be a matter of configuration, and SHALL be evaluated when an
account is created rather than when it is used.

Changing the configuration SHALL NOT change whether an existing account is usable.

#### Scenario: Opening the door does not release those already waiting

- **WHEN** approval ceases to be required
- **THEN** accounts awaiting approval remain unusable until somebody approves them

#### Scenario: Closing the door does not lock out those already admitted

- **WHEN** approval becomes required
- **THEN** every account that was usable remains usable

### Requirement: Accepting an invitation approves the accepter

Accepting an invitation SHALL approve the accepting person's own account if it is not already
approved.

#### Scenario: An invited person does not wait

- **WHEN** a person who registered while approval was required accepts an invitation
- **THEN** their own account is usable, and they are not presented for review
