## ADDED Requirements

### Requirement: A satellite serves DNS from a replica and holds no datastore

The service SHALL support running as a satellite: a process that answers authoritative DNS from an
in-memory replica of the answerable state, opens no connection to the datastore, and serves no
management interface or console.

A satellite SHALL answer queries identically to the centre for the same state.

#### Scenario: A satellite answers without a datastore

- **WHEN** a satellite runs with no datastore reachable and holds a loaded replica
- **THEN** it answers queries for names in that replica

#### Scenario: A satellite serves no management interface

- **WHEN** a satellite's listeners are examined
- **THEN** no management interface and no console are served

#### Scenario: Answers do not differ between centre and satellite

- **WHEN** the same query is made against the centre and against a satellite holding the same state
- **THEN** the responses are equivalent

### Requirement: The run mode is configuration and is reported

The service SHALL select whether it runs as a centre or as a satellite from configuration read at
startup, from a single artefact, and SHALL report the selected mode in its startup log and on its
readiness endpoint.

#### Scenario: The mode is visible at runtime

- **WHEN** a running process is inspected through its readiness endpoint
- **THEN** the mode it is running in is reported

#### Scenario: One artefact serves both modes

- **WHEN** the released artefact is examined
- **THEN** it can be started in either mode without rebuilding

### Requirement: A satellite is not ready before it holds a replica

A satellite SHALL NOT report readiness, and SHALL NOT accept DNS queries, until it has successfully
loaded a replica at least once.

A satellite holding a replica SHALL report readiness whether or not it can currently reach the
centre.

#### Scenario: An unloaded satellite is not ready

- **WHEN** a satellite has started and has never loaded a replica
- **THEN** it does not report readiness and answers no queries

#### Scenario: A disconnected satellite with a replica is ready

- **WHEN** a satellite holds a replica and cannot reach the centre
- **THEN** it reports readiness and continues to answer

### Requirement: A satellite is a platform object with no owning account

A satellite SHALL have no owning account, SHALL be administered through the staff authorization
plane, and SHALL NOT be visible through any account's management interface.

A satellite SHALL hold no permission that is scoped to an account.

#### Scenario: Satellites are administered by staff

- **WHEN** a satellite is registered, observed or retired
- **THEN** the operation requires the platform administration permission

#### Scenario: Satellites are invisible to accounts

- **WHEN** an account member reads their management interface
- **THEN** no satellite is listed and no operation names one

### Requirement: A satellite is enrolled with a single-use token and thereafter holds its own credential

Registering a satellite SHALL produce an enrolment token that is displayed once, stored only as a
hash, and expires after a configured interval. A satellite SHALL exchange that token exactly once for
a credential unique to it, after which the token SHALL be permanently invalid.

The satellite's credential SHALL authenticate both the distribution stream and the report channel,
and SHALL be revocable individually without affecting any other satellite.

A satellite whose credential has been revoked SHALL NOT be able to establish or resume a
distribution connection.

#### Scenario: A token is usable once

- **WHEN** an enrolment token is exchanged for a credential and then presented again
- **THEN** the second exchange is refused

#### Scenario: An expired token is refused

- **WHEN** an enrolment token is presented after its expiry
- **THEN** the exchange is refused and no credential is issued

#### Scenario: Revocation stops one satellite only

- **WHEN** one satellite's credential is revoked
- **THEN** that satellite cannot connect or report, and every other satellite is unaffected

#### Scenario: One credential serves both channels

- **WHEN** a satellite connects to the distribution stream and posts a report
- **THEN** both are authenticated by the same per-satellite credential

### Requirement: Every satellite holds the whole replica and none is operated by a customer

The distribution stream SHALL deliver the same set of zones to every satellite, and SHALL NOT be
partitioned per satellite. A satellite SHALL NOT be enrolled by, or operated on behalf of, an
account.

#### Scenario: Replicas do not differ between satellites

- **WHEN** two satellites are current at the same position
- **THEN** they hold the same zones and answer identically

#### Scenario: Enrolment is a staff operation only

- **WHEN** enrolment is attempted through an account's management interface
- **THEN** it is refused, and no account-facing operation registers a satellite

### Requirement: A satellite's position and staleness are observable

Each satellite SHALL report the position in the centre's change sequence that it has applied and when
it last applied one. The centre SHALL expose, per satellite, how far behind it is in events and in
time.

#### Scenario: Lag is reported per satellite

- **WHEN** the fleet is inspected
- **THEN** each satellite reports its applied position and the time it was applied

#### Scenario: A stalled satellite is distinguishable from a healthy one

- **WHEN** a satellite holds an open connection but has stopped applying events
- **THEN** its reported lag grows and it is distinguishable from a satellite that is current
