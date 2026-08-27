## ADDED Requirements

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
