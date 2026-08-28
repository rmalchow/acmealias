## ADDED Requirements

### Requirement: An API key is of one of two kinds

An API key SHALL be either a project key, bound to one project, or a system key, holding a role.

Both kinds SHALL be authorized by the same mechanism as any other subject. Neither SHALL introduce a
decision path of its own.

#### Scenario: A project key reaches its project

- **WHEN** a project key is presented for an operation on its own project
- **THEN** it is permitted, as its kind allows

#### Scenario: A project key reaches nothing else

- **WHEN** a project key is presented for an operation on another project
- **THEN** it is refused

#### Scenario: A system key acts as its role

- **WHEN** a system key is presented for an operation
- **THEN** it is permitted exactly where a member holding the same role would be

### Requirement: A project key does not outlive its project

Deleting a project SHALL delete the keys bound to it.

#### Scenario: The project goes and takes its keys

- **WHEN** a project holding keys is deleted
- **THEN** those keys no longer exist and no longer authenticate

### Requirement: A system key exists only where roles do

A system key SHALL NOT be creatable in a personal account.

The refusal SHALL report that such a key cannot exist there, distinguishably from a refusal of
permission.

#### Scenario: A personal account has no system keys

- **WHEN** a system key is requested in a personal account
- **THEN** it is refused as impossible rather than as forbidden

#### Scenario: A personal account still has project keys

- **WHEN** a project key is created in a personal account
- **THEN** it is created and works

### Requirement: A system key's role cannot be deleted from under it

Deleting a role held by a system key SHALL be refused.

A system key whose role is absent SHALL authorize nothing.

#### Scenario: The role is defended

- **WHEN** a role held by a system key is deleted
- **THEN** the deletion is refused and names what holds it

#### Scenario: An absent role grants nothing

- **WHEN** a system key's role cannot be resolved
- **THEN** the key authorizes nothing
