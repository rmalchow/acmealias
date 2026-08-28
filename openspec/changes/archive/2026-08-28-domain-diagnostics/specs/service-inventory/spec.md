## ADDED Requirements

### Requirement: A domain carries a certificate observation switch and a test hostname

A domain SHALL carry a flag recording whether its certificate is observed, and an
optional hostname to present as the TLS server name when it is.

The flag SHALL default to disabled on creation. Both SHALL be writable through the
same permission that writes the rest of the domain, and SHALL be readable wherever
the domain is read, so that a client can render a domain's row without a second
request.

Where the domain is a wildcard, the service SHALL refuse to enable the flag unless
a test hostname is supplied, and SHALL refuse a hostname the wildcard does not
cover. Where the domain is not a wildcard, the service SHALL NOT require one, and
SHALL present the domain's own name as the server name.

The hostname SHALL be normalized and validated by the same rules the service
applies to a domain name, and SHALL be retained when the flag is disabled.

Deleting a domain SHALL remove both, along with the domain.

#### Scenario: The flag defaults to off

- **WHEN** a domain is created
- **THEN** its observation flag is disabled and it carries no test hostname

#### Scenario: A wildcard cannot be enabled without a hostname

- **WHEN** the observation flag is enabled for `*.example.com` with no test hostname
- **THEN** the write is refused

#### Scenario: A hostname the wildcard does not cover is refused

- **WHEN** a test hostname of `www.other.example` is written to `*.example.com`
- **THEN** the write is refused

#### Scenario: A non-wildcard needs no hostname

- **WHEN** the observation flag is enabled for `shop.example.com` with no test hostname
- **THEN** the write is accepted

#### Scenario: Both are readable with the domain

- **WHEN** a domain is read
- **THEN** its observation flag and its test hostname are present in the result

#### Scenario: The hostname survives the flag being cleared

- **WHEN** the observation flag is disabled on a domain that carried a test hostname
- **THEN** the hostname is retained

### Requirement: A domain carries the records that must be published for it

The service SHALL report, with each domain, everything that must be published
outside the service for that domain to work: its challenge name, the target that
challenge name's `CNAME` must point at, and the CAA entries expected at the
registrable name governing it.

The target SHALL be the alias issued for that domain's challenge name within the
served zone. Where a domain has no alias, the service SHALL report the absence
rather than omitting the domain, since the domain still exists and the absence is
the service's own to resolve.

Two domains sharing a challenge name SHALL each report that one target, so that a
name and its wildcard each state the delegation they share rather than one of them
appearing to need nothing.

The CAA entries SHALL be those the configured certificate authority identifiers
require: for each identifier, an `issue` entry and an `issuewild` entry, expected
at the registrable domain determined by public suffix and never at an individual
host beneath it, because CAA is inherited and the nearest ancestor wins. Where no
identifier is configured, the service SHALL report no CAA entries rather than
assuming one.

The service SHALL NOT report address records for a domain.

#### Scenario: A domain states its delegation

- **WHEN** a domain is read
- **THEN** it carries its challenge name and the target that name's `CNAME` must point at

#### Scenario: A wildcard and its apex state the same delegation

- **WHEN** `example.com` and `*.example.com` are read
- **THEN** both carry the same challenge name and the same target

#### Scenario: A domain with no alias says so

- **WHEN** a domain has no alias
- **THEN** it is still reported, carrying no target, rather than omitted

#### Scenario: CAA is stated at the registrable name

- **WHEN** `shop.example.com` is read with an issuer configured
- **THEN** its expected CAA entries are named at `example.com` and not at `shop.example.com`

#### Scenario: Both CAA tags are stated

- **WHEN** a domain's expected CAA entries are read with an issuer configured
- **THEN** they contain an `issue` entry and an `issuewild` entry for that issuer

#### Scenario: No issuer configured means no CAA entries

- **WHEN** a domain is read with no certificate authority identifier configured
- **THEN** it carries no expected CAA entries

#### Scenario: No address records are reported

- **WHEN** a domain is read
- **THEN** it carries no address record for any node of its project

### Requirement: A domain's published records can be verified live

The service SHALL resolve, per domain and on demand, the records that domain
requires, and SHALL report per record type whether it is correct, missing, points
elsewhere, or could not be determined. Results SHALL include what was actually
found when it differs from what is required, and SHALL NOT be stored as
authoritative state.

The answer SHALL be reported per domain. The service SHALL NOT additionally report
the same answers keyed by record, since one name's answer belongs to every domain
that requires it and a second keying invites the two to disagree.

Where no resolver is configured, the service SHALL report every domain as not
checked rather than refusing.

#### Scenario: Correct delegation verifies

- **WHEN** a domain's challenge name resolves through a `CNAME` to the target it requires
- **THEN** the domain's delegation is reported as correct

#### Scenario: Missing record is reported

- **WHEN** a domain's challenge name does not resolve
- **THEN** the domain's delegation is reported as missing

#### Scenario: Wrong target is reported with what was found

- **WHEN** a domain's challenge name resolves to a different target
- **THEN** the domain's delegation is reported as incorrect together with the observed target

#### Scenario: Resolution failure is distinguished from absence

- **WHEN** the lookup fails or times out
- **THEN** the record is reported as undetermined and the check is retryable

#### Scenario: A shared delegation answers both domains

- **WHEN** verification runs for a project holding `example.com` and `*.example.com`
- **THEN** both domains report the state of the one delegation they share

#### Scenario: No resolver is not a refusal

- **WHEN** verification runs on a deployment with no configured resolver
- **THEN** every domain reports as not checked and the request succeeds

## REMOVED Requirements

### Requirement: The required DNS records are generated per load balancer

**Reason**: The set was dominated by address records — one per domain per node
address per family — whose only possible finding is that a customer's DNS
disagrees with the node addresses that same customer typed into the same screen.
Removing them removes the reason for the set to exist as a shape at all: what
remains that a customer must publish is a `CNAME` and, sometimes, a CAA entry,
and both are properties of a domain rather than members of a collection (design
G2). Keeping the collection alive would leave two shapes answering "what must I
publish", which is the disagreement this change exists to end, moved one layer
down where nobody can see it.

**Migration**: "A domain carries the records that must be published for it",
added above, states the surviving content per domain: the challenge name, the
`CNAME` target, and the expected CAA entries. The structured plan read, its
zone-file rendering, and the administrative subcommand that printed them are
removed with the requirement. Address records are not replaced anywhere; if
grading them against a project's nodes proves worth doing, it belongs to the
node, not the domain.

### Requirement: The plan includes CAA records when an issuer is configured

**Reason**: The rules it states are unchanged and remain in force — one set per
distinct registrable domain determined by public suffix, never at a host beneath
it, both `issue` and `issuewild` for every configured identifier, and nothing at
all when none is configured. What changes is only what carries them: they are
stated per domain rather than as rows of a plan that no longer exists.

**Migration**: The same rules appear verbatim in "A domain carries the records
that must be published for it", above, expressed as the CAA entries expected for
a domain. The scenario asserting a zone-file rendering of the plan is dropped
along with the rendering.

### Requirement: Required DNS records can be verified live

**Reason**: The requirement is phrased per record of a plan. The plan is removed,
and the answer has been per domain since `domain-setup-feedback` — which added
the per-domain answer alongside the per-record one rather than replacing it,
because the plan table was still consuming the older shape. It no longer is.

**Migration**: "A domain's published records can be verified live", added above,
states the same semantics keyed by domain, and additionally forbids reporting the
same answers a second time keyed by record. The verification route, its
resolver-absent behaviour and its status vocabulary are otherwise unchanged, and
"CAA verification distinguishes absent from conflicting" continues to govern the
CAA half untouched.
