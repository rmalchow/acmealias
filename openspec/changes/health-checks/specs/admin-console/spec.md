## ADDED Requirements

### Requirement: The console shows the observed certificate per node address

The console SHALL present, for each address of each node, the certificate observed on it with its
expiry and issuer, the names it covers, and a history of those observations.

The view SHALL identify domains of the load balancer that no observed certificate covers, and
certificates whose expiry is approaching.

#### Scenario: The current observation is shown per node address

- **WHEN** a user opens a load balancer's certificate view
- **THEN** each node address shows the certificate most recently observed on it

#### Scenario: Address families are distinguished

- **WHEN** a node serves one certificate over IPv4 and a different one over IPv6
- **THEN** the view shows both, attributed to their address families

#### Scenario: History is shown

- **WHEN** a user opens a node address's certificate history
- **THEN** the observations recorded for it are shown, most recent first

#### Scenario: A failed handshake is distinguishable from an absent certificate

- **WHEN** the most recent observation for a node address records a failed handshake
- **THEN** the view says the address could not be examined, and does not report the node as having no
  certificate

#### Scenario: Uncovered domains are surfaced

- **WHEN** a domain of the load balancer appears in no observed certificate
- **THEN** the view identifies it

#### Scenario: An approaching expiry is surfaced

- **WHEN** an observed certificate's expiry falls within the configured threshold
- **THEN** the view identifies it

#### Scenario: Observations require permission to read them

- **WHEN** a user holding no grant permitting certificate observations to be read opens the view
- **THEN** no observation is shown
