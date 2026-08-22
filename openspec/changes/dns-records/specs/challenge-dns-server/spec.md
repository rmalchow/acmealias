## MODIFIED Requirements

### Requirement: Authoritative server listens on standard DNS transports

The service SHALL answer DNS queries for every zone it is configured or provisioned to serve, on both
UDP and TCP port 53. It SHALL set the Authoritative Answer flag on responses for names within any
such zone and SHALL NOT perform recursion, setting Recursion Available to false.

The service SHALL answer every record type held for a name, not only the challenge record type, and
SHALL distinguish a name that does not exist from a name that exists with no records of the type
queried.

#### Scenario: Query over UDP

- **WHEN** a UDP query for a name in a served zone is received
- **THEN** the service returns an authoritative response

#### Scenario: Query over TCP

- **WHEN** the same query is sent over TCP
- **THEN** the service returns an equivalent authoritative response

#### Scenario: Recursion is refused

- **WHEN** a query arrives with the Recursion Desired flag set
- **THEN** the response has Recursion Available unset and no recursion is performed

#### Scenario: Several zones are served by one responder

- **WHEN** two zones are provisioned and a name in each is queried
- **THEN** both are answered authoritatively by the same responder

#### Scenario: A type with no records at an existing name

- **WHEN** a name exists with an address recordset and a mail exchanger query targets it
- **THEN** the response is `NOERROR` with an empty answer section and the zone `SOA` in authority

#### Scenario: A name that does not exist

- **WHEN** a query targets a name held by no recordset in a served zone
- **THEN** the response is `NXDOMAIN`
