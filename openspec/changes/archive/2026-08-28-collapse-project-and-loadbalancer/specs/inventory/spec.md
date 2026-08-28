## ADDED Requirements

### Requirement: A project holds the configuration directly

A project SHALL hold its nodes, domains, aliases and API keys directly. There SHALL be no
intermediate object between an account's project and the configuration within it.

#### Scenario: A new project is immediately usable

- **WHEN** a person creates a project
- **THEN** they can add a node and a domain to it without creating anything else first

#### Scenario: The configuration belongs to the project

- **WHEN** a project is deleted
- **THEN** its nodes, domains, aliases and keys are deleted with it

### Requirement: A project is listed with what it contains

A listing of projects SHALL report, for each project, how many nodes and how many domains it holds.

A project holding neither SHALL be reported as empty rather than as two zeroes.

#### Scenario: A list says how big each project is

- **WHEN** a person lists their projects
- **THEN** each is shown with its number of nodes and its number of domains

#### Scenario: An empty project says so

- **WHEN** a project holds no nodes and no domains
- **THEN** it is described as empty

#### Scenario: The counts do not cost a query each

- **WHEN** a listing of many projects is produced
- **THEN** the number of queries does not grow with the number of projects
