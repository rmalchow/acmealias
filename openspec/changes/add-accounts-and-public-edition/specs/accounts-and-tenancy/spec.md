## ADDED Requirements

### Requirement: Every inventory object has exactly one owning account

The service SHALL define an account as the owner of inventory and the boundary of every
authorization decision. Every project SHALL belong to exactly one account, and every load
balancer, node, domain, alias, API key, challenge record and interaction record SHALL
derive its owning account from the project of its load balancer.

An object SHALL NOT carry an owning account that can disagree with the one it inherits.

#### Scenario: A project cannot exist without an account

- **WHEN** a project is created
- **THEN** it is owned by the account of the requester, and a project with no account cannot be stored

#### Scenario: Ownership is inherited, not repeated

- **WHEN** the account owning a project is determined for a domain
- **THEN** it is the account of that domain's load balancer's project, and no other source

### Requirement: Reads and writes are confined to one account

Every management operation SHALL be evaluated against exactly one account, and SHALL NOT read or
modify an object owned by another. An operation naming an object of another account SHALL be
refused as though the object did not exist, and SHALL NOT reveal that it exists.

#### Scenario: Listing shows only the caller's account

- **WHEN** a member of one account lists projects while other accounts hold projects
- **THEN** only the projects of that account are returned

#### Scenario: Addressing another account's object is refused

- **WHEN** a member of one account requests a load balancer belonging to another by its identifier
- **THEN** the request is refused and the response does not distinguish that load balancer from one that does not exist

#### Scenario: Confinement does not depend on the caller's role

- **WHEN** a member holding the highest role in their account addresses another account's object
- **THEN** the request is refused

### Requirement: The internal edition runs as a single account

In the internal edition the service SHALL operate with exactly one account, created when the
schema is migrated, owning every project. The console SHALL NOT present the account, offer a
choice of account, or require one to be named.

#### Scenario: Existing inventory is preserved on migration

- **WHEN** an internal deployment is migrated to a schema that has accounts
- **THEN** every existing project belongs to the one account and remains reachable exactly as before

#### Scenario: No account is visible internally

- **WHEN** an operator uses the internal console
- **THEN** no screen names an account and no operation requires one

### Requirement: A person's roles are held per account

A person SHALL hold roles in an account through a membership, and MAY hold memberships in more
than one account with different roles in each. A role held in one account SHALL confer nothing in
another.

#### Scenario: Roles do not cross accounts

- **WHEN** a person holds an administrative role in one account and a read-only role in another
- **THEN** they may perform administrative operations only in the first

#### Scenario: The internal edition maps groups to memberships

- **WHEN** an operator signs in through the identity provider in the internal edition
- **THEN** the roles derived from their group claims are held in the single account
