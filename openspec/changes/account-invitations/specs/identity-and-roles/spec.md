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

#### Scenario: Confirming the address does not admit the person

- **WHEN** a user is created through an invitation link while registrations require approval
- **THEN** the address is confirmed and the user still cannot authenticate until approved

### Requirement: A membership may hold no role, and confers nothing while it does

The service SHALL permit a membership that names no role.

A subject whose memberships name no role SHALL hold no grant and SHALL be unrestricted in no account,
and every authorization decision for it SHALL be a refusal.

Such a membership SHALL still make its holder a member: the account SHALL be selectable by them and
SHALL appear among the accounts they belong to.

A role SHALL be assignable to such a membership afterwards, after which it confers what that role
confers.

#### Scenario: A roleless member is authorised for nothing

- **WHEN** a member holding no role attempts any account-scoped operation
- **THEN** it is refused, and no object of the account is returned to them

#### Scenario: A roleless member is still a member

- **WHEN** a member holding no role lists the accounts they belong to
- **THEN** the account appears, and they may select it

#### Scenario: Assigning a role takes effect

- **WHEN** a role is assigned to a member who held none
- **THEN** the member holds what that role confers
