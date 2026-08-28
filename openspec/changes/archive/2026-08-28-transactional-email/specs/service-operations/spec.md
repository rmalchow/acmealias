## ADDED Requirements

### Requirement: Every message the deployment sends comes from one identity

All transactional mail, whoever sends it, SHALL carry the same sender address and
the same sender display name.

Every host that sends on the domain's behalf SHALL be authorised by the domain's
published sender policy and SHALL sign its messages with a published key, and
that authentication SHALL align with the sender address a recipient sees.

Messages SHALL carry a reply address that reaches a person.

#### Scenario: Two senders, one correspondent

- **WHEN** the identity provider and the service each send a message
- **THEN** both carry the same sender address and display name

#### Scenario: Authentication aligns with what the reader sees

- **WHEN** a message is sent by either sender
- **THEN** it authenticates as the same domain that appears as its sender

#### Scenario: A reply reaches somebody

- **WHEN** a recipient replies to any message
- **THEN** the reply is delivered to an address that is read

### Requirement: The agreement between senders is asserted, not assumed

The deployment SHALL be able to demonstrate, by test, that both senders produce
the same envelope and the same footer.

#### Scenario: Both senders are compared

- **WHEN** a message is produced by each sender
- **THEN** their sender identity and footer are shown to match

### Requirement: Subjects name what the message is about

A subject SHALL begin with the thing the message concerns. It SHALL NOT carry a
constant prefix identifying the sender, which the sender field already reports.

#### Scenario: A subject is useful in a list

- **WHEN** a recipient sees a message in a narrow list or a notification
- **THEN** the visible beginning of the subject identifies what it concerns
