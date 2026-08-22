## MODIFIED Requirements

### Requirement: Wildcard domains are supported and constrained

The service SHALL accept a domain name whose leftmost label is `*`. It SHALL reject names with
more than one wildcard label, with a wildcard label that is not leftmost, and wildcards whose
remainder is a public suffix.

Creating a specific host that an existing wildcard already covers SHALL be permitted and SHALL
be reported as a warning.

A domain creation request MAY ask for the submitted name's counterpart to be created with it:
the wildcard of a name that is not one, or the base name of a name that is. The service SHALL
create both domains in one action, referencing the one alias their shared challenge name
resolves to, and SHALL record both creations as separate entries in the audit trail.

Both names SHALL be validated, and every conflict SHALL be detected, before either domain is
created. Where either name is refused, the service SHALL refuse the whole request, name the
refused name and the rule it broke, and create neither domain. The two domains SHALL be
persisted in one transaction, so a caller SHALL NOT observe one of them without the other.

The request SHALL create only the submitted name unless the counterpart is asked for
explicitly. Its absence SHALL NOT be read as a request for it.

#### Scenario: Wildcard is accepted

- **WHEN** `*.example.com` is created
- **THEN** it is stored as a domain of its load balancer

#### Scenario: Malformed wildcards are rejected

- **WHEN** `*.*.example.com`, `foo.*.example.com`, or `*` is created
- **THEN** the request is refused with a validation error

#### Scenario: Wildcard at a public suffix is rejected

- **WHEN** `*.co.uk` is created
- **THEN** the request is refused with a validation error

#### Scenario: Redundant specific host warns

- **WHEN** `shop.example.com` is created while `*.example.com` exists on the same load balancer
- **THEN** the domain is created and the response includes a warning that the wildcard already covers it

#### Scenario: One action creates the apex and its wildcard against one alias

- **WHEN** `example.com` is created with its counterpart requested
- **THEN** `example.com` and `*.example.com` both exist on that load balancer, both reference the single alias for `_acme-challenge.example.com`, and the audit trail records both

#### Scenario: A wildcard's counterpart is its base name

- **WHEN** `*.example.com` is created with its counterpart requested
- **THEN** `*.example.com` and `example.com` both exist, and no second wildcard was created

#### Scenario: A refused counterpart refuses the whole request

- **WHEN** a name is created with its counterpart requested and the counterpart breaks a rule the submitted name does not, such as a wildcard at a public suffix or an over-length name
- **THEN** the request is refused with an error naming the counterpart and the rule, and neither domain exists

#### Scenario: A conflicting counterpart refuses the whole request

- **WHEN** a name is created with its counterpart requested while the counterpart's challenge name is already claimed by another load balancer
- **THEN** the request is refused with an error naming that load balancer, and neither domain exists

#### Scenario: The counterpart is created only when requested

- **WHEN** `example.com` is created without asking for its counterpart
- **THEN** only `example.com` exists and no wildcard domain was created

### Requirement: The inventory is administrable without the console

The service SHALL provide command-line subcommands to create and list projects, load balancer
lbs, nodes and domains, to render a load balancer's DNS plan, and to create API keys,
usable against the configured datastore without the server process running.

Domain creation from the command line SHALL offer the same counterpart creation as the
management interface, defaulting to creating only the name given.

#### Scenario: Operator adds a domain from the command line

- **WHEN** an operator runs the domain creation subcommand for a load balancer
- **THEN** the domain and its alias are persisted
- **AND** the command prints the CNAME record that must be published

#### Scenario: Operator adds a pair from the command line

- **WHEN** an operator runs the domain creation subcommand for `example.com` asking for its counterpart
- **THEN** both domains are persisted against one alias, and one CNAME record is printed
