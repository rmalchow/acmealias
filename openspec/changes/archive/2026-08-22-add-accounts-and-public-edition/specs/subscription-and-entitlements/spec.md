## ADDED Requirements

### Requirement: Each account has a plan that bounds what it may hold

The service SHALL associate every account with a plan, and SHALL enforce that plan's quotas when
an object is created. A creation that would exceed a quota SHALL be refused with a reason naming
the quota reached.

Quotas SHALL cover the number of domains, load balancers and API keys an account may hold,
the number of challenge values it may have published at one time, and the number of publications
it may make within a billing period.

#### Scenario: A quota refuses the creation that would exceed it

- **WHEN** an account at its domain quota adds another domain
- **THEN** the request is refused and the response names the quota

#### Scenario: Existing objects are unaffected by a quota

- **WHEN** an account is over a quota because its plan changed
- **THEN** existing objects continue to work and only further creations are refused

### Requirement: Concurrency and rate quotas protect the shared service

The limits on simultaneously published challenge values and on publications per period SHALL
apply to every account in every edition, independently of whether the account is charged.

#### Scenario: One account cannot exhaust the shared zone

- **WHEN** an account reaches its limit of simultaneously published challenge values
- **THEN** further publications by that account are refused and other accounts are unaffected

#### Scenario: A runaway client is bounded

- **WHEN** an account exceeds its publications-per-period limit
- **THEN** further publications are refused until the period rolls over

### Requirement: Subscription state is derived from the payment provider, never entered by hand

In the public edition the service SHALL determine an account's plan and its suspension state from
the payment provider's reported subscription, applied through provider notifications. The service
SHALL NOT offer an interface for setting a subscription state directly.

#### Scenario: A completed payment activates the plan

- **WHEN** the provider reports a subscription as active
- **THEN** the account holds the plan of that subscription

#### Scenario: A notification that cannot be trusted is ignored

- **WHEN** a provider notification fails verification
- **THEN** no subscription state changes and the attempt is recorded

### Requirement: Suspension does not break certificate renewal without warning

A suspended account SHALL continue to answer published challenges for a configured grace period,
and the service SHALL notify the account before answering stops.

#### Scenario: Renewals survive a lapsed payment briefly

- **WHEN** an account is suspended for non-payment and a certificate renewal is attempted within the grace period
- **THEN** the challenge is answered

#### Scenario: The end of the grace period is announced first

- **WHEN** an account's grace period is due to end
- **THEN** the account has been notified before challenges stop being answered

### Requirement: The internal edition is unmetered and unbilled

In the internal edition every account SHALL hold an unlimited plan for the product quotas, SHALL
NOT be suspendable for payment, and SHALL NOT expose any billing interface.

#### Scenario: No billing surface internally

- **WHEN** the internal edition is running
- **THEN** no screen, route or notification concerns payment
