## MODIFIED Requirements

### Requirement: Roles are global and fixed

Roles SHALL be held per account rather than globally. The four role names and the permissions
each confers SHALL be unchanged; what changes is that holding a role is a fact about a person in
one account, and confers nothing in another.

Within an account there SHALL be no finer scoping: a role holder may act on every object of that
account.

#### Scenario: A role confers nothing outside its account

- **WHEN** a person holding a role in one account acts on an object of another account
- **THEN** the request is refused

#### Scenario: Within an account the role covers everything

- **WHEN** a person holds a role in an account
- **THEN** it applies to every project and load balancer of that account alike

### Requirement: Group claims are mapped to roles by configuration

In an edition that authenticates through an identity provider, group claims SHALL be mapped to
roles by configuration as before, and the resulting roles SHALL be held in the single account of
that edition.

#### Scenario: Configured groups still grant the same roles

- **WHEN** an operator whose groups match the configured mapping signs in to the internal edition
- **THEN** they hold the mapped roles, in the one account, exactly as before this change

## ADDED Requirements

### Requirement: Authentication is a seam with one implementation per edition

The service SHALL support more than one way of authenticating a person, selected by edition, and
SHALL produce the same session from each: a subject, a display name, and the account memberships
that subject holds.

Session handling, revocation, expiry and cross-site request forgery protection SHALL NOT differ
between implementations.

#### Scenario: The session is the same shape whatever authenticated it

- **WHEN** a person is authenticated by any supported means
- **THEN** the resulting session carries a subject, a display name and memberships, and is
  revocable and time-bounded exactly as any other

### Requirement: A person can create an account with an email address they control

In the public edition a person SHALL be able to register with an email address and a password,
and the service SHALL NOT grant any capability beyond signing in until that address has been
confirmed by following a token sent to it.

The token SHALL expire, SHALL be usable once, and SHALL NOT be guessable.

#### Scenario: An unconfirmed address cannot use the service

- **WHEN** a person registers and signs in without confirming their address
- **THEN** they can confirm or resend, and nothing else

#### Scenario: Confirmation is single-use

- **WHEN** a confirmation token is used a second time
- **THEN** it is refused

#### Scenario: Registration does not reveal existing accounts

- **WHEN** a person registers with an address that already has an account
- **THEN** the response does not differ from one for an address that does not

### Requirement: Passwords are stored and reset safely

Passwords SHALL be stored only as a memory-hard hash, never recoverable. A reset SHALL be
initiated by email, SHALL expire, SHALL be usable once, and SHALL end every existing session of
that person on use.

Repeated authentication attempts against one account or from one source SHALL be rate limited.

#### Scenario: A reset ends existing sessions

- **WHEN** a person completes a password reset
- **THEN** sessions established before the reset are no longer valid

#### Scenario: Reset does not disclose whether an address is registered

- **WHEN** a reset is requested for an address with no account
- **THEN** the response is indistinguishable from one for an address that has one

### Requirement: A person may hold memberships in several accounts

The service SHALL allow one authenticated person to hold memberships in more than one account,
SHALL make every request act in exactly one of them, and SHALL make which one unambiguous from
the request rather than from prior state alone.

#### Scenario: The acting account is explicit

- **WHEN** a person who belongs to two accounts performs an operation
- **THEN** the account it acts in is determined by the request, not guessed
