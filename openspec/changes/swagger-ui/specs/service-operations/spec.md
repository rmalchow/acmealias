## ADDED Requirements

### Requirement: The published contract is readable by a person

The service SHALL serve a browsable rendering of its public API document at a stable path.

The rendering SHALL require no credentials, matching the document it renders.

#### Scenario: A stranger reads the API

- **WHEN** an unauthenticated person opens the documentation path
- **THEN** the public API document is presented in a readable form

### Requirement: The documentation renders without reaching any other host

Every asset the documentation page needs SHALL be served by this service.

#### Scenario: An isolated deployment documents itself

- **WHEN** the page is opened on a deployment with no route to the internet
- **THEN** it renders completely

### Requirement: A page that can send requests says what it will act on

Where the documentation page can send authenticated requests, it SHALL name the account those
requests will act in, before any is sent.

#### Scenario: The reader knows whose data is at stake

- **WHEN** a signed-in person opens the documentation page
- **THEN** the account their requests would act in is named on the page
