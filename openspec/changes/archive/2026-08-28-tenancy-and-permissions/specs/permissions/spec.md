## ADDED Requirements

### Requirement: A permission names an object type, an object and an action

The service SHALL express every permission as a grant of three parts — an object type, an object
identifier, and an action — together with a flag stating whether the grant reaches objects contained
by the one it names.

Each of the three parts MAY be a wildcard. The object identifier MAY instead be a typed selector
where the object type defines one.

#### Scenario: A grant on one object permits that object

- **WHEN** a subject holds a grant naming one load balancer and the action of publishing challenge values
- **THEN** it may publish challenge values for that load balancer

#### Scenario: A grant on one object permits nothing else

- **WHEN** a subject holds a grant naming one load balancer and acts on a different load balancer
- **THEN** the request is refused

#### Scenario: A wildcard action permits every action on the named object

- **WHEN** a subject holds a grant naming one project and a wildcard action
- **THEN** every action defined for that project is permitted on it

### Requirement: A wildcard is confined to the account the grant belongs to

Every grant SHALL belong to an account, and a wildcard in any part of a grant SHALL match only
objects owned by that account. No grant SHALL be capable of naming, matching or authorizing an
object of another account.

#### Scenario: A wildcard does not reach another account

- **WHEN** a subject holds a grant with a wildcard object identifier and acts on an object of another account
- **THEN** the request is refused

#### Scenario: A wildcard covers the account it belongs to

- **WHEN** a subject holds a grant with a wildcard object identifier and acts on an object of its own account
- **THEN** the grant matches that object

### Requirement: Containment is a fixed hierarchy and inheritance follows it

The service SHALL define one containment hierarchy: an account contains projects, a project contains
load balancers, and a load balancer contains its nodes, domains, aliases and API keys.

A grant marked inheritable SHALL match every object contained, directly or indirectly, by the object
it names. A grant not so marked SHALL match only the object it names.

#### Scenario: An inheritable grant reaches contained objects

- **WHEN** a subject holds an inheritable grant naming a project
- **THEN** it matches every load balancer, node, domain and alias of that project

#### Scenario: A non-inheritable grant does not reach contained objects

- **WHEN** a subject holds a non-inheritable grant naming a project and acts on a load balancer of that project
- **THEN** the request is refused

#### Scenario: Inheritance does not reach upward

- **WHEN** a subject holds an inheritable grant naming a load balancer and acts on the project containing it
- **THEN** the request is refused

### Requirement: Permissions are granted only, never denied

A subject's authority SHALL be the union of its grants, and the absence of a matching grant SHALL be
a refusal. The service SHALL NOT provide a grant that removes authority conferred by another.

Evaluation SHALL NOT depend on the order in which grants are held or written.

#### Scenario: Adding a grant never removes authority

- **WHEN** a grant is added to a subject that already holds others
- **THEN** everything it could do before, it may still do

#### Scenario: An unmatched action is refused

- **WHEN** a subject holds no grant matching an action on an object
- **THEN** the request is refused

### Requirement: A role is a named set of grants belonging to one account

The service SHALL represent a role as a record within one account, holding zero or more grants, with
a name unique in that account. A person SHALL hold roles through membership in an account, and roles
held in one account SHALL confer nothing in another.

Roles SHALL be creatable, editable and deletable through the management interface, subject to the
permission required to administer roles.

#### Scenario: A role confers its grants

- **WHEN** a person is a member of an account through a role holding a grant
- **THEN** that grant is evaluated for their requests in that account

#### Scenario: A role confers nothing in another account

- **WHEN** a person holds a role in one account and acts on an object of another
- **THEN** the request is refused

#### Scenario: Roles are additive

- **WHEN** a person holds two roles in one account
- **THEN** their authority is the union of both

### Requirement: One role is fixed, unrestricted and always populated

The service SHALL provide in every account one role conferring every action on every object of that
account. That role SHALL NOT be editable or deletable, and SHALL always have at least one member.

An operation that would leave it with no member SHALL be refused. The service SHALL evaluate the
membership count and apply the change indivisibly, so that concurrent operations cannot each observe
a remaining member and together remove the last.

#### Scenario: The unrestricted role permits everything in its account

- **WHEN** a member of that role acts on any object of that account
- **THEN** the request is permitted

#### Scenario: The last member cannot be removed

- **WHEN** removal is requested for the only member of that role
- **THEN** the request is refused and the membership remains

#### Scenario: Concurrent removals cannot empty the role

- **WHEN** two removals of the two remaining members are applied concurrently
- **THEN** one succeeds, the other is refused, and one member remains

#### Scenario: The role cannot be edited away

- **WHEN** a change to that role's grants or its deletion is requested
- **THEN** the request is refused

### Requirement: A subject may grant only what it holds

When a grant is written to a role or to an API key, the service SHALL refuse it unless it is covered
by a grant the writer itself holds.

Coverage SHALL be evaluated against a single held grant. The service SHALL NOT combine several held
grants to cover one proposed grant, and SHALL refuse a proposed grant that only their union would
permit.

#### Scenario: A grant within the writer's authority is accepted

- **WHEN** a writer holding an inheritable grant on a project writes a grant naming a load balancer of that project
- **THEN** the grant is accepted

#### Scenario: A grant outside the writer's authority is refused

- **WHEN** a writer writes a grant naming an object it holds no grant over
- **THEN** the request is refused

#### Scenario: An unrestricted grant cannot be self-issued

- **WHEN** a writer that does not hold an unrestricted grant writes one
- **THEN** the request is refused

#### Scenario: Coverage is not evaluated across several held grants

- **WHEN** a writer holds separate grants on two objects and writes one grant covering both
- **THEN** the request is refused

### Requirement: A DNS record grant selects by zone, name and record type

For DNS records the service SHALL express the object identifier as a selector naming one zone, a name
pattern, and a set of record types. A grant SHALL match a record only when all three match.

A name pattern MAY begin with a wildcard label, which SHALL match one or more labels and SHALL NOT
match the empty string. A wildcard label SHALL be permitted only as the first label; the service SHALL
refuse a pattern with a wildcard elsewhere.

Patterns and names SHALL be compared in normalized form.

#### Scenario: A wildcard pattern matches names beneath it

- **WHEN** a grant selects the pattern `*.staging.example.com` and a record `web.staging.example.com` is written
- **THEN** the pattern matches

#### Scenario: A wildcard pattern matches several labels

- **WHEN** a grant selects the pattern `*.staging.example.com` and a record `db.web.staging.example.com` is written
- **THEN** the pattern matches

#### Scenario: A wildcard pattern does not match its own apex

- **WHEN** a grant selects the pattern `*.staging.example.com` and a record `staging.example.com` is written
- **THEN** the pattern does not match and the request is refused

#### Scenario: A record type outside the grant is refused

- **WHEN** a grant selects the record types `A` and `AAAA` and an `MX` record is written for a matching name
- **THEN** the request is refused

#### Scenario: A pattern with an interior wildcard is refused

- **WHEN** a grant is written with the pattern `web-*.example.com` or `*.web.*.example.com`
- **THEN** the request is refused

### Requirement: Authorization is evaluated server-side for every request

The service SHALL evaluate every management operation against the acting subject's grants, on the
server, independently of what any client rendered or requested.

#### Scenario: The check does not depend on the client

- **WHEN** a request is made directly to a management route without the console
- **THEN** the same permission check is applied

### Requirement: Reads are restricted in the query, not after it

The service SHALL derive from the acting subject's grants a condition applied as part of every read, so
that an object the subject may not see is not returned by the datastore.

The service SHALL NOT read objects and then discard those the subject may not see.

#### Scenario: An unreadable object is never fetched

- **WHEN** a collection is read by a subject whose grants cover some of its objects
- **THEN** only the covered objects are retrieved, and the others are excluded by the read itself

#### Scenario: The restriction follows the grants

- **WHEN** a grant is added to a subject and the same collection is read again
- **THEN** the objects the new grant covers are returned, without any change to the read

### Requirement: A count reported with a collection is restricted identically

A count reported alongside a page of a collection SHALL be produced under the same restriction as the
page itself, and SHALL NOT include an object the subject may not see.

#### Scenario: The total excludes invisible objects

- **WHEN** a subject reads a page of a collection in which objects exist that its grants do not cover
- **THEN** the reported total counts only the covered objects

#### Scenario: The total does not reveal existence

- **WHEN** an object the subject may not see is created
- **THEN** no total the subject can read changes

### Requirement: Addressing an object is a restricted read of one object

The service SHALL authorize the reading, modification and deletion of an existing object by the same
restricted read that serves collections, narrowed to the object's identifier.

An object that does not exist and an object the subject may not see SHALL produce the same result, and
the service SHALL NOT provide a means of distinguishing them.

#### Scenario: An object outside the subject's grants is not found

- **WHEN** a subject addresses an object by identifier that its grants do not cover
- **THEN** the result reports that no such object was found

#### Scenario: Absence and denial are indistinguishable

- **WHEN** a subject addresses an identifier that does not exist, and one that exists but is not covered by its grants
- **THEN** the two responses are identical in status, body and timing characteristics

#### Scenario: Modification is authorized the same way

- **WHEN** a subject modifies or deletes an object its grants do not cover
- **THEN** the request reports that no such object was found and nothing is changed

### Requirement: Creating an object is authorized on its parent

Because a created object does not yet exist, the service SHALL authorize creation by an action on the
object that will contain it, retrieved by the restricted read.

Creating a project SHALL be the exception: it SHALL require membership of the account's unrestricted
role and SHALL NOT be expressible as a grant. The service SHALL NOT accept a grant naming an account as
its object.

#### Scenario: Creation under a permitted parent succeeds

- **WHEN** a subject holding the creating action on a load balancer creates a domain under it
- **THEN** the domain is created

#### Scenario: Creation under an unreadable parent is not found

- **WHEN** a subject creates an object under a parent its grants do not cover
- **THEN** the request reports that no such parent was found, and does not reveal whether it exists

#### Scenario: Creation without the creating action is refused

- **WHEN** a subject that may read a parent but does not hold the creating action on it creates a child
- **THEN** the request is refused

#### Scenario: Only the unrestricted role creates a project

- **WHEN** a member who is not in the account's unrestricted role creates a project
- **THEN** the request is refused, whatever grants that member holds

#### Scenario: An account cannot be named by a grant

- **WHEN** a grant is written naming an account as its object
- **THEN** the request is refused

### Requirement: Identifiers do not disclose existence, order or volume

Every object SHALL be identified by a value that cannot be guessed, enumerated, or used to infer how
many objects exist or in what order they were created.

#### Scenario: An identifier cannot be derived from another

- **WHEN** two objects are created in succession
- **THEN** neither identifier can be computed from the other

#### Scenario: Identifiers do not collide across object types

- **WHEN** the identifier of an object of one type is used to address an object of another
- **THEN** no object is found
