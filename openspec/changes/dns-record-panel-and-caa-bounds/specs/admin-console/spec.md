## RENAMED Requirements

- FROM: `### Requirement: CAA is reported by what it permits, not by whether it exists`
- TO: `### Requirement: CAA is reported by how much issuance it leaves open`

- FROM: `### Requirement: A domain's delegation is checked and reported without being asked for`
- TO: `### Requirement: A domain's records are stated without being asked for, and checked on request`

## ADDED Requirements

### Requirement: A project states its DNS records as records

The console SHALL show, for a project, the records its domains need published: the
delegation for each distinct challenge name, and the CAA set for each distinct
registrable name. It SHALL show no other record type, since no other record is
this service's to ask for.

Each record SHALL be shown as a complete zone-file line — owner name, TTL, type
and rdata — rather than as rdata alone, and SHALL name the domains it covers where
one record serves several. Beside it the console SHALL show what a check found at
that name, or that no check has been run.

Where the recommended record contains a value the service cannot know, the console
SHALL render a placeholder that cannot be mistaken for a real value, and SHALL say
where the customer can obtain the real one.

#### Scenario: The records are shown before any check

- **WHEN** a project with domains is opened
- **THEN** every delegation and CAA record it needs is printed in full, each stating that it has not been checked

#### Scenario: One record covers a wildcard and its base

- **WHEN** a project holds a name and its wildcard
- **THEN** one delegation record is shown, naming both domains as covered

#### Scenario: The account value is a placeholder, with a way to resolve it

- **WHEN** the CAA record recommends limiting issuance to an ACME account
- **THEN** the account value is rendered as a placeholder and the console states where the customer's own value can be read

## MODIFIED Requirements

### Requirement: CAA is reported by how much issuance it leaves open

The console SHALL report a domain's CAA state as how far the record set bounds
who may obtain a certificate for the name, and not as whether issuance succeeds.

A delegated domain permits this service to complete challenge validation for that
name and for its wildcard, which is the service. The console SHALL therefore
report a CAA set naming an authority without limiting issuance to a specific ACME
account as leaving issuance open, distinctly from one that limits it; SHALL report
the absence of any CAA record as the least bounded state; and SHALL report a set
that does not authorise a configured authority as a fault, because it prevents
issuance however the delegation is configured. The absence of a record and a
blocking record SHALL remain distinguishable, since one calls for publishing a
record and the other for changing one.

Where the console reports issuance as limited to an account, it SHALL NOT assert
whose account it is, which is not observable.

The console SHALL show no CAA indicator at all where the deployment has no
configured certificate authority identifier, since nothing about CAA is then
asserted for the domain.

#### Scenario: No CAA record is the least bounded state

- **WHEN** a domain publishes no CAA record
- **THEN** it is reported as leaving issuance open to any authority and any account

#### Scenario: An authority without an account is distinct from one with

- **WHEN** a domain publishes a set naming a configured authority and no account, and another publishes the same set limited to an account
- **THEN** the first is reported as leaving issuance open and the second as limited, each with its own explanation

#### Scenario: An account limit is reported without claiming whose it is

- **WHEN** a domain publishes a set limiting issuance to an ACME account
- **THEN** it is reported as limited to one account, and nothing states that the account belongs to the reader

#### Scenario: A blocking set is a fault, and distinct from an absent one

- **WHEN** a domain publishes a CAA set that does not name a configured authority
- **THEN** it is presented as a fault, and not as the same state as publishing no record at all

#### Scenario: No configured issuer means no CAA indicator

- **WHEN** the deployment has no configured certificate authority identifier
- **THEN** no domain row carries a CAA indicator

### Requirement: A domain's records are stated without being asked for, and checked on request

The console SHALL state, for each domain of a project, the records that domain
needs published, and SHALL do so when the screen is opened, since what a record
must contain is knowable without querying anything.

The console SHALL NOT query DNS unless asked to. Whether those records are in fact
published SHALL be reported only after the customer requests a check, and until
one has been requested every record SHALL read as not checked.

Each record type SHALL be reported separately, on the domain's own row, as an
indicator carrying that record type's name and coloured by what its state means.
The console SHALL NOT reduce a domain's record types to a single verdict, and
SHALL NOT label an indicator with its verdict, so that a row's layout does not
change as its state does.

A record type that does not apply to a domain SHALL have no indicator at all,
rather than an indicator in an unchecked state.

The console SHALL distinguish, for a domain's delegation: that it has not been
checked, that no record is published, that a record is published but does not
match what is required, that it matches, and that the check could not be
completed.

A record that is absent and a record that is present but wrong SHALL NOT be
reported alike, because the actions they call for are different, and the console
SHALL present the absent record as a step not yet taken and the wrong record as a
fault, since a wrong record will never begin working on its own.

A state meaning that the console has not looked — not yet checked, lookup failed,
or nothing to ask about — SHALL be presented neutrally and SHALL NOT be presented
as a fault in the customer's configuration.

Where no resolver is available, every domain SHALL read as not checked, and this
SHALL NOT be presented as a failure.

A check SHALL NOT be stored, and a slow or failed lookup SHALL degrade only the
domain it concerns.

The console SHALL offer a recorded re-check on every domain row, in every state.

#### Scenario: A customer sees what to publish without asking

- **WHEN** a project with domains is opened
- **THEN** the exact record each name needs is shown, and DNS is not queried

#### Scenario: The state of a record is reported on request

- **WHEN** the customer requests a check
- **THEN** each record reports what is published at its name, and each domain row reports its delegation state

#### Scenario: Record types are reported separately and named

- **WHEN** a domain's row is rendered
- **THEN** it carries one indicator per applicable record type, each naming the record type rather than the verdict

#### Scenario: Absent is distinguished from unchecked

- **WHEN** a domain's delegation is absent
- **THEN** it reports that no record is published, and not that it has never been checked

#### Scenario: Wrong is distinguished from absent, and more severely

- **WHEN** a domain's delegation resolves to something other than what is required
- **THEN** it is reported as a fault, distinctly from a delegation that is merely absent

#### Scenario: A wildcard and its apex share one delegation

- **WHEN** a project holds a name and its wildcard, which share one challenge name
- **THEN** both report the state of the one delegation they share

#### Scenario: No resolver is not a failure

- **WHEN** the deployment has no verification resolver
- **THEN** every domain reads as not checked and no error is shown

#### Scenario: Re-checking is available on every row

- **WHEN** a domain's delegation is published, seen and healthy
- **THEN** the row still offers a recorded re-check
