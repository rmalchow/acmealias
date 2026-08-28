## REMOVED Requirements

### Requirement: A grant may name a load balancer

**Reason**: The load balancer is no longer a distinct object; a project holds the configuration
directly, so a grant naming a project reaches what a grant naming a load balancer used to reach.

**Migration**: Stored grants naming a load balancer are rewritten to name the project it became. The
object type is refused thereafter rather than accepted and ignored.

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
