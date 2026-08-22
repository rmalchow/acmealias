## ADDED Requirements

### Requirement: An acceptance suite runs the full issuance chain against the development stack

The repository SHALL provide an acceptance test suite, excluded from ordinary builds and test
runs by a build tag, that runs against the repository's own development stack rather than a
dedicated environment. The suite SHALL bring that stack up from empty and tear it down using the
stack's existing commands, and SHALL require no credentials, no network access to a real
certificate authority, and no manual steps.

#### Scenario: Suite runs from a clean checkout

- **WHEN** the acceptance suite is run on a machine with no prior stack state
- **THEN** the stack is created, becomes usable, a certificate is issued end to end, and the stack is stopped

#### Scenario: Only one stack definition exists

- **WHEN** the suite runs
- **THEN** it uses the development stack's own compose definition, configuration and seed, and defines no second copy of them

#### Scenario: The proxy under test is unmodified

- **WHEN** issuance is exercised
- **THEN** the proxy obtaining the certificate is the released image with configuration only, and no patched build or plugin

### Requirement: The test authority performs real validation

The certificate authority the suite issues against SHALL implement the ACME protocol and SHALL
perform DNS-01 validation by resolving the challenge record through the stack's DNS server,
rather than accepting challenges unconditionally. It SHALL be configured deterministically,
without injected delays or randomized failures.

#### Scenario: Validation actually queries DNS

- **WHEN** the service does not publish a challenge value
- **THEN** issuance fails rather than succeeding

#### Scenario: Runs are deterministic

- **WHEN** the acceptance suite runs repeatedly
- **THEN** results do not vary because of injected randomness in the test authority

### Requirement: The stand-in DNS serves the customer side of the delegation

The stack's DNS server SHALL be authoritative for the test domains, SHALL serve the
`_acme-challenge` CNAME records that delegate to the service's zone, SHALL resolve the service's
zone to the service, and SHALL serve the address records for the test domains. Every participant
in the stack SHALL resolve through it.

#### Scenario: Challenge lookup follows the CNAME into the service

- **WHEN** the challenge name for a test domain is resolved through the stack's DNS
- **THEN** the answer is the value published to the service

#### Scenario: The propagation check uses the same view

- **WHEN** the ACME client performs its own propagation check
- **THEN** it resolves through the stack's DNS server

### Requirement: Issued certificates are verified against the test trust chain

The acceptance suite SHALL obtain the test authority's full issuing chain — every certificate
needed to verify a leaf, not the root alone — and verify served certificates against it. It SHALL
NOT disable certificate verification on the path under test.

#### Scenario: Served certificate chains to the test root

- **WHEN** the suite requests a test domain over HTTPS
- **THEN** the response succeeds and the certificate verifies against the test chain with the expected subject alternative name

#### Scenario: A self-signed fallback fails the test

- **WHEN** the proxy serves a certificate it generated itself instead of one issued by the test authority
- **THEN** the assertion fails

### Requirement: The acceptance suite asserts the wildcard case

The suite SHALL assert that a certificate covering both a seeded base domain and its wildcard is
issued, and that both challenge values were answerable at the shared challenge name at the same
time.

#### Scenario: Wildcard certificate is issued

- **WHEN** a certificate covering the seeded domain and its wildcard is requested
- **THEN** it is issued and carries both names

#### Scenario: Two values coexist during validation

- **WHEN** the service's DNS is queried for the shared challenge name during validation
- **THEN** both values are returned

### Requirement: The acceptance suite asserts the authorization boundaries

The suite SHALL assert that a valid credential cannot publish a challenge for a name belonging
to another load balancer, that a source-restricted credential is refused from a disallowed
address, and that a forwarding header from an untrusted peer does not defeat that restriction.
Where a boundary case needs inventory the development seed does not create, the suite SHALL
create it through the service's own API and remove it afterwards.

#### Scenario: Out-of-scope name is refused

- **WHEN** a credential publishes a challenge for a name outside its load balancer
- **THEN** the request is refused and nothing becomes answerable

#### Scenario: The source restriction is enforced in both directions

- **WHEN** a source-restricted credential is used from an address inside and then outside its permitted range
- **THEN** the first request is accepted and the second is refused

#### Scenario: Spoofed forwarding header does not bypass the restriction

- **WHEN** a request from an untrusted peer carries a forwarding header naming an allowed address
- **THEN** the request is refused

### Requirement: The acceptance suite asserts durability and cleanup

The suite SHALL assert that restarting the service during an issuance does not cause the
issuance to fail, and that after a successful issuance no challenge records remain while the
alias still resolves as an existing name without values.

#### Scenario: Restart mid-issuance

- **WHEN** the service is restarted between publishing a challenge and its validation
- **THEN** the certificate is still issued

#### Scenario: State after issuance

- **WHEN** issuance has completed
- **THEN** no challenge records remain, and a query for the alias returns an empty answer rather than a non-existent name

### Requirement: The generated DNS plan is checked against the stand-in DNS

The suite SHALL generate the required DNS records for a seeded load balancer and assert that the
challenge alias records it generates are exactly those the stack's DNS server serves, with no
such record present on only one side. The assertion SHALL NOT require a browser. Records the
stack deliberately serves differently from the generated plan SHALL be excluded explicitly and
documented, not silently omitted.

#### Scenario: Generated alias records match reality

- **WHEN** the DNS plan for the seeded load balancer is generated
- **THEN** its challenge alias records are exactly those served by the stack's DNS server, which are the ones the issuance under test validated against

#### Scenario: A drifted zone fails the test

- **WHEN** the served alias record no longer matches the label the plan derives
- **THEN** the assertion fails

### Requirement: The acceptance suite exercises sign-in and role enforcement against a real identity provider

The suite SHALL complete the authorization code flow against the stack's identity provider over
HTTP, without a browser, for each seeded user, and SHALL use the resulting session to assert that
the configured group-to-role mapping is enforced by the management API. The suite SHALL NOT
weaken the service's own cookie attributes in order to complete the flow.

#### Scenario: Sign-in establishes a session and reports the mapped role

- **WHEN** a seeded user completes the sign-in flow
- **THEN** a session is established, and the identity endpoint reports that user's groups and the role its group is configured to grant

#### Scenario: A read-only user is refused a change

- **WHEN** the `READ_ONLY` user attempts to create a domain
- **THEN** the request is refused

#### Scenario: A configuration administrator is allowed the same change

- **WHEN** the `CONFIG_ADMIN` user attempts the same creation
- **THEN** the request succeeds

#### Scenario: Key rotation is refused the inventory but allowed a key

- **WHEN** the `KEY_ROTATION` user attempts to create a domain and then to create an API key
- **THEN** the domain creation is refused and the key creation succeeds

### Requirement: The suite is a single-command local gate, and the pipeline does not run it

The suite SHALL be runnable by one command that needs no arguments, and that command SHALL exit
non-zero when any assertion fails. The pipeline SHALL NOT run it: the continuous integration
pipeline SHALL lint, unit-test, build the image and publish it, and SHALL declare no acceptance
stage. That exclusion SHALL be recorded where the stage would be, as a decision rather than an
omission, and the suite and its cost SHALL be documented where a contributor will find them. No
script or pipeline comment SHALL refer to a task group that does not exist.

#### Scenario: One command runs the suite

- **WHEN** a contributor runs the acceptance script with no arguments
- **THEN** the stack is created, the tagged suite runs, and the exit status reflects the assertions

#### Scenario: The pipeline declares no acceptance stage

- **WHEN** the pipeline runs
- **THEN** it performs lint, test, build and publish only, and the reason the suite is excluded is stated in the pipeline definition
