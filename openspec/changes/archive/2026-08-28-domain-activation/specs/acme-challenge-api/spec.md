## ADDED Requirements

### Requirement: A challenge is refused for a domain whose delegation has never been seen

The service SHALL refuse to publish a challenge value for a domain that has never
been activated, and SHALL determine this before publishing anything.

The refusal SHALL be distinguishable from a refusal caused by the account's
state and from one caused by the caller's permissions, and SHALL name the
delegation the domain requires.

The determination SHALL NOT require a name resolution on the request path.

#### Scenario: An unactivated domain is refused

- **WHEN** a challenge is published for a domain whose delegation has never been seen
- **THEN** it is refused, and nothing is published

#### Scenario: The refusal says which problem it is

- **WHEN** the refusal above is returned
- **THEN** it is distinguishable from an unfunded account, a suspended account and a permission failure

#### Scenario: The request path performs no lookup

- **WHEN** a challenge is published
- **THEN** the decision uses stored state and performs no name resolution
