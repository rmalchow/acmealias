## ADDED Requirements

### Requirement: The account picker always offers somewhere to work

The console SHALL present the accounts a user may act in, and that list SHALL
always include their personal account. The personal account SHALL be presented as
a distinct entry rather than as one named account among the team ones.

A suspended team account SHALL appear in the picker for its owner rather than
being omitted, since the owner is the only person who can restore it.

#### Scenario: A new user has somewhere to go

- **WHEN** a user with no team accounts opens the picker
- **THEN** their personal account is offered

#### Scenario: A suspended account is reachable by its owner

- **WHEN** the owner of a suspended team account opens the picker
- **THEN** it is listed and shown as suspended

#### Scenario: A suspended account is not selectable by a member

- **WHEN** a member who is not the owner attempts to select it
- **THEN** selection is refused and the reason is that the account is suspended, not that they lack permission

### Requirement: Navigation follows the selected account

The console's navigation SHALL reflect the capabilities of the selected account,
and SHALL change when the selection changes without requiring a reload.

A screen for a capability the account kind does not have SHALL be absent. A
control the caller merely lacks authority for SHALL remain visible and disabled
with a reason.

#### Scenario: A personal account offers no member management

- **WHEN** a personal account is selected
- **THEN** no members or roles screen is offered

#### Scenario: Switching accounts changes the navigation

- **WHEN** the selection changes from a personal account to a team account
- **THEN** the navigation updates without a reload

#### Scenario: Lacking a grant does not remove a control

- **WHEN** a user without the necessary grant views a screen whose account kind supports it
- **THEN** the control is shown, disabled, and states why

### Requirement: Settings holds what administers the account

The console SHALL provide a Settings screen holding the administration of the
account itself — its members, the roles they hold, and its credits — separately
from the inventory the account contains.

For a suspended team account, its owner SHALL be shown the balance and the means
to add to it, and nothing else.

#### Scenario: An owner can restore a suspended account

- **WHEN** the owner selects a suspended team account
- **THEN** they are shown the balance and a way to add credit, and no inventory screens

#### Scenario: Credit is added and the account resumes

- **WHEN** the owner completes a checkout for one of the offered amounts
- **THEN** the balance reflects it and the account becomes usable without a new session
