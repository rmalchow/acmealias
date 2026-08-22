## MODIFIED Requirements

### Requirement: Unknown and out-of-zone names are handled correctly

For a name inside a served zone that holds no records, the service SHALL respond `NXDOMAIN`. For a
query outside every served zone, the service SHALL respond `REFUSED`. For a name that exists in a
served zone queried with a type it does not hold, the service SHALL respond `NOERROR` with an empty
answer section.

A zone that has been withdrawn from service SHALL cease to be a served zone, so that queries for its
names are outside every served zone and are answered `REFUSED`. The service SHALL NOT retain a
withdrawn zone in order to answer `NXDOMAIN` for its names.

#### Scenario: Unknown name in zone

- **WHEN** a query targets `nosuchalias.<zone>`
- **THEN** the service responds `NXDOMAIN` with the SOA in authority

#### Scenario: Name outside the zone

- **WHEN** a query targets `www.example.org`
- **THEN** the service responds `REFUSED`

#### Scenario: Wrong type for an existing alias

- **WHEN** an A query targets a configured alias that holds only TXT values
- **THEN** the service responds `NOERROR` with an empty answer section

#### Scenario: Withdrawn zone is refused, not denied

- **WHEN** a zone is withdrawn from service and a name within it is queried
- **THEN** the service responds `REFUSED` and does not respond `NXDOMAIN`

#### Scenario: A restored zone answers again without delay

- **WHEN** a withdrawn zone is restored to service
- **THEN** its names are answered authoritatively without waiting for a previously cached denial to expire
