## REMOVED Requirements

### Requirement: The console shows the required DNS records and their live status

**Reason**: The panel restated, one row per record, what the domain table above it
already said one row per domain — and added address records whose only possible
finding is that a customer's DNS disagrees with the node addresses that same
customer typed into the panel above. The diagnostic half of it becomes the status
dots on the domain row (design G1, G3); the records a customer actually has to
publish become the per-record dialogs, which can print a complete record with that
domain's own values rather than making somebody assemble it from four columns
(G7).

**Migration**: The records a customer must publish are carried by the domain
itself — see `service-inventory`, "A domain carries the records that must be
published for it" — and the console prints them, complete and ready to copy, in
the per-record explanation opened from that domain's indicator. The plan read,
its zone-file rendering and the administrative subcommand that printed them are
removed by the same change, so nothing renders the set anywhere. `Run
verification` is replaced by the per-row **Check now**, now offered on every row
(G18). Address records are not shown anywhere and have no replacement.

## MODIFIED Requirements

### Requirement: A domain's delegation is checked and reported without being asked for

The console SHALL check, for each domain of a project, whether the records that
domain needs are published, and SHALL do so when the screen is opened rather than
only on request.

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

#### Scenario: A customer sees the state without asking

- **WHEN** a project with domains is opened
- **THEN** each domain reports on its own row whether its delegation is published

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

### Requirement: CAA is reported by what it permits, not by whether it exists

The console SHALL report a domain's CAA state as what the record set permits.

A CAA record set that authorises this service and the absence of any CAA record
SHALL be presented alike, because both permit issuance and both call for no
action, while the console SHALL still state which of the two is the case. A CAA
set that does not authorise this service SHALL be presented as a fault, because it
prevents issuance however the delegation is configured.

The console SHALL show no CAA indicator at all where the deployment has no
configured certificate authority identifier, since nothing about CAA is then
asserted for the domain.

#### Scenario: No CAA record is not a warning

- **WHEN** a domain publishes no CAA record
- **THEN** it is reported as permitting issuance, and not as misconfigured

#### Scenario: Permitting states are distinguishable in words

- **WHEN** a domain publishes no CAA record and another publishes a set naming this service
- **THEN** both are presented as calling for no action, and each states which case it is

#### Scenario: A blocking set is a fault

- **WHEN** a domain publishes a CAA set that does not name this service
- **THEN** it is presented as a fault

#### Scenario: No configured issuer means no CAA indicator

- **WHEN** the deployment has no configured certificate authority identifier
- **THEN** no domain row carries a CAA indicator

## ADDED Requirements

### Requirement: Each record type carries its own explanation, with that domain's values

Every indicator on a domain's row SHALL open an explanation for that record type
alone, in every state including a healthy one.

The explanation SHALL state what the current state means, and where there is
something for the customer to publish it SHALL print the complete record with this
domain's own name and value and offer it for copying, rather than describing it in
prose or requiring it to be assembled from parts.

The explanation for a delegation SHALL print the delegation record. The explanation
for CAA SHALL print CAA entries covering both plain and wildcard issuance for every
configured issuer, and SHALL NOT offer a delegation record as the remedy for a
blocking CAA set.

The console SHALL NOT present, as a finding about the customer's configuration,
the diagnostic text of a lookup that failed.

#### Scenario: A healthy record still explains itself

- **WHEN** a domain's delegation is published and correct and its indicator is opened
- **THEN** the explanation states so and prints the record that is in effect

#### Scenario: The record is printed ready to copy

- **WHEN** a domain's delegation is absent and its indicator is opened
- **THEN** the delegation record for this domain is printed complete, with a control that copies it

#### Scenario: A blocking CAA is answered with CAA entries

- **WHEN** a domain's CAA set does not authorise this service and its indicator is opened
- **THEN** the explanation prints CAA entries for the configured issuers, covering plain and wildcard issuance, and prints no delegation record as the remedy

#### Scenario: One record type at a time

- **WHEN** a domain's CAA indicator is opened
- **THEN** the explanation shown concerns CAA, and does not open on another record type

#### Scenario: A failed lookup's diagnostics are not shown to the customer

- **WHEN** a lookup could not be completed and the indicator is opened
- **THEN** the explanation says the check could not be completed and does not print the lookup's error text as a finding

### Requirement: The console enables certificate observation per domain and collects a wildcard's test hostname

The console SHALL offer, per domain, a control that enables and disables
certificate observation for that domain, presented so that it is clear nothing
connects to the customer's machines until it is switched on.

Where the domain is a wildcard, the console SHALL require a concrete test hostname
before observation can be enabled, SHALL validate it against the same wildcard
rules the service enforces, and SHALL NOT suggest or prefill one.

The console SHALL show which hostname is being tested wherever that domain's
observed state is presented.

#### Scenario: The switch is off for a new domain

- **WHEN** a domain is created
- **THEN** its observation control reads as disabled

#### Scenario: A wildcard asks for a hostname

- **WHEN** observation is enabled for a wildcard domain
- **THEN** the console requires a concrete hostname covered by that wildcard before the change is submitted

#### Scenario: A non-wildcard asks for nothing further

- **WHEN** observation is enabled for a non-wildcard domain
- **THEN** the change is submitted without asking for a hostname

#### Scenario: The tested hostname is visible

- **WHEN** a wildcard domain's observed certificate state is presented
- **THEN** the hostname being tested is named

### Requirement: The console shows a domain's observed certificate state on its row

Where observation is enabled for a domain, the console SHALL carry an indicator for
it on that domain's row, alongside the indicators for its DNS records, coloured by
the band of the observed certificate's remaining lifetime and reporting the most
severe state across the domain's node addresses.

Where observation is disabled, the row SHALL carry no such indicator.

The indicator SHALL open an explanation listing every node address separately,
identified by node and address family, with what was observed at each: the
certificate's expiry and issuer, whether it covers the name tested, and, where the
handshake did not complete, that the address could not be examined and why.

An address that could not be examined SHALL NOT be presented as an address with no
certificate, and SHALL NOT displace the state of an address that was examined
successfully.

The explanation SHALL present the observation history for the domain on a team
account, and on a personal account SHALL state that history is a team-account
feature rather than omitting it silently or presenting it as unavailable.

#### Scenario: An unobserved domain has no certificate indicator

- **WHEN** a domain has observation disabled
- **THEN** its row carries no certificate indicator

#### Scenario: The row reports the worst address

- **WHEN** one node address serves a certificate expiring in ninety days and another one expiring in three days
- **THEN** the domain's indicator reports the state derived from the certificate expiring in three days

#### Scenario: Address families are distinguished

- **WHEN** a node serves one certificate over IPv4 and a different one over IPv6
- **THEN** the explanation shows both, attributed to their address families

#### Scenario: An unreachable address is named as unreachable

- **WHEN** the latest observation for one address records a failed handshake
- **THEN** the explanation says that address could not be examined and why, and does not say it has no certificate

#### Scenario: A domain covered by no certificate is surfaced on its own row

- **WHEN** the certificate observed at every node address does not cover the name tested
- **THEN** the domain's indicator reports the most severe state, on the domain's own row and not in a separate report

#### Scenario: History is shown on a team account

- **WHEN** a team account opens an observed domain's certificate explanation
- **THEN** the observations recorded for it are shown, most recent first

#### Scenario: History is named as withheld on a personal account

- **WHEN** a personal account opens an observed domain's certificate explanation
- **THEN** the latest observation per address is shown and the explanation states that history is part of a team account
