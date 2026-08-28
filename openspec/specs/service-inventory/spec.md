# service-inventory Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
### Requirement: The inventory is projects, load balancers and domains

The service SHALL store accounts; each account SHALL own zero or more projects; each project SHALL
own zero or more load balancers; each load balancer SHALL own zero or more nodes with their
addresses and zero or more domains. Each project SHALL belong to exactly one account, each load
balancer SHALL belong to exactly one project, and each domain SHALL belong to exactly one load
balancer.

Deleting an account, a project or a load balancer that still owns children SHALL be refused.

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

#### Scenario: Every object reports the account that owns it

- **WHEN** a load balancer, node, domain or alias is read
- **THEN** the account it reports is the account of the project containing it

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

The service SHALL store an alias for each distinct challenge name within an account, consisting of a
single label within the served zone. Challenge names SHALL be unique within an account. Alias labels
SHALL be unique across the service, because the served zone is shared by every account. Several
domains MAY share one alias.

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

#### Scenario: Two accounts holding one name get two aliases

- **WHEN** `shop.example.com` is added in one account and in another
- **THEN** each account has its own alias with its own label, and neither is visible to the other

#### Scenario: A derived label colliding across accounts is resolved

- **WHEN** labels are derived from the challenge name and the same domain is added in two accounts
- **THEN** the second alias is given another valid label and the domain is created

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

### Requirement: An alias belongs to exactly one load balancer

The service SHALL refuse to create a domain whose derived challenge name already has an alias
owned by a different load balancer **of the same account**, and SHALL name the conflicting load
balancer in the error. An alias owned by another account SHALL NOT conflict and SHALL NOT be named.

#### Scenario: Conflicting ownership is refused

- **WHEN** `*.example.com` is added to one load balancer while `example.com` on another load balancer of the same account already owns the alias for `_acme-challenge.example.com`
- **THEN** the request is refused with an error naming the owning load balancer

#### Scenario: Another account's alias does not conflict

- **WHEN** a domain is added whose challenge name already has an alias in a different account
- **THEN** the request succeeds and the error names nothing

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

Domain creation from the command line SHALL offer the same counterpart creation as the
management interface, defaulting to creating only the name given.

#### Scenario: Operator adds a domain from the command line

- **WHEN** an operator runs the domain creation subcommand for a load balancer
- **THEN** the domain and its alias are persisted
- **AND** the command prints the CNAME record that must be published

#### Scenario: Operator adds a pair from the command line

- **WHEN** an operator runs the domain creation subcommand for `example.com` asking for its counterpart
- **THEN** both domains are persisted against one alias, and one CNAME record is printed

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

### Requirement: A domain name is unique within an account and not across the service

The service SHALL permit the same domain name to exist in more than one account, and SHALL refuse a
second domain of the same name within one account.

The service SHALL rely on the challenge alias record the domain owner publishes as the proof of
control, and SHALL NOT arbitrate between accounts claiming the same name.

#### Scenario: The same name in two accounts is permitted

- **WHEN** `shop.example.com` is added in one account and then in another
- **THEN** both are created

#### Scenario: The same name twice in one account is refused

- **WHEN** `shop.example.com` is added twice in one account
- **THEN** the second request is refused

#### Scenario: Only the account whose alias is published can validate

- **WHEN** two accounts hold `shop.example.com` and the alias record published in the wider internet points at one account's label
- **THEN** challenges presented for that name succeed only for that account

### Requirement: Creating a project requires the account's unrestricted role

The service SHALL permit the creation and deletion of a project only to a member of the unrestricted
role of the account that owns it. This SHALL NOT be expressible as a grant, and no other permission
SHALL confer it.

#### Scenario: An unrestricted member creates a project

- **WHEN** a member of the account's unrestricted role creates a project
- **THEN** it is created and owned by that account

#### Scenario: Any other member is refused

- **WHEN** a member who is not in that role creates a project
- **THEN** the request is refused, whatever other authority they hold

#### Scenario: A created project is reachable by its creator

- **WHEN** a member of the unrestricted role creates a project
- **THEN** they may act on it, as that role covers every object of the account

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

