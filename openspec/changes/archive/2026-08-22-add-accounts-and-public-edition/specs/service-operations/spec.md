## ADDED Requirements

### Requirement: One artifact serves both editions, selected at startup

The service SHALL be built as one artifact and SHALL determine its edition from configuration
read at startup, not from how it was compiled. The edition in force SHALL be reported in the
startup log and on the readiness endpoint.

#### Scenario: The running edition is discoverable

- **WHEN** an operator inspects a running deployment
- **THEN** the edition it is running is reported without reading its configuration files

#### Scenario: An unknown edition refuses to start

- **WHEN** the configured edition is not one the service supports
- **THEN** startup fails with a message naming the setting and the accepted values

### Requirement: Editions do not share a datastore or a zone

A deployment of one edition SHALL NOT share its datastore with a deployment of another, and each
edition SHALL be authoritative for its own DNS zone.

#### Scenario: Editions are isolated deployments

- **WHEN** both editions are operated
- **THEN** each has its own datastore and its own zone, and neither can read the other's data

### Requirement: The public site is delivered separately from the service

Content addressed to people who have no account SHALL be delivered by a deployment separate from
the service, and SHALL be releasable without releasing the service.

#### Scenario: Public content changes without a service release

- **WHEN** public-facing content is changed
- **THEN** it can be published without deploying the service

### Requirement: Both editions are exercised before release

The acceptance environment SHALL exercise both editions, including in the public edition a
registration, an address confirmation, a domain reaching verification, and a quota refusing a
creation.

#### Scenario: A release covers the edition that is not used daily

- **WHEN** the acceptance suite runs
- **THEN** it covers both editions and fails if either is broken
