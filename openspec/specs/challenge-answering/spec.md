# challenge-answering Specification

## Purpose
TBD - created by archiving change api-key-kinds. Update Purpose after archive.
## Requirements
### Requirement: A project key may be limited to its project's node addresses

A project key SHALL be able to require that requests arrive from one of the addresses configured on
its project's nodes.

The set of permitted addresses SHALL be resolved when a request is evaluated, so that changing a
project's nodes changes what the key permits without any further action.

#### Scenario: A node added later is permitted

- **WHEN** a node is added to a project whose key carries the limit
- **THEN** requests from that node's address are permitted, with no change to the key

#### Scenario: A node removed later is refused

- **WHEN** a node is removed from that project
- **THEN** requests from its address are refused

#### Scenario: An address outside the project is refused

- **WHEN** a request carrying the key arrives from an address no node holds
- **THEN** it is refused, and the refusal names the address it saw

### Requirement: A limit with nothing to permit permits nothing

Where the limit is required and the project has no node addresses, every request SHALL be refused.

This SHALL be distinguishable from a key carrying no limit at all.

#### Scenario: No nodes means no access

- **WHEN** a key requires its project's addresses and the project has none
- **THEN** requests are refused rather than permitted

### Requirement: The service accepts publication in the acme-dns protocol

The service SHALL accept a challenge publication addressed in the acme-dns protocol, identifying the
alias by its label and carrying the value to publish.

A publication so addressed SHALL be subject to the same authorization as one addressed in any other
supported protocol.

#### Scenario: A client publishes in the acme-dns dialect

- **WHEN** an authenticated caller publishes a value naming an alias by its label
- **THEN** the value is answered for that alias exactly as if it had been published by challenge name

#### Scenario: A spelling does not confer authority

- **WHEN** a caller that may not publish for an alias addresses it by its label instead of by its
  challenge name
- **THEN** the publication is refused

### Requirement: A value published without a withdrawal signal still expires

Where a protocol provides no means to withdraw a published value, the value SHALL still cease to be
answered after the configured lifetime.

#### Scenario: A client that never withdraws

- **WHEN** a value is published by a protocol with no withdrawal operation and is not republished
- **THEN** it stops being answered once its lifetime has passed

### Requirement: Account creation is not reachable from the publication protocol

The service SHALL NOT create an account, a domain or an alias in response to an unauthenticated
request in any publication protocol.

A request attempting it SHALL be refused with a response naming where credentials are obtained.

#### Scenario: Unauthenticated registration is refused

- **WHEN** an unauthenticated caller attempts the protocol's registration operation
- **THEN** nothing is created, and the refusal says where credentials come from

