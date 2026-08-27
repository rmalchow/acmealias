## ADDED Requirements

### Requirement: A self-registered user belongs to nothing until membership is written

Where the identity provider permits a person to create their own account, the resulting user SHALL
hold no group that any role is configured for, and SHALL therefore hold no role and be refused
access by the service.

Access SHALL become possible only when account membership is written through the service's own
mechanisms, and SHALL NOT become possible through any change of group membership made at the
identity provider alone.

This is the observable form of the rule that tenancy is never derived from a directory: an identity
provider can create a person, and cannot place that person inside an account.

#### Scenario: Self-registration produces an authenticated user with no access

- **WHEN** a person registers themselves at the identity provider and signs in
- **THEN** authentication succeeds and the service refuses access, reporting the groups it observed

#### Scenario: A directory group does not confer membership of an account

- **WHEN** a self-registered user is added to a group at the identity provider that no role is configured for
- **THEN** they hold no role and remain without access

#### Scenario: The refusal explains itself to someone who has just signed up

- **WHEN** a self-registered user is refused for holding no role
- **THEN** the response distinguishes "your account is not yet associated with anything" from a misconfiguration of the service
