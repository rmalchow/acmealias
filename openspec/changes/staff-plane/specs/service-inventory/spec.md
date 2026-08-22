## MODIFIED Requirements

### Requirement: The inventory is administrable without the console

The service SHALL provide command-line subcommands to create and list projects, load balancer
lbs, nodes and domains, to render a load balancer's DNS plan, and to create API keys,
usable against the configured datastore without the server process running.

Domain creation from the command line SHALL offer the same counterpart creation as the
management interface, defaulting to creating only the name given.

These subcommands SHALL be documented as a bootstrap and emergency surface, for use before any
authenticated surface is reachable and while the service is unavailable, and SHALL NOT be presented
as an ordinary way to administer inventory.

The command line SHALL NOT authenticate a subject, and the authority behind a command-line change
SHALL be recorded as such. A command-line change to an account's inventory SHALL be recorded as
visible to that account.

#### Scenario: Operator adds a domain from the command line

- **WHEN** an operator runs the domain creation subcommand for a load balancer
- **THEN** the domain and its alias are persisted
- **AND** the command prints the CNAME record that must be published

#### Scenario: Operator adds a pair from the command line

- **WHEN** an operator runs the domain creation subcommand for `example.com` asking for its counterpart
- **THEN** both domains are persisted against one alias, and one CNAME record is printed

#### Scenario: The command line works with the service stopped

- **WHEN** the server process is not running and the datastore is reachable
- **THEN** the subcommands operate

#### Scenario: A command-line change is not invisible to the account

- **WHEN** an operator changes an account's inventory from the command line
- **THEN** that account's audit trail records the change
