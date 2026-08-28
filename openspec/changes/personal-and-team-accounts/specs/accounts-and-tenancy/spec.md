## ADDED Requirements

### Requirement: An account is either personal or team, and neither becomes the other

Every account SHALL have a kind, fixed at creation, of either `personal` or
`team`. The service SHALL refuse any change of kind after creation.

A user SHALL own at most one personal account, and any number of team accounts.
The limit SHALL be enforced by the datastore rather than only by the service.

A personal account SHALL be free, SHALL hold no members other than its owner, and
SHALL NOT be transferable. A team account SHALL be created only by funding it,
MAY hold other members, and MAY be transferred to another user.

#### Scenario: A second personal account is refused

- **WHEN** a user who already owns a personal account creates another
- **THEN** the creation is refused and the existing account is unaffected

#### Scenario: Team accounts are not limited

- **WHEN** a user who owns a personal account and several team accounts funds another
- **THEN** it is created

#### Scenario: A personal account cannot be transferred

- **WHEN** a transfer of a personal account is requested
- **THEN** it is refused, and the owner is unchanged

#### Scenario: A transfer never produces a second personal account

- **WHEN** a transfer would leave the recipient owning two personal accounts
- **THEN** it is refused

#### Scenario: Kind is immutable

- **WHEN** a request attempts to change an account's kind
- **THEN** it is refused

### Requirement: The account's owner administers it

The owner of a team account SHALL be a member of the account's unrestricted role
at all times. The service SHALL refuse to remove the owner from that role, and
SHALL establish the recipient's unrestricted membership as part of every
transfer.

An account SHALL also keep at least one member of its unrestricted role. The two
refusals SHALL be distinguishable to the caller, because their remedies differ:
one is resolved by appointing another unrestricted member, the other only by
transferring the account.

#### Scenario: The owner cannot be removed from the unrestricted role

- **WHEN** a removal of the owner's unrestricted membership is requested, whether
  or not the account has other unrestricted members
- **THEN** it is refused, and the refusal names ownership rather than the count

#### Scenario: A transfer leaves the recipient able to administer the account

- **WHEN** a team account is transferred to somebody who is already a member of it
  holding a restricted role, or no role
- **THEN** the recipient also holds the unrestricted role once the transfer completes

#### Scenario: The last unrestricted member cannot be removed

- **WHEN** a removal would leave the unrestricted role with no members
- **THEN** it is refused, and the refusal names the count rather than ownership

### Requirement: A team account works only while it is funded

A team account SHALL be usable only while its credit balance is greater than
zero. A personal account SHALL always be usable and SHALL have no balance.

While a team account is not funded:

- a member other than the owner SHALL NOT be able to select it;
- its account-scoped management routes SHALL be refused;
- its owner SHALL still be able to select it, and SHALL be offered its balance and
  the means to add to it, and nothing else.

Suspension for non-payment and suspension by staff SHALL be separate state.
Adding credit SHALL NOT lift a staff suspension.

#### Scenario: A member cannot select an unfunded account

- **WHEN** a member who is not the owner selects an unfunded team account
- **THEN** selection is refused and the account is reported as suspended

#### Scenario: The owner can always reach the way back

- **WHEN** the owner selects an unfunded team account
- **THEN** selection succeeds and the balance and the means to add to it are available

#### Scenario: The boundary is exactly zero

- **WHEN** an account holds one credit
- **THEN** it is usable
- **WHEN** it holds none
- **THEN** it is suspended

#### Scenario: Paying does not lift a staff suspension

- **WHEN** credit is added to an account suspended by staff
- **THEN** the balance increases and the account remains suspended

### Requirement: A personal account is limited by a project quota; a team account is limited by its balance

A personal account SHALL hold no more than a configured number of projects. The
limit SHALL be configuration rather than a compiled-in constant.

A team account SHALL NOT be subject to a project quota.

Exceeding the quota SHALL be refused as a lack of room rather than as a lack of
authority, and the refusal SHALL state what would resolve it.

#### Scenario: A personal account fills up

- **WHEN** a personal account at its limit creates another project
- **THEN** it is refused, and the refusal says there is no room rather than that the caller may not

#### Scenario: The refusal is not a permission error

- **WHEN** the refusal above is returned to a caller who holds every grant
- **THEN** it is still refused, and still on grounds of room

#### Scenario: A team account has no project limit

- **WHEN** a funded team account creates projects beyond the personal limit
- **THEN** they are created

### Requirement: Credit is added through a checkout that records a real charge

The service SHALL provide a checkout that credits an account by an amount chosen
from a fixed set. An amount supplied by the caller outside that set SHALL be
refused.

A credit SHALL be recorded in the audit trail, attributed to the user who
performed it, naming the account and the amount.

Creating a team account SHALL happen in the same transaction as its first credit,
so that no team account exists that has never been funded.

#### Scenario: Only offered amounts are accepted

- **WHEN** a checkout requests an amount outside the offered set
- **THEN** it is refused and no credit is recorded

#### Scenario: A credit is auditable

- **WHEN** an account is credited
- **THEN** the audit trail records who did it, which account, and how much

#### Scenario: No unfunded team account is created

- **WHEN** creating a team account fails at any point before its first credit is recorded
- **THEN** no account exists
