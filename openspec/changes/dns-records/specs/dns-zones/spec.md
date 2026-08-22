## ADDED Requirements

### Requirement: A zone is owned through a project

The service SHALL store zones, each belonging to exactly one project and therefore owned by exactly
one account. A zone SHALL NOT exist without a project.

A zone name SHALL be unique within an account. The same zone name MAY exist in more than one account.

#### Scenario: A zone is created under a project

- **WHEN** a zone is created for a project
- **THEN** it is owned by that project's account and is listed for that project

#### Scenario: A zone of another account is not reachable

- **WHEN** a member of one account addresses a zone of another by its identifier
- **THEN** the request is refused and the response does not distinguish it from a zone that does not exist

#### Scenario: A duplicate zone name in one account is refused

- **WHEN** a zone name already held by an account is created again in that account
- **THEN** the request is refused

### Requirement: Records are stored as recordsets

The service SHALL store the records of a zone as recordsets, where a recordset is every record sharing
one owner name and one type, holding one or more values and a single time to live.

A change to a recordset SHALL be applied as a whole. The service SHALL NOT expose an operation that
leaves a recordset partially changed.

#### Scenario: Several values at one name are one recordset

- **WHEN** two addresses are stored for one name and type
- **THEN** they form one recordset with one time to live

#### Scenario: Replacing values is atomic

- **WHEN** the values of a recordset are replaced
- **THEN** at no point is the name answerable with neither the old values nor the new

#### Scenario: A recordset is owned by its zone's account

- **WHEN** a recordset is read
- **THEN** the account owning it is that of its zone, and no other source

### Requirement: A zone is validated so that it remains servable

The service SHALL refuse a recordset write that would make the zone invalid. It SHALL refuse at least:
a name holding a `CNAME` together with any other type; a `CNAME` at the zone apex; a name outside the
zone; and a value that is malformed for its record type.

Validation SHALL occur when the change is written, and an invalid state SHALL NOT be stored.

#### Scenario: A CNAME alongside another type is refused

- **WHEN** a `CNAME` is written for a name that already holds an `A` recordset
- **THEN** the request is refused

#### Scenario: A CNAME at the apex is refused

- **WHEN** a `CNAME` is written for the zone's own name
- **THEN** the request is refused

#### Scenario: A name outside the zone is refused

- **WHEN** a recordset named outside the zone is written
- **THEN** the request is refused

#### Scenario: A malformed value is refused

- **WHEN** a value that is not valid for its record type is written
- **THEN** the request is refused and nothing is stored

### Requirement: A zone whose parent publishes a delegation signer is refused

The service SHALL query a zone's parent for a delegation signer record at that name when the zone is
added, and SHALL refuse the zone while one exists. The refusal SHALL state that the domain is
signed, that the service does not sign, and that the delegation signer must be removed at the
registrar and its time to live allowed to expire before the zone can be added.

If the parent cannot be queried, the zone SHALL NOT be accepted.

The service SHALL repeat this check periodically for zones it already serves, and SHALL raise an
alert against a zone for which a delegation signer subsequently appears.

#### Scenario: A signed zone is refused

- **WHEN** a zone is added whose parent publishes a delegation signer for that name
- **THEN** the zone is not created, and the refusal names the delegation signer as the cause

#### Scenario: An unreachable parent is not treated as unsigned

- **WHEN** the parent cannot be queried for a delegation signer
- **THEN** the zone is not created, and the caller is told to retry

#### Scenario: A delegation signer appearing later is detected

- **WHEN** a zone already served unsigned acquires a delegation signer at its parent
- **THEN** the periodic check raises an alert against that zone

### Requirement: The fleet is delegated through names in more than one parent domain

The apex nameserver records SHALL be generated from configuration rather than from constants, and
the configured set SHALL contain at least two names in at least two distinct parent domains under
at least two distinct top-level domains. A configuration providing fewer SHALL be refused at
startup.

#### Scenario: A single-parent nameserver set is refused

- **WHEN** the service starts with nameserver names that all share one parent domain
- **THEN** startup fails with an error naming the requirement

#### Scenario: Apex delegation reflects the configured names

- **WHEN** a zone is created
- **THEN** its apex nameserver records are those configured for the fleet

### Requirement: The apex housekeeping records are maintained by the service

The service SHALL generate and maintain the `SOA` and the apex `NS` records of every zone it serves,
and SHALL NOT permit them to be created, changed or deleted through the management interface.

The `SOA` serial SHALL advance when the zone changes.

`NS` records below the apex SHALL be writable, subject to permission, so that a subtree may be
delegated elsewhere.

#### Scenario: Apex records are not editable

- **WHEN** a change to the `SOA` or to an apex `NS` recordset is requested
- **THEN** the request is refused

#### Scenario: The serial advances on change

- **WHEN** a recordset in a zone is changed
- **THEN** the zone's `SOA` serial advances

#### Scenario: Delegation below the apex is permitted

- **WHEN** an `NS` recordset is written for a name below the apex by a subject permitted the `NS` type
- **THEN** it is stored and served

### Requirement: Changing a record requires a grant matching its zone, name and type

The service SHALL authorize every read and every change of a recordset against the acting subject's
permission grants, matching the zone, the owner name and the record type.

Deleting a recordset SHALL be authorized as a change to it.

#### Scenario: A grant limited by type refuses another type

- **WHEN** a subject holding a grant for the address types writes a mail exchanger recordset
- **THEN** the request is refused

#### Scenario: A grant limited by name pattern refuses another name

- **WHEN** a subject holding a grant for a subtree writes a name outside it
- **THEN** the request is refused

#### Scenario: A subtree grant does not include its own apex

- **WHEN** a subject holding a grant for a subtree writes the subtree's own name
- **THEN** the request is refused

#### Scenario: Deletion is authorized as a change

- **WHEN** a subject not permitted to change a recordset deletes it
- **THEN** the request is refused
