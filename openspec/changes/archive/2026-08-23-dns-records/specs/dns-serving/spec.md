## ADDED Requirements

### Requirement: The authoritative data is held centrally and satellites hold replicas

The service SHALL hold the authoritative contents of every zone it serves centrally. A satellite
SHALL hold a replica and SHALL NOT be the source of truth for any record.

A change SHALL be applied centrally and distributed; a satellite SHALL NOT originate a change.

#### Scenario: A satellite originates no change

- **WHEN** a satellite's interfaces are examined
- **THEN** no operation changes the authoritative contents of a zone

#### Scenario: The centre's contents govern

- **WHEN** a satellite's replica differs from the centre's contents
- **THEN** the satellite converges on the centre's contents

### Requirement: The serving backend is an interface with an in-process implementation

The service SHALL express how records reach a nameserver as an interface accepting whole recordsets,
and SHALL provide an implementation that answers from the process's own memory with no external
nameserver.

The interface SHALL accept a complete replacement of a zone's recordsets and an application of a
single recordset change.

#### Scenario: The in-process implementation answers without an external server

- **WHEN** a satellite runs with the in-process backend
- **THEN** it answers authoritative queries with no other nameserver present

#### Scenario: A complete replacement converges

- **WHEN** a complete set of recordsets is applied for a zone
- **THEN** the zone answers exactly those recordsets and no others

#### Scenario: Backends are interchangeable

- **WHEN** the same recordsets are applied through two implementations of the interface
- **THEN** the answers served are equivalent

### Requirement: A standard dynamic update backend is provided

The service SHALL provide a backend implementation that applies changes to an external nameserver
using the standard dynamic update protocol, authenticated with a shared key, so that a nameserver the
customer already operates can be driven without a product-specific integration.

#### Scenario: A change reaches an external nameserver

- **WHEN** a recordset is applied through the dynamic update backend
- **THEN** the external nameserver answers the new contents

#### Scenario: Updates are authenticated

- **WHEN** a dynamic update is sent
- **THEN** it is authenticated with the configured shared key, and an unauthenticated update is refused by the nameserver

#### Scenario: A failed update does not report success

- **WHEN** an external nameserver refuses an update
- **THEN** the failure is reported and the centre does not record the change as distributed

### Requirement: Distributed records carry a place for signatures

The distribution of a recordset SHALL include a field for its signature, whether or not signatures are
produced. A satellite SHALL accept a recordset whose signature field is empty.

#### Scenario: A recordset without a signature is served

- **WHEN** a recordset is distributed with an empty signature field
- **THEN** the satellite serves it

#### Scenario: The field is present in the format

- **WHEN** the distribution format is examined
- **THEN** a signature field is defined for a recordset

### Requirement: Challenge values are served through the same path as other records

The service SHALL serve challenge values as recordsets of the challenge zone, answered by the same
mechanism that answers every other recordset.

Challenge values SHALL retain their individual expiry, applied when the view is read. Other
recordsets SHALL have no expiry and SHALL persist until changed.

#### Scenario: A challenge value is answered like any recordset

- **WHEN** a challenge value is published and its name is queried
- **THEN** it is answered by the same lookup that answers other names

#### Scenario: Challenge expiry still applies

- **WHEN** a published challenge value passes its expiry
- **THEN** it is no longer answered

#### Scenario: Ordinary recordsets do not expire

- **WHEN** a recordset is stored and time passes without change
- **THEN** it continues to be answered
