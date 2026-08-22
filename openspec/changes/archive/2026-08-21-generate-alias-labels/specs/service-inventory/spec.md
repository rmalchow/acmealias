## MODIFIED Requirements

### Requirement: An alias is one label in the served zone per challenge name

The service SHALL store an alias for each distinct challenge name, consisting of a single label
within the served zone. Challenge names and alias labels SHALL each be unique across the
service. Several domains MAY share one alias.

An alias label SHALL be a single DNS label of lowercase letters, digits and hyphens, not
beginning or ending with a hyphen, and at most 63 octets.

The service SHALL determine the label of a new alias itself. It SHALL NOT accept a label from a
caller of the management interface, the command line, or any other entry point.

By default the label SHALL be randomly generated with at least 80 bits of entropy and SHALL NOT
be derivable from the challenge name, the domain name, or any other value visible outside the
service. A configuration setting SHALL make the service derive the label from the challenge name
instead, for a deployment that prefers a self-describing label.

The setting SHALL govern only aliases created after it takes effect. The label of an existing
alias SHALL NOT change, whatever the setting, because it is published in a record the service
does not control.

Where a candidate label is already in use, the service SHALL resolve the collision and complete
the request rather than refuse it.

#### Scenario: Shared alias for wildcard and apex

- **WHEN** `example.com` and `*.example.com` are added to the same load balancer
- **THEN** exactly one alias exists for `_acme-challenge.example.com` and both domains reference it

#### Scenario: A generated label does not disclose its domain

- **WHEN** a domain is added with the default labelling
- **THEN** its alias label is a valid label that cannot be computed from the domain name by anyone outside the service

#### Scenario: A supplied label is not honoured

- **WHEN** a domain creation request carries an alias label
- **THEN** the request is refused as carrying an unknown field, and no alias bears that label

#### Scenario: The setting restores derived labels

- **WHEN** the setting to derive labels from the challenge name is enabled and `demo.example.com` is added
- **THEN** its alias label is derived from `_acme-challenge.demo.example.com` and is self-describing

#### Scenario: An existing alias keeps its label when the setting changes

- **WHEN** the labelling setting is changed and the service restarts
- **THEN** every alias created before the change keeps the label it already published, and only aliases created afterwards use the new labelling

#### Scenario: A colliding label is resolved, not refused

- **WHEN** the label the service would use for a new alias is already in use
- **THEN** the service uses another valid label and the domain is created

#### Scenario: Invalid label is rejected

- **WHEN** a label containing a dot, an underscore, a leading hyphen, or more than 63 octets would be stored as an alias label
- **THEN** it is refused with a validation error
