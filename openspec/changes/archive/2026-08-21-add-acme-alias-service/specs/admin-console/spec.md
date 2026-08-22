## ADDED Requirements

### Requirement: The console presents the inventory as a navigable hierarchy

The console SHALL let an authorized user browse projects, the load balancers of a project,
and the nodes and domains of a load balancer, and SHALL let an authorized user create,
edit and remove each of them subject to their role.

#### Scenario: Operator navigates to a domain

- **WHEN** a signed-in user opens a project and then one of its load balancers
- **THEN** the load balancer's node addresses and its domains are shown

#### Scenario: Controls reflect the user's role

- **WHEN** a user without permission to change a project views it
- **THEN** the actions that would change it are not offered

### Requirement: The console validates domain input against the wildcard rules

The domain editor SHALL apply the same validation as the service to wildcard and specific
names, report the reason for a rejection, and state that a wildcard covers exactly one label
and does not cover its own base name.

#### Scenario: Invalid wildcard is reported before submission

- **WHEN** a user enters `*.*.example.com`
- **THEN** the editor reports the rule that rejects it

#### Scenario: Server remains the authority

- **WHEN** a value that passes client-side validation is rejected by the service
- **THEN** the service's reason is shown to the user

### Requirement: The console shows the required DNS records and their live status

For a load balancer the console SHALL display the generated set of required DNS records,
SHALL offer them in a form that can be copied or downloaded as a zone-file snippet, and SHALL
offer an on-demand verification whose per-record result is displayed.

#### Scenario: Records are shown per load balancer

- **WHEN** a user opens a load balancer
- **THEN** the required address records and CNAME records are listed

#### Scenario: Verification results are displayed per record

- **WHEN** the user runs verification
- **THEN** each record is shown as correct, missing, incorrect, or undetermined, with what was found where it differs

#### Scenario: Records can be handed to a third party

- **WHEN** the user requests the zone-file rendering
- **THEN** the complete snippet is produced for copying or download

### Requirement: The console manages API keys and reveals a secret only once

The console SHALL let an authorized user create, list, restrict, rotate and revoke API keys for
a load balancer. A newly created secret SHALL be displayed once, with an explicit
acknowledgement before the display can be dismissed, and SHALL never be displayed again.

Listings SHALL show the key identifier, display prefix, restrictions, expiry and last use, and
SHALL NOT show the secret.

#### Scenario: Secret is acknowledged before dismissal

- **WHEN** a key is created
- **THEN** the secret is shown and the dialog cannot be dismissed until the user confirms it has been stored

#### Scenario: Secret is unavailable afterwards

- **WHEN** the user reopens the key later
- **THEN** only the identifier, prefix and metadata are shown

#### Scenario: Node addresses are offered as restrictions

- **WHEN** the user adds a source-address restriction to a key
- **THEN** the addresses of the load balancer's nodes are offered as defaults

#### Scenario: A read-only user cannot create a key

- **WHEN** a user holding only the `READ_ONLY` role opens the key panel
- **THEN** no creation action is offered, and the service refuses one if attempted directly

### Requirement: The console lists active sessions and can end them

The console SHALL let a holder of `ADMIN` see the sessions that are currently valid — showing for
each the person it belongs to, when it was established, when it was last used and when it expires
— and SHALL let them revoke a single session or every session belonging to one person, with a
confirmation before either.

Revocation is what makes ending someone's access immediate: because roles are derived from group
membership read at sign-in, removing a person from a group does not disturb a session they
already hold. The screen is therefore half of the procedure an operator follows when somebody
leaves.

#### Scenario: Active sessions are listed

- **WHEN** an `ADMIN` opens the sessions screen while other users are signed in
- **THEN** each valid session is listed with its subject, start time, last use and expiry

#### Scenario: Revoking a session ends it

- **WHEN** an `ADMIN` revokes a listed session
- **THEN** the next request made with that session is unauthenticated

#### Scenario: Revocation is confined to its target

- **WHEN** one person's session is revoked
- **THEN** other people's sessions remain usable

#### Scenario: The screen is closed to everyone else

- **WHEN** a user not holding `ADMIN` opens the console
- **THEN** the sessions screen is not offered, and the service refuses the routes if called directly

### Requirement: The console shows the effective configuration

The console SHALL present the service's effective configuration read-only with secrets redacted,
including which group is configured to grant each role. It SHALL NOT offer any means of editing
that mapping, which is configuration rather than data.

This is where an operator sees which groups the *running process* actually maps, which is the
first thing to check when someone reports they cannot do something they should be able to.

#### Scenario: Operator reads the configured role mapping

- **WHEN** an authorized user reads the effective configuration
- **THEN** the group configured for each role is reported as the running process has it

#### Scenario: Secrets are redacted

- **WHEN** the effective configuration is rendered
- **THEN** the database password and the client secret are redacted

### Requirement: The console shows live challenges and the audit trail

The console SHALL display the challenge values currently answerable, with their alias, age and
publishing key, and SHALL display the audit trail filterable by project, actor and action.

#### Scenario: An issuance in progress is visible

- **WHEN** a challenge value is active
- **THEN** it appears in the live challenge view with its alias and publishing key

#### Scenario: Audit trail is filterable

- **WHEN** the user filters the audit trail by project
- **THEN** only entries affecting that project are listed

### Requirement: The console handles authentication transitions without losing the user

The console SHALL direct an unauthenticated user to sign in, SHALL return them to the page they
requested afterwards, and SHALL detect an expired session during use and prompt to sign in
again rather than failing silently.

#### Scenario: Deep link survives sign-in

- **WHEN** an unauthenticated user opens a link to a specific load balancer
- **THEN** after signing in they arrive at that page

#### Scenario: Expired session is surfaced

- **WHEN** a request fails because the session expired
- **THEN** the user is prompted to sign in again

### Requirement: The console is built and delivered with the service

The console SHALL be built from source in the repository as part of the release build and
embedded in the service binary. Development SHALL be possible with the console and the service
running as separate processes behind a single origin, without any difference in authentication
behaviour from production.

#### Scenario: Release build produces one artifact

- **WHEN** the release build runs
- **THEN** the console is compiled and embedded, and no separate console artifact is required

#### Scenario: Development uses the same origin

- **WHEN** the development environment is running
- **THEN** the console and the management API are reachable under one origin and the session cookie behaves as in production
