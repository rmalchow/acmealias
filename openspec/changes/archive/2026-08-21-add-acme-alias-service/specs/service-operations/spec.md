## ADDED Requirements

### Requirement: Configuration comes from the environment and is validated at startup

The service SHALL read all configuration from environment variables, covering at minimum: served
zone, SOA and NS values, listen addresses for DNS and for each HTTP listener, database
connection, TTLs, challenge lifetime, trusted proxy networks, the resolver used for
verification, the OpenID Connect issuer and client, the group claim name, the group that grants
each role, the session idle and absolute lifetimes, and the certificate authority identifiers used
for generated CAA records. Invalid or missing required configuration SHALL cause the process to exit
non-zero with a message naming the offending setting, before any listener is opened.

Validation SHALL be local: it SHALL NOT require any external service to be reachable, so that an
unavailable dependency cannot prevent the service from starting.

#### Scenario: Missing required setting

- **WHEN** the service starts without a served zone configured
- **THEN** it exits non-zero naming that setting and opens no listeners

#### Scenario: Identity provider unavailable at startup

- **WHEN** the configured identity provider cannot be reached while the service starts
- **THEN** the service starts and serves the challenge API and DNS, and sign-in fails only while the provider is unavailable

#### Scenario: Secrets are not logged

- **WHEN** the effective configuration is logged at startup
- **THEN** the database password, the OpenID Connect client secret and API secrets are redacted

### Requirement: The three HTTP surfaces are served on separate listeners

The service SHALL serve the challenge API, the management API together with the console, and
the health and metrics endpoints on three separately configurable listeners. No listener SHALL
serve the routes of another.

#### Scenario: Management route is absent from the challenge listener

- **WHEN** a management path is requested on the challenge listener
- **THEN** it is not served

#### Scenario: Metrics are absent from both public listeners

- **WHEN** the metrics path is requested on the challenge listener or the management listener
- **THEN** it is not served

### Requirement: The console is served from the binary with a history fallback

The service SHALL serve the compiled console from resources embedded in the binary, returning
the application entry document for unmatched navigation requests on the management listener.
Requests under the management API path prefix SHALL NOT fall back to the entry document.

#### Scenario: Deep link loads the application

- **WHEN** an unmatched navigation path on the management listener is requested
- **THEN** the console entry document is returned

#### Scenario: Unknown API path returns a structured error

- **WHEN** an unmatched path under the management API prefix is requested
- **THEN** the response is a JSON `404` rather than the entry document

#### Scenario: Immutable assets are cacheable

- **WHEN** a content-hashed asset is requested
- **THEN** it is served with a long-lived cache directive, while the entry document is not cached

### Requirement: The database schema is owned by migrations, not by the object mapper

The database schema SHALL be defined as ordered, versioned SQL migrations embedded in the
binary and applied by the service at startup or by an explicit command. The object-relational
mapper SHALL NOT create or alter schema objects. Applying migrations SHALL be idempotent and
SHALL be safe when two instances start simultaneously.

The build SHALL verify that the mapped models and the migrated schema agree.

#### Scenario: Fresh database is initialized

- **WHEN** migrations are applied to an empty database
- **THEN** the full schema is created

#### Scenario: Migrations applied twice

- **WHEN** migrations are applied to an already-migrated database
- **THEN** no changes are made and no error is reported

#### Scenario: Concurrent startup does not corrupt the schema

- **WHEN** two instances start against the same unmigrated database at the same time
- **THEN** migrations are applied once and both instances start

#### Scenario: Model without a migration fails the build

- **WHEN** a mapped model declares a field with no corresponding column in the migrated schema
- **THEN** the consistency check fails

#### Scenario: Unsupported schema version

- **WHEN** the service starts against a database whose schema version it does not support
- **THEN** it exits with a diagnostic instead of serving requests

### Requirement: Health and readiness are separately observable

The service SHALL expose a liveness endpoint reporting process health without dependency
checks, and a readiness endpoint reporting `200` only when the DNS and HTTP listeners are
serving and the database is reachable. Neither endpoint SHALL require authentication, and
neither SHALL be exposed on a public listener.

#### Scenario: Liveness while the database is down

- **WHEN** the database is unreachable but the process is running
- **THEN** the liveness endpoint responds `200`

#### Scenario: Readiness while the database is down

- **WHEN** the database is unreachable
- **THEN** the readiness endpoint responds with a non-`200` status

### Requirement: Operations are logged as structured events

The service SHALL emit structured logs at a configurable level. Every challenge publish,
withdraw, authorization refusal, and authentication failure SHALL be logged with the credential
identifier, the requested name, the resolved alias, and the outcome. Challenge values MAY be
logged; API key secrets, session identifiers and tokens SHALL NOT be.

#### Scenario: Publish is logged

- **WHEN** a challenge is published
- **THEN** a structured log entry records the key identifier, the FQDN, the resolved alias, and success

#### Scenario: Authentication failure is logged

- **WHEN** a request fails authentication
- **THEN** a structured log entry records the attempt and the reason, without the presented secret

### Requirement: Metrics are exposed for every surface

The service SHALL expose Prometheus-format metrics covering at least: HTTP requests by listener,
route and status; DNS queries by type and response code; active challenge record count; records
removed by the expiry sweep; database availability; and authentication outcomes by reason.

#### Scenario: Metrics reflect DNS activity

- **WHEN** DNS queries have been answered
- **THEN** the metrics endpoint reports counts by query type and response code

#### Scenario: Metrics distinguish refusal reasons

- **WHEN** requests are refused for an invalid key and for a disallowed source address
- **THEN** the two outcomes are counted separately

### Requirement: Shutdown is graceful

On receiving a termination signal the service SHALL stop accepting new work, allow in-flight
HTTP requests and DNS queries a bounded grace period to complete, close the database pool, and
exit zero. Exceeding the grace period SHALL cause a forced exit with a logged warning.

#### Scenario: In-flight request completes

- **WHEN** a termination signal arrives while an HTTP request is in flight
- **THEN** that request completes before the process exits

#### Scenario: Grace period exceeded

- **WHEN** work is still in flight after the grace period
- **THEN** the process exits and logs that shutdown was forced

### Requirement: The service ships as one container image built by CI

The repository SHALL build the console and the binary into a single container image through
GitLab CI, running lint, unit tests, race-enabled integration tests and the acceptance suite,
and publishing tagged images. The image SHALL run as a non-root user and SHALL be able to bind
port 53 without full root privileges.

#### Scenario: Pipeline runs the test suites

- **WHEN** a pipeline runs on a commit
- **THEN** unit and integration tests execute and a failure fails the pipeline

#### Scenario: Image contains the console

- **WHEN** the published image is run
- **THEN** the console is served from the binary without any additional artifact

#### Scenario: Image runs unprivileged

- **WHEN** the published image is run
- **THEN** the process runs as a non-root user and binds the configured DNS port
