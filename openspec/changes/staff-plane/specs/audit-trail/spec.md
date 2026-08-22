## ADDED Requirements

### Requirement: An entry carries a presented identity distinct from the acting subject

An entry recording an action taken by a staff subject or by a command-line invocation SHALL carry two
identities: the acting subject, retained for the operator of the service, and a presented identity,
shown to the account the action affected.

The presented identity SHALL NOT identify an individual person. It SHALL be accompanied by the reason
recorded for the access under which the action was taken, where one exists.

The account's view of the trail SHALL read the presented identity and SHALL NOT have access to the
acting subject.

#### Scenario: A staff change shows a presented identity to the account

- **WHEN** a staff subject revokes an API key of an account under an access grant
- **THEN** the account's audit trail records the change with the presented identity and the reason

#### Scenario: The acting subject is retained

- **WHEN** the same entry is read by the operator of the service
- **THEN** it identifies the individual staff subject who acted

#### Scenario: The account's view cannot reach the acting subject

- **WHEN** an account reads, filters or exports its audit trail
- **THEN** no individual staff subject is disclosed in any field or by any filter

### Requirement: Entries are marked as visible to the account or not

Each entry SHALL record whether it is readable by the account it concerns. An account's read of the
trail SHALL return only entries so marked.

Entries recording a change to an account's inventory SHALL be marked visible, whether the change was
made by a member, by a staff subject, or by a command-line invocation.

#### Scenario: A command-line change is visible to the account

- **WHEN** an operator changes an account's inventory from the command line
- **THEN** that account's audit trail records the change with a presented identity

#### Scenario: An entry not concerning an account is not returned to one

- **WHEN** an entry records an action on a platform object with no owning account
- **THEN** no account's audit trail returns it

#### Scenario: Visibility does not depend on the surface used

- **WHEN** the same change is made through the console, through the staff surface and through the command line
- **THEN** each is recorded as visible to the account it affected
