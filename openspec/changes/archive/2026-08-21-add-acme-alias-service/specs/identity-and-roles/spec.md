## ADDED Requirements

### Requirement: Humans authenticate through OpenID Connect

The service SHALL authenticate console users through an OpenID Connect authorization code flow
with PKCE, performed by the server. It SHALL validate the issuer, audience, expiry and nonce of
the identity token against the provider's published keys. It SHALL NOT provide local password
accounts.

Tokens obtained from the provider SHALL NOT be exposed to the browser.

#### Scenario: Unauthenticated request is directed to sign in

- **WHEN** an unauthenticated request is made to the management API
- **THEN** the service responds `401 Unauthorized` and indicates where to begin sign-in

#### Scenario: Successful sign-in establishes a session

- **WHEN** the provider redirects back with a valid authorization code and matching state
- **THEN** the service establishes a session and the user's identity is readable from the management API

#### Scenario: Invalid identity token is rejected

- **WHEN** the identity token fails signature, issuer, audience, expiry or nonce validation
- **THEN** sign-in fails and no session is established

### Requirement: Sessions are server-side, revocable and time-bounded

The service SHALL represent a session as server-side state referenced by an opaque,
cryptographically random identifier in a cookie marked `HttpOnly`, `Secure` and `SameSite`.
Sessions SHALL expire after a configured idle timeout and a configured absolute lifetime, and
SHALL be revocable.

#### Scenario: Sign-out ends the session

- **WHEN** a user signs out
- **THEN** the session is destroyed and subsequent requests presenting its cookie are unauthenticated

#### Scenario: Expired session is rejected

- **WHEN** a session exceeds its idle timeout or absolute lifetime
- **THEN** requests presenting it are unauthenticated

### Requirement: An administrator can end another person's sessions

The service SHALL allow a user holding `ADMIN` to enumerate the sessions that are currently
valid, to revoke an individual session, and to revoke every session belonging to one subject.
Revocation SHALL take effect on the next request presenting the revoked session, and SHALL NOT
affect any session it was not applied to.

Enumeration SHALL report, per session, the subject it belongs to, when it was established, when
it was last used and when it expires. It SHALL NOT expose the session identifier in a form that
could be used to authenticate as that session.

#### Scenario: Administrator revokes one session

- **WHEN** an `ADMIN` revokes a session belonging to another user
- **THEN** the next request presenting that session is unauthenticated

#### Scenario: Other sessions are untouched

- **WHEN** one session is revoked
- **THEN** sessions belonging to other subjects remain valid

#### Scenario: All sessions for one subject are revoked together

- **WHEN** an `ADMIN` revokes every session of one subject
- **THEN** no session belonging to that subject remains valid

#### Scenario: Only an administrator may revoke

- **WHEN** a user not holding `ADMIN` attempts to enumerate or revoke sessions
- **THEN** the request is refused

### Requirement: State-changing management requests are protected against cross-site request forgery

The service SHALL require a token bound to the session, submitted in a request header, on every
state-changing management request, and SHALL reject requests without it.

#### Scenario: Request without the token is rejected

- **WHEN** a state-changing management request is made with a valid session cookie but no matching token header
- **THEN** the service responds with a client error and performs no change

### Requirement: Group claims are mapped to roles by configuration

The service SHALL read the group membership of the authenticated user from a configurable claim
of the identity token, and SHALL derive roles by comparing those groups against one configuration
setting per role, each naming the group that grants it. The mapping SHALL NOT be stored in the
database and SHALL NOT be modifiable through the management API.

A user's roles SHALL be the union of every role whose configured group appears in their groups
claim. A role whose setting is unset SHALL be held by nobody.

#### Scenario: Configured group grants its role

- **WHEN** a user's groups include the group named by a role's setting
- **THEN** the user holds that role

#### Scenario: Roles are additive

- **WHEN** a user's groups include the groups configured for two different roles
- **THEN** the user holds both, and may do anything either permits

#### Scenario: Unset role is held by nobody

- **WHEN** no group is configured for a role
- **THEN** no user holds that role

#### Scenario: Mapping is not editable at runtime

- **WHEN** any management route is searched for an operation that changes the group-to-role mapping
- **THEN** no such operation exists

#### Scenario: Authenticated user with no mapped group is told why

- **WHEN** a user authenticates successfully but is in none of the configured groups
- **THEN** access is refused and the response reports the groups that were observed

### Requirement: Roles are global and fixed

The service SHALL provide exactly four roles — `ADMIN`, `CONFIG_ADMIN`, `KEY_ROTATION` and
`READ_ONLY` — and SHALL enforce them on every management operation, server-side. No role SHALL be
scoped to a subset of projects.

| Operation | `ADMIN` | `CONFIG_ADMIN` | `KEY_ROTATION` | `READ_ONLY` |
| --- | --- | --- | --- | --- |
| Create or delete projects | yes | no | no | no |
| Create, edit or delete load balancers, nodes and domains | yes | yes | no | no |
| Create or revoke API keys | yes | yes | yes | no |
| See a key secret at creation | yes | yes | yes | no |
| View the DNS plan and run verification | yes | yes | yes | yes |
| View the inventory and the audit log | yes | yes | yes | yes |

#### Scenario: Only an administrator creates a project

- **WHEN** a user holding `CONFIG_ADMIN` but not `ADMIN` attempts to create a project
- **THEN** the request is refused

#### Scenario: Key rotation may not change the inventory

- **WHEN** a user holding only `KEY_ROTATION` attempts to create a domain
- **THEN** the request is refused

#### Scenario: Key rotation may create a key

- **WHEN** a user holding only `KEY_ROTATION` creates an API key
- **THEN** the request succeeds and the secret is returned once

#### Scenario: Read-only cannot change the inventory

- **WHEN** a user holding only `READ_ONLY` attempts to create a domain
- **THEN** the request is refused

#### Scenario: Every role may read

- **WHEN** a user holding only `READ_ONLY` requests the inventory, a DNS plan or the audit log
- **THEN** the request succeeds

#### Scenario: Server enforces independently of the client

- **WHEN** a request is made directly to a management route without the console
- **THEN** the same role check is applied

### Requirement: Role changes take effect on sign-in, and sessions are the immediate lever

A change to a user's group membership SHALL take effect at their next sign-in, and SHALL be
bounded by the configured absolute session lifetime, because group membership is read from the
identity token when a session is established. A change to the group-to-role configuration SHALL
take effect when the service is restarted.

The service SHALL allow an `ADMIN` to revoke a session, and revocation SHALL take effect on the
next request made with it.

#### Scenario: Group removal does not end an existing session

- **WHEN** a user is removed from a mapped group while holding an active session
- **THEN** that session retains the role until it is revoked or expires

#### Scenario: Revocation is immediate

- **WHEN** an administrator revokes a session
- **THEN** the next request presenting it is unauthenticated

#### Scenario: Exposure is bounded by the absolute lifetime

- **WHEN** a session reaches the configured absolute lifetime
- **THEN** it is no longer usable and the user's roles are re-derived at their next sign-in

### Requirement: Privileged actions are recorded

The service SHALL record every creation, modification and deletion of inventory, API keys and
sessions — including the revocation of a session by an administrator — with the acting identity,
the affected object, the action and the time. Records SHALL NOT contain key secrets or tokens.

#### Scenario: Key creation is attributable

- **WHEN** a user creates an API key
- **THEN** an audit record names that user, the key identifier and the load balancer
- **AND** the record does not contain the secret

#### Scenario: Session revocation is attributable

- **WHEN** an `ADMIN` revokes another user's session
- **THEN** an audit record names the administrator, the subject whose session was ended, and the time
