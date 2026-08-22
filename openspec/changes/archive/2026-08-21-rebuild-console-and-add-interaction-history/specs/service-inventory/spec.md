## ADDED Requirements

### Requirement: Collection reads are filtered, paged and counted by the service

The management interface SHALL apply filtering and paging to a collection read on the service side,
and SHALL report, alongside each page, the number of rows matching the filter before paging.

A page SHALL be requested with a row limit and a row offset. A limit exceeding the maximum the
service accepts for that collection SHALL be refused with an error naming the bound, and SHALL NOT
be silently reduced.

Free-text filtering SHALL match case-insensitively, and the characters a pattern match treats as
wildcards SHALL be escaped in the submitted text.

#### Scenario: The filter narrows the whole collection

- **WHEN** a collection is read with a filter matching only a row that would fall on a later page
- **THEN** that row is returned

#### Scenario: The total accompanies the page

- **WHEN** a page of a filtered collection is read
- **THEN** the response reports the number of rows matching the filter before paging

#### Scenario: An excessive limit is refused

- **WHEN** a page is requested with a limit above the maximum for that collection
- **THEN** the request is refused with an error naming the maximum, and no rows are returned

#### Scenario: Wildcard characters in the filter are literal

- **WHEN** a filter contains a character the underlying pattern match treats as a wildcard
- **THEN** it matches that character literally

### Requirement: A date named as a range bound includes the whole of that day

Where a time range is submitted as a date without a time, the lower bound SHALL be the start of that
day and the upper bound SHALL be the end of that day. A range whose upper bound precedes its lower
bound SHALL be refused.

#### Scenario: The named day is included

- **WHEN** a time range ends on a given date and a record was created during that date
- **THEN** the record is included

#### Scenario: An inverted range is refused

- **WHEN** a range is submitted whose upper bound is earlier than its lower bound
- **THEN** the request is refused

### Requirement: An inventory object is readable by its identifier

The management interface SHALL allow each inventory object that a screen or a link can address to be
read individually by its identifier, without reading the collection it belongs to.

#### Scenario: A project is read by identifier

- **WHEN** a project is requested by its identifier
- **THEN** it is returned with its load balancers, their nodes and their domains

#### Scenario: Reading by identifier is independent of paging

- **WHEN** an object is requested by an identifier that would fall outside the first page of its
  collection
- **THEN** it is returned

### Requirement: Node addresses are editable and the node label is not

The service SHALL allow the addresses of an existing node to be replaced. It SHALL NOT allow the
node's label to be changed, the label being the name under which the node appears in a DNS plan
already published to a third party.

A replacement SHALL be refused when it would leave the node with no address, and each address SHALL
be refused unless it belongs to the address family of the field it is supplied for.

#### Scenario: A renumbered node keeps its identity

- **WHEN** a node's addresses are replaced
- **THEN** the node retains its identifier and label, and the load balancer's DNS plan reports the new
  addresses

#### Scenario: Removing every address is refused

- **WHEN** a replacement supplies neither an IPv4 nor an IPv6 address
- **THEN** the request is refused and the node is unchanged

#### Scenario: An address of the wrong family is refused

- **WHEN** an IPv6 address is supplied as the node's IPv4 address
- **THEN** the request is refused

## MODIFIED Requirements

### Requirement: The inventory is projects, load balancers and domains

The service SHALL store projects; each project SHALL own zero or more load balancers; each
load balancer SHALL own zero or more nodes with their addresses and zero or more domains.
Each load balancer SHALL belong to exactly one project, and each domain SHALL belong to
exactly one load balancer.

Deleting a project or a load balancer that still owns children SHALL be refused.

A read that reports the number of domains of a load balancer SHALL load those domains.

#### Scenario: Domain is reachable through the hierarchy

- **WHEN** a domain is created under a load balancer of a project
- **THEN** the domain is listed for that load balancer, and the load balancer is listed for that project

#### Scenario: Non-empty parent is not deleted

- **WHEN** deletion is requested for a project that still has load balancers
- **THEN** the request is refused and nothing is deleted

#### Scenario: A load balancer's domain count is the number it has

- **WHEN** the load balancers of a project are listed and one of them owns two domains
- **THEN** that load balancer reports two domains and not zero
