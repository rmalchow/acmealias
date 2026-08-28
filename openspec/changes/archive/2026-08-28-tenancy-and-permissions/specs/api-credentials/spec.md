## MODIFIED Requirements

### Requirement: Load balancers authenticate with API keys

The service SHALL authenticate challenge API requests with API keys. Each key SHALL be anchored to
exactly one project or one load balancer, and SHALL carry zero or more permission grants confined to
that anchor.

A key SHALL authorize an operation only when one of its grants matches the object the operation is
about. A key with no grants SHALL authorize nothing.

A load balancer MAY have more than one active key, so that a key can be replaced without
an interruption.

#### Scenario: Key authorizes only what it is granted

- **WHEN** a key granted challenge publication for one load balancer is used to present a challenge for a name belonging to another load balancer
- **THEN** the request is refused

#### Scenario: A project-anchored key covers the load balancers it is granted

- **WHEN** a key anchored to a project holds an inheritable grant on that project and presents a challenge for a name of any load balancer in it
- **THEN** the request is authorized

#### Scenario: An anchor alone authorizes nothing

- **WHEN** a key anchored to a project holds no grant and presents a challenge for a name of that project
- **THEN** the request is refused

#### Scenario: A key cannot reach outside its account

- **WHEN** a key is used to present a challenge for a name owned by another account
- **THEN** the request is refused and the response does not distinguish it from a name that does not exist

#### Scenario: Authorization follows the inventory

- **WHEN** a domain is removed from a load balancer
- **THEN** keys granted challenge publication for that load balancer can no longer present challenges for the corresponding name, without any change to the keys

## ADDED Requirements

### Requirement: A key is issued with no more authority than its issuer holds

The service SHALL refuse to create or amend an API key carrying a grant not covered by a grant the
issuing subject holds, evaluated as the permissions capability specifies.

#### Scenario: A key within the issuer's authority is created

- **WHEN** an issuer holding an inheritable grant on a project creates a key granted an action on a load balancer of that project
- **THEN** the key is created

#### Scenario: A key exceeding the issuer's authority is refused

- **WHEN** an issuer creates a key granted an action it does not itself hold
- **THEN** the request is refused and no key is created

#### Scenario: Amending a key is bounded the same way

- **WHEN** a grant exceeding the amending subject's authority is added to an existing key
- **THEN** the request is refused and the key is unchanged

### Requirement: A key's authority is evaluated without an additional lookup

The service SHALL resolve an authenticated key's grants as part of resolving the key, so that
authorizing a challenge request requires no further datastore read beyond those the request already
performs.

#### Scenario: Authorization adds no query

- **WHEN** a challenge is presented with a valid key
- **THEN** the number of datastore reads performed is not greater than before grants were introduced
