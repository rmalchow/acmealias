# audit-trail Specification

## Purpose
TBD - created by archiving change add-audit-and-usage-reporting. Update Purpose after archive.
## Requirements
### Requirement: Configuration changes are recorded with before and after state

The service SHALL record an audit entry for every creation, modification and deletion of a project,
load balancer, node, domain or alias. Each entry SHALL carry the prior state of the object and
its resulting state, with a creation carrying no prior state and a deletion carrying no resulting
state.

#### Scenario: Modification records both states

- **WHEN** a load balancer's node address is changed
- **THEN** an entry records the object, the previous address and the new address

#### Scenario: Deletion is recorded with what was removed

- **WHEN** a domain is deleted
- **THEN** an entry records the domain's state before deletion and no resulting state

#### Scenario: Cascading effects are recorded

- **WHEN** deleting the last domain for an alias also removes that alias
- **THEN** the removal of the alias is recorded as its own entry

### Requirement: Credential events are recorded as distinct actions

The service SHALL record API key creation, source-address restriction changes, expiry changes,
revocation and automatic expiry as separate, individually identifiable actions. Each SHALL carry
the key identifier, the load balancer, and the source-address restrictions in effect.

Audit entries SHALL NOT contain a key secret or the stored hash of one, in any field.

#### Scenario: Rotation is reconstructable

- **WHEN** a key is created for a load balancer and an older key is later revoked
- **THEN** both actions are recorded as distinct entries identifying the keys and the load balancer

#### Scenario: Automatic expiry is recorded

- **WHEN** a key passes its expiry without human action
- **THEN** an entry records the expiry with a system actor

#### Scenario: No secret material is stored

- **WHEN** any audit entry concerning an API key is read
- **THEN** it contains neither the secret nor its stored hash

### Requirement: Entries identify the actor and the authority they held

Each entry SHALL identify the actor as one of: an authenticated human, a command-line invocation, or
the service itself. For a human actor the entry SHALL record the subject identifier, the display
name, the group membership observed at sign-in, and the roles resolved from that membership at the
time of the action.

#### Scenario: Human action records resolved roles

- **WHEN** a user creates a domain
- **THEN** the entry records their subject, display name, groups and the roles they held at that moment

#### Scenario: Roles are not re-resolved on read

- **WHEN** the group-to-role configuration changes after an entry was written
- **THEN** the entry continues to report the roles that were in effect when the action occurred

#### Scenario: Non-human actors are distinguishable

- **WHEN** a change is made by a command-line invocation or by an automatic process
- **THEN** the entry identifies which, and is distinguishable from a console action

### Requirement: Entries survive the objects they describe

Each entry SHALL carry a human-readable label for its object and the identifier of the project it
belonged to, stored independently of the referenced rows, so that entries remain meaningful and
filterable after those rows are deleted.

#### Scenario: Entry remains readable after deletion

- **WHEN** a domain is deleted and its audit entries are read afterwards
- **THEN** the entries still name the domain

#### Scenario: Filtering survives project deletion

- **WHEN** a project is deleted and entries are filtered by that project
- **THEN** its entries are still returned

### Requirement: The trail is append-only to the application

The service SHALL contain no operation that modifies or deletes an audit entry, and the database
role it uses SHALL hold only insert and select privileges on the audit store.

#### Scenario: No modification path exists

- **WHEN** the management API and command-line surfaces are examined
- **THEN** no operation updates or deletes an audit entry

#### Scenario: The datastore refuses modification

- **WHEN** the service's database role attempts to update or delete an audit entry
- **THEN** the attempt is refused

### Requirement: The audit entry is written atomically with the change

An audit entry SHALL be written within the same transaction as the change it describes. If the
change is rolled back the entry SHALL NOT persist, and if the entry cannot be written the change
SHALL fail.

#### Scenario: Rolled-back change leaves no entry

- **WHEN** a change fails after its audit entry is written but before commit
- **THEN** no entry persists

#### Scenario: Failed entry fails the change

- **WHEN** the audit entry cannot be written
- **THEN** the change is not applied and the request fails

### Requirement: Entries are readable and filterable

The service SHALL expose audit entries through the management interface, filterable by project,
actor, object type, action and time range, ordered most recent first, and paginated. All roles that
may read the inventory SHALL be able to read the audit trail.

#### Scenario: Filtering by actor

- **WHEN** entries are requested for one actor
- **THEN** only that actor's entries are returned, most recent first

#### Scenario: Read-only role may read the trail

- **WHEN** a user holding only the read-only role requests audit entries
- **THEN** the entries are returned

