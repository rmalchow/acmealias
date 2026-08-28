## MODIFIED Requirements

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

## ADDED Requirements

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
