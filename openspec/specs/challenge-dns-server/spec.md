# challenge-dns-server Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
### Requirement: Authoritative server listens on standard DNS transports

The service SHALL answer DNS queries for its configured zone on both UDP and TCP port 53. It
SHALL set the Authoritative Answer flag on responses for names within the zone and SHALL NOT
perform recursion, setting Recursion Available to false.

#### Scenario: Query over UDP

- **WHEN** a UDP query for a name in the zone is received
- **THEN** the service returns an authoritative response

#### Scenario: Query over TCP

- **WHEN** the same query is sent over TCP
- **THEN** the service returns an equivalent authoritative response

#### Scenario: Recursion is refused

- **WHEN** a query arrives with the Recursion Desired flag set
- **THEN** the response has Recursion Available unset and no recursion is performed

### Requirement: TXT queries return active challenge values

For a TXT query whose name matches an alias in the zone, the service SHALL return one TXT
record per active challenge value for that alias, in a single response. When the alias exists
but has no active values, the service SHALL return `NOERROR` with an empty answer section and
the zone SOA in the authority section.

#### Scenario: Single active value

- **WHEN** one value is active for alias `shop-example-com` and a TXT query for `shop-example-com.<zone>` arrives
- **THEN** the answer section contains exactly that one TXT record

#### Scenario: Multiple active values

- **WHEN** two values are active for the same alias
- **THEN** the answer section contains both TXT records

#### Scenario: Alias with no active values

- **WHEN** a TXT query targets a configured alias with no active challenge
- **THEN** the response is `NOERROR` with an empty answer section and the SOA in authority

### Requirement: Challenge answers are not cached beyond their lifetime

TXT records for challenge values SHALL be served with a TTL short enough that a withdrawn value
stops being observable promptly, and negative answers SHALL be bounded by the SOA minimum
field. Both values SHALL be configurable.

#### Scenario: Short TTL on challenge answers

- **WHEN** a TXT answer for a challenge value is returned
- **THEN** its TTL equals the configured challenge TTL

### Requirement: Zone housekeeping records are served

The service SHALL answer SOA and NS queries for the zone apex from configuration, and SHALL
include the SOA in the authority section of negative responses. The SOA serial SHALL NOT be
required to change when challenge records change.

#### Scenario: SOA at the apex

- **WHEN** a SOA query for the zone apex arrives
- **THEN** the configured SOA record is returned authoritatively

#### Scenario: NS at the apex

- **WHEN** an NS query for the zone apex arrives
- **THEN** the configured NS record set is returned authoritatively

### Requirement: Unknown and out-of-zone names are handled correctly

For a name inside the zone that is not a configured alias, the service SHALL respond
`NXDOMAIN`. For a query outside the zone, the service SHALL respond `REFUSED`. For a
configured alias queried with a type it does not hold, the service SHALL respond `NOERROR` with
an empty answer section.

#### Scenario: Unknown name in zone

- **WHEN** a query targets `nosuchalias.<zone>`
- **THEN** the service responds `NXDOMAIN` with the SOA in authority

#### Scenario: Name outside the zone

- **WHEN** a query targets `www.example.org`
- **THEN** the service responds `REFUSED`

#### Scenario: Wrong type for an existing alias

- **WHEN** an A query targets a configured alias that holds only TXT values
- **THEN** the service responds `NOERROR` with an empty answer section

### Requirement: Server tolerates malformed and oversized traffic

The service SHALL respond `FORMERR` to unparseable queries and SHALL NOT terminate on them. It
SHALL support EDNS(0) advertised UDP payload sizes, and SHALL set the truncated flag on UDP
responses that exceed the negotiated size so the client retries over TCP.

#### Scenario: Malformed query

- **WHEN** a datagram that is not a valid DNS message is received
- **THEN** the service responds `FORMERR` where a header can be parsed, and continues serving

#### Scenario: Response exceeds UDP size

- **WHEN** the answer set does not fit the negotiated UDP payload size
- **THEN** the UDP response is truncated with the TC flag set

### Requirement: DNS reads are resilient to datastore unavailability

DNS query handling SHALL continue to answer from a cached view of active records when the
datastore is unreachable, rather than returning `SERVFAIL` for names whose values were known.
The cache SHALL be refreshed on write and on a configured interval.

#### Scenario: Datastore is down

- **WHEN** the datastore is unreachable and a TXT query targets an alias whose value was already known
- **THEN** the service still returns that value

#### Scenario: Cache reflects a write immediately

- **WHEN** a challenge value is published through the API
- **THEN** the next DNS query returns it without waiting for the refresh interval

