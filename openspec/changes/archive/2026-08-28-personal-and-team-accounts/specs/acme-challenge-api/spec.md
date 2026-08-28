## ADDED Requirements

### Requirement: Challenges are refused for an account that is not usable

The challenge API SHALL refuse to publish or withdraw for a domain whose account
is not usable, and SHALL make that determination after resolving the alias to its
account and before changing any state.

The refusal SHALL be distinguishable from an authentication failure: an API key
that is valid and permitted SHALL NOT be reported as unauthorized because the
account it belongs to is unfunded.

Refusal SHALL NOT remove challenge values already published; those SHALL be
withdrawn by the usual expiry.

#### Scenario: Publishing for a suspended account is refused

- **WHEN** a valid, permitted API key publishes a challenge for an account with no credit
- **THEN** the request is refused with a status meaning payment is required, and a message naming the reason

#### Scenario: The refusal does not implicate the key

- **WHEN** the refusal above is returned
- **THEN** it is not an authentication or authorization failure, and the key remains valid

#### Scenario: Nothing is published before the check

- **WHEN** the account is found to be unusable
- **THEN** no challenge value has been written and the DNS answer set is unchanged

#### Scenario: A personal account is never refused for funding

- **WHEN** a challenge is published for a domain in a personal account
- **THEN** it is accepted, because a personal account has no balance
