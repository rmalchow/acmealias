## MODIFIED Requirements

### Requirement: The console validates domain input against the wildcard rules

The domain editor SHALL apply the same validation as the service to wildcard and specific
names, report the reason for a rejection, and state that a wildcard covers exactly one label
and does not cover its own base name.

The editor SHALL offer one control that adds the typed name's counterpart in the same action —
the wildcard of a name that is not one, the base name of a name that is — naming in its label
the exact name it would add, so the control reads differently for `example.com` than for
`*.example.com`.

The control SHALL be off whenever the editor opens, and SHALL NOT be preselected on the
operator's behalf, because the counterpart carries a wildcard address record over the customer's
whole zone and a certificate covering every host beneath the domain. The editor SHALL state what
the counterpart covers alongside the control.

A refusal of either name SHALL be reported as the service reported it, and the editor SHALL NOT
report a partial success.

#### Scenario: Invalid wildcard is reported before submission

- **WHEN** a user enters `*.*.example.com`
- **THEN** the editor reports the rule that rejects it

#### Scenario: Server remains the authority

- **WHEN** a value that passes client-side validation is rejected by the service
- **THEN** the service's reason is shown to the user

#### Scenario: The control names the wildcard it would add

- **WHEN** an operator types `example.com`
- **THEN** the control offers to add `*.example.com` and names it

#### Scenario: The control mirrors for a wildcard

- **WHEN** an operator types `*.example.com`
- **THEN** the control offers to add the base name `example.com` and names it

#### Scenario: The control opens off

- **WHEN** an operator opens the domain editor, having used the control previously
- **THEN** the control is off and submitting without touching it creates only the typed name

#### Scenario: A refused pair is reported as a refusal

- **WHEN** the service refuses a creation because the counterpart broke a rule
- **THEN** the dialog stays open showing the service's reason, and reports no domain as created
