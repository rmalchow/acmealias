## REMOVED Requirements

### Requirement: The console lists active sessions and can end them

**Reason**: Ending someone's access is a support action taken on the rare day it is needed, by
someone already at a terminal following the two-step procedure for removing a person — remove them
from the group *and* revoke their sessions. A screen for it occupies a permanent place in the
navigation of every operator who never uses it.

**Migration**: The capability itself is unchanged and is restated below as *"Session revocation is
available, outside the console"*. The session routes remain and remain restricted to `ADMIN`;
only the screen is gone.

### Requirement: The console shows the effective configuration

**Reason**: The effective configuration is read while diagnosing, not browsed. The same answer is
available from the service's startup log and from a single management API call.

**Migration**: Restated below as *"The effective configuration is readable, outside the console"*.
The route remains and remains role-gated; only the screen is gone.

## ADDED Requirements

### Requirement: Session revocation is available, outside the console

The service SHALL let a holder of `ADMIN` enumerate the sessions that are currently valid —
showing for each the person it belongs to, when it was established, when it was last used and
when it expires — and SHALL let them revoke a single session or every session belonging to one
person. This capability is served by the management API. The console
SHALL NOT present it.

Revocation is what makes ending someone's access immediate: because roles are derived from group
membership read at sign-in, removing a person from a group does not disturb a session they
already hold.

#### Scenario: Active sessions are listed

- **WHEN** an `ADMIN` asks the management API for current sessions while other users are signed in
- **THEN** each valid session is listed with its subject, start time, last use and expiry

#### Scenario: Revoking a session ends it

- **WHEN** an `ADMIN` revokes a listed session
- **THEN** the next request made with that session is unauthenticated

#### Scenario: Revocation is confined to its target

- **WHEN** one person's session is revoked
- **THEN** other people's sessions remain usable

#### Scenario: The capability is closed to everyone else

- **WHEN** a user not holding `ADMIN` calls the session routes directly
- **THEN** the service refuses the operation

#### Scenario: No console screen offers it

- **WHEN** a signed-in `ADMIN` browses the console
- **THEN** no navigation entry or screen for sessions is present

### Requirement: The effective configuration is readable, outside the console

The service SHALL expose its effective configuration read-only with secrets redacted, including
which group is configured to grant each role. It SHALL NOT offer any means of editing that
mapping, which is configuration rather than data. The console SHALL NOT present this view.

#### Scenario: Operator reads the configured role mapping

- **WHEN** an authorized user reads the effective configuration
- **THEN** the group configured for each role is reported as the running process has it

#### Scenario: Secrets are redacted

- **WHEN** the effective configuration is rendered
- **THEN** the database password and the client secret are redacted

#### Scenario: No console screen offers it

- **WHEN** a signed-in user browses the console
- **THEN** no navigation entry or screen for the effective configuration is present

### Requirement: Every list in the console is filtered and paged by the service

Each list screen SHALL offer a filter appropriate to what it lists and SHALL present its rows one
page at a time, with the page size under the operator's control. Filtering and paging SHALL both
be applied by the service, not by the browser: a screen that filtered only the rows it had
already fetched would silently search one page and report "no match" for a record that exists.

Each list screen SHALL state the number of rows matching the filter in total, as reported by the
service, and SHALL return to the first page whenever a filter changes.

The project list SHALL filter on a substring of the slug or the display name. The live-challenge
list SHALL filter on a substring of the alias label, the challenge name or the publishing key. The
audit trail SHALL filter on a date range in addition to its project, actor and action filters.

#### Scenario: A filter narrows the whole collection, not the page

- **WHEN** an operator filters the project list by a substring that matches only a project which
  would fall on a later page
- **THEN** that project is listed

#### Scenario: The total is displayed

- **WHEN** a list screen shows a page of rows
- **THEN** it states how many rows match the filter in total

#### Scenario: Changing a filter returns to the first page

- **WHEN** an operator on a later page changes a filter
- **THEN** the first page of the new result set is shown

#### Scenario: The operator chooses the page size

- **WHEN** an operator changes the number of rows per page
- **THEN** the list is re-read from the service at that size

### Requirement: A screen addressed by identifier reads by identifier

A console screen reachable by a deep link SHALL load its subject by identifier and SHALL NOT locate
it by searching a paged collection.

#### Scenario: A deep link to any project works

- **WHEN** an operator opens a link to a project that would fall outside the first page of the
  project list
- **THEN** the project's detail screen is shown

### Requirement: Every screen opens with the same header block

Every screen SHALL begin with a breadcrumb, a headline and a subheadline, in that order, and the
space for all three SHALL be reserved whether or not a screen has anything to put in them. The
first row of content below therefore begins at the same position on every screen, so moving
between screens does not shift the control the operator is reaching for.

#### Scenario: Content starts at the same position on every screen

- **WHEN** an operator moves from one screen to another
- **THEN** the first row of content below the header block is at the same position

#### Scenario: An empty breadcrumb still occupies its space

- **WHEN** a screen has no breadcrumb
- **THEN** the space for it is reserved and the headline below it does not move up

### Requirement: Creating an object happens in a modal dialog

Every action that creates an object — a project, a load balancer, a node, a domain, an API key — SHALL
be presented as a modal dialog rather than as a form embedded in the screen behind it. A dialog
that fails SHALL remain open, keeping what the operator typed and showing the service's own reason
next to the field that caused it; the most common failure is a slug or a name that is already
taken, and the operator's next act is to edit exactly that field.

A dialog whose submission succeeded with warnings SHALL remain open until the operator dismisses
it, so the warnings are read.

#### Scenario: A rejected creation keeps the form

- **WHEN** a creation is refused by the service
- **THEN** the dialog stays open with the entered values and the service's reason

#### Scenario: The service's reason is shown verbatim

- **WHEN** the service refuses a request with a message naming the rule or the conflicting object
- **THEN** that message is displayed to the operator unaltered

#### Scenario: A warning is not dismissed on the operator's behalf

- **WHEN** a creation succeeds and the service returned a warning
- **THEN** the dialog remains open showing the warning until the operator closes it

### Requirement: Destructive actions are confirmed in a dialog the browser cannot suppress

Every action that deletes or revokes an object SHALL be confirmed first, in a dialog that names the
object and states the consequence, and SHALL NOT use a confirmation the browser allows a user to
suppress for the origin.

Row-level destructive actions SHALL be presented as icons carrying both a tooltip and an accessible
label, rather than as repeated text.

#### Scenario: A destructive action is confirmed before anything is sent

- **WHEN** an operator deletes or revokes an object
- **THEN** a dialog naming that object asks them to confirm before any request is made

#### Scenario: The confirmation cannot be turned off

- **WHEN** the operator's browser has been told to suppress the origin's dialogs
- **THEN** the confirmation is still shown

#### Scenario: An icon action is still announced

- **WHEN** a row's delete action is reached with a screen reader or hovered
- **THEN** it is announced and labelled with the action and its object

### Requirement: The console loads nothing from the network at runtime

Every asset the console needs — fonts, icons, stylesheets and scripts — SHALL be bundled at build
time and served from the service binary. The console SHALL NOT request any asset from a host other
than the one serving it, because the binary runs where no such request can succeed.

Where the component library resolves an unregistered asset by fetching it remotely, every such asset
SHALL be registered ahead of use.

#### Scenario: The console renders without external access

- **WHEN** the console is loaded on a host with no route off its own network
- **THEN** every font, icon and style renders as intended and no request leaves the origin

#### Scenario: An unregistered icon is not a runtime fetch

- **WHEN** an icon is used in a screen
- **THEN** it is registered with the component library at startup

### Requirement: The console presents one theme

The console SHALL present a single dark theme and SHALL NOT offer a light theme or a theme control.

#### Scenario: No theme control is offered

- **WHEN** an operator uses the console
- **THEN** there is no setting that changes its theme

### Requirement: The console shows a load balancer's challenge interactions

The load balancer screen SHALL present the load balancer's challenge interactions, most recent first,
showing for each when it was published and how long it stood, the domain names the alias served at
publication, the address it was published from and which form of the name was submitted, the
publishing key, whether and how often it was queried, and its outcome.

The panel SHALL offer a filter restricting it to interactions that were never queried, and SHALL
explain that such an interaction indicates a customer `_acme-challenge` record that was never
published.

#### Scenario: A successful renewal is visible after the fact

- **WHEN** a value was published, queried and withdrawn
- **THEN** the panel lists it as validated with its query count

#### Scenario: A missing customer record is identifiable

- **WHEN** an operator restricts the panel to interactions never queried
- **THEN** only publications with no observations are listed

#### Scenario: An empty result is explained

- **WHEN** the never-queried filter matches nothing
- **THEN** the panel states that every publication was queried rather than showing an empty table

### Requirement: The console edits node addresses and not node labels

The load balancer screen SHALL offer editing of a node's addresses, SHALL NOT offer editing of
its label, and SHALL state that a DNS plan already published by the customer continues to name the
previous address until they republish. It SHALL refresh the load balancer's DNS plan after an address
changes.

#### Scenario: Addresses are editable in place

- **WHEN** an operator edits a node's addresses
- **THEN** the node keeps its label and the load balancer's DNS plan is re-read

#### Scenario: The label is not offered

- **WHEN** an operator opens the node editor
- **THEN** no control changes the node's label

### Requirement: The API key dialog can pin restrictions to the load balancer's node addresses

The API key creation dialog SHALL offer a single control that fills the key's source-address
restrictions with the addresses of the load balancer's own nodes, one host-sized block per address, and SHALL
display the addresses it is pinning. Turning the control off SHALL remove only the entries it added
and SHALL leave restrictions the operator entered by hand.

Where the load balancer has no node addresses, the dialog SHALL say so instead of offering the control.

#### Scenario: Restrictions are pinned in one action

- **WHEN** an operator enables the control on a load balancer whose nodes have addresses
- **THEN** the key's restrictions are set to those addresses as host-sized blocks

#### Scenario: Hand-entered restrictions survive

- **WHEN** an operator enables the control, adds a further block by hand, and disables the control
- **THEN** the hand-entered block remains

#### Scenario: A load balancer with no addresses is explained

- **WHEN** the load balancer has no node addresses
- **THEN** the dialog states that there is nothing to pin the key to

## MODIFIED Requirements

### Requirement: The console presents the inventory as a navigable hierarchy

The console SHALL let an authorized user browse projects, the load balancers of a project,
and the nodes and domains of a load balancer, and SHALL let an authorized user create,
edit and remove each of them subject to their role.

Every screen SHALL be laid out within one page container of fixed maximum width, so that a table
does not change column widths with the size of the window.

#### Scenario: Operator navigates to a domain

- **WHEN** a signed-in user opens a project and then one of its load balancers
- **THEN** the load balancer's node addresses and its domains are shown

#### Scenario: Controls reflect the user's role

- **WHEN** a user without permission to change a project views it
- **THEN** the actions that would change it are not offered

#### Scenario: Layout is stable across window sizes

- **WHEN** the console is viewed on a window wider than the page container
- **THEN** the content stays at the container's width and remains centred
