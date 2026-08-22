## ADDED Requirements

### Requirement: Authentication is a seam with one implementation per edition

The service SHALL support more than one way of authenticating a person, selected by edition, and SHALL
produce the same session from each.

Session storage, idle and absolute expiry, revocation, and cross-site request forgery protection SHALL
NOT differ between implementations.

#### Scenario: The session is the same shape whatever authenticated it

- **WHEN** a person is authenticated by any supported means
- **THEN** the resulting session is stored, expires and is revocable exactly as any other

#### Scenario: One implementation is in force at a time

- **WHEN** a deployment is running
- **THEN** exactly one authentication implementation is wired, determined by the edition

#### Scenario: Cross-site request forgery protection is not edition-specific

- **WHEN** a state-changing management request is made with a valid session and no matching token header
- **THEN** it is refused, whichever implementation established the session

### Requirement: A person can register as a user with an email address they control

The service SHALL allow a person, in an edition that authenticates locally, to register as a user with
an email address and a password, and SHALL grant no capability beyond signing in until that address has
been confirmed by following a token sent to it.

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

### Requirement: Passwords are stored and reset safely

Passwords SHALL be stored only as a memory-hard hash and SHALL NOT be recoverable.

A reset SHALL be initiated by email, SHALL expire, SHALL be usable once, and SHALL end every existing
session of that user when used.

Repeated authentication attempts against one user or from one source SHALL be rate limited.

#### Scenario: A reset ends existing sessions

- **WHEN** a person completes a password reset
- **THEN** sessions established before the reset are no longer usable

#### Scenario: Reset does not disclose whether an address is registered

- **WHEN** a reset is requested for an address with no user
- **THEN** the response is indistinguishable from one for an address that has one

#### Scenario: The stored form is not the password

- **WHEN** the stored representation of a password is read directly from the datastore
- **THEN** it does not contain the password in a form usable for authentication

#### Scenario: Repeated attempts are slowed

- **WHEN** authentication is attempted repeatedly against one user or from one source
- **THEN** further attempts are refused until the limit clears
