## ADDED Requirements

### Requirement: A person may become a user by enrolling with an address they control

The service SHALL allow a person to become a user through the identity provider's enrolment flow, and
SHALL grant no capability beyond signing in until the provider has verified control of the address.

A verification token SHALL expire, SHALL be usable once, and SHALL NOT be guessable.

A person who enrols SHALL become a member of no account other than the personal account every user
holds, and SHALL join a team account only by invitation or by creating one.

#### Scenario: An unverified address cannot use the service

- **WHEN** a person enrols and signs in without verifying their address
- **THEN** they can verify or request another verification, and nothing else

#### Scenario: Verification is single-use

- **WHEN** a verification token is used a second time
- **THEN** it is refused

#### Scenario: Enrolment does not reveal existing users

- **WHEN** a person enrols with an address that already has a user
- **THEN** the response does not differ from one for an address that does not

#### Scenario: A self-enrolled user reaches no team account

- **WHEN** a person completes enrolment and signs in
- **THEN** they hold their personal account and no membership of any other account

### Requirement: Enrolment and account creation are separately governed, and each is refused where it is performed

Self-service enrolment SHALL be governed at the identity provider, and self-service creation of a
team account SHALL be governed by the service's own configuration and refused by the endpoint itself
with a stated reason.

The refusal of team account creation SHALL NOT depend on the interface omitting a link, and SHALL NOT
depend on enrolment being closed. Accepting an invitation SHALL remain possible while team account
creation is closed.

Closing self-service enrolment SHALL NOT be assumed to close team account creation, and closing team
account creation SHALL NOT be assumed to close enrolment.

#### Scenario: A closed deployment refuses the endpoint directly

- **WHEN** the team account creation endpoint is called directly while self-service creation is disabled
- **THEN** it refuses with a reason and no account is created

#### Scenario: An enrolled person still has somewhere to work

- **WHEN** a person who has enrolled signs in while team account creation is disabled
- **THEN** they hold a session and their personal account, and can create no team account

#### Scenario: Invitation acceptance is unaffected

- **WHEN** an invited person enrols through an invitation while self-service team account creation is disabled
- **THEN** enrolment succeeds and they join the inviting account

#### Scenario: Enabling account creation requires no code change

- **WHEN** the configuration enabling self-service team account creation is set
- **THEN** the same endpoint accepts account creation

### Requirement: Credential handling is verified in the provider, and a reset bounds the service's sessions

The deployment SHALL verify that the identity provider's enrolment verifies address control, that its
recovery flow verifies address control, that neither enrolment nor recovery nor sign-in discloses
whether an address already has a user, and that repeated failed attempts against one user or from one
source are limited.

When a person's credential is reset or their access is revoked at the provider, sessions the service
established for them SHALL cease to be usable within a bounded period, and that period SHALL be
stated rather than implied.

#### Scenario: Recovery does not disclose whether an address is known

- **WHEN** recovery is requested for an address with no user
- **THEN** the response is indistinguishable from one for an address that has one

#### Scenario: Sign-in does not disclose whether an address is known

- **WHEN** sign-in is attempted with an identifier that has no user
- **THEN** the response is indistinguishable from one for an identifier that has one with a wrong credential

#### Scenario: Repeated attempts are slowed

- **WHEN** authentication is attempted repeatedly against one user or from one source
- **THEN** further attempts are refused until the limit clears

#### Scenario: A reset does not leave a session alive indefinitely

- **WHEN** a person's credential is reset at the provider
- **THEN** sessions the service established before the reset cease to be usable within the configured idle period

#### Scenario: Unverified enrolment grants nothing

- **WHEN** a person enrols without completing address verification
- **THEN** no session usable against the management interface is established

### Requirement: The staff and customer surfaces are separated within the identity provider

Staff and customers MAY share one identity provider instance. Where they do, customers SHALL be
registered as users with no access to the provider's administration interface or application
dashboard, staff and customers SHALL be admitted through separate applications each bound to its own
group, and the staff application SHALL have no enrolment flow.

The service SHALL verify which application issued a token rather than accepting any validly signed
token, and a person admitted to one application SHALL NOT thereby be admitted to another.

Membership of any account SHALL NOT be derived from a group or attribute held by the identity
provider.

#### Scenario: A customer cannot enrol into the staff surface

- **WHEN** enrolment is attempted against the staff application
- **THEN** no enrolment flow is reachable and no user is created

#### Scenario: A customer token is refused on the staff surface

- **WHEN** a token issued for the customer application is presented to the staff surface
- **THEN** it is refused

#### Scenario: A customer identity reaches no administrative interface

- **WHEN** a customer signs in
- **THEN** they reach neither the provider's administration interface nor its application dashboard

#### Scenario: A provider group does not place anyone in an account

- **WHEN** a person is added to any group in the identity provider
- **THEN** they gain no membership of any account
