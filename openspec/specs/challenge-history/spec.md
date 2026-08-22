# challenge-history Specification

## Purpose
TBD - created by archiving change rebuild-console-and-add-interaction-history. Update Purpose after archive.
## Requirements
### Requirement: Every published challenge is recorded durably

The service SHALL record a durable interaction when a challenge value is published, independent of
the live challenge record, and SHALL retain it after the live record is deleted.

The record SHALL carry the alias label, the challenge name, the domain names served by that alias at
the time of publication, the load balancer **and the project** it belonged to, the identifier of the
API key that published it, the client address it was published from, which form of the name the
client submitted, and a cryptographic digest of the value. It SHALL NOT store the challenge value
itself.

The recorded project SHALL be the one the alias belonged to at publication, and SHALL NOT change
when the alias or its load balancer is later moved to another project.

The record SHALL NOT reference the alias, domains or load balancer in a way that causes it to
be deleted or made unreadable when any of them is deleted.

One interaction SHALL be recorded per publication. Republishing a value that is already live at the
same alias SHALL reuse the existing interaction and SHALL NOT alter its publication time.

#### Scenario: Record outlives the live challenge

- **WHEN** a challenge is published and later withdrawn
- **THEN** the live record is removed and the interaction remains

#### Scenario: Record is self-contained

- **WHEN** the domain, alias or load balancer described by an interaction is deleted
- **THEN** the interaction is still readable and still reports their names as they were at
  publication

#### Scenario: The value is not retained

- **WHEN** an interaction is read
- **THEN** it contains a digest of the challenge value and not the value

#### Scenario: The project is recorded alongside the load balancer

- **WHEN** an interaction is read
- **THEN** it reports the project it belonged to at publication, not only its load balancer

#### Scenario: A move does not rewrite recorded interactions

- **WHEN** a load balancer is moved to another project after an interaction was recorded
- **THEN** that interaction still reports the project it was published under

#### Scenario: The submitted form of the name is recorded

- **WHEN** a client publishes using the alias name rather than the challenge name
- **THEN** the interaction records which of the two forms was submitted

### Requirement: DNS query observations are attributed without datastore access on the query path

The service SHALL count DNS queries it answers with at least one challenge value, and SHALL
attribute those counts to interactions, without performing any datastore operation while handling a
query.

Counts SHALL be accumulated in memory, keyed by alias label, and applied periodically. Each batch
SHALL carry the window it was accumulated over, and that window SHALL be determined by the
accumulation, not by the moment the batch is applied.

An observation SHALL be attributed to every interaction at that alias which was published before the
window ended and was not withdrawn before the window began. Attribution SHALL NOT be restricted to
interactions that are still live when the batch is applied.

A batch that cannot be applied SHALL be retained, together with the start of its window, and applied
later.

#### Scenario: Query handling does not touch the datastore

- **WHEN** the datastore is unavailable and DNS queries are answered from the in-memory view
- **THEN** the queries are still answered and their counts accumulate

#### Scenario: Counts are applied after the datastore returns

- **WHEN** the datastore becomes available again
- **THEN** the retained counts are applied to the affected interactions over their original window

#### Scenario: A short-lived validation is credited

- **WHEN** a value is published, queried and withdrawn entirely between two applications of the
  counts
- **THEN** its interaction receives the observation

#### Scenario: One query serves several concurrent authorizations

- **WHEN** two values are live at one alias and a single query is answered returning both
- **THEN** both interactions receive the observation

#### Scenario: Only answered queries count

- **WHEN** a query for a known alias is answered with no value, or a query names an unknown alias
- **THEN** no interaction is modified

An observation that cannot be attributed to any interaction SHALL be counted in an aggregate metric,
carrying no identifier of the alias or domain as a label value, and SHALL NOT be reported only as a
log line.

#### Scenario: Unmatched observations are discarded

- **WHEN** counts are applied for an alias label that no longer resolves
- **THEN** they are discarded and the remaining labels in the batch are still applied

#### Scenario: Unattributed observations are countable

- **WHEN** observations are applied and some match no interaction, whether because the label no
  longer resolves or because no interaction at that alias covers the window
- **THEN** they are counted in an aggregate metric that an alert can be built on

### Requirement: Each interaction is classified by outcome

The service SHALL report exactly one outcome per interaction, derived when the interaction is read
rather than stored, from whether it has been withdrawn and whether it has ever been queried:

- **validated** — withdrawn, having been queried at least once
- **unqueried** — withdrawn, never queried
- **live** — not withdrawn, queried at least once
- **pending** — not withdrawn, never queried

An outcome SHALL follow its inputs: an observation applied after an interaction was first read SHALL
change the reported outcome accordingly.

#### Scenario: Successful validation

- **WHEN** a published value is queried and then withdrawn
- **THEN** its interaction reports the outcome validated

#### Scenario: Nobody looked

- **WHEN** a published value is withdrawn having never been queried
- **THEN** its interaction reports the outcome unqueried

#### Scenario: A validation in progress is distinguishable

- **WHEN** a value is still published
- **THEN** its interaction reports live if it has been queried and pending if it has not

#### Scenario: A late observation corrects the outcome

- **WHEN** an interaction read as unqueried subsequently receives an observation for a window it
  overlaps
- **THEN** it is thereafter read as validated

### Requirement: A publication that expires without withdrawal is closed

The service SHALL close an interaction whose challenge value expired without the publisher
withdrawing it, SHALL distinguish that ending from a withdrawal by the client, and SHALL retain any
query count the interaction accumulated, so that abandonment and non-observation remain separately
visible.

#### Scenario: Publisher never returned

- **WHEN** a published value expires and is swept without a withdrawal
- **THEN** its interaction is closed and reported as abandoned rather than remaining open

#### Scenario: Abandonment does not erase observations

- **WHEN** an abandoned interaction had been queried
- **THEN** its query count is retained

### Requirement: Interactions are readable per load balancer

The service SHALL expose the interactions of one load balancer, most recent publication first,
filtered and paged on the service side with the number of matching rows reported alongside the page,
and readable by every role that may read the inventory.

The filter SHALL match a substring of the alias label, the challenge name, the recorded domain
names, the publishing key identifier or the client address, and SHALL additionally offer restriction
to interactions that were never queried.

#### Scenario: Never-queried interactions can be listed alone

- **WHEN** the interactions of a load balancer are read restricted to those never queried
- **THEN** only interactions with no observations are returned

#### Scenario: History survives its subject

- **WHEN** the interactions of a load balancer are read after a domain they name has been deleted
- **THEN** they are still returned with that domain name

#### Scenario: Read-only role may read interactions

- **WHEN** a user holding only the read-only role reads a load balancer's interactions
- **THEN** the interactions are returned

### Requirement: Cleanup without a matching interaction is not an error

The service SHALL tolerate a withdrawal for which no live interaction exists, without failing the
request and without creating one retroactively. A withdrawal SHALL close every live interaction for
that value at that alias.

#### Scenario: Cleanup after data loss

- **WHEN** a withdrawal arrives for a value whose interaction is absent
- **THEN** the request succeeds and no interaction is created

#### Scenario: Nothing is left live

- **WHEN** more than one live interaction exists for one value at one alias
- **THEN** a withdrawal closes all of them

