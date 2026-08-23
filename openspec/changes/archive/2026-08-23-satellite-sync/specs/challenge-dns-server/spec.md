## MODIFIED Requirements

### Requirement: DNS reads are resilient to datastore unavailability

DNS query handling SHALL continue to answer from a cached view of active records when the source of
that view is unreachable, rather than returning `SERVFAIL` for names whose values were known. The
source SHALL be either the datastore or a synchronisation stream, and the behaviour SHALL NOT differ
between them.

The cache SHALL be refreshed on write where the source is the datastore, on receipt of a change where
the source is a synchronisation stream, and on a configured interval in both cases.

Expiry SHALL be applied when the view is read, so that a value ceases to be answered at its expiry
regardless of when the view was last refreshed and regardless of whether the source is reachable.

#### Scenario: Datastore is down

- **WHEN** the datastore is unreachable and a TXT query targets an alias whose value was already known
- **THEN** the service still returns that value

#### Scenario: Synchronisation stream is down

- **WHEN** the synchronisation stream is unreachable and a TXT query targets an alias whose value was already known
- **THEN** the service still returns that value

#### Scenario: Cache reflects a write immediately

- **WHEN** a challenge value is published through the API
- **THEN** the next DNS query returns it without waiting for the refresh interval

#### Scenario: Cache reflects a distributed change immediately

- **WHEN** a change is received over the synchronisation stream
- **THEN** the next DNS query reflects it without waiting for the refresh interval

#### Scenario: An expired value is not answered by a disconnected server

- **WHEN** the source of the view is unreachable and a held value passes its expiry
- **THEN** it is no longer answered
