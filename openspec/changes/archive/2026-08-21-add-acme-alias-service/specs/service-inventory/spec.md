## ADDED Requirements

### Requirement: The inventory is projects, load balancers and domains

The service SHALL store projects; each project SHALL own zero or more load balancers; each
load balancer SHALL own zero or more nodes with their addresses and zero or more domains.
Each load balancer SHALL belong to exactly one project, and each domain SHALL belong to
exactly one load balancer.

Deleting a project or a load balancer that still owns children SHALL be refused.

#### Scenario: Domain is reachable through the hierarchy

- **WHEN** a domain is created under a load balancer of a project
- **THEN** the domain is listed for that load balancer, and the load balancer is listed for that project

#### Scenario: Non-empty parent is not deleted

- **WHEN** deletion is requested for a project that still has load balancers
- **THEN** the request is refused and nothing is deleted

### Requirement: Domain names are normalized and validated

The service SHALL store domain names normalized to lowercase without a trailing dot, and SHALL
reject names that are empty, over-length, or contain invalid labels. Domain names SHALL be
unique across the inventory.

#### Scenario: Name is normalized on creation

- **WHEN** a domain is created as `Shop.Example.COM.`
- **THEN** it is stored as `shop.example.com`

#### Scenario: Duplicate domain is refused

- **WHEN** a domain name already present in the inventory is created again
- **THEN** the request is refused with a conflict error naming the existing owner

### Requirement: Wildcard domains are supported and constrained

The service SHALL accept a domain name whose leftmost label is `*`. It SHALL reject names with
more than one wildcard label, with a wildcard label that is not leftmost, and wildcards whose
remainder is a public suffix.

Creating a specific host that an existing wildcard already covers SHALL be permitted and SHALL
be reported as a warning.

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

### Requirement: Challenge names are derived from domain names

For each domain the service SHALL derive its challenge name by removing a leading wildcard
label and prefixing `_acme-challenge.`. A wildcard domain and its base domain SHALL derive the
same challenge name.

#### Scenario: Wildcard and apex share a challenge name

- **WHEN** `example.com` and `*.example.com` both exist
- **THEN** both derive the challenge name `_acme-challenge.example.com`

#### Scenario: Specific host derives its own challenge name

- **WHEN** `shop.example.com` exists
- **THEN** it derives the challenge name `_acme-challenge.shop.example.com`

### Requirement: An alias is one label in the served zone per challenge name

The service SHALL store an alias for each distinct challenge name, consisting of a single label
within the served zone. Challenge names and alias labels SHALL each be unique across the
service. Several domains MAY share one alias.

An alias label SHALL be a single DNS label of lowercase letters, digits and hyphens, not
beginning or ending with a hyphen, and at most 63 octets.

The service SHALL propose a label derived from the challenge name when an alias is created, and
SHALL allow the operator to replace it.

#### Scenario: Shared alias for wildcard and apex

- **WHEN** `example.com` and `*.example.com` are added to the same load balancer
- **THEN** exactly one alias exists for `_acme-challenge.example.com` and both domains reference it

#### Scenario: Invalid label is rejected

- **WHEN** an alias label containing a dot, an underscore, a leading hyphen, or more than 63 octets is supplied
- **THEN** the request is refused with a validation error

#### Scenario: Duplicate label is refused

- **WHEN** an alias label already in use is supplied for another challenge name
- **THEN** the request is refused with a conflict error

### Requirement: An alias belongs to exactly one load balancer

The service SHALL refuse to create a domain whose derived challenge name already has an alias
owned by a different load balancer, and SHALL name the conflicting load balancer in the error.

#### Scenario: Conflicting ownership is refused

- **WHEN** `*.example.com` is added to one load balancer while `example.com` on another load balancer already owns the alias for `_acme-challenge.example.com`
- **THEN** the request is refused with an error naming the owning load balancer

### Requirement: Removing a domain revokes its challenge writes

When the last domain referencing an alias is removed, the service SHALL remove the alias and
its active challenge records.

#### Scenario: Writes stop after removal

- **WHEN** the last domain for an alias is removed and a challenge is presented for its challenge name
- **THEN** the request is refused as unauthorized

#### Scenario: Name disappears from DNS after removal

- **WHEN** the last domain for an alias is removed and its label is queried
- **THEN** the response is `NXDOMAIN`

#### Scenario: Shared alias survives partial removal

- **WHEN** one of two domains sharing an alias is removed
- **THEN** the alias remains and challenges for it are still authorized

### Requirement: The required DNS records are generated per load balancer

For a load balancer the service SHALL produce the complete set of DNS records that must
exist outside the service, comprising address records for every domain pointing at every node
address of the load balancer, and one CNAME record per distinct challenge name pointing at the
corresponding alias within the served zone.

A node MAY carry an IPv4 address, an IPv6 address, or both. The plan SHALL emit an A record for
every IPv4 address and an AAAA record for every IPv6 address, and SHALL omit the family a node
does not have.

The set SHALL be available as structured data and as a zone-file rendering.

#### Scenario: Plan lists both record kinds

- **WHEN** the DNS plan is requested for a load balancer with two nodes and one domain
- **THEN** it contains an address record per node address for that domain, and one CNAME record for its challenge name

#### Scenario: Dual-stack nodes yield both families

- **WHEN** the plan is requested for a load balancer whose two nodes each have an IPv4 and an IPv6 address, holding one domain
- **THEN** it contains two A records and two AAAA records for that domain

#### Scenario: A node without IPv6 yields no AAAA record

- **WHEN** a node of the load balancer has no IPv6 address
- **THEN** the plan contains no AAAA record for that node

#### Scenario: Shared challenge name yields one CNAME

- **WHEN** the plan is requested for a load balancer holding `example.com`, `*.example.com` and `shop.example.com`
- **THEN** it contains exactly two CNAME records

### Requirement: The plan includes CAA records when an issuer is configured

When one or more certificate authority identifiers are configured, the plan SHALL include CAA
records restricting issuance to them. The records SHALL be emitted once per distinct registrable
domain covered by the plan, determined by public suffix, and SHALL NOT be emitted for individual
hosts beneath it. Each set SHALL carry both an `issue` and an `issuewild` entry for every
configured identifier.

When no identifier is configured, the plan SHALL omit CAA records entirely rather than assuming
one.

#### Scenario: CAA is emitted at the registrable domain

- **WHEN** the plan is requested for a load balancer holding `example.com`, `*.example.com` and `shop.example.com` with an issuer configured
- **THEN** it contains one CAA set for `example.com` and no CAA record for `shop.example.com`

#### Scenario: Both tags are present

- **WHEN** a CAA set is generated for a configured issuer
- **THEN** it contains an `issue` entry and an `issuewild` entry for that issuer

#### Scenario: Two registrable domains yield two sets

- **WHEN** the plan covers domains under two different registrable domains
- **THEN** it contains one CAA set for each

#### Scenario: No issuer configured means no CAA rows

- **WHEN** the plan is requested with no certificate authority identifier configured
- **THEN** it contains no CAA records

#### Scenario: Plan is renderable as a zone snippet

- **WHEN** a zone-file rendering of the plan is requested
- **THEN** the output is valid zone-file syntax with fully-qualified names

### Requirement: Required DNS records can be verified live

The service SHALL resolve each record of a load balancer's DNS plan against a configured
resolver on demand and report, per record, whether it is correct, missing, points elsewhere, or
could not be determined. Results SHALL include what was actually found when it differs from
what is required, and SHALL NOT be stored as authoritative state.

#### Scenario: Correct delegation verifies

- **WHEN** the challenge name resolves through a CNAME to the expected alias within the served zone
- **THEN** the record is reported as correct

#### Scenario: Missing record is reported

- **WHEN** the challenge name does not resolve
- **THEN** the record is reported as missing

#### Scenario: Wrong target is reported with what was found

- **WHEN** the challenge name resolves to a different target
- **THEN** the record is reported as incorrect together with the observed target

#### Scenario: Resolution failure is distinguished from absence

- **WHEN** the lookup fails or times out
- **THEN** the record is reported as undetermined and the check is retryable

### Requirement: CAA verification distinguishes absent from conflicting

Verification of a CAA record SHALL report a distinct result when no CAA record set exists at or
above the registrable domain, and SHALL NOT report that as a failure, since an absent CAA record
permits any authority to issue.

A CAA record set that exists but does not authorize a configured issuer SHALL be reported as
blocking, together with what was found, because it prevents issuance outright.

#### Scenario: Absent CAA is not a failure

- **WHEN** no CAA record exists for the registrable domain
- **THEN** the result reports that none is published, and is not counted as a failure

#### Scenario: Authorizing CAA verifies

- **WHEN** a CAA record set exists and authorizes a configured issuer for both plain and wildcard issuance
- **THEN** the record is reported as correct

#### Scenario: Conflicting CAA is reported as blocking

- **WHEN** a CAA record set exists and authorizes only some other certificate authority
- **THEN** the record is reported as blocking, with the authorities that were found

### Requirement: The inventory is administrable without the console

The service SHALL provide command-line subcommands to create and list projects, load balancer
lbs, nodes and domains, to render a load balancer's DNS plan, and to create API keys,
usable against the configured datastore without the server process running.

#### Scenario: Operator adds a domain from the command line

- **WHEN** an operator runs the domain creation subcommand for a load balancer
- **THEN** the domain and its alias are persisted
- **AND** the command prints the CNAME record that must be published
