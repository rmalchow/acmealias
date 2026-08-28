# service-operations Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
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

### Requirement: HTTP surfaces are separated by path prefix on one listener

The service SHALL serve the public API, the internal administrative API, the third-party challenge
protocol, and the health and metrics endpoints on one configurable listener, each under a distinct path
prefix, and SHALL apply to each prefix the authentication and authorization appropriate to it.

A path prefix SHALL NOT be reachable under the authentication of another.

#### Scenario: Each prefix carries its own authentication

- **WHEN** a request is made to a path prefix with a credential that prefix does not accept
- **THEN** it is refused

#### Scenario: An unknown management path is not the console

- **WHEN** an unknown path under an API prefix is requested
- **THEN** the response is a structured error and not the console document

#### Scenario: Health and metrics remain distinguishable

- **WHEN** the health, readiness and metrics paths are requested
- **THEN** they are served without authentication and are separable by the reverse proxy from every other prefix

### Requirement: The third-party challenge protocol keeps its existing paths

The service SHALL serve the challenge protocol under a namespaced prefix and SHALL continue to serve it
at the paths used before this change, so that a deployment already configured against the previous
endpoint continues to work unchanged.

#### Scenario: A previously configured client is unaffected

- **WHEN** a client configured with the previous endpoint publishes a challenge value
- **THEN** the request is served exactly as before

#### Scenario: The namespaced prefix serves the same protocol

- **WHEN** the same client is reconfigured to the namespaced prefix
- **THEN** the request is served identically

### Requirement: Satellites are served on their own mutually authenticated listener

The service SHALL serve the satellite synchronisation surface on a listener separate from every other
surface, requiring a client certificate verified against a configured authority.

The service SHALL NOT accept a client identity presented as a forwarded header on that surface.

#### Scenario: A satellite without a client certificate is refused

- **WHEN** a connection is made to the satellite listener without a verified client certificate
- **THEN** it is refused before any route is matched

#### Scenario: The satellite surface is absent from the main listener

- **WHEN** a satellite path is requested on the main listener
- **THEN** it is not served

#### Scenario: A forwarded identity is not honoured

- **WHEN** a request to the satellite listener carries a header naming a client identity
- **THEN** the header is ignored

### Requirement: The public API is versioned and published; the administrative API is neither

The service SHALL expose two management surfaces: a public API, carrying a version in its path and
described by a machine-readable interface document served publicly; and an internal administrative API,
carrying no version and no published description.

The public API SHALL be the interface used by the console and by programmatic callers alike, with no
operation available to the console that is absent from it.

#### Scenario: The interface document describes the public API

- **WHEN** the interface document is retrieved
- **THEN** it describes the public API's operations, and no operation of the administrative API

#### Scenario: The console uses no private operation

- **WHEN** the console's requests are enumerated
- **THEN** every one of them is an operation of the published public API

#### Scenario: The administrative API is not published

- **WHEN** the administrative API is searched for in the interface document
- **THEN** it does not appear

### Requirement: Configuration is layered, and the environment has the last word

The service SHALL read its configuration from an optional base file and any
number of optional overlay files, applied in a defined order, with environment
variables applied last.

Structures SHALL be merged key by key. A list SHALL replace the list it overlays
rather than being appended to it.

A missing overlay file SHALL NOT prevent start-up. A base file that was
explicitly configured and cannot be read SHALL prevent it.

The service SHALL report, at start-up, which layers it applied and which
configured files were absent.

#### Scenario: An overlay states only its differences

- **WHEN** an overlay sets one value of many
- **THEN** that value is used and every other value comes from beneath it

#### Scenario: A list is replaced, not merged

- **WHEN** a base file lists two values and an overlay lists one
- **THEN** the effective configuration holds exactly the overlay's one

#### Scenario: The environment wins

- **WHEN** a value is set both in a file and in the environment
- **THEN** the environment's value is used

#### Scenario: An absent overlay is not an error

- **WHEN** a configured overlay file does not exist
- **THEN** the service starts and reports the file as absent

#### Scenario: An absent configured base file is an error

- **WHEN** a base file is explicitly configured and cannot be read
- **THEN** the service does not start

### Requirement: Configuration files may refer to the environment with a fallback

The service SHALL resolve placeholders in configuration files. A placeholder
names an environment variable and MAY supply a default, which SHALL be used when
that variable is unset.

Placeholders SHALL be resolved before the document is interpreted, so that a
resolved value keeps the type the document gives it.

A means of escaping the placeholder syntax SHALL exist, so that a literal value
resembling a placeholder can be expressed.

#### Scenario: A placeholder falls back to its default

- **WHEN** a placeholder names an unset variable and supplies a default
- **THEN** the default is used

#### Scenario: A resolved value keeps its type

- **WHEN** a placeholder supplying a number is resolved into a numeric field
- **THEN** the value is a number and not a string

#### Scenario: A secret has no default

- **WHEN** a placeholder naming a secret has no default and the variable is unset
- **THEN** the value is empty and the service refuses to start rather than running with it

#### Scenario: The syntax can be escaped

- **WHEN** a value contains an escaped placeholder
- **THEN** it resolves to the literal text rather than to a variable

### Requirement: The published contract is readable by a person

The service SHALL serve a browsable rendering of its public API document at a stable path.

The rendering SHALL require no credentials, matching the document it renders.

#### Scenario: A stranger reads the API

- **WHEN** an unauthenticated person opens the documentation path
- **THEN** the public API document is presented in a readable form

### Requirement: The documentation renders without reaching any other host

Every asset the documentation page needs SHALL be served by this service.

#### Scenario: An isolated deployment documents itself

- **WHEN** the page is opened on a deployment with no route to the internet
- **THEN** it renders completely

### Requirement: A page that can send requests says what it will act on

Where the documentation page can send authenticated requests, it SHALL name the account those
requests will act in, before any is sent.

#### Scenario: The reader knows whose data is at stake

- **WHEN** a signed-in person opens the documentation page
- **THEN** the account their requests would act in is named on the page

### Requirement: Every message the deployment sends comes from one identity

All transactional mail, whoever sends it, SHALL carry the same sender address and
the same sender display name.

Every host that sends on the domain's behalf SHALL be authorised by the domain's
published sender policy and SHALL sign its messages with a published key, and
that authentication SHALL align with the sender address a recipient sees.

Messages SHALL carry a reply address that reaches a person.

#### Scenario: Two senders, one correspondent

- **WHEN** the identity provider and the service each send a message
- **THEN** both carry the same sender address and display name

#### Scenario: Authentication aligns with what the reader sees

- **WHEN** a message is sent by either sender
- **THEN** it authenticates as the same domain that appears as its sender

#### Scenario: A reply reaches somebody

- **WHEN** a recipient replies to any message
- **THEN** the reply is delivered to an address that is read

### Requirement: The agreement between senders is asserted, not assumed

The deployment SHALL be able to demonstrate, by test, that both senders produce
the same envelope and the same footer.

#### Scenario: Both senders are compared

- **WHEN** a message is produced by each sender
- **THEN** their sender identity and footer are shown to match

### Requirement: Subjects name what the message is about

A subject SHALL begin with the thing the message concerns. It SHALL NOT carry a
constant prefix identifying the sender, which the sender field already reports.

#### Scenario: A subject is useful in a list

- **WHEN** a recipient sees a message in a narrow list or a notification
- **THEN** the visible beginning of the subject identifies what it concerns

