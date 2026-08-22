## ADDED Requirements

### Requirement: The satellite opens the connection to the centre

Synchronisation SHALL be initiated by the satellite. The centre SHALL NOT require a network path to a
satellite in order to distribute state, and SHALL NOT require a satellite to be reachable at a
publicly routable address other than for DNS.

#### Scenario: A satellite behind address translation synchronises

- **WHEN** a satellite runs behind network address translation with no inbound rule for the control channel
- **THEN** it synchronises

#### Scenario: The centre initiates nothing

- **WHEN** the centre's outbound connections are examined
- **THEN** none is made to a satellite for the purpose of distributing state

### Requirement: State is distributed as an ordered event stream with resumable positions

The centre SHALL maintain a monotonic sequence over changes to the answerable state and SHALL send
each change as an event carrying its position.

A satellite SHALL send the position it last applied when it connects. The centre SHALL respond with
the changes since that position, or with a complete snapshot followed by the stream when the position
is unknown, absent, or older than the centre retains.

#### Scenario: A new satellite receives a snapshot

- **WHEN** a satellite connects with no position
- **THEN** it receives a complete snapshot and then subsequent changes

#### Scenario: A reconnecting satellite resumes

- **WHEN** a satellite reconnects with a position the centre retains
- **THEN** it receives the changes since that position and no snapshot

#### Scenario: A satellite too far behind receives a snapshot

- **WHEN** a satellite reconnects with a position older than the centre retains
- **THEN** it receives a complete snapshot rather than an error

#### Scenario: Changes are applied in order

- **WHEN** a satellite applies the stream
- **THEN** changes are applied in the order of their positions

### Requirement: A satellite that cannot apply a change requests a complete snapshot

A satellite SHALL request a complete snapshot when it receives an event out of sequence or cannot
apply one. It SHALL NOT continue from a state it cannot account for.

#### Scenario: An out-of-sequence event triggers a snapshot

- **WHEN** a satellite receives an event whose position does not follow the one it last applied
- **THEN** it requests a complete snapshot

#### Scenario: Divergence is self-correcting

- **WHEN** a satellite's replica cannot be reconciled with the stream
- **THEN** it converges by replacing its replica wholesale without operator intervention

### Requirement: A satellite serves its last known state when the centre is unreachable

A satellite SHALL continue to answer from the replica it holds when it cannot reach the centre, and
SHALL NOT withdraw names whose values it knows because synchronisation has failed.

A satellite SHALL apply the expiry carried by each value locally, so that a value stops being
answered at its expiry whether or not the centre is reachable.

#### Scenario: A disconnected satellite keeps answering

- **WHEN** a satellite cannot reach the centre and a query targets a name whose value it holds
- **THEN** it returns that value

#### Scenario: Expiry is enforced without the centre

- **WHEN** a value held by a disconnected satellite passes its expiry
- **THEN** the satellite stops answering it

#### Scenario: Failure to synchronise is reported

- **WHEN** synchronisation fails
- **THEN** the failure is recorded in the satellite's status and does not change what is served

### Requirement: Reports are sent upward in batches that may be re-delivered safely

A satellite SHALL send observations to the centre as batches, separately from the stream that
distributes state. Each batch SHALL carry an identifier, and the centre SHALL accept a re-delivered
batch without applying it twice.

A satellite SHALL NOT be required to retain a batch it cannot deliver.

#### Scenario: A re-delivered batch is applied once

- **WHEN** a satellite delivers a batch, receives no response, and delivers it again
- **THEN** the centre records its contents exactly once

#### Scenario: Reports do not delay distribution

- **WHEN** a large batch is being delivered
- **THEN** changes continue to be distributed to that satellite

#### Scenario: An undelivered batch may be discarded

- **WHEN** a satellite cannot deliver a batch
- **THEN** it may discard it, and nothing is recorded for it
