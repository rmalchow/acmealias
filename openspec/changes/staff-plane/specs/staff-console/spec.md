## ADDED Requirements

### Requirement: Staff can find an account without first knowing which one it is

The staff surface SHALL provide a search across every account, requiring no
access grant, that answers which account something belongs to.

It SHALL search projects by project name and by domain name, and accounts by
account name and by owner.

It SHALL NOT return the contents of any account — not a load balancer's nodes,
not a credential, not an audit entry, not a challenge value. Answering *which
account* is its whole purpose; answering *what is in it* requires an access
grant.

A domain SHALL be reachable only by supplying one. The staff surface SHALL NOT
offer a listing of all domains.

#### Scenario: A domain leads to its account

- **WHEN** a member of staff searches for a domain name a customer reported
- **THEN** the project and account holding it are identified

#### Scenario: Search returns no contents

- **WHEN** any search returns results
- **THEN** no inventory belonging to those accounts is included

#### Scenario: Domains cannot be enumerated

- **WHEN** a member of staff asks for all domains
- **THEN** no such listing is available

#### Scenario: Finding needs no grant

- **WHEN** a member of staff searches while holding no access grant
- **THEN** the search succeeds

### Requirement: An account view shows who owns it, what is in it, and whether it is paid for

The staff surface SHALL show, for one account and without an access grant: its
owner, its projects, and its credit status.

Credit status SHALL distinguish an account stopped by its balance from one
suspended by staff, since these are indistinguishable from inside the customer's
console and only staff can tell the customer which it is.

#### Scenario: The commonest question is answered without a second lookup

- **WHEN** a member of staff opens an account
- **THEN** its owner, its projects and its credit status are shown together

#### Scenario: The two stopped states are told apart

- **WHEN** an account is stopped
- **THEN** the view distinguishes an exhausted balance from a staff suspension

### Requirement: A search is recorded once, and not in the accounts it touched

A search SHALL be recorded in the staff plane's own read record, naming who
searched and for what.

It SHALL NOT write an entry into the audit trail of any account it matched.

#### Scenario: One record per search

- **WHEN** a search matches many accounts
- **THEN** exactly one staff read record is written

#### Scenario: Tenants are not told about searches

- **WHEN** a search matches an account
- **THEN** nothing appears in that account's audit trail
