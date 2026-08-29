## ADDED Requirements

### Requirement: A member's role may be changed, and never by that member

The service SHALL allow the role of an existing membership to be changed in place,
not only assigned to a membership that holds none. Changing a role SHALL preserve
the membership rather than requiring it to be removed and recreated.

The service SHALL refuse a change that would leave the account's unrestricted role
with no member, and SHALL refuse one that would move the account's owner out of
it, naming the transfer as the remedy in the second case. These are the refusals
that already govern removal, and they SHALL be enforced against concurrent
changes, since demoting the last administrator and removing them leave the
identical account behind.

The service SHALL refuse a change to the role of the membership belonging to the
person making the request. The authority to change roles is membership of the
unrestricted role, so a request to alter one's own membership is a request to
alter the source of that authority. The refusal SHALL be by PERSON and not by
membership row, since one person may hold several memberships in one account. The
account's owner SHALL NOT be exempt.

Pointing a membership at the role it already holds SHALL succeed and change
nothing.

#### Scenario: A role is changed without removing the member

- **WHEN** an administrator changes another member's role
- **THEN** the membership keeps its identity and confers what the new role confers

#### Scenario: The last administrator cannot be demoted

- **WHEN** a change would leave the unrestricted role with no member
- **THEN** it is refused

#### Scenario: The owner cannot be moved out of the unrestricted role

- **WHEN** a change would move the account's owner out of the unrestricted role
- **THEN** it is refused, and the refusal names transferring the account

#### Scenario: Nobody changes their own role

- **WHEN** an administrator requests a change to a membership of their own
- **THEN** it is refused, whether or not they own the account

#### Scenario: Setting the role already held is not an error

- **WHEN** a membership is pointed at the role it already holds
- **THEN** the request succeeds and nothing changes

### Requirement: The address a person authenticates with is recorded

The service SHALL record the email address the identity provider reports for a
person, and SHALL keep it current as the provider reports changes. It SHALL NOT
erase a recorded address because a later caller had none to offer.

The service SHALL NOT require the provider to assert that it has verified the
address. What makes the address trustworthy SHALL be that the identity provider's
own enrolment confirms it before the account can be used; where that ceases to be
true — a federated source whose addresses this deployment never confirmed — the
service SHALL obtain the assurance from the provider rather than from the claim.

An invitation is bound to an address, so this record is what decides whether an
invited person is shown their invitation.

#### Scenario: The address is recorded at sign-in

- **WHEN** a person signs in and the provider reports an address
- **THEN** it is recorded against that person

#### Scenario: An absent address does not erase a recorded one

- **WHEN** an operation that knows no address acts for a person who has one recorded
- **THEN** the recorded address is unchanged

#### Scenario: An invited person is shown their invitation

- **WHEN** a person whose recorded address matches an outstanding invitation signs in
- **THEN** that invitation is offered to them
