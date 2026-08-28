## MODIFIED Requirements

### Requirement: The console manages API keys and reveals a secret only once

The console SHALL let an authorized user create, list, restrict, rotate and revoke API keys for
a load balancer or a project. A newly created secret SHALL be displayed once, with an explicit
acknowledgement before the display can be dismissed, and SHALL never be displayed again.

Creating a key SHALL require choosing its anchor and the grants it carries. The console SHALL offer
only grants the acting user itself holds, and SHALL show the authority a key confers wherever the
key is listed.

Listings SHALL show the key identifier, display prefix, anchor, grants, restrictions, expiry and
last use, and SHALL NOT show the secret.

#### Scenario: Secret is acknowledged before dismissal

- **WHEN** a key is created
- **THEN** the secret is shown and the dialog cannot be dismissed until the user confirms it has been stored

#### Scenario: Secret is unavailable afterwards

- **WHEN** the user reopens the key later
- **THEN** only the identifier, prefix, anchor, grants and metadata are shown

#### Scenario: Only grants the user holds are offered

- **WHEN** a user opens the key creation dialog
- **THEN** the grants offered are within the authority that user holds, and the service refuses any other if attempted directly

#### Scenario: Node addresses are offered as restrictions

- **WHEN** the user adds a source-address restriction to a key
- **THEN** the addresses of the load balancer's nodes are offered as defaults

#### Scenario: A user without the permission cannot create a key

- **WHEN** a user holding no grant permitting key creation opens the key panel
- **THEN** no creation action is offered, and the service refuses one if attempted directly

## ADDED Requirements

### Requirement: The console edits roles and their grants

The console SHALL let a user holding the role-administration permission create, edit and delete
roles of the account being acted in, and manage which people hold them.

A grant SHALL be composed by choosing an object type, an object or a wildcard, an action, and
whether it is inheritable. The console SHALL offer only what the acting user holds.

The unrestricted role SHALL be presented as uneditable, and the console SHALL NOT offer removal of
its last member.

#### Scenario: A role is created with grants

- **WHEN** an authorized user creates a role and adds a grant
- **THEN** the role is stored with that grant and may be assigned

#### Scenario: The unrestricted role is not editable

- **WHEN** an authorized user opens the unrestricted role
- **THEN** no action to change its grants or delete it is offered

#### Scenario: Removing the last unrestricted member is not offered

- **WHEN** the unrestricted role has one member and an authorized user opens it
- **THEN** no action removing that member is offered, and the service refuses one if attempted directly

#### Scenario: A user without the permission sees no editor

- **WHEN** a user holding no role-administration grant opens the console
- **THEN** no role editing is offered

### Requirement: The console is told what the user may do per object

The management interface SHALL report, for each object it returns in a listing or a read, the
actions the acting subject may perform on that object. The console SHALL use that report to decide
which controls to render.

The report SHALL NOT be the enforcement point; the service SHALL check every request regardless of
what was reported.

#### Scenario: Controls follow the per-object answer

- **WHEN** a user may edit one project and only read another
- **THEN** editing controls are rendered for the first and not for the second

#### Scenario: The report does not authorize

- **WHEN** a request is made for an action the report did not offer
- **THEN** the service refuses it

#### Scenario: The answer accompanies the listing

- **WHEN** a listing is read
- **THEN** the permitted actions accompany each row without a further request per row
