# admin-console Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
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

### Requirement: The console validates domain input against the wildcard rules

The domain editor SHALL apply the same validation as the service to wildcard and specific
names, report the reason for a rejection, and state that a wildcard covers exactly one label
and does not cover its own base name.

The editor SHALL offer one control that adds the typed name's counterpart in the same action —
the wildcard of a name that is not one, the base name of a name that is — naming in its label
the exact name it would add, so the control reads differently for `example.com` than for
`*.example.com`.

The control SHALL be off whenever the editor opens, and SHALL NOT be preselected on the
operator's behalf, because the counterpart carries a wildcard address record over the customer's
whole zone and a certificate covering every host beneath the domain. The editor SHALL state what
the counterpart covers alongside the control.

A refusal of either name SHALL be reported as the service reported it, and the editor SHALL NOT
report a partial success.

#### Scenario: Invalid wildcard is reported before submission

- **WHEN** a user enters `*.*.example.com`
- **THEN** the editor reports the rule that rejects it

#### Scenario: Server remains the authority

- **WHEN** a value that passes client-side validation is rejected by the service
- **THEN** the service's reason is shown to the user

#### Scenario: The control names the wildcard it would add

- **WHEN** an operator types `example.com`
- **THEN** the control offers to add `*.example.com` and names it

#### Scenario: The control mirrors for a wildcard

- **WHEN** an operator types `*.example.com`
- **THEN** the control offers to add the base name `example.com` and names it

#### Scenario: The control opens off

- **WHEN** an operator opens the domain editor, having used the control previously
- **THEN** the control is off and submitting without touching it creates only the typed name

#### Scenario: A refused pair is reported as a refusal

- **WHEN** the service refuses a creation because the counterpart broke a rule
- **THEN** the dialog stays open showing the service's reason, and reports no domain as created

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

### Requirement: The account picker always offers somewhere to work

The console SHALL present the accounts a user may act in, and that list SHALL
always include their personal account. The personal account SHALL be presented as
a distinct entry rather than as one named account among the team ones.

The picker SHALL be searchable, and SHALL be present but inert for a user with a
single account, so that the navigation does not change shape when a second
account is obtained. It SHALL be accompanied by an action that obtains a new team
account, which SHALL lead to the same place as any other offer to obtain one.

A suspended team account SHALL appear in the picker for its owner rather than
being omitted, since the owner is the only person who can restore it.

#### Scenario: A new user has somewhere to go

- **WHEN** a user with no team accounts opens the picker
- **THEN** their personal account is offered, and the picker offers nothing to switch to

#### Scenario: Obtaining an account is reachable from the picker

- **WHEN** a user opens the action beside the picker
- **THEN** they are told what a team account is and offered a way to buy one

#### Scenario: A suspended account is reachable by its owner

- **WHEN** the owner of a suspended team account opens the picker
- **THEN** it is listed and shown as suspended

#### Scenario: A suspended account is not selectable by a member

- **WHEN** a member who is not the owner attempts to select it
- **THEN** selection is refused and the reason is that the account is suspended, not that they lack permission

### Requirement: Navigation follows the selected account

The console's navigation SHALL reflect the capabilities of the selected account,
and SHALL change when the selection changes without requiring a reload.

Navigation SHALL list only the screens the selected account has. Where an account
kind lacks capabilities, the console SHALL offer a single affordance naming what
would unlock them, rather than listing the absent screens.

Within a screen, a control the caller merely lacks authority for SHALL remain
visible and disabled, stating why.

Where both refusals can occur, they SHALL NOT share wording: a refusal a grant
would lift and a refusal only a different kind of account would lift do not share
a remedy.

#### Scenario: A personal account's navigation names the remedy once

- **WHEN** a personal account is selected
- **THEN** the navigation offers its projects and a single way to obtain a team account, and does not list the screens a team account would have

#### Scenario: A control inside a screen is not removed

- **WHEN** a caller lacks the grant for a control on a screen the account does have
- **THEN** the control is shown, disabled, and states why

#### Scenario: The two refusals do not share wording

- **WHEN** a capability is unavailable because of the account kind
- **THEN** it does not say the caller lacks permission

#### Scenario: Switching accounts changes the navigation

- **WHEN** the selection changes from a personal account to a team account
- **THEN** the navigation updates without a reload

#### Scenario: Lacking a grant does not remove a control

- **WHEN** a user without the necessary grant views a screen whose account kind supports it
- **THEN** the control is shown, disabled, and states why

### Requirement: Settings holds what administers the account

The console SHALL provide a Settings screen holding the administration of the
account itself — its members, the roles they hold, and its credits — separately
from the inventory the account contains.

For a suspended team account, its owner SHALL be shown the balance and the means
to add to it, and nothing else.

#### Scenario: An owner can restore a suspended account

- **WHEN** the owner selects a suspended team account
- **THEN** they are shown the balance and a way to add credit, and no inventory screens

#### Scenario: Credit is added and the account resumes

- **WHEN** the owner completes a checkout for one of the offered amounts
- **THEN** the balance reflects it and the account becomes usable without a new session

### Requirement: A domain's delegation is checked and reported without being asked for

The console SHALL check, for each domain of a load balancer, whether the records
that domain needs are published, and SHALL do so when the screen is opened rather
than only on request.

Each record type SHALL be reported separately. The console SHALL NOT reduce them
to a single verdict.

The console SHALL distinguish, for a domain's delegation: that it has not been
checked, that no record is published, that a record is published but does not
match what is required, that it matches, and that the check could not be
completed.

In particular, a record that is absent and a record that is present but wrong
SHALL NOT be reported alike, because the actions they call for are different.

Where no resolver is available, every domain SHALL read as not checked, and this
SHALL NOT be presented as a failure.

A check SHALL NOT be stored, and a slow or failed lookup SHALL degrade only the
domain it concerns.

#### Scenario: A customer sees the state without asking

- **WHEN** a load balancer with domains is opened
- **THEN** each domain reports whether its delegation is published

#### Scenario: Absent is distinguished from unchecked

- **WHEN** a domain's delegation is absent
- **THEN** it says no record is published, and not that it has never been checked

#### Scenario: Wrong is distinguished from absent

- **WHEN** a domain's delegation resolves to something other than what is required
- **THEN** it says a record is published but does not match, and not that none exists

#### Scenario: A wildcard and its apex share one delegation

- **WHEN** a load balancer holds a name and its wildcard, which share one challenge name
- **THEN** both report the state of the one delegation they share

#### Scenario: No resolver is not a failure

- **WHEN** the deployment has no verification resolver
- **THEN** every domain reads as not checked and no error is shown

### Requirement: CAA is reported by what it permits, not by whether it exists

The console SHALL report a domain's CAA state as what the record set permits.
The absence of a CAA record SHALL NOT be presented as a problem, and a CAA set
that does not authorise this service SHALL be presented as one.

#### Scenario: No CAA record is not a warning

- **WHEN** a domain publishes no CAA record
- **THEN** it is reported as carrying no restriction, and not as misconfigured

#### Scenario: A blocking CAA set is surfaced

- **WHEN** a domain's CAA set does not authorise this service
- **THEN** it is reported as blocking issuance

### Requirement: The console explains how to fix what it reports

Where a domain's state requires action, the console SHALL offer an explanation of
what the state means and what to publish to resolve it.

That explanation SHALL contain the values for the domain in question rather than
placeholders.

#### Scenario: The fix names the customer's own record

- **WHEN** a customer opens the explanation for an unpublished delegation
- **THEN** it shows the exact record for that domain, including its own alias

#### Scenario: A mismatched record shows what was found

- **WHEN** a customer opens the explanation for a delegation that does not match
- **THEN** it shows what was found alongside what is required

#### Scenario: A blocking CAA is explained as a CAA fix

- **WHEN** a customer opens the explanation for a blocking CAA set
- **THEN** it describes authorising an issuer rather than publishing a delegation

### Requirement: Each kind of key is managed where it belongs

The console SHALL offer project keys on a project's own screen, and system keys in a team account's
settings.

#### Scenario: Project keys sit with the project

- **WHEN** a person opens a project
- **THEN** they can create and revoke that project's keys there

#### Scenario: System keys sit with the roster

- **WHEN** a person opens a team account's settings
- **THEN** they can create and revoke system keys there, beside roles and members

### Requirement: The address limit explains itself before it is chosen

Where a project has no node addresses, the console SHALL say so where the address limit is offered,
rather than presenting a control that would refuse everything.

#### Scenario: A project with no nodes

- **WHEN** the address limit is offered on a project holding no nodes
- **THEN** the console reports that there are no addresses to permit yet

### Requirement: The console manages an account's invitations

The console SHALL let a member of the account's unrestricted role — its owner and its administrators —
invite a person by email address, and revoke or re-send an outstanding invitation. It SHALL NOT ask for
a role when inviting.

Outstanding invitations SHALL be shown to every member of the account alongside its members, reporting
the invited address and when the invitation was issued.

A member who holds no role SHALL be listed as a member, marked as having none, and the console SHALL
offer assigning one to a member of the unrestricted role.

The console SHALL NOT offer these actions to a member who is not in the unrestricted role.

#### Scenario: An administrator invites somebody

- **WHEN** a member of the unrestricted role enters an address
- **THEN** an invitation is issued and appears among the account's outstanding invitations

#### Scenario: Inviting does not ask for a role

- **WHEN** a member of the unrestricted role opens the invitation form
- **THEN** it asks for an address and offers no choice of role

#### Scenario: Every member sees who is pending

- **WHEN** a member holding no grants opens the account's members
- **THEN** outstanding invitations are listed alongside the members

#### Scenario: A member with no role is visible as one

- **WHEN** a person who has accepted an invitation has not yet been given a role
- **THEN** they are listed among the members, marked as holding no role, and a role can be assigned to them

#### Scenario: Other members are not offered the actions

- **WHEN** a member who is not in the unrestricted role opens the account's members
- **THEN** no action inviting, revoking or re-sending is offered, and the service refuses one if attempted directly

### Requirement: The console renders the invitation offer to a user who belongs to nothing

The console SHALL render the invitation offer for a session with no account selected, naming the account
and requiring an explicit action to accept. It SHALL NOT name a role, because the invitation offers
none.

Where no user holds the invited address the console SHALL offer registration; where one does it SHALL
offer sign-in. Where the authenticated user is not the invited one, the console SHALL report that the
invitation is not for them and SHALL show the invited address only partially masked.

An expired or revoked invitation SHALL be reported as such, with no action offered other than to ask the
account for a new one.

#### Scenario: The offer states what is being joined

- **WHEN** an invited person opens the link
- **THEN** the account is named, and accepting requires an explicit action

#### Scenario: A person without a user is offered registration

- **WHEN** an invited person with no user opens the link
- **THEN** registration is offered, and after it the offer is presented

#### Scenario: The wrong signed-in user is told so

- **WHEN** a user who is not the invited one opens the link while signed in
- **THEN** the console reports the invitation is not for them and shows the invited address partially masked

#### Scenario: The console continues into the joined account

- **WHEN** an invited person accepts
- **THEN** the console continues in the account they joined rather than returning to the empty state

### Requirement: An account a member holds no role in explains itself

The console SHALL, for a member of an account who holds no role in it, report on every account-scoped
screen that they hold no role and that an administrator must assign one, rather than presenting the
screen's ordinary empty state.

The account SHALL remain selectable by such a member.

#### Scenario: An empty account is not mistaken for an empty product

- **WHEN** a member holding no role opens a screen listing the account's inventory
- **THEN** the console reports that they hold no role in the account and that an administrator must assign one

#### Scenario: Selection still works

- **WHEN** a member holding no role selects the account
- **THEN** the selection succeeds

#### Scenario: Being given a role restores the ordinary screens

- **WHEN** a member holding no role is assigned one
- **THEN** the account-scoped screens render what that role permits

### Requirement: A project is configured on one screen

The console SHALL present a project's nodes, domains, plan, keys and interactions on a single screen.

#### Scenario: One step from the list to the configuration

- **WHEN** a person opens a project from the project list
- **THEN** they reach its configuration without passing through an intermediate screen

### Requirement: The project list reports each project's size

The console SHALL show, for each project in the list, how many nodes and domains it holds, and SHALL
describe a project with neither as empty.

#### Scenario: The list is informative without being opened

- **WHEN** a person looks at their project list
- **THEN** each row says how many nodes and domains that project holds

