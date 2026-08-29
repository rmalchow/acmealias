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

By default the label SHALL be randomly generated with at least 48 bits of entropy and SHALL NOT
be derivable from the challenge name, the domain name, or any other value visible outside the
service. The floor is 48 rather than the 80 it was: the label is typed into a DNS provider's form
by hand and read back to verify it, so every character costs a chance to transpose one, and
guessing a label without holding the matching delegation is worth nothing.

A configuration setting SHALL make the service derive the label from the challenge name instead,
for a deployment that prefers a self-describing label.

The setting SHALL govern only aliases created after it takes effect. The label of an existing
alias SHALL NOT change, whatever the setting, because it is published in a record the service
does not control.

Where a candidate label is already in use, the service SHALL resolve the collision and complete
the request rather than refuse it. This SHALL hold for a collision detected only when the record
is written — two requests choosing one label concurrently SHALL NOT fail the later one — and a
label conflict SHALL be distinguished from a conflict over the challenge name itself, which is a
genuine refusal and SHALL NOT be retried.

#### Scenario: A label chosen twice at once still succeeds

- **WHEN** two concurrent requests choose the same alias label and the second is refused by the store
- **THEN** the second chooses again and completes, rather than failing the request

#### Scenario: A claimed challenge name is refused, not retried

- **WHEN** a request's challenge name is already claimed elsewhere
- **THEN** it is refused, and the refusal is not retried as though it were a label collision

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

### Requirement: CAA verification distinguishes absent, unconstrained and conflicting

Verification of a CAA record SHALL report four distinct results: that no CAA record set exists,
that one exists and does not authorize a configured issuer, that one authorizes a configured issuer
without limiting issuance to a specific ACME account, and that one authorizes a configured issuer
and limits issuance to a specific account.

The distinction between the last two SHALL be drawn on the presence of a non-empty account
identifier parameter on every issuance authorization the set grants for a configured issuer, both
for plain and for wildcard issuance. A set in which any such authorization carries no account
identifier SHALL be reported as unconstrained, since a set is only as narrow as its widest entry.

Verification SHALL report what was found alongside every result that has published data to report,
and not only when the set is blocking.

None of these SHALL be reported as a lookup failure; a failure means the query did not complete.

#### Scenario: Absent CAA is reported as such

- **WHEN** no CAA record exists for the registrable domain
- **THEN** the result reports that none is published, and is not counted as a lookup failure

#### Scenario: An authorizing set without an account is unconstrained

- **WHEN** a CAA record set authorizes a configured issuer for both plain and wildcard issuance and names no ACME account
- **THEN** the record is reported as unconstrained, with what was found

#### Scenario: An authorizing set limited to an account is correct

- **WHEN** a CAA record set authorizes a configured issuer for both plain and wildcard issuance and every such authorization names an ACME account
- **THEN** the record is reported as correct

#### Scenario: One unlimited authorization makes the set unconstrained

- **WHEN** a CAA record set limits wildcard issuance to an ACME account and leaves plain issuance limited only to the issuer
- **THEN** the record is reported as unconstrained

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

### Requirement: A domain carries a certificate observation switch and a test hostname

A domain SHALL carry a flag recording whether its certificate is observed, and an
optional hostname to present as the TLS server name when it is.

The flag SHALL default to disabled on creation. Both SHALL be writable through the
same permission that writes the rest of the domain, and SHALL be readable wherever
the domain is read, so that a client can render a domain's row without a second
request.

Where the domain is a wildcard, the service SHALL refuse to enable the flag unless
a test hostname is supplied, and SHALL refuse a hostname the wildcard does not
cover. Where the domain is not a wildcard, the service SHALL NOT require one, and
SHALL present the domain's own name as the server name.

The hostname SHALL be normalized and validated by the same rules the service
applies to a domain name, and SHALL be retained when the flag is disabled.

Deleting a domain SHALL remove both, along with the domain.

#### Scenario: The flag defaults to off

- **WHEN** a domain is created
- **THEN** its observation flag is disabled and it carries no test hostname

#### Scenario: A wildcard cannot be enabled without a hostname

- **WHEN** the observation flag is enabled for `*.example.com` with no test hostname
- **THEN** the write is refused

#### Scenario: A hostname the wildcard does not cover is refused

- **WHEN** a test hostname of `www.other.example` is written to `*.example.com`
- **THEN** the write is refused

#### Scenario: A non-wildcard needs no hostname

- **WHEN** the observation flag is enabled for `shop.example.com` with no test hostname
- **THEN** the write is accepted

#### Scenario: Both are readable with the domain

- **WHEN** a domain is read
- **THEN** its observation flag and its test hostname are present in the result

#### Scenario: The hostname survives the flag being cleared

- **WHEN** the observation flag is disabled on a domain that carried a test hostname
- **THEN** the hostname is retained

### Requirement: A domain carries the records that must be published for it

The service SHALL report, with each domain, everything that must be published
outside the service for that domain to work: its challenge name, the target that
challenge name's `CNAME` must point at, and the CAA entries expected at the
registrable name governing it.

The target SHALL be the alias issued for that domain's challenge name within the
served zone. Where a domain has no alias, the service SHALL report the absence
rather than omitting the domain, since the domain still exists and the absence is
the service's own to resolve.

Two domains sharing a challenge name SHALL each report that one target, so that a
name and its wildcard each state the delegation they share rather than one of them
appearing to need nothing.

The CAA entries SHALL be those the configured certificate authority identifiers
require: for each identifier, an `issue` entry and an `issuewild` entry, expected
at the registrable domain determined by public suffix and never at an individual
host beneath it, because CAA is inherited and the nearest ancestor wins. Where no
identifier is configured, the service SHALL report no CAA entries rather than
assuming one.

The service SHALL NOT report address records for a domain.

#### Scenario: A domain states its delegation

- **WHEN** a domain is read
- **THEN** it carries its challenge name and the target that name's `CNAME` must point at

#### Scenario: A wildcard and its apex state the same delegation

- **WHEN** `example.com` and `*.example.com` are read
- **THEN** both carry the same challenge name and the same target

#### Scenario: A domain with no alias says so

- **WHEN** a domain has no alias
- **THEN** it is still reported, carrying no target, rather than omitted

#### Scenario: CAA is stated at the registrable name

- **WHEN** `shop.example.com` is read with an issuer configured
- **THEN** its expected CAA entries are named at `example.com` and not at `shop.example.com`

#### Scenario: Both CAA tags are stated

- **WHEN** a domain's expected CAA entries are read with an issuer configured
- **THEN** they contain an `issue` entry and an `issuewild` entry for that issuer

#### Scenario: No issuer configured means no CAA entries

- **WHEN** a domain is read with no certificate authority identifier configured
- **THEN** it carries no expected CAA entries

#### Scenario: No address records are reported

- **WHEN** a domain is read
- **THEN** it carries no address record for any node of its project

### Requirement: A domain's published records can be verified live

The service SHALL resolve, per domain and on demand, the records that domain
requires, and SHALL report per record type whether it is correct, missing, points
elsewhere, or could not be determined. Results SHALL include what was actually
found when it differs from what is required, and SHALL NOT be stored as
authoritative state.

The answer SHALL be reported per domain. The service SHALL NOT additionally report
the same answers keyed by record, since one name's answer belongs to every domain
that requires it and a second keying invites the two to disagree.

Where no resolver is configured, the service SHALL report every domain as not
checked rather than refusing.

#### Scenario: Correct delegation verifies

- **WHEN** a domain's challenge name resolves through a `CNAME` to the target it requires
- **THEN** the domain's delegation is reported as correct

#### Scenario: Missing record is reported

- **WHEN** a domain's challenge name does not resolve
- **THEN** the domain's delegation is reported as missing

#### Scenario: Wrong target is reported with what was found

- **WHEN** a domain's challenge name resolves to a different target
- **THEN** the domain's delegation is reported as incorrect together with the observed target

#### Scenario: Resolution failure is distinguished from absence

- **WHEN** the lookup fails or times out
- **THEN** the record is reported as undetermined and the check is retryable

#### Scenario: A shared delegation answers both domains

- **WHEN** verification runs for a project holding `example.com` and `*.example.com`
- **THEN** both domains report the state of the one delegation they share

#### Scenario: No resolver is not a refusal

- **WHEN** verification runs on a deployment with no configured resolver
- **THEN** every domain reports as not checked and the request succeeds

