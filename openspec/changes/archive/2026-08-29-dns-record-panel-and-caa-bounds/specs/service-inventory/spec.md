## RENAMED Requirements

- FROM: `### Requirement: CAA verification distinguishes absent from conflicting`
- TO: `### Requirement: CAA verification distinguishes absent, unconstrained and conflicting`

## MODIFIED Requirements

### Requirement: CAA verification distinguishes absent, unconstrained and conflicting

Verification of a CAA record SHALL report four distinct results: that no CAA record set exists,
that one exists and does not authorize a configured issuer, that one authorizes a configured issuer
without limiting issuance to a specific ACME account, and that one authorizes a configured issuer
and limits issuance to a specific account.

The distinction between the last two SHALL be drawn on the presence of a non-empty account
identifier parameter on every issuance authorization the set grants for a configured issuer, both
for plain and for wildcard issuance. A set in which any such authorization carries no account
identifier SHALL be reported as unconstrained, since a set is only as narrow as its widest entry.

Verification SHALL report what was found alongside every result that has published data to report,
and not only when the set is blocking.

None of these SHALL be reported as a lookup failure; a failure means the query did not complete.

#### Scenario: Absent CAA is reported as such

- **WHEN** no CAA record exists for the registrable domain
- **THEN** the result reports that none is published, and is not counted as a lookup failure

#### Scenario: An authorizing set without an account is unconstrained

- **WHEN** a CAA record set authorizes a configured issuer for both plain and wildcard issuance and names no ACME account
- **THEN** the record is reported as unconstrained, with what was found

#### Scenario: An authorizing set limited to an account is correct

- **WHEN** a CAA record set authorizes a configured issuer for both plain and wildcard issuance and every such authorization names an ACME account
- **THEN** the record is reported as correct

#### Scenario: One unlimited authorization makes the set unconstrained

- **WHEN** a CAA record set limits wildcard issuance to an ACME account and leaves plain issuance limited only to the issuer
- **THEN** the record is reported as unconstrained

#### Scenario: Conflicting CAA is reported as blocking

- **WHEN** a CAA record set exists and authorizes only some other certificate authority
- **THEN** the record is reported as blocking, with the authorities that were found

### Requirement: An alias is one label in the served zone per challenge name

The service SHALL store an alias for each distinct challenge name within an account, consisting of a
single label within the served zone. Challenge names SHALL be unique within an account. Alias labels
SHALL be unique across the service, because the served zone is shared by every account. Several
domains MAY share one alias.

An alias label SHALL be a single DNS label of lowercase letters, digits and hyphens, not
beginning or ending with a hyphen, and at most 63 octets.

The service SHALL determine the label of a new alias itself. It SHALL NOT accept a label from a
caller of the management interface, the command line, or any other entry point.

By default the label SHALL be randomly generated with at least 48 bits of entropy and SHALL NOT
be derivable from the challenge name, the domain name, or any other value visible outside the
service. The floor is 48 rather than the 80 it was: the label is typed into a DNS provider's form
by hand and read back to verify it, so every character costs a chance to transpose one, and
guessing a label without holding the matching delegation is worth nothing.

A configuration setting SHALL make the service derive the label from the challenge name instead,
for a deployment that prefers a self-describing label.

The setting SHALL govern only aliases created after it takes effect. The label of an existing
alias SHALL NOT change, whatever the setting, because it is published in a record the service
does not control.

Where a candidate label is already in use, the service SHALL resolve the collision and complete
the request rather than refuse it. This SHALL hold for a collision detected only when the record
is written — two requests choosing one label concurrently SHALL NOT fail the later one — and a
label conflict SHALL be distinguished from a conflict over the challenge name itself, which is a
genuine refusal and SHALL NOT be retried.

#### Scenario: A label chosen twice at once still succeeds

- **WHEN** two concurrent requests choose the same alias label and the second is refused by the store
- **THEN** the second chooses again and completes, rather than failing the request

#### Scenario: A claimed challenge name is refused, not retried

- **WHEN** a request's challenge name is already claimed elsewhere
- **THEN** it is refused, and the refusal is not retried as though it were a label collision

#### Scenario: Shared alias for wildcard and apex

- **WHEN** `example.com` and `*.example.com` are added to the same load balancer
- **THEN** exactly one alias exists for `_acme-challenge.example.com` and both domains reference it

#### Scenario: Two accounts holding one name get two aliases

- **WHEN** `shop.example.com` is added in one account and in another
- **THEN** each account has its own alias with its own label, and neither is visible to the other

#### Scenario: A derived label colliding across accounts is resolved

- **WHEN** labels are derived from the challenge name and the same domain is added in two accounts
- **THEN** the second alias is given another valid label and the domain is created

#### Scenario: A generated label does not disclose its domain

- **WHEN** a domain is added with the default labelling
- **THEN** its alias label is a valid label that cannot be computed from the domain name by anyone outside the service

#### Scenario: A supplied label is not honoured

- **WHEN** a domain creation request carries an alias label
- **THEN** the request is refused as carrying an unknown field, and no alias bears that label

#### Scenario: The setting restores derived labels

- **WHEN** the setting to derive labels from the challenge name is enabled and `demo.example.com` is added
- **THEN** its alias label is derived from `_acme-challenge.demo.example.com` and is self-describing

#### Scenario: An existing alias keeps its label when the setting changes

- **WHEN** the labelling setting is changed and the service restarts
- **THEN** every alias created before the change keeps the label it already published, and only aliases created afterwards use the new labelling

#### Scenario: A colliding label is resolved, not refused

- **WHEN** the label the service would use for a new alias is already in use
- **THEN** the service uses another valid label and the domain is created

#### Scenario: Invalid label is rejected

- **WHEN** a label containing a dot, an underscore, a leading hyphen, or more than 63 octets would be stored as an alias label
- **THEN** it is refused with a validation error
