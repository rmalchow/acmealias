## RENAMED Requirements

- FROM: `### Requirement: A team account works only while it is funded`
- TO: `### Requirement: A team account works only while it is funded, where funding is real`

## MODIFIED Requirements

### Requirement: A team account works only while it is funded, where funding is real

Whether a balance gates anything SHALL be a property of the DEPLOYMENT, configured
once, and every rule below SHALL read it from one place.

Where the deployment charges: a team account SHALL be usable only while its credit
balance is greater than zero, and creating one SHALL require a payment.

Where the deployment charges nothing: a team account SHALL be usable whatever its
balance, SHALL be created without a payment, and no interface SHALL state a price,
a balance, or an offer to buy. A balance of zero is not a statement about a
customer who was never asked for money.

A personal account SHALL always be usable and SHALL have no balance, in either
case.

While a team account is not funded AND the deployment charges:

- a member other than the owner SHALL NOT be able to select it;
- its account-scoped management routes SHALL be refused;
- its owner SHALL still be able to select it, and SHALL be offered its balance and
  the means to add to it, and nothing else.

Suspension for non-payment and suspension by staff SHALL be separate state.
Adding credit SHALL NOT lift a staff suspension.

The stored balance, the record of top-ups and their audit entries SHALL be kept
whatever the deployment charges, so that the setting can be changed without
migrating anything.

#### Scenario: A member cannot select an unfunded account where money is real

- **WHEN** a member who is not the owner selects an unfunded team account, in a deployment that charges
- **THEN** selection is refused and the account is reported as suspended

#### Scenario: The owner can always reach the way back

- **WHEN** the owner selects an unfunded team account, in a deployment that charges
- **THEN** selection succeeds and the balance and the means to add to it are available

#### Scenario: The boundary is exactly zero, where money is real

- **WHEN** an account holds one credit, in a deployment that charges
- **THEN** it is usable
- **WHEN** it holds none
- **THEN** it is suspended

#### Scenario: A zero balance decides nothing where nothing is charged

- **WHEN** a team account holds no credit, in a deployment that charges nothing
- **THEN** it is usable, and no interface reports it as suspended

#### Scenario: A team account is created without a payment where nothing is charged

- **WHEN** a team account is created in a deployment that charges nothing
- **THEN** it is created and works, and no amount is required

#### Scenario: No price is stated where nothing is charged

- **WHEN** any interface would name a price, a balance or an offer to buy, in a deployment that charges nothing
- **THEN** it states none

#### Scenario: Paying does not lift a staff suspension

- **WHEN** credit is added to an account suspended by staff
- **THEN** the balance increases and the account remains suspended
