# accounts-and-tenancy Specification

## Purpose
TBD - created by archiving change tenancy-and-permissions. Update Purpose after archive.
## Requirements
### Requirement: Every inventory object has exactly one owning account

The service SHALL define an account as the owner of inventory and the anchor of every authorization
decision. Every project SHALL belong to exactly one account, and every load balancer, node, domain,
alias, API key, challenge record and interaction record SHALL be owned by the account of the project
that contains it.

An object SHALL NOT be storable with an owning account that differs from the one its parent has.

#### Scenario: A project cannot exist without an account

- **WHEN** a project is created
- **THEN** it is owned by the account of the requester, and a project with no account cannot be stored

#### Scenario: Ownership cannot disagree with the parent

- **WHEN** a load balancer, node, domain, alias or API key is stored with an owning account other than that of its parent
- **THEN** the write is refused

### Requirement: Reads and writes are confined to one account

Every management operation SHALL be evaluated against exactly one account and SHALL NOT read or
modify an object owned by another. An operation naming an object of another account SHALL be refused
as though the object did not exist, and SHALL NOT reveal that it exists.

Confinement SHALL be enforced in the query layer, independently of the permissions the acting subject
holds.

#### Scenario: Listing shows only the caller's account

- **WHEN** a member of one account lists projects while other accounts hold projects
- **THEN** only the projects of that account are returned

#### Scenario: Addressing another account's object is refused

- **WHEN** a member of one account requests a load balancer belonging to another by its identifier
- **THEN** the request is refused and the response does not distinguish it from an object that does not exist

#### Scenario: Confinement does not depend on the caller's permissions

- **WHEN** a member holding every permission in their account addresses another account's object
- **THEN** the request is refused

#### Scenario: Confinement does not depend on the permission check

- **WHEN** an operation is evaluated whose permission check would permit it
- **THEN** the query is still confined to the account it was evaluated against

### Requirement: Confinement is enforced by the database, not only by the query

Every table carrying a materialized owning account SHALL have a database policy restricting rows to
the account named by a per-connection setting, and that setting SHALL be established by the same
mechanism that produces the confinement value for the request. A query that omits the ownership
predicate SHALL still return no row of another account.

The setting SHALL be scoped to the transaction in which the query runs, so that a connection
returned to a pool carries no residual account.

Components that must read across accounts — zone distribution, usage aggregation, check ingestion,
command-line administration and migrations — SHALL connect as a distinct database role exempt from
the policy, and no other component SHALL use that role.

A table that carries no owning account SHALL still be confined where it references one that does, by
a policy requiring its parent row to be visible. Records of challenge activity that deliberately hold
no reference to their subject SHALL be exempt, and SHALL be confined by the operation that lists
them.

#### Scenario: A query missing its ownership predicate still returns nothing

- **WHEN** a read is executed for one account against a table carrying an owning account, without an ownership predicate in the query itself
- **THEN** no row belonging to another account is returned

#### Scenario: The account setting does not survive a pooled connection

- **WHEN** a connection used for one account's request is returned to the pool and re-used for another account's request
- **THEN** the second request observes only its own account

#### Scenario: A child table without an owning account is confined through its parent

- **WHEN** a read is executed for one account against a table whose rows reference a table carrying an owning account
- **THEN** only rows whose referenced row belongs to that account are returned

#### Scenario: A write into another account's parent is refused

- **WHEN** a row is inserted referencing a parent belonging to another account
- **THEN** the write is refused

#### Scenario: A cross-account component uses a named exempt role

- **WHEN** a component that must read across accounts connects to the database
- **THEN** it authenticates as the exempt role, and a component connecting as the ordinary role observes the policy

### Requirement: A query cannot be issued without naming an account

The service SHALL require every inventory query to carry the account it is confined to, in a form
that cannot be constructed from a value supplied by a caller.

#### Scenario: An account is required to query

- **WHEN** the inventory query layer is searched for a read that does not take an account
- **THEN** no such read exists

#### Scenario: A caller-supplied identifier cannot become a confinement

- **WHEN** a request carries an account identifier as a parameter
- **THEN** it is not used to confine the query, and the confinement is derived from the caller's membership

### Requirement: The internal edition runs as a single account

In an edition that authenticates through an identity provider the service SHALL operate with exactly
one account, created when the schema is migrated, owning every project. The console SHALL NOT present
the account, offer a choice of account, or require one to be named.

#### Scenario: Existing inventory is preserved on migration

- **WHEN** a deployment is migrated to a schema that has accounts
- **THEN** every existing project belongs to the one account and remains reachable exactly as before

#### Scenario: No account is visible

- **WHEN** an operator uses the console in that edition
- **THEN** no screen names an account and no operation requires one

### Requirement: A session acts in one selected account, named on every request

A session SHALL be in exactly one of two states: no account selected, or one account selected. A session
with no account selected SHALL be able to read its own user profile, list the accounts it belongs to,
create an account, and accept an invitation, and SHALL be refused every other operation with a response
stating that no account is selected.

A session MAY change its selected account without re-authenticating. Changing it SHALL re-evaluate the
user's membership of the account being selected, and SHALL be refused if that membership no longer
exists.

An account-scoped request SHALL name the account it acts in, and the service SHALL refuse it when the
named account is not the session's selected account. A request authenticated by an API key SHALL take
its account from the key's anchor and SHALL NOT require a selection.

#### Scenario: Nothing is reachable before an account is selected

- **WHEN** a session with no account selected requests an account-scoped operation
- **THEN** it is refused, and the response states that no account is selected

#### Scenario: A user with no membership can still begin

- **WHEN** a session with no account selected reads its own profile, lists its accounts, creates an account, or accepts an invitation
- **THEN** the request succeeds

#### Scenario: The account is named as well as selected

- **WHEN** a session with one account selected addresses a resource naming a different account it also belongs to
- **THEN** the request is refused

#### Scenario: Switching accounts re-checks membership

- **WHEN** a session selects an account whose membership was revoked after the session began
- **THEN** the selection is refused and the previous selection is unchanged

#### Scenario: A key needs no selection

- **WHEN** a request is authenticated by an API key
- **THEN** it acts in the account of the key's anchor, and no selection is required

#### Scenario: A single-account edition selects automatically

- **WHEN** a person signs in to an edition that operates as one account
- **THEN** that account is selected, and no screen offers a selection or a switch

### Requirement: A user exists independently of every account

The service SHALL represent a person as a user that belongs to no account, owns no inventory, and is not
removed when an account is. A user MAY hold memberships in several accounts or in none.

A user holding no membership SHALL be able to authenticate, to create an account, and to accept an
invitation, and SHALL NOT be able to perform any other operation.

#### Scenario: A user survives losing every membership

- **WHEN** a user's last membership is removed
- **THEN** the user continues to exist and can still authenticate

#### Scenario: A user with no membership can still begin

- **WHEN** a user holding no membership signs in
- **THEN** they may create an account or accept an invitation, and no other operation is available

#### Scenario: Deleting an account does not delete its members

- **WHEN** an account is deleted
- **THEN** the users who were its members continue to exist

### Requirement: Any authenticated user may create an account and becomes its unrestricted member

The service SHALL allow any authenticated user to create an account, and SHALL make the creator a member
of that account's unrestricted role as part of the same operation.

#### Scenario: The creator can administer what they created

- **WHEN** a user creates an account
- **THEN** they hold its unrestricted role and may create projects in it

#### Scenario: An account is never created without a member

- **WHEN** account creation fails after the account row is written
- **THEN** no account exists without a member of its unrestricted role

### Requirement: Operations on an account are governed by membership, not by grants

The service SHALL govern operations acting on an account itself by membership rather than by grants, and
SHALL refuse any grant naming an account as its object.

Creating and deleting a project, inviting a user, and changing a membership SHALL require membership of
the account's unrestricted role. Reading the account's members SHALL require only membership of that
account.

#### Scenario: An account cannot be named by a grant

- **WHEN** a grant is written naming an account as its object
- **THEN** the request is refused

#### Scenario: Membership changes require the unrestricted role

- **WHEN** a member who is not in the unrestricted role invites a user or changes a membership
- **THEN** the request is refused, whatever grants that member holds

### Requirement: Every member of an account can see who else is in it

The service SHALL allow any member of an account to read that account's members and its outstanding
invitations, without requiring a grant.

The reading SHALL be confined to the caller's own account, and SHALL report for each entry the user or
invited address, the role held or offered, and when the membership or invitation began.

#### Scenario: A member sees the roster

- **WHEN** a member holding no grants reads the account's members
- **THEN** every member of that account is returned

#### Scenario: Outstanding invitations are part of the roster

- **WHEN** a user has been invited and has not yet accepted
- **THEN** that invitation appears to every member of the account

#### Scenario: The roster does not cross accounts

- **WHEN** a member of one account reads members while other accounts have members
- **THEN** only the members of that account are returned

### Requirement: An account is either personal or team, and neither becomes the other

Every account SHALL have a kind, fixed at creation, of either `personal` or
`team`. The service SHALL refuse any change of kind after creation.

A user SHALL own at most one personal account, and any number of team accounts.
The limit SHALL be enforced by the datastore rather than only by the service.

A personal account SHALL be free, SHALL hold no members other than its owner, and
SHALL NOT be transferable. A team account SHALL be created only by funding it,
MAY hold other members, and MAY be transferred to another user.

#### Scenario: A second personal account is refused

- **WHEN** a user who already owns a personal account creates another
- **THEN** the creation is refused and the existing account is unaffected

#### Scenario: Team accounts are not limited

- **WHEN** a user who owns a personal account and several team accounts funds another
- **THEN** it is created

#### Scenario: A personal account cannot be transferred

- **WHEN** a transfer of a personal account is requested
- **THEN** it is refused, and the owner is unchanged

#### Scenario: A transfer never produces a second personal account

- **WHEN** a transfer would leave the recipient owning two personal accounts
- **THEN** it is refused

#### Scenario: Kind is immutable

- **WHEN** a request attempts to change an account's kind
- **THEN** it is refused

### Requirement: The account's owner administers it

The owner of a team account SHALL be a member of the account's unrestricted role
at all times. The service SHALL refuse to remove the owner from that role, and
SHALL establish the recipient's unrestricted membership as part of every
transfer.

An account SHALL also keep at least one member of its unrestricted role. The two
refusals SHALL be distinguishable to the caller, because their remedies differ:
one is resolved by appointing another unrestricted member, the other only by
transferring the account.

#### Scenario: The owner cannot be removed from the unrestricted role

- **WHEN** a removal of the owner's unrestricted membership is requested, whether
  or not the account has other unrestricted members
- **THEN** it is refused, and the refusal names ownership rather than the count

#### Scenario: A transfer leaves the recipient able to administer the account

- **WHEN** a team account is transferred to somebody who is already a member of it
  holding a restricted role, or no role
- **THEN** the recipient also holds the unrestricted role once the transfer completes

#### Scenario: The last unrestricted member cannot be removed

- **WHEN** a removal would leave the unrestricted role with no members
- **THEN** it is refused, and the refusal names the count rather than ownership

### Requirement: A team account works only while it is funded

A team account SHALL be usable only while its credit balance is greater than
zero. A personal account SHALL always be usable and SHALL have no balance.

While a team account is not funded:

- a member other than the owner SHALL NOT be able to select it;
- its account-scoped management routes SHALL be refused;
- its owner SHALL still be able to select it, and SHALL be offered its balance and
  the means to add to it, and nothing else.

Suspension for non-payment and suspension by staff SHALL be separate state.
Adding credit SHALL NOT lift a staff suspension.

#### Scenario: A member cannot select an unfunded account

- **WHEN** a member who is not the owner selects an unfunded team account
- **THEN** selection is refused and the account is reported as suspended

#### Scenario: The owner can always reach the way back

- **WHEN** the owner selects an unfunded team account
- **THEN** selection succeeds and the balance and the means to add to it are available

#### Scenario: The boundary is exactly zero

- **WHEN** an account holds one credit
- **THEN** it is usable
- **WHEN** it holds none
- **THEN** it is suspended

#### Scenario: Paying does not lift a staff suspension

- **WHEN** credit is added to an account suspended by staff
- **THEN** the balance increases and the account remains suspended

### Requirement: A personal account is limited by a project quota; a team account is limited by its balance

A personal account SHALL hold no more than a configured number of projects. The
limit SHALL be configuration rather than a compiled-in constant.

A team account SHALL NOT be subject to a project quota.

Exceeding the quota SHALL be refused as a lack of room rather than as a lack of
authority, and the refusal SHALL state what would resolve it.

#### Scenario: A personal account fills up

- **WHEN** a personal account at its limit creates another project
- **THEN** it is refused, and the refusal says there is no room rather than that the caller may not

#### Scenario: The refusal is not a permission error

- **WHEN** the refusal above is returned to a caller who holds every grant
- **THEN** it is still refused, and still on grounds of room

#### Scenario: A team account has no project limit

- **WHEN** a funded team account creates projects beyond the personal limit
- **THEN** they are created

### Requirement: Credit is added through a checkout that records a real charge

The service SHALL provide a checkout that credits an account by an amount chosen
from a fixed set. An amount supplied by the caller outside that set SHALL be
refused.

A credit SHALL be recorded in the audit trail, attributed to the user who
performed it, naming the account and the amount.

Creating a team account SHALL happen in the same transaction as its first credit,
so that no team account exists that has never been funded.

#### Scenario: Only offered amounts are accepted

- **WHEN** a checkout requests an amount outside the offered set
- **THEN** it is refused and no credit is recorded

#### Scenario: A credit is auditable

- **WHEN** an account is credited
- **THEN** the audit trail records who did it, which account, and how much

#### Scenario: No unfunded team account is created

- **WHEN** creating a team account fails at any point before its first credit is recorded
- **THEN** no account exists

