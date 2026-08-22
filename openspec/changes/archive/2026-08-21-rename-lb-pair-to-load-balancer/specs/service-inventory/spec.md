## MODIFIED Requirements

### Requirement: The inventory is projects, load balancers and domains

The service SHALL store projects; each project SHALL own zero or more load balancers; each
load balancer SHALL own one or more nodes with their addresses and zero or more domains.
Each load balancer SHALL belong to exactly one project, and each domain SHALL belong to
exactly one load balancer.

The service SHALL NOT require, assume or imply any particular number of nodes beyond that
minimum, and SHALL treat a load balancer with a single node as complete.

Deleting a project or a load balancer that still owns children SHALL be refused.

#### Scenario: Domain is reachable through the hierarchy

- **WHEN** a domain is created under a load balancer of a project
- **THEN** the domain is listed for that load balancer, and the load balancer is listed for that project

#### Scenario: Non-empty parent is not deleted

- **WHEN** deletion is requested for a project that still has load balancers
- **THEN** the request is refused and nothing is deleted

#### Scenario: A single node is a complete load balancer

- **WHEN** a load balancer has exactly one node
- **THEN** its DNS plan is generated, its domains are served and no operation reports it as incomplete

#### Scenario: More than two nodes are served

- **WHEN** a load balancer has more than two nodes
- **THEN** its DNS plan contains an address record for every node address
