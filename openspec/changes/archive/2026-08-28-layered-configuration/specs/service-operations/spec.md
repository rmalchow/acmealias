## ADDED Requirements

### Requirement: Configuration is layered, and the environment has the last word

The service SHALL read its configuration from an optional base file and any
number of optional overlay files, applied in a defined order, with environment
variables applied last.

Structures SHALL be merged key by key. A list SHALL replace the list it overlays
rather than being appended to it.

A missing overlay file SHALL NOT prevent start-up. A base file that was
explicitly configured and cannot be read SHALL prevent it.

The service SHALL report, at start-up, which layers it applied and which
configured files were absent.

#### Scenario: An overlay states only its differences

- **WHEN** an overlay sets one value of many
- **THEN** that value is used and every other value comes from beneath it

#### Scenario: A list is replaced, not merged

- **WHEN** a base file lists two values and an overlay lists one
- **THEN** the effective configuration holds exactly the overlay's one

#### Scenario: The environment wins

- **WHEN** a value is set both in a file and in the environment
- **THEN** the environment's value is used

#### Scenario: An absent overlay is not an error

- **WHEN** a configured overlay file does not exist
- **THEN** the service starts and reports the file as absent

#### Scenario: An absent configured base file is an error

- **WHEN** a base file is explicitly configured and cannot be read
- **THEN** the service does not start

### Requirement: Configuration files may refer to the environment with a fallback

The service SHALL resolve placeholders in configuration files. A placeholder
names an environment variable and MAY supply a default, which SHALL be used when
that variable is unset.

Placeholders SHALL be resolved before the document is interpreted, so that a
resolved value keeps the type the document gives it.

A means of escaping the placeholder syntax SHALL exist, so that a literal value
resembling a placeholder can be expressed.

#### Scenario: A placeholder falls back to its default

- **WHEN** a placeholder names an unset variable and supplies a default
- **THEN** the default is used

#### Scenario: A resolved value keeps its type

- **WHEN** a placeholder supplying a number is resolved into a numeric field
- **THEN** the value is a number and not a string

#### Scenario: A secret has no default

- **WHEN** a placeholder naming a secret has no default and the variable is unset
- **THEN** the value is empty and the service refuses to start rather than running with it

#### Scenario: The syntax can be escaped

- **WHEN** a value contains an escaped placeholder
- **THEN** it resolves to the literal text rather than to a variable
