## ADDED Requirements

### Requirement: The grant grammar does not name a load balancer

The authorization vocabulary SHALL NOT accept `load_balancer` as an object type.

A grant naming a project SHALL reach the nodes, domains, aliases and keys within it.

#### Scenario: The removed object type is refused

- **WHEN** a grant naming a load balancer is written
- **THEN** it is refused rather than stored

#### Scenario: A project grant reaches the configuration

- **WHEN** a subject holds a grant on a project
- **THEN** it may act on that project's nodes, domains, aliases and keys as the action permits
