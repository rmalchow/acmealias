## ADDED Requirements

### Requirement: The invitation lifecycle is recorded

The service SHALL record the issuing, revocation, re-sending, acceptance and expiry of an invitation as
distinct actions, each naming the account, the invited address and the role offered.

An entry for an acceptance SHALL name both the invited address and the accepting user, so that the record
evidences that the person who joined is the person who was invited.

An entry recording an expiry SHALL identify the service itself as the actor.

#### Scenario: Issuing is attributable

- **WHEN** an invitation is issued
- **THEN** an entry names the issuing user, the invited address and the role offered

#### Scenario: Acceptance evidences who joined

- **WHEN** an invitation is accepted
- **THEN** an entry names both the invited address and the user that accepted

#### Scenario: Expiry is not attributed to a person

- **WHEN** an invitation expires
- **THEN** an entry records it with the service as the actor

#### Scenario: Revocation and re-sending are distinguishable

- **WHEN** an invitation is revoked, and another is re-sent
- **THEN** the two entries record different actions

### Requirement: An invitation entry never contains its token

The service SHALL exclude an invitation's token, and any stored form of it, from every audit entry.

#### Scenario: The token is absent from the record

- **WHEN** any entry describing an invitation is read
- **THEN** it contains neither the token nor a stored form of it

### Requirement: Refused invitation issues are recorded

The service SHALL record an invitation refused for exceeding a rate limit, identifying the acting user
and that a limit was reached.

#### Scenario: A rate-limited attempt leaves a trace

- **WHEN** an invitation is refused because a rate limit was reached
- **THEN** the refusal is recorded with the acting user, and no invitation is created
