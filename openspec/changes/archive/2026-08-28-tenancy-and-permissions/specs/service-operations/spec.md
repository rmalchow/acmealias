## REMOVED Requirements

### Requirement: The three HTTP surfaces are served on separate listeners

**Reason**: The three-listener split existed so that a stolen API key could reach no management route
even if a routing rule in front of the service were wrong. That guarantee is now carried by the
permission model — a key reaches only what its grants permit — rather than by network topology.
Separating by path allows one listener, one certificate and one port, with per-group middleware doing
the work the listener boundary did.

**Migration**: Surfaces move to path prefixes on a single listener. The challenge protocol keeps
serving its existing root paths alongside its namespaced ones, so deployments configured with the
previous endpoint continue to work without change. Satellites are the exception and keep a listener of
their own, because mutual TLS is a listener property.

## ADDED Requirements

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
