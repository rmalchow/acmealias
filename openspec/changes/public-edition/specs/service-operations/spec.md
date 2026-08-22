## ADDED Requirements

### Requirement: One artifact serves both editions, selected at startup

The service SHALL be built as one artifact and SHALL determine its edition from configuration read at
startup, not from how it was compiled.

The edition in force SHALL be reported in the startup log and on the readiness endpoint.

#### Scenario: The running edition is discoverable

- **WHEN** an operator inspects a running deployment
- **THEN** the edition it is running is reported without reading its configuration files

#### Scenario: An unknown edition refuses to start

- **WHEN** the configured edition is not one the service supports
- **THEN** startup fails with a message naming the setting and the accepted values, and no listener is opened

#### Scenario: An edition missing what it requires refuses to start

- **WHEN** an edition is configured without a setting that edition requires
- **THEN** startup fails naming the missing setting

### Requirement: Editions do not share a datastore or a zone

A deployment of one edition SHALL NOT share its datastore with a deployment of another, and each edition
SHALL be authoritative for its own DNS zone.

#### Scenario: Editions are isolated deployments

- **WHEN** both editions are operated
- **THEN** each has its own datastore and its own zone, and neither can read the other's data

#### Scenario: An incident in one zone does not reach the other

- **WHEN** one edition's zone is unavailable
- **THEN** the other edition continues to answer for its own zone

### Requirement: The public site is delivered separately from the service

Content addressed to people who have no account SHALL be delivered by a deployment separate from the
service, and SHALL be releasable without releasing the service.

#### Scenario: Public content changes without a service release

- **WHEN** public-facing content is changed
- **THEN** it can be published without deploying the service

#### Scenario: The public site needs no session

- **WHEN** a person with no account requests public-facing content
- **THEN** it is served without authentication

### Requirement: Both editions are exercised before release

The acceptance environment SHALL exercise both editions, and SHALL fail if either is broken.

In the edition that authenticates locally it SHALL cover a registration, an address confirmation, a
domain reaching verification, a credit purchase, and a protective limit refusing an operation.

#### Scenario: A release covers the edition that is not used daily

- **WHEN** the acceptance suite runs
- **THEN** it covers both editions and fails if either is broken

#### Scenario: The commercial path is covered

- **WHEN** the acceptance suite runs against the locally authenticated edition
- **THEN** registration, confirmation, verification, purchase and a limit refusal are all exercised

### Requirement: The public site states the service's limits, contacts and obligations

The public site SHALL state what the service does and does not do, including that it does not issue
or hold certificates and that DNSSEC is not supported. It SHALL state that availability is
best-effort and carries no service level agreement.

It SHALL publish an abuse contact address together with the response time the service commits to,
a statement of what personal data is held and for how long, and the identity of the operator.

The site SHALL NOT be released without them.

#### Scenario: The limits are stated before registration

- **WHEN** the public site is read by someone with no account
- **THEN** it states that certificates are not issued and that DNSSEC is not supported

#### Scenario: No availability promise is implied

- **WHEN** the terms are read
- **THEN** they state that availability is best-effort and that no service level is guaranteed

#### Scenario: Abuse reports have somewhere to go

- **WHEN** the public site is read
- **THEN** an abuse contact address and the committed response time are published

#### Scenario: Retention of personal data is disclosed

- **WHEN** the privacy statement is read
- **THEN** it covers registration addresses, zone contents, staff access records and blocked identities, each with a retention period
