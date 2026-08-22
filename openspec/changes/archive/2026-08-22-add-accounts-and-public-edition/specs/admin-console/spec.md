## ADDED Requirements

### Requirement: The console makes the acting account unambiguous

In an edition where a person may belong to more than one account, every screen SHALL show which
account it is acting in, and SHALL offer a way to change it. Changing the acting account SHALL
return the person to a screen of that account rather than leaving them on one they can no longer
see.

In an edition with a single account, no screen SHALL name it.

#### Scenario: The acting account is visible

- **WHEN** a person who belongs to more than one account opens any screen
- **THEN** the account it acts in is shown

#### Scenario: Switching does not strand the person

- **WHEN** a person changes the acting account while on a screen of a specific object
- **THEN** they are taken to a screen of the newly selected account

### Requirement: A new customer reaches a verified domain without help

The public edition SHALL lead a person who has just confirmed their address to a first verified
domain: it SHALL show the exact delegation record to publish, SHALL let them check it, and SHALL
report the result plainly enough to act on.

#### Scenario: The record to publish is shown exactly

- **WHEN** a person adds their first domain
- **THEN** the record to publish is shown in the form it must be published in

#### Scenario: The check reports what to do next

- **WHEN** the delegation has not been published or is wrong
- **THEN** the console says what was found and what was expected

### Requirement: Plan and payment state are visible to the account, and only to it

The public edition SHALL show an account its plan, its quota consumption and its subscription
state, and SHALL show them only to members of that account.

An account approaching or exceeding a quota SHALL be told which quota and what it prevents.

#### Scenario: Quota consumption is visible before it bites

- **WHEN** an account is near a quota
- **THEN** the console reports which quota and what will be refused

#### Scenario: Billing state is confined to the account

- **WHEN** a member of one account views plan or payment state
- **THEN** nothing of another account's plan or payment is shown
