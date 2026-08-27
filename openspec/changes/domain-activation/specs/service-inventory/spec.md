## ADDED Requirements

### Requirement: A domain is offline until its delegation has been seen, and then stays online

A domain SHALL begin offline when added, and SHALL become active the first time a
check observes its delegation.

The service SHALL check domains that have never been activated repeatedly, and
SHALL NOT stop checking them.

An active domain SHALL be checked occasionally thereafter, and a failed check
SHALL NOT return it to being offline.

Where two domains share one delegation, an observation of that delegation SHALL
activate both.

#### Scenario: A new domain begins offline

- **WHEN** a domain is added
- **THEN** it is not active

#### Scenario: Seeing the delegation activates it

- **WHEN** a check observes the delegation of an inactive domain
- **THEN** it becomes active

#### Scenario: A domain is not given up on

- **WHEN** a domain's delegation has been absent for a long time
- **THEN** it continues to be checked

#### Scenario: Activation survives a failed check

- **WHEN** an active domain's delegation cannot be observed
- **THEN** it remains active

#### Scenario: A shared delegation activates both domains

- **WHEN** a name and its wildcard share one delegation and it is observed
- **THEN** both become active
