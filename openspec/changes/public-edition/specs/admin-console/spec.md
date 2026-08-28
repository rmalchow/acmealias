## ADDED Requirements

### Requirement: A person arriving for the first time is led to a working domain

The console SHALL lead a person who has just signed in for the first time, and whose account contains
nothing, to a first domain whose delegation has been seen, without them contacting anyone and without
requiring them to understand the inventory hierarchy before they begin.

The path SHALL have a stated end, and reaching it SHALL be reported.

#### Scenario: An empty account offers a way to begin

- **WHEN** a person signs in and their account contains no project, load balancer or domain
- **THEN** the console offers a way to add a first domain rather than an empty list

#### Scenario: First run ends at a working domain

- **WHEN** the first domain's delegation has been seen
- **THEN** the console reports that first run is complete and what the service will now do

#### Scenario: No certificate is promised

- **WHEN** first run completes
- **THEN** nothing in it states or implies that a certificate has been issued or installed

### Requirement: An account can see what it has bought

The console SHALL show an account its purchase history — what was bought, when, for how much, and the
provider's reference for the payment — and SHALL show it only to members of that account.

The console SHALL NOT offer any control that sets a balance directly.

#### Scenario: Purchases are visible to a member

- **WHEN** a member of an account opens the billing screen
- **THEN** that account's purchases are listed with their amounts, dates and provider references

#### Scenario: Billing state is confined to the account

- **WHEN** a member of one account views its purchases
- **THEN** nothing of another account's purchases is shown

#### Scenario: No control sets a balance

- **WHEN** the billing screen is inspected for a control that sets a balance
- **THEN** no such control is offered, and buying credits is the only way within the account to raise it
