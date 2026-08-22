## ADDED Requirements

### Requirement: The certificate a node serves is observed and recorded

The service SHALL observe the certificate presented by a node by completing a secure transport
handshake against it, and SHALL record the certificate's expiry, its issuer, and the names it covers.

Observation SHALL be performed once per node per distinct certificate, and SHALL NOT require one
observation per configured domain.

#### Scenario: A certificate is observed once for many names

- **WHEN** a node serves one certificate covering fifty names
- **THEN** one observation records that certificate and the fifty names it covers

#### Scenario: Expiry and issuer are recorded

- **WHEN** a certificate is observed
- **THEN** the recorded observation includes when it expires and who issued it

#### Scenario: An unreachable node records no certificate

- **WHEN** a handshake with a node cannot be completed
- **THEN** no certificate observation is recorded and the failure is recorded instead

### Requirement: Domains covered by no observed certificate are identified

The service SHALL compare the domains of an account's inventory against the names covered by
certificates observed on the nodes serving them, and SHALL report domains covered by none.

#### Scenario: An uncovered domain is reported

- **WHEN** a domain is present in the inventory and appears in no observed certificate
- **THEN** it is reported as uncovered

#### Scenario: A covered domain is not reported

- **WHEN** a domain appears in an observed certificate on the nodes serving it
- **THEN** it is not reported as uncovered

#### Scenario: Coverage is evaluated per account

- **WHEN** two accounts hold the same domain name
- **THEN** each is evaluated against the certificates observed on its own nodes

### Requirement: Approaching expiry is reported before it occurs

The service SHALL report a certificate whose expiry is approaching, in advance of that expiry, and
SHALL make the threshold configurable.

#### Scenario: An approaching expiry is reported

- **WHEN** an observed certificate's expiry falls within the configured threshold
- **THEN** it is reported

#### Scenario: An expired certificate is reported as expired

- **WHEN** an observed certificate's expiry has passed
- **THEN** it is reported as expired rather than as approaching expiry

### Requirement: Certificate observations are retained as history

The service SHALL retain certificate observations as history, so that changes of issuer, of covered
names and of expiry are visible over time.

#### Scenario: A renewal is visible

- **WHEN** a node begins serving a certificate with a later expiry
- **THEN** the history shows both the previous and the new observation

#### Scenario: A change of covered names is visible

- **WHEN** an observed certificate covers different names than the previous observation
- **THEN** the difference is visible in the history

### Requirement: Certificate observation does not obtain or install certificates

The service SHALL NOT request, obtain, store or install a certificate for a node, and SHALL NOT hold
a private key belonging to one.

#### Scenario: No issuance is performed

- **WHEN** an uncovered or expiring domain is reported
- **THEN** no certificate is requested or installed by the service

#### Scenario: No private key is held

- **WHEN** the stored certificate observations are examined
- **THEN** they contain no private key
