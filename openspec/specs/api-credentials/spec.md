# api-credentials Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
### Requirement: Load balancers authenticate with API keys

The service SHALL authenticate challenge API requests with API keys. Each key SHALL be scoped
to exactly one load balancer, and SHALL authorize writes only for the challenge names
derived from that load balancer's domains.

A load balancer MAY have more than one active key, so that a key can be replaced without
an interruption.

#### Scenario: Key authorizes only its own load balancer

- **WHEN** a key issued for one load balancer is used to present a challenge for a name belonging to another load balancer
- **THEN** the request is refused

#### Scenario: Authorization follows the inventory

- **WHEN** a domain is removed from a load balancer
- **THEN** keys for that load balancer can no longer present challenges for the corresponding name, without any change to the keys

### Requirement: Key secrets are high-entropy, stored hashed, and shown once

The service SHALL generate key secrets from a cryptographically secure random source with at
least 128 bits of entropy. It SHALL store only a cryptographic hash of the secret, together
with a non-secret identifier and a short display prefix.

The full secret SHALL be returned exactly once, in the response to the creation request, and
SHALL NOT be retrievable afterwards by any interface.

#### Scenario: Secret is revealed only at creation

- **WHEN** a key is created
- **THEN** the response contains the full secret
- **AND** every subsequent read of that key returns only its identifier, display prefix and metadata

#### Scenario: Stored form is not the secret

- **WHEN** the stored representation of a key is read directly from the datastore
- **THEN** it does not contain the secret in a form usable for authentication

### Requirement: Keys are accepted over HTTP Basic and equivalent transports

The service SHALL accept a key presented as HTTP Basic credentials with the key identifier as
the username and the secret as the password. It SHALL additionally accept the same key as a
bearer token and as a dedicated API key header.

Verification SHALL use a constant-time comparison, and an unknown identifier SHALL cost the
same as a known one.

#### Scenario: Basic credentials are accepted

- **WHEN** a request presents a valid identifier and secret as HTTP Basic credentials
- **THEN** the request is authenticated

#### Scenario: Unknown identifier is indistinguishable by timing

- **WHEN** requests are made with an unknown identifier and with a known identifier and wrong secret
- **THEN** both respond `401 Unauthorized` with comparable processing cost

### Requirement: Keys may be restricted to source addresses

A key MAY carry zero or more CIDR blocks. A key with no CIDR blocks SHALL be usable from any
address. A key with one or more CIDR blocks SHALL be accepted only when the request's client
address falls within one of them.

A request rejected because of its source address SHALL receive the same response as a request
with an invalid key, and the distinction SHALL be recorded in the log.

#### Scenario: Request from an allowed address succeeds

- **WHEN** a restricted key is used from an address inside one of its CIDR blocks
- **THEN** the request is authenticated

#### Scenario: Request from a disallowed address is refused

- **WHEN** a restricted key is used from an address outside all of its CIDR blocks
- **THEN** the service responds `401 Unauthorized` and records the reason

### Requirement: The client address is determined only from trusted proxies

The service SHALL derive the client address from forwarding headers only when the immediate
peer is within a configured set of trusted proxy networks, and SHALL otherwise use the peer
address.

The service SHALL refuse to start when any key carries a CIDR restriction while no trusted
proxy networks are configured.

#### Scenario: Forwarded header from an untrusted peer is ignored

- **WHEN** a request from a peer outside the trusted proxy networks carries a forwarding header naming an allowed address
- **THEN** the restriction is evaluated against the peer address and the request is refused

#### Scenario: Misconfiguration fails closed at startup

- **WHEN** the service starts with restricted keys present and no trusted proxy networks configured
- **THEN** startup fails with an error naming the setting, and no listener is opened

### Requirement: Keys have a managed lifecycle

A key MAY have an expiry. The service SHALL reject expired, disabled and deleted keys. It SHALL
record the time each key was last used, and SHALL expose that time through the management
interface.

#### Scenario: Expired key is refused

- **WHEN** a key whose expiry has passed is presented
- **THEN** the request is refused

#### Scenario: Revoked key stops working immediately

- **WHEN** a key is revoked
- **THEN** the next request presenting it is refused

#### Scenario: Rotation without interruption

- **WHEN** a second key is created for a load balancer and the first is then revoked
- **THEN** requests presenting the second key succeed throughout

