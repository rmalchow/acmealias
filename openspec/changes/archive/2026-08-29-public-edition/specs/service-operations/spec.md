## ADDED Requirements

### Requirement: Deployments of different environments do not share a datastore or a zone

A deployment SHALL NOT share its datastore or its DNS zone with a deployment serving a different
environment.

The service's own hostnames — the public site, the console, the staff surface and the identity
provider — SHALL be served by a zone this service is not the sole authority for, so that they remain
reachable while the service is not.

#### Scenario: Environments are isolated deployments

- **WHEN** more than one environment is operated
- **THEN** each has its own datastore and its own zone, and neither can read the other's data

#### Scenario: The service being down does not hide the pages that explain it

- **WHEN** the service is unavailable
- **THEN** the public site and the pages describing the service's state are still resolvable and served

### Requirement: The public site is delivered separately from the service

Content addressed to people who have no account SHALL be delivered by a deployment separate from the
service, and SHALL be releasable without releasing the service.

#### Scenario: Public content changes without a service release

- **WHEN** public-facing content is changed
- **THEN** it can be published without deploying the service

#### Scenario: The public site needs no session

- **WHEN** a person with no account requests public-facing content
- **THEN** it is served without authentication

### Requirement: The staff surface is unreachable through the customer hostname

A deployment SHALL refuse the staff paths on the customer hostname, and SHALL restrict which
addresses may reach the staff hostname, wherever the two surfaces are served by one listener and
distinguished only by hostname. Neither rule SHALL be relied on without the other.

#### Scenario: The staff tree is not served on the customer name

- **WHEN** a staff path is requested through the customer hostname
- **THEN** it is refused, whoever is asking and whatever authority they hold

#### Scenario: The staff hostname is restricted

- **WHEN** the staff hostname is reached from an address that is not permitted
- **THEN** the request does not reach the service

### Requirement: The public site states the service's limits, contacts and obligations

The public site SHALL state what the service does and does not do, including that it does not host
DNS, does not issue certificates, and does not hold private keys. It SHALL state that availability is
best-effort and carries no service level agreement.

It SHALL publish an abuse contact address together with the response time the service commits to, a
statement of what personal data is held and for how long, and the identity of the operator.

The site SHALL NOT be released without them.

#### Scenario: The boundary is stated before anyone signs up

- **WHEN** the public site is read by someone with no account
- **THEN** it states that DNS is not hosted, that certificates are not issued, and that no private key is held

#### Scenario: No availability promise is implied

- **WHEN** the terms are read
- **THEN** they state that availability is best-effort and that no service level is guaranteed

#### Scenario: Abuse reports have somewhere to go

- **WHEN** the public site is read
- **THEN** an abuse contact address and the committed response time are published

#### Scenario: Retention of personal data is disclosed

- **WHEN** the privacy statement is read
- **THEN** it covers registration addresses, zone contents, challenge interaction records, staff access records and blocked identities, each with a retention period

#### Scenario: The operator is identifiable

- **WHEN** the public site is read
- **THEN** the identity and contact address of the operator are published
