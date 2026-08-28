## ADDED Requirements

### Requirement: A project is configured on one screen

The console SHALL present a project's nodes, domains, plan, keys and interactions on a single screen.

#### Scenario: One step from the list to the configuration

- **WHEN** a person opens a project from the project list
- **THEN** they reach its configuration without passing through an intermediate screen

### Requirement: The project list reports each project's size

The console SHALL show, for each project in the list, how many nodes and domains it holds, and SHALL
describe a project with neither as empty.

#### Scenario: The list is informative without being opened

- **WHEN** a person looks at their project list
- **THEN** each row says how many nodes and domains that project holds
