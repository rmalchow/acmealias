## ADDED Requirements

### Requirement: The account picker always offers somewhere to work

The console SHALL present the accounts a user may act in, and that list SHALL
always include their personal account. The personal account SHALL be presented as
a distinct entry rather than as one named account among the team ones.

The picker SHALL be searchable, and SHALL be present but inert for a user with a
single account, so that the navigation does not change shape when a second
account is obtained. It SHALL be accompanied by an action that obtains a new team
account, which SHALL lead to the same place as any other offer to obtain one.

A suspended team account SHALL appear in the picker for its owner rather than
being omitted, since the owner is the only person who can restore it.

#### Scenario: A new user has somewhere to go

- **WHEN** a user with no team accounts opens the picker
- **THEN** their personal account is offered, and the picker offers nothing to switch to

#### Scenario: Obtaining an account is reachable from the picker

- **WHEN** a user opens the action beside the picker
- **THEN** they are told what a team account is and offered a way to buy one

#### Scenario: A suspended account is reachable by its owner

- **WHEN** the owner of a suspended team account opens the picker
- **THEN** it is listed and shown as suspended

#### Scenario: A suspended account is not selectable by a member

- **WHEN** a member who is not the owner attempts to select it
- **THEN** selection is refused and the reason is that the account is suspended, not that they lack permission

### Requirement: Navigation follows the selected account

The console's navigation SHALL reflect the capabilities of the selected account,
and SHALL change when the selection changes without requiring a reload.

Navigation SHALL list only the screens the selected account has. Where an account
kind lacks capabilities, the console SHALL offer a single affordance naming what
would unlock them, rather than listing the absent screens.

Within a screen, a control the caller merely lacks authority for SHALL remain
visible and disabled, stating why.

Where both refusals can occur, they SHALL NOT share wording: a refusal a grant
would lift and a refusal only a different kind of account would lift do not share
a remedy.

#### Scenario: A personal account's navigation names the remedy once

- **WHEN** a personal account is selected
- **THEN** the navigation offers its projects and a single way to obtain a team account, and does not list the screens a team account would have

#### Scenario: A control inside a screen is not removed

- **WHEN** a caller lacks the grant for a control on a screen the account does have
- **THEN** the control is shown, disabled, and states why

#### Scenario: The two refusals do not share wording

- **WHEN** a capability is unavailable because of the account kind
- **THEN** it does not say the caller lacks permission

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
