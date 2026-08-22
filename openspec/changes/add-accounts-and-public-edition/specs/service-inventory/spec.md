## MODIFIED Requirements

### Requirement: The inventory is projects, load balancers and domains

Each project SHALL belong to exactly one account, in addition to the hierarchy already required.
The rest of the hierarchy is unchanged, and no object below a project SHALL record an owning
account of its own.

Challenge names SHALL be unique within an account rather than across the service: two accounts
may each serve the same domain name, and neither may answer the other's challenges.

#### Scenario: Domain is reachable through the hierarchy

- **WHEN** a domain is created under a load balancer of a project
- **THEN** the domain is listed for that load balancer, and the load balancer is listed for that project

#### Scenario: Non-empty parent is not deleted

- **WHEN** deletion is requested for a project that still has load balancers
- **THEN** the request is refused and nothing is deleted

#### Scenario: Two accounts may serve the same domain

- **WHEN** two accounts each add the same domain name
- **THEN** each has its own challenge name and alias, and neither can answer challenges for the other

### Requirement: An alias is one label in the served zone per challenge name

Alias labels SHALL remain unique across the service and SHALL keep the syntactic constraints
already required.

Where the service serves mutually untrusting customers, the label SHALL be unguessable and SHALL
NOT be derived from the domain, and the service SHALL NOT be configured to derive it. The served zone is
public: a derived label collides when two accounts hold the same domain name, and discloses
which domains the service serves to anyone who reads the zone — including in the customer's own
zone, where the delegation record is world-readable by design.

#### Scenario: Shared alias for wildcard and apex

- **WHEN** `example.com` and `*.example.com` are added to the same load balancer
- **THEN** exactly one alias exists for `_acme-challenge.example.com` and both domains reference it

#### Scenario: A public alias does not disclose its domain

- **WHEN** an alias label serving mutually untrusting customers is read from the zone
- **THEN** it does not reveal the domain it serves, and it was not chosen by the customer

#### Scenario: A label is never reused across accounts

- **WHEN** two accounts each add the same domain name
- **THEN** each alias carries its own label and no label is shared between them

## ADDED Requirements

### Requirement: A domain earns nothing until it is verified

A domain that has not been shown to delegate to the service SHALL NOT appear in any other
account's DNS plan and SHALL NOT hold a claim that prevents another account from adding the same
name.

Control of a domain SHALL NOT require a separate proof: publishing the delegation the service
asks for is itself the proof, because it can only be published by whoever controls that zone.

#### Scenario: Adding a domain does not claim it

- **WHEN** an account adds a domain it does not control
- **THEN** no other account is prevented from adding the same domain

#### Scenario: Verification is the proof of control

- **WHEN** the delegation for a domain resolves to that account's alias
- **THEN** the domain is verified for that account
