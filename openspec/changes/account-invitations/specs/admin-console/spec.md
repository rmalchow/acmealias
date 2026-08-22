## ADDED Requirements

### Requirement: The console manages an account's invitations

The console SHALL let a member of the account's unrestricted role invite a person by email address and
role, and revoke or re-send an outstanding invitation.

Outstanding invitations SHALL be shown to every member of the account alongside its members, reporting
the invited address, the role offered and when the invitation was issued. An invitation whose role no
longer exists SHALL be shown as invalid.

The console SHALL NOT offer these actions to a member who is not in the unrestricted role.

#### Scenario: An administrator invites somebody

- **WHEN** a member of the unrestricted role enters an address and chooses a role
- **THEN** an invitation is issued and appears among the account's outstanding invitations

#### Scenario: Every member sees who is pending

- **WHEN** a member holding no grants opens the account's members
- **THEN** outstanding invitations are listed alongside the members

#### Scenario: An invalid invitation is marked

- **WHEN** the role an outstanding invitation names has been deleted
- **THEN** the invitation is shown as invalid and re-sending is offered

#### Scenario: Other members are not offered the actions

- **WHEN** a member who is not in the unrestricted role opens the account's members
- **THEN** no action inviting, revoking or re-sending is offered, and the service refuses one if attempted directly

### Requirement: The console renders the invitation offer to a user who belongs to nothing

The console SHALL render the invitation offer for a session with no account selected, naming the account
and the role offered and requiring an explicit action to accept.

Where no user holds the invited address the console SHALL offer registration; where one does it SHALL
offer sign-in. Where the authenticated user is not the invited one, the console SHALL report that the
invitation is not for them and SHALL show the invited address only partially masked.

An expired or revoked invitation SHALL be reported as such, with no action offered other than to ask the
account for a new one.

#### Scenario: The offer states what is being joined

- **WHEN** an invited person opens the link
- **THEN** the account and the role offered are named, and accepting requires an explicit action

#### Scenario: A person without a user is offered registration

- **WHEN** an invited person with no user opens the link
- **THEN** registration is offered, and after it the offer is presented

#### Scenario: The wrong signed-in user is told so

- **WHEN** a user who is not the invited one opens the link while signed in
- **THEN** the console reports the invitation is not for them and shows the invited address partially masked

#### Scenario: The console continues into the joined account

- **WHEN** an invited person accepts
- **THEN** the console continues in the account they joined rather than returning to the empty state
