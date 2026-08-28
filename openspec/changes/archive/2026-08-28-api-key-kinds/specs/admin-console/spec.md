## ADDED Requirements

### Requirement: Each kind of key is managed where it belongs

The console SHALL offer project keys on a project's own screen, and system keys in a team account's
settings.

#### Scenario: Project keys sit with the project

- **WHEN** a person opens a project
- **THEN** they can create and revoke that project's keys there

#### Scenario: System keys sit with the roster

- **WHEN** a person opens a team account's settings
- **THEN** they can create and revoke system keys there, beside roles and members

### Requirement: The address limit explains itself before it is chosen

Where a project has no node addresses, the console SHALL say so where the address limit is offered,
rather than presenting a control that would refuse everything.

#### Scenario: A project with no nodes

- **WHEN** the address limit is offered on a project holding no nodes
- **THEN** the console reports that there are no addresses to permit yet
