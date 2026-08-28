## ADDED Requirements

### Requirement: Certificate observation is enabled per domain and is off by default

The service SHALL observe a domain's certificate only when observation has been
enabled for that domain. A domain SHALL be created with observation disabled, and
the service SHALL NOT connect to any customer address on behalf of a domain whose
observation is disabled.

Disabling observation SHALL stop further handshakes and SHALL NOT delete the
observations already recorded.

#### Scenario: A new domain is not observed

- **WHEN** a domain is created
- **THEN** observation is disabled for it and no handshake is attempted

#### Scenario: Enabling starts observation

- **WHEN** observation is enabled for a domain
- **THEN** the domain becomes due for observation and is observed on the next pass

#### Scenario: Disabling stops observation and keeps history

- **WHEN** observation is disabled for a domain that has been observed
- **THEN** no further handshake is attempted for it and its recorded observations remain readable

### Requirement: A wildcard domain requires a concrete test hostname

The service SHALL require a test hostname before observation is enabled for a
wildcard domain, and SHALL refuse the enablement when none is supplied, because a
wildcard name cannot be presented as a TLS server name.

The test hostname SHALL be validated to be a name the wildcard covers, under the
same normalization and label rules the service already applies to domain names.
The service SHALL use it as the server name for every handshake made on that
domain's behalf, and SHALL evaluate the observed certificate against it.

The service SHALL NOT derive a test hostname on the customer's behalf.

A test hostname SHALL be retained when observation is disabled.

#### Scenario: Enabling a wildcard without a hostname is refused

- **WHEN** observation is enabled for `*.example.com` with no test hostname
- **THEN** the request is refused and observation stays disabled

#### Scenario: A hostname outside the wildcard is refused

- **WHEN** a test hostname of `www.other.example` is supplied for `*.example.com`
- **THEN** the request is refused

#### Scenario: A covered hostname is accepted and used

- **WHEN** a test hostname of `www.example.com` is supplied for `*.example.com` and observation is enabled
- **THEN** handshakes for that domain present `www.example.com` as the server name

#### Scenario: A non-wildcard domain needs no hostname

- **WHEN** observation is enabled for `shop.example.com` with no test hostname
- **THEN** the request is accepted and handshakes present `shop.example.com` as the server name

#### Scenario: The hostname survives being switched off

- **WHEN** observation is disabled and then enabled again for a wildcard domain that had a test hostname
- **THEN** the previously stored hostname is still in effect and is not asked for again

### Requirement: An observed domain is probed at every node address of its project, per address family

For each domain with observation enabled, the service SHALL attempt one TLS
handshake against each address of each node of that domain's project, treating
each address family as a separate attempt.

A node carrying both an IPv4 and an IPv6 address SHALL be probed twice, and the
two results SHALL be recorded and reported separately.

A node carrying only one family SHALL contribute one attempt.

A project with no nodes SHALL produce no attempts, and this SHALL NOT be recorded
as a failure.

#### Scenario: A dual-stack node is two observations

- **WHEN** an observed domain's project holds one node with an IPv4 and an IPv6 address
- **THEN** two observations are recorded for that domain, one per family

#### Scenario: Two nodes are probed independently

- **WHEN** an observed domain's project holds two nodes and one serves a renewed certificate while the other does not
- **THEN** the observations differ per node and both are retained

#### Scenario: A single-family node is one observation

- **WHEN** a node has an IPv4 address and no IPv6 address
- **THEN** one observation is recorded for it and no IPv6 attempt is made

#### Scenario: No nodes is not a failure

- **WHEN** an observed domain's project holds no nodes
- **THEN** no observation is recorded and nothing is reported as failing

### Requirement: A successful handshake records what the certificate says

When a handshake completes and a certificate is read, the service SHALL record the
certificate's validity start, its expiry, its issuer, its serial, and the names it
covers, together with the address and family probed, the server name presented,
and the time of the observation.

The service SHALL record whether the presented certificate covers the name that
was asked for, and whether its chain was trusted.

#### Scenario: The certificate is recorded

- **WHEN** a handshake completes against a node address
- **THEN** an observation records the certificate's expiry, issuer, serial and covered names

#### Scenario: Coverage of the asked-for name is recorded

- **WHEN** the certificate presented covers `example.com` and the server name asked for was `shop.example.com`
- **THEN** the observation records that the name asked for is not covered

### Requirement: The service evaluates the certificate rather than delegating to the transport

The service SHALL complete the handshake without letting the transport's
certificate verification abort it, and SHALL make every judgement about expiry,
name coverage and chain validity itself, from the certificate that was presented.

An expired certificate, and a certificate from an authority the service does not
trust, SHALL each be recorded as an observed certificate and SHALL NOT be recorded
as a failure to observe.

#### Scenario: An expired certificate is observed, not refused

- **WHEN** a node address presents an expired certificate
- **THEN** an observation records that certificate and its expiry, and does not report that the address could not be examined

#### Scenario: An untrusted chain is observed and noted

- **WHEN** a node address presents a certificate whose chain the service does not trust
- **THEN** an observation records the certificate and records that its chain was not trusted

### Requirement: A failed handshake is recorded as an observation carrying its error

The service SHALL record an observation carrying a classification of the failure
when an address refuses the connection, does not answer within the configured
timeout, rejects the handshake, or presents something that cannot be read as a
certificate.

The service SHALL NOT omit the row, and SHALL NOT report a failure to look as a
statement about the customer's deployment.

#### Scenario: A refused connection is recorded

- **WHEN** a node address refuses the connection
- **THEN** an observation is recorded for that address carrying the refusal

#### Scenario: A timeout is recorded

- **WHEN** a node address does not answer within the handshake timeout
- **THEN** an observation is recorded for that address carrying the timeout

#### Scenario: A failure is not an absent certificate

- **WHEN** the latest observation for an address records a failed handshake
- **THEN** the state reported for that address says the address could not be examined, and does not say that no certificate is installed

### Requirement: A domain's certificate state is derived from its latest observations

The service SHALL derive a domain's current certificate state from the most recent
observation for each of its node addresses.

The state per address SHALL be one of: not observed, could not be examined,
expired, or a remaining lifetime. A certificate that does not cover the name asked
for SHALL be reported as not covering it, whatever its remaining lifetime.

A single state for the domain SHALL be derived as the most severe state across its
addresses, in the order: not covering the name or expired, then remaining lifetime
shortest first, then observed and healthy. A state of "could not be examined" SHALL
NOT displace a state derived from an address that was successfully examined.

#### Scenario: The domain reports its worst address

- **WHEN** one address serves a certificate expiring in ninety days and another serves one expiring in three days
- **THEN** the domain's state is the one expiring in three days

#### Scenario: Uncovered outranks a long lifetime

- **WHEN** one address serves a long-lived certificate that does not cover the name asked for
- **THEN** the domain's state reports that the name is not covered

#### Scenario: An unexaminable address does not hide a real answer

- **WHEN** one address could not be examined and another was examined successfully
- **THEN** the domain's state is derived from the address that was examined, and the unexaminable address is still reported individually

#### Scenario: Every address is reported individually

- **WHEN** a domain's certificate state is read
- **THEN** the state of each node address is available separately, identified by node and address family

### Requirement: Remaining certificate lifetime is reported in bands

The service SHALL band a certificate's remaining lifetime so that a domain's state
can be reported without a client applying its own thresholds.

The bands SHALL be: more than twenty-eight days remaining; between seven and
twenty-eight days remaining; fewer than seven days remaining; and expired. A
certificate that does not cover the name asked for SHALL be reported in the same
band as an expired one.

#### Scenario: A fresh certificate is healthy

- **WHEN** the observed certificate has sixty days remaining
- **THEN** it is banded as more than twenty-eight days

#### Scenario: A certificate past its renewal point is approaching

- **WHEN** the observed certificate has twenty days remaining
- **THEN** it is banded as between seven and twenty-eight days

#### Scenario: A certificate about to lapse is urgent

- **WHEN** the observed certificate has three days remaining
- **THEN** it is banded as fewer than seven days

#### Scenario: An expired certificate is expired

- **WHEN** the observed certificate's expiry has passed
- **THEN** it is banded as expired

### Requirement: Nothing acts on an observation

An observation SHALL NOT change any answer the service gives, SHALL NOT gate any
request, and SHALL NOT cause a domain to be deactivated, an account to be
suspended, or any record to be withheld.

Observation SHALL be disableable for the whole deployment, and disabling it SHALL
affect no other path.

#### Scenario: A failing observation changes nothing

- **WHEN** every observation for a domain records a failed handshake
- **THEN** the domain continues to be answerable, its challenge records are unchanged, and nothing is suspended

#### Scenario: Observation can be switched off

- **WHEN** observation is disabled for the deployment
- **THEN** no handshake is attempted and every other path behaves as before

### Requirement: Observations are scheduled, bounded and never on a request path

Observation SHALL run as a background pass on a configured interval measured in
hours, and SHALL NOT be performed while serving a request that answers a challenge.

The service SHALL observe a domain at most once an hour. It SHALL refuse to start
with an observation interval shorter than an hour, rather than accepting it and
observing more often, since nothing else bounds how frequently the service
connects to a customer's machine.

Each pass SHALL select a bounded number of due observations, SHALL bound how many
handshakes are outstanding at once, and SHALL apply a configured handshake timeout
per attempt.

A domain whose observation has just been enabled SHALL be selected before a domain
due only for routine re-observation.

#### Scenario: Challenge answering never waits on a handshake

- **WHEN** a challenge is presented or answered
- **THEN** no TLS handshake against a customer address is performed as part of serving it

#### Scenario: A blackholing address costs one bounded pass

- **WHEN** a node address accepts connections and never completes a handshake
- **THEN** each attempt ends at the configured timeout and the pass completes

#### Scenario: An interval below the floor is refused

- **WHEN** the deployment is configured to observe every five minutes
- **THEN** the service refuses to start and names the setting

#### Scenario: Newly enabled work goes first

- **WHEN** more observations are due than one pass may take, and one of them is a domain enabled a moment ago
- **THEN** the newly enabled domain is observed in that pass

### Requirement: Observation history is retained, downsampled and gated by account kind

The service SHALL retain observations so that the history of a domain's addresses
is readable, SHALL keep full resolution for a configured recent window, and SHALL
thereafter retain no more than one observation per domain per address per day.

The service SHALL make a domain's current certificate state readable on any
account. The service SHALL make the observation HISTORY readable only on a team
account, and SHALL refuse a history read on a personal account with a refusal that
identifies the account kind as the reason rather than the caller's permissions.

That refusal SHALL be distinguishable by a client from every other refusal the
service makes — an unfunded account, an unauthorised caller, and an account whose
shape makes a request meaningless — because the remedy differs in each case and
only this one is answered by changing what the customer pays for.

#### Scenario: Current state is readable everywhere

- **WHEN** a personal account reads an observed domain's certificate state
- **THEN** the current state of each address is returned

#### Scenario: History is refused on a personal account

- **WHEN** a personal account reads an observed domain's observation history
- **THEN** the read is refused, and the refusal names the account kind rather than a missing permission

#### Scenario: History is available on a team account

- **WHEN** a team account reads an observed domain's observation history
- **THEN** the recorded observations are returned, most recent first

#### Scenario: Old observations are downsampled rather than dropped

- **WHEN** observations older than the full-resolution window are retained
- **THEN** at most one observation per domain per address per day remains, and the period is not emptied

### Requirement: Observations are confined to their account like every other tenant row

An observation SHALL carry the materialized ownership columns and SHALL be subject
to the same row-level security as every other tenant-owned row, so that reading a
domain's certificate state can be granted without granting the inventory it
describes.

#### Scenario: An observation is not visible across accounts

- **WHEN** a caller scoped to one account reads observations
- **THEN** no observation belonging to another account is returned

### Requirement: Certificate observation is not metered

Certificate observation SHALL NOT be metered. The service SHALL NOT emit a usage
or rate event for an observed domain, for a handshake, for a node, or for an
address family, and observations SHALL NOT produce ledger entries.

Enabling or disabling observation for a domain SHALL NOT change what an account
is charged.

#### Scenario: Switching observation on costs nothing

- **WHEN** observation is enabled for a domain
- **THEN** no usage or rate event is emitted for it and the account's charges are unchanged

#### Scenario: The volume of observation never reaches an invoice

- **WHEN** one observed domain is probed across four node addresses, repeatedly, over a period
- **THEN** nothing is metered for any of those handshakes
