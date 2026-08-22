## ADDED Requirements

### Requirement: Authentication is delegated, and the session is the boundary

The service SHALL authenticate every person through an external identity provider in every edition,
and SHALL hold no credential of its own: no password, no password hash, and no credential reset
token.

Authentication SHALL produce a server-side session, and session storage, idle and absolute expiry,
revocation and cross-site request forgery protection SHALL be properties of the session layer rather
than of the provider. No component downstream of the session SHALL depend on the provider's tokens,
claims or flows.

Editions and planes SHALL differ in which application of the provider they authenticate against, and
the service SHALL verify which application issued a token rather than accepting any validly signed
token. A person admitted to one application SHALL NOT thereby be admitted to another.

#### Scenario: No credential is stored

- **WHEN** the datastore is inspected for a user
- **THEN** it contains no password, password hash or reset token

#### Scenario: No interface accepts a password

- **WHEN** the management interface and API are enumerated
- **THEN** no route or screen accepts a password

#### Scenario: Cross-site request forgery protection does not depend on the provider

- **WHEN** a state-changing management request is made with a valid session and no matching token header
- **THEN** it is refused

#### Scenario: A customer identity does not reach the staff surface

- **WHEN** a person admitted to the customer application reaches the staff surface
- **THEN** they are not authenticated there, and a token issued for the customer application is refused

#### Scenario: A session's idle life is bounded

- **WHEN** a session is not used for the configured idle period
- **THEN** it is no longer usable, whatever the provider's own session state

### Requirement: A person becomes a user by enrolling with an address they control

The service SHALL allow a person to become a user through the identity provider's enrolment, and SHALL
grant no capability beyond signing in until the provider has verified control of the address.

The token SHALL expire, SHALL be usable once, and SHALL NOT be guessable.

Registration SHALL create a user that belongs to no account.

#### Scenario: An unconfirmed address cannot use the service

- **WHEN** a person registers and signs in without confirming their address
- **THEN** they can confirm or request another confirmation, and nothing else

#### Scenario: Confirmation is single-use

- **WHEN** a confirmation token is used a second time
- **THEN** it is refused

#### Scenario: Registration does not reveal existing users

- **WHEN** a person registers with an address that already has a user
- **THEN** the response does not differ from one for an address that does not

#### Scenario: A registered user belongs to nothing yet

- **WHEN** a person completes registration and confirmation
- **THEN** they hold no membership, and may create an account or accept an invitation

### Requirement: Credential handling is verified in the provider, and a reset ends the service's sessions

The deployment SHALL verify that the identity provider's enrolment verifies address control, that its
recovery flow verifies address control, that neither enrolment nor recovery discloses whether an
address already has a user, and that repeated failed attempts against one user or from one source are
limited.

When a person's credential is reset or their access is revoked at the provider, sessions the service
established for them SHALL cease to be usable.

#### Scenario: Recovery does not disclose whether an address is known

- **WHEN** recovery is requested for an address with no user
- **THEN** the response is indistinguishable from one for an address that has one

#### Scenario: Repeated attempts are slowed

- **WHEN** authentication is attempted repeatedly against one user or from one source
- **THEN** further attempts are refused until the limit clears

#### Scenario: A reset does not leave a session alive

- **WHEN** a person's credential is reset at the provider
- **THEN** sessions the service established before the reset are no longer usable

#### Scenario: Unverified enrolment grants nothing

- **WHEN** a person enrols without completing address verification
- **THEN** no session usable against the management interface is established

### Requirement: Self-registration is governed by configuration and refused by the server when closed

The service SHALL expose its account-creation endpoint in every deployment, and SHALL refuse it with
a stated reason whenever self-service account creation is not enabled by configuration.

The refusal SHALL be applied by the endpoint itself, and SHALL NOT depend on the interface omitting a
link, and SHALL NOT depend on the identity provider's enrolment being closed. Accepting an invitation
SHALL remain possible while account creation is closed.

#### Scenario: A closed deployment refuses the endpoint directly

- **WHEN** the account-creation endpoint is called directly while self-service account creation is disabled
- **THEN** it refuses with a reason and no account is created

#### Scenario: An enrolled person without an account can do nothing

- **WHEN** a person who has enrolled at the provider signs in while account creation is disabled
- **THEN** they hold a session, belong to no account, and no inventory or spend is reachable

#### Scenario: Invitation acceptance is unaffected

- **WHEN** an invited person enrols through an invitation while self-service account creation is disabled
- **THEN** enrolment succeeds and they join the inviting account

#### Scenario: Enabling account creation requires no code change

- **WHEN** the configuration enabling self-service account creation is set
- **THEN** the same endpoint accepts account creation

### Requirement: The staff and customer surfaces are separated within the identity provider

Staff and customers MAY share one identity provider instance. Where they do, customers SHALL be
registered as external users with no access to the provider's administration interface, staff and
customers SHALL be admitted through separate applications each bound to its own group, and the staff
application SHALL have no enrolment flow.

Membership of a customer's account SHALL NOT be derived from any group or attribute held by the
identity provider.

#### Scenario: A customer cannot enrol into the staff surface

- **WHEN** enrolment is attempted against the staff application
- **THEN** no enrolment flow is reachable and no account is created

#### Scenario: A customer identity reaches no administrative interface

- **WHEN** a customer signs in
- **THEN** they reach neither the provider's administration interface nor its application dashboard

#### Scenario: A provider group does not place anyone in an account

- **WHEN** a person is added to any group in the identity provider
- **THEN** they gain no membership of any customer account
