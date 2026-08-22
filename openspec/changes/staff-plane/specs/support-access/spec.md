## ADDED Requirements

### Requirement: Reaching a tenant's data requires an access grant

Holding a staff permission SHALL NOT by itself permit reading or modifying any account's inventory.
A staff subject SHALL reach one account's data only through an access grant naming that account.

An access grant SHALL record the staff subject, the account, a reason, the time it was opened and the
time it expires.

#### Scenario: A staff permission alone reaches nothing

- **WHEN** a staff subject holding every staff permission and no access grant reads an account's inventory
- **THEN** the request is refused

#### Scenario: An access grant admits its account only

- **WHEN** a staff subject holds an access grant for one account and addresses an object of another
- **THEN** the request is refused

#### Scenario: Opening an access grant requires its permission

- **WHEN** a staff subject that does not hold the permission to open access attempts to open one
- **THEN** the request is refused

### Requirement: An access grant is bounded in time and states a reason

An access grant SHALL carry an expiry and a non-empty reason, both recorded when it is opened. The
service SHALL refuse to open a grant without a reason.

A grant SHALL be closable before its expiry.

#### Scenario: A grant without a reason is refused

- **WHEN** an access grant is opened with no reason or an empty one
- **THEN** the request is refused and no grant is created

#### Scenario: An expired grant admits nothing

- **WHEN** a request is made with an access grant whose expiry has passed
- **THEN** the request is refused

#### Scenario: A closed grant admits nothing

- **WHEN** a grant is closed and a request is made under it
- **THEN** the request is refused

### Requirement: An access grant is evaluated on every request

The service SHALL evaluate whether an access grant is open, unexpired and unclosed on every request
made under it, and SHALL NOT record the grant on the session such that it survives its expiry or its
closure.

#### Scenario: Expiry takes effect within a session

- **WHEN** a grant expires while the staff subject's session remains valid
- **THEN** the next request under it is refused

#### Scenario: Closure takes effect immediately

- **WHEN** a grant is closed while a session is open
- **THEN** the next request under it is refused

### Requirement: Staff reach tenant data through the same confinement as tenants

The service SHALL confine a staff request to the account named by its access grant using the same
mechanism that confines a member's request to their own account. Staff operations SHALL NOT use a
separate query path and SHALL NOT evaluate tenant permission grants.

The confinement SHALL be constructible from an access grant only, and SHALL NOT be constructible from
an account identifier supplied in a request.

#### Scenario: A staff read is confined like a member read

- **WHEN** a staff subject with an access grant lists an account's load balancers
- **THEN** only that account's load balancers are returned

#### Scenario: An account identifier in a request does not confer access

- **WHEN** a staff request carries an account identifier for which no access grant is open
- **THEN** the request is refused

#### Scenario: No tenant grant is consulted

- **WHEN** a staff request reads tenant data under an access grant
- **THEN** no tenant permission grant is evaluated

### Requirement: Staff activity within an account is recorded and readable by that account

The service SHALL record what a staff subject reached under an access grant, and SHALL make the fact
of the access readable by the account it was opened against, together with the reason and the window.

Recording of staff reads SHALL be kept separately from the audit trail of changes, and MAY be
recorded per access grant rather than per request.

#### Scenario: An account can see that support accessed it

- **WHEN** an access grant is opened against an account and used
- **THEN** the account can read that support accessed it, with the reason and the time

#### Scenario: Read recording does not enter the change trail

- **WHEN** a staff subject reads under an access grant and makes no change
- **THEN** no entry is added to the trail of creations, modifications and deletions

#### Scenario: Staff changes are recorded as changes

- **WHEN** a staff subject modifies an object under an access grant
- **THEN** the change is recorded in the audit trail as any other change is
