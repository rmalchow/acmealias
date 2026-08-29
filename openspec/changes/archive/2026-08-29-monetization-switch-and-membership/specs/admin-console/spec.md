## ADDED Requirements

### Requirement: The console states no price where the deployment charges nothing

Where the deployment charges nothing, the console SHALL NOT show a price, a
balance, an amount to choose, a warning about a balance running low, or a control
described as a purchase. The control that creates a team account SHALL describe
creating one.

Where the deployment charges, the console SHALL show all of these, and the two
presentations SHALL be the same screens rather than separate ones, so that
changing the setting is the whole of the change.

#### Scenario: No price is offered where nothing is charged

- **WHEN** a person opens the screen that creates a team account, in a deployment that charges nothing
- **THEN** it offers to create one, names no amount, and states no balance

#### Scenario: No balance warning where nothing is charged

- **WHEN** a team account holds a low or zero balance, in a deployment that charges nothing
- **THEN** no warning about it is shown anywhere in the console

### Requirement: The member roster identifies people by name and address

The console SHALL identify each member of an account by their display name and the
address they authenticate with, and SHALL NOT require an administrator to read an
identity-provider subject to tell two people apart. Where no address is recorded
for a member, the name SHALL be shown alone.

The roster SHALL allow a member's role to be changed, and SHALL present the
control for the reader's own membership as unavailable with the reason, rather
than omitting it.

#### Scenario: A member is identified by name and address

- **WHEN** the roster is rendered for a member whose address is recorded
- **THEN** it shows their name and their address

#### Scenario: A member without a recorded address

- **WHEN** the roster is rendered for a member whose address is not recorded
- **THEN** it shows their name alone

#### Scenario: The reader's own role is not editable

- **WHEN** the roster is rendered
- **THEN** the control that changes the reader's own role is present, unavailable, and states why
