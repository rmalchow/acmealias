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
name, the group membership observed at sign-in, and the roles held in the acting account at the time
of the action.

Recorded roles SHALL be the names the roles bore at that moment, retained as written rather than as
references, so that renaming or deleting a role does not alter what an entry reports.

Each entry SHALL record the account it occurred in.

#### Scenario: Human action records held roles

- **WHEN** a user creates a domain
- **THEN** the entry records their subject, display name, groups and the roles they held in that account at that moment

#### Scenario: Roles are not re-resolved on read

- **WHEN** the group-to-role configuration changes after an entry was written
- **THEN** the entry continues to report the roles that were in effect when the action occurred

#### Scenario: A renamed role does not alter past entries

- **WHEN** a role is renamed or deleted after an entry naming it was written
- **THEN** the entry continues to report the name the role bore at the time

#### Scenario: Non-human actors are distinguishable

- **WHEN** a change is made by a command-line invocation or by an automatic process
- **THEN** the entry identifies which, and is distinguishable from a console action

#### Scenario: An entry names the account it occurred in

- **WHEN** any recorded action is read
- **THEN** the entry reports the account the action acted in

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

### Requirement: Changes to roles, grants and memberships are recorded

The service SHALL record the creation, modification and deletion of a role, of the grants a role or
an API key holds, and of a person's memberships, as distinct actions in the audit vocabulary, with
the state before and after the change.

An entry recording a grant SHALL record it in full — object type, object identifier or selector,
action, and whether it is inheritable — so that the authority conferred is readable from the entry
alone.

#### Scenario: Granting is attributable

- **WHEN** a grant is added to a role
- **THEN** an entry names the acting subject, the role and the grant in full

#### Scenario: Membership changes are attributable

- **WHEN** a person is added to or removed from a role
- **THEN** an entry names the acting subject, the person and the role

#### Scenario: A refused escalation is distinguishable from a change

- **WHEN** a grant is refused because it exceeds the writer's own authority
- **THEN** no entry records a change, and the refusal is recorded in the log

### Requirement: The audit trail is confined to the account that reads it

A read of the audit trail SHALL return only entries recorded in the account the request acts in.

#### Scenario: Another account's entries are not returned

- **WHEN** a member of one account reads the audit trail while other accounts hold entries
- **THEN** only entries of that account are returned

### Requirement: The invitation lifecycle is recorded

The service SHALL record the issuing, revocation, re-sending, acceptance, decline and expiry of an
invitation as distinct actions, each naming the account and the invited address.

An entry for an acceptance SHALL name both the invited address and the accepting user, so that the record
evidences that the person who joined is the person who was invited.

An entry recording an expiry SHALL identify the service itself as the actor.

#### Scenario: Issuing is attributable

- **WHEN** an invitation is issued
- **THEN** an entry names the issuing user and the invited address

#### Scenario: Acceptance evidences who joined

- **WHEN** an invitation is accepted
- **THEN** an entry names both the invited address and the user that accepted

#### Scenario: Expiry is not attributed to a person

- **WHEN** an invitation expires
- **THEN** an entry records it with the service as the actor

#### Scenario: Revocation and re-sending are distinguishable

- **WHEN** an invitation is revoked, and another is re-sent
- **THEN** the two entries record different actions

#### Scenario: A decline is distinguishable from an expiry

- **WHEN** an invitation is declined, and another expires unanswered
- **THEN** the two entries record different actions, and the decline names the user that declined

### Requirement: An invitation entry never contains its token

The service SHALL exclude an invitation's token, and any stored form of it, from every audit entry.

#### Scenario: The token is absent from the record

- **WHEN** any entry describing an invitation is read
- **THEN** it contains neither the token nor a stored form of it

### Requirement: Refused invitation issues are recorded

The service SHALL record an invitation refused for exceeding a rate limit, identifying the acting user
and that a limit was reached.

#### Scenario: A rate-limited attempt leaves a trace

- **WHEN** an invitation is refused because a rate limit was reached
- **THEN** the refusal is recorded with the acting user, and no invitation is created

