## ADDED Requirements

### Requirement: The console shows a status view per node

The console SHALL present, for each node, its current reachability, the certificate observed on it
with its expiry and issuer, and a history of both.

The view SHALL identify domains of the load balancer that no observed certificate covers, and
certificates whose expiry is approaching.

#### Scenario: Current state is shown per node

- **WHEN** a user opens a load balancer's status view
- **THEN** each node shows its current reachability and its observed certificate

#### Scenario: History is shown

- **WHEN** a user opens a node's status
- **THEN** a history of reachability and of certificate observations is shown

#### Scenario: Uncovered domains are surfaced

- **WHEN** a domain of the load balancer appears in no observed certificate
- **THEN** the view identifies it

#### Scenario: An approaching expiry is surfaced

- **WHEN** an observed certificate's expiry falls within the configured threshold
- **THEN** the view identifies it

#### Scenario: Status requires permission to read it

- **WHEN** a user holding no grant permitting status to be read opens the view
- **THEN** no status is shown

### Requirement: The console shows when a recordset is being filtered by health

The console SHALL indicate, for a health-checked recordset, which of its stored values are currently
served and which are excluded, and SHALL indicate when every value is excluded and all are therefore
being served.

#### Scenario: Excluded values are distinguishable

- **WHEN** a health-checked recordset has an unavailable node
- **THEN** the view shows that value as stored and not served

#### Scenario: The all-unavailable state is visible

- **WHEN** every node of a health-checked recordset is unavailable
- **THEN** the view shows that all values are being served despite failing checks
