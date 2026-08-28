## ADDED Requirements

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
