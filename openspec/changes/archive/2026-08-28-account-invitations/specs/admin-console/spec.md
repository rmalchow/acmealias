## ADDED Requirements

### Requirement: The console manages an account's invitations

The console SHALL let a member of the account's unrestricted role — its owner and its administrators —
invite a person by email address, and revoke or re-send an outstanding invitation. It SHALL NOT ask for
a role when inviting.

Outstanding invitations SHALL be shown to every member of the account alongside its members, reporting
the invited address and when the invitation was issued.

A member who holds no role SHALL be listed as a member, marked as having none, and the console SHALL
offer assigning one to a member of the unrestricted role.

The console SHALL NOT offer these actions to a member who is not in the unrestricted role.

#### Scenario: An administrator invites somebody

- **WHEN** a member of the unrestricted role enters an address
- **THEN** an invitation is issued and appears among the account's outstanding invitations

#### Scenario: Inviting does not ask for a role

- **WHEN** a member of the unrestricted role opens the invitation form
- **THEN** it asks for an address and offers no choice of role

#### Scenario: Every member sees who is pending

- **WHEN** a member holding no grants opens the account's members
- **THEN** outstanding invitations are listed alongside the members

#### Scenario: A member with no role is visible as one

- **WHEN** a person who has accepted an invitation has not yet been given a role
- **THEN** they are listed among the members, marked as holding no role, and a role can be assigned to them

#### Scenario: Other members are not offered the actions

- **WHEN** a member who is not in the unrestricted role opens the account's members
- **THEN** no action inviting, revoking or re-sending is offered, and the service refuses one if attempted directly

### Requirement: The console renders the invitation offer to a user who belongs to nothing

The console SHALL render the invitation offer for a session with no account selected, naming the account
and requiring an explicit action to accept. It SHALL NOT name a role, because the invitation offers
none.

Where no user holds the invited address the console SHALL offer registration; where one does it SHALL
offer sign-in. Where the authenticated user is not the invited one, the console SHALL report that the
invitation is not for them and SHALL show the invited address only partially masked.

An expired or revoked invitation SHALL be reported as such, with no action offered other than to ask the
account for a new one.

#### Scenario: The offer states what is being joined

- **WHEN** an invited person opens the link
- **THEN** the account is named, and accepting requires an explicit action

#### Scenario: A person without a user is offered registration

- **WHEN** an invited person with no user opens the link
- **THEN** registration is offered, and after it the offer is presented

#### Scenario: The wrong signed-in user is told so

- **WHEN** a user who is not the invited one opens the link while signed in
- **THEN** the console reports the invitation is not for them and shows the invited address partially masked

#### Scenario: The console continues into the joined account

- **WHEN** an invited person accepts
- **THEN** the console continues in the account they joined rather than returning to the empty state

### Requirement: An account a member holds no role in explains itself

The console SHALL, for a member of an account who holds no role in it, report on every account-scoped
screen that they hold no role and that an administrator must assign one, rather than presenting the
screen's ordinary empty state.

The account SHALL remain selectable by such a member.

#### Scenario: An empty account is not mistaken for an empty product

- **WHEN** a member holding no role opens a screen listing the account's inventory
- **THEN** the console reports that they hold no role in the account and that an administrator must assign one

#### Scenario: Selection still works

- **WHEN** a member holding no role selects the account
- **THEN** the selection succeeds

#### Scenario: Being given a role restores the ordinary screens

- **WHEN** a member holding no role is assigned one
- **THEN** the account-scoped screens render what that role permits
