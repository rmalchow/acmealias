# challenge-record-store Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
### Requirement: Challenge records are persisted durably

Challenge values SHALL be stored in PostgreSQL, associated with the alias mapping they belong
to, and SHALL survive process restart. A record SHALL carry its creation time and expiry time.

#### Scenario: Values survive a restart

- **WHEN** a value is published and the service process is restarted
- **THEN** DNS queries for the alias still return that value

#### Scenario: Record carries timestamps

- **WHEN** a challenge record is created
- **THEN** it stores the creation time and a computed expiry time

### Requirement: An alias may hold several values simultaneously

The store SHALL allow multiple distinct active values for one alias, as required when a
certificate covers both a domain and its wildcard. Storing a value that is already active for
the same alias SHALL leave a single record and refresh its expiry.

#### Scenario: Concurrent values coexist

- **WHEN** two different values are stored for one alias
- **THEN** both are active and both are returned by DNS

#### Scenario: Re-storing refreshes rather than duplicates

- **WHEN** a value already active for an alias is stored again
- **THEN** exactly one record exists and its expiry is extended

### Requirement: Records expire automatically

Records SHALL become unanswerable once their expiry time has passed, whether or not cleanup was
called, and expired rows SHALL be deleted by a periodic sweep. The record lifetime SHALL be
configurable.

#### Scenario: Expired value is not answered

- **WHEN** a record's expiry time has passed
- **THEN** DNS queries for its alias no longer return that value, before the sweep has run

#### Scenario: Sweep removes expired rows

- **WHEN** the periodic sweep runs
- **THEN** rows whose expiry has passed are deleted and the count removed is recorded

### Requirement: Concurrent writes are safe

Simultaneous publish and withdraw operations for the same alias SHALL not lose records, produce
duplicates, or deadlock. Uniqueness of `(alias, value)` SHALL be enforced by a datastore
constraint.

#### Scenario: Simultaneous presents for one alias

- **WHEN** two publish operations for the same alias with different values run concurrently
- **THEN** both records exist afterwards

#### Scenario: Simultaneous identical presents

- **WHEN** two publish operations with the same alias and value run concurrently
- **THEN** exactly one record exists and neither operation reports an error

### Requirement: Records are attributed to the credential that published them

Each challenge record SHALL retain the identifier of the API key that published it, for the
lifetime of the record. Revoking or deleting that key SHALL NOT delete or invalidate records it
published.

#### Scenario: Live record names its publisher

- **WHEN** a challenge record is inspected through the management interface
- **THEN** it reports the key that published it and when

#### Scenario: Key revocation does not withdraw published values

- **WHEN** the key that published an active value is revoked
- **THEN** the value remains answerable until it is cleaned up or expires

