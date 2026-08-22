## ADDED Requirements

### Requirement: An invitation link proves control of the address it was sent to

The service SHALL treat following an invitation link as proof of control of the address the invitation
names, equivalent to following a confirmation token sent to that address.

A user created through an invitation SHALL have the invited address recorded as confirmed, and the
service SHALL NOT require a further confirmation of that address.

Registration reached by any other means SHALL be unchanged and SHALL still require confirmation.

#### Scenario: Signing up through an invitation needs no confirmation

- **WHEN** a person with no user follows an invitation link and registers
- **THEN** the user is created with the invited address confirmed, and no confirmation message is sent

#### Scenario: The new user proceeds directly to the offer

- **WHEN** a user has been created through an invitation link
- **THEN** they are asked whether to accept the invitation, without an intervening confirmation step

#### Scenario: Ordinary registration is unaffected

- **WHEN** a person registers without an invitation
- **THEN** the address is unconfirmed and confirmation is required as before

#### Scenario: The proof extends to no other address

- **WHEN** a user is created through an invitation link
- **THEN** only the invited address is recorded as confirmed
