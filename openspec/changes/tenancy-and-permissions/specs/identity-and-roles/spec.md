## REMOVED Requirements

### Requirement: Roles are global and fixed

**Reason**: The four roles were constants in code, held globally, with no scoping to a subset of
projects. Per-project API keys, user-created projects and DNS record management each require
authority to be a fact about a subject *and an object*, which a fixed global role cannot express.
Roles become records within an account holding permission grants, and the permission model is
specified by the `permissions` capability.

**Migration**: The four role names survive as seeded records in the single account of an
identity-provider edition, holding grants that reproduce the previous permission matrix exactly. An
operator whose groups mapped to `CONFIG_ADMIN` continues to hold a role of that name conferring the
same authority. Callers of `Can(roles, action)` become callers of `Can(subject, action, object)`.

## MODIFIED Requirements

### Requirement: Group claims are mapped to roles by configuration

The service SHALL read the group membership of the authenticated user from a configurable claim
of the identity token, and SHALL derive roles by comparing those groups against one configuration
setting per role, each naming the group that grants it. The mapping SHALL NOT be stored in the
database and SHALL NOT be modifiable through the management API.

Each setting SHALL name a role record of the account the edition operates as. A user's roles SHALL
be the union of every role whose configured group appears in their groups claim, held as memberships
in that account. A role whose setting is unset SHALL be held by nobody.

#### Scenario: Configured group grants its role

- **WHEN** a user's groups include the group named by a role's setting
- **THEN** the user holds that role in the account the edition operates as

#### Scenario: Roles are additive

- **WHEN** a user's groups include the groups configured for two different roles
- **THEN** the user holds both, and their authority is the union of both roles' grants

#### Scenario: Unset role is held by nobody

- **WHEN** no group is configured for a role
- **THEN** no user holds that role

#### Scenario: Mapping is not editable at runtime

- **WHEN** any management route is searched for an operation that changes the group-to-role mapping
- **THEN** no such operation exists

#### Scenario: A setting naming no role is held by nobody

- **WHEN** a setting names a role record that does not exist
- **THEN** no user holds it, and the service reports the mismatch at startup

#### Scenario: Authenticated user with no mapped group is told why

- **WHEN** a user authenticates successfully but is in none of the configured groups
- **THEN** access is refused and the response reports the groups that were observed

## ADDED Requirements

### Requirement: Administering roles is itself a permission

The service SHALL require a permission to create, edit or delete a role, and to change a person's
memberships. That permission SHALL be grantable like any other, and SHALL confer authority only
within the account holding it.

A subject administering roles SHALL be bound by the rule that it may grant only what it holds.

#### Scenario: Role administration is refused without the permission

- **WHEN** a member without the role-administration permission creates or edits a role
- **THEN** the request is refused

#### Scenario: Role administration does not cross accounts

- **WHEN** a member holding the role-administration permission in one account edits a role of another
- **THEN** the request is refused

#### Scenario: A role cannot be given authority its author lacks

- **WHEN** a member holding the role-administration permission adds a grant it does not itself hold
- **THEN** the request is refused

### Requirement: Seeded roles reproduce the previous authority exactly

When the schema is migrated the service SHALL create, in the single account, one role for each of
the four previously fixed roles, bearing the same name and holding grants conferring the same
authority the permission matrix conferred before the migration.

#### Scenario: A seeded role confers what its predecessor conferred

- **WHEN** an operator whose groups map to a seeded role performs an operation that role permitted before the migration
- **THEN** the operation is permitted

#### Scenario: A seeded role confers nothing more

- **WHEN** that operator performs an operation the role did not permit before the migration
- **THEN** the request is refused

#### Scenario: Seeded roles are ordinary records

- **WHEN** a member holding the role-administration permission inspects the seeded roles
- **THEN** they are listed with their grants, and all but the unrestricted role may be edited
