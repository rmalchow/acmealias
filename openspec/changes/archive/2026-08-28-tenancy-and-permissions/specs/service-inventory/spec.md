## MODIFIED Requirements

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

## ADDED Requirements

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
