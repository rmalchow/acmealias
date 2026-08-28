## ADDED Requirements

### Requirement: A person awaiting approval is told they are being reviewed

The console SHALL show a person whose account awaits approval that their registration is being
reviewed, and SHALL set an expectation of when they will hear.

It SHALL NOT describe them as suspended, and SHALL NOT offer to take payment — no amount of money
changes the state they are in.

#### Scenario: The screen says what is happening

- **WHEN** a person whose account awaits approval signs in
- **THEN** they are told their registration is under review, and are not told their account is
  suspended

#### Scenario: Nothing is for sale

- **WHEN** that screen is shown
- **THEN** it offers no way to pay

### Requirement: Staff can see who is waiting and decide

The staff console SHALL list the accounts awaiting approval, each with the address that registered,
when it registered, and whether that address is verified.

A member of staff SHALL be able to approve or refuse each one. A refused registration SHALL be
distinguishable from one nobody has yet reached, and SHALL NOT be presented for review again.

#### Scenario: The queue shows the waiting

- **WHEN** a member of staff opens the review queue
- **THEN** every account awaiting approval is listed with its address and when it registered

#### Scenario: A refusal is final until somebody revisits it

- **WHEN** a registration is refused
- **THEN** it leaves the queue and is recorded as refused rather than as unreviewed

### Requirement: The staff console reports a backlog left by a configuration change

Where approval is no longer required and accounts are still awaiting it, the staff console SHALL say
so and SHALL offer to approve them together.

#### Scenario: The stranded are visible

- **WHEN** approval is not required and accounts await it
- **THEN** the queue reports that they are waiting for a gate that no longer applies
