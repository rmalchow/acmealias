## ADDED Requirements

### Requirement: Staff are authorized by a plane separate from the tenant permission model

The service SHALL authorize operators of the service through a permission system distinct from the
one that authorizes account members and API keys. Staff authorization SHALL NOT evaluate tenant
permission grants, and tenant authorization SHALL NOT evaluate staff permissions.

#### Scenario: Staff authorization does not consult tenant grants

- **WHEN** a staff operation is authorized
- **THEN** no tenant permission grant is evaluated

#### Scenario: A tenant subject holds no staff permission

- **WHEN** an account member or an API key attempts a staff operation
- **THEN** the request is refused

#### Scenario: A staff subject holds no tenant permission

- **WHEN** a staff subject attempts an operation authorized by the tenant permission model
- **THEN** the request is refused unless it is performed through an access grant

### Requirement: Staff permissions are flat named permissions derived from group claims

A staff permission SHALL be a name and SHALL NOT be scoped to an object. Staff permissions SHALL be
derived from the group membership of the authenticated staff subject by configuration, SHALL NOT be
stored in the database, and SHALL NOT be modifiable through any management interface.

A staff subject's permissions SHALL be the union of every permission whose configured group appears
in their group claim. A permission whose setting is unset SHALL be held by nobody.

#### Scenario: A configured group grants its permission

- **WHEN** a staff subject's groups include the group configured for a permission
- **THEN** the subject holds that permission

#### Scenario: An unset permission is held by nobody

- **WHEN** no group is configured for a staff permission
- **THEN** no subject holds it

#### Scenario: The mapping is not editable at runtime

- **WHEN** any management route is searched for an operation that changes the staff group mapping
- **THEN** no such operation exists

#### Scenario: A staff subject mapping to nothing is told why

- **WHEN** a staff subject authenticates successfully but is in none of the configured groups
- **THEN** access is refused and the response reports the groups that were observed

#### Scenario: A staff permission names no object

- **WHEN** the staff permission vocabulary is examined
- **THEN** no permission takes an object, an identifier or a pattern

### Requirement: Operations without a tenant equivalent are staff permissions

The service SHALL express as staff permissions those operations that have no owning account or that
act upon an account rather than within it, including at least: opening access into an account,
suspending and restoring an account, adjusting an account's credit balance, account lifecycle,
reassigning account ownership, administering platform objects, and reading the staff activity trail.

Each SHALL be separately grantable.

#### Scenario: An operation is refused without its permission

- **WHEN** a staff subject performs an operation whose permission it does not hold
- **THEN** the request is refused

#### Scenario: Permissions are separately grantable

- **WHEN** a staff subject holds the permission to open access but not to suspend an account
- **THEN** it may open access and may not suspend

#### Scenario: Platform administration reaches no tenant inventory

- **WHEN** a staff subject holding only the platform administration permission reads a tenant's inventory
- **THEN** the request is refused

### Requirement: Account ownership can be reassigned from outside the account

The service SHALL provide a staff operation that grants a person the unrestricted role of an account,
without regard to the rule that a subject may grant only what it holds and without regard to the
requirement that the unrestricted role always has a member.

The operation SHALL name one account and one person, SHALL be recorded with the acting staff subject,
and SHALL be visible to the account it acts on.

#### Scenario: An account with no reachable administrator is repaired

- **WHEN** an account's unrestricted role has no reachable member and a staff subject holding the reassignment permission grants it to a person
- **THEN** that person holds the unrestricted role in that account

#### Scenario: Reassignment is refused without its permission

- **WHEN** a staff subject that does not hold the reassignment permission attempts it
- **THEN** the request is refused

#### Scenario: Reassignment is attributable and visible

- **WHEN** ownership is reassigned
- **THEN** an entry records the acting staff subject, and the account can read that its ownership was changed

### Requirement: Staff authorization is evaluated per request

The service SHALL derive a staff subject's permissions on every request from the running
configuration and the subject's group claim, and SHALL NOT store the derived permissions on the
session.

#### Scenario: A configuration change takes effect without re-authentication

- **WHEN** the staff group mapping is changed and the service is restarted while a staff session is open
- **THEN** the next request derives permissions from the new mapping

### Requirement: Suspending an account is a staff operation that requires no access into it

The service SHALL provide a staff permission and operation that suspends an account, taking the
account and a reason, without opening an access grant into that account and without consulting any
tenant grant.

Suspension SHALL mark the account suspended with its reason and the acting staff identity, SHALL
block the identities whose only membership is that account, SHALL refuse and invalidate their
sessions, and SHALL add the account's zone names to the zone blocklist.

Suspension SHALL be an override: it SHALL NOT alter the enabled state of any zone, recordset, check or
credential it suppresses, and lifting it SHALL restore the state that existed beforehand.

#### Scenario: Suspension needs no support access

- **WHEN** an account is suspended
- **THEN** no access grant is opened and no tenant data is read beyond what the operation reports

#### Scenario: A reason is required

- **WHEN** suspension is requested without a reason
- **THEN** it is refused

#### Scenario: Lifting a suspension restores prior state

- **WHEN** an account whose customer had disabled one zone is suspended and later restored
- **THEN** that zone is still disabled and the others are served again

#### Scenario: Blocked identities cannot start again

- **WHEN** a user blocked by a suspension registers a new account or signs in
- **THEN** the attempt is refused

### Requirement: Suspension reports its collateral before it is applied

Before applying a suspension, the service SHALL report every other account the affected identities
belong to, and SHALL indicate any such account in which an affected identity is the last remaining
owner.

#### Scenario: Other memberships are surfaced

- **WHEN** a suspension would block an identity that belongs to another account
- **THEN** that account is named in the report presented before the suspension is applied

#### Scenario: A last owner is flagged

- **WHEN** an affected identity is the only owner of another account
- **THEN** the report marks that account as being left without an owner

### Requirement: Blocklists are readable, reasoned, and scoped to where they apply

The service SHALL hold a blocklist of identity entries — canonicalized email addresses and email
domains — and a blocklist of zone names. Domain and zone entries SHALL match by suffix, and a zone
entry MAY be a top-level domain.

Each entry SHALL record what it applies to — registration and sign-in, zone creation, or both — and
SHALL carry a reason and the time it was added. Entries SHALL be readable only through the staff
plane, and SHALL be retained for a stated period.

Identity entries SHALL be checked at registration, at sign-in, and when an invitation is accepted.

#### Scenario: An entry applies only where it is scoped

- **WHEN** an entry scoped to registration is present and a zone of the same name is created
- **THEN** the zone is not refused by that entry

#### Scenario: A suffix entry covers subordinate names

- **WHEN** a zone entry names a domain and a zone beneath it is created
- **THEN** the zone is refused

#### Scenario: A blocked address is refused at invitation acceptance

- **WHEN** an invitation to a blocked address is accepted
- **THEN** acceptance is refused and the account administrator is told the address cannot be used

#### Scenario: An entry states why it exists

- **WHEN** a blocklist entry is read by staff
- **THEN** it carries its reason and the time it was added

### Requirement: Adding a blocklist entry does not itself stop existing service

Adding an entry SHALL refuse subsequent registrations, sign-ins or zone creations that it matches, and
SHALL NOT by itself suspend an existing account or withdraw an existing zone. Matching existing
objects SHALL be flagged for review, and enforcing an entry against one SHALL be a separate explicit
action.

#### Scenario: An existing zone keeps serving

- **WHEN** an entry is added that matches a zone already being served
- **THEN** that zone continues to be served and is flagged for review

#### Scenario: A new zone matching the entry is refused

- **WHEN** a zone matching an entry is created after the entry is added
- **THEN** it is refused
