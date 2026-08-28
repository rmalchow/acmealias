## ADDED Requirements

### Requirement: Approving and refusing a registration are recorded decisions

The service SHALL record the approval or refusal of an account, naming the member of staff who
decided and the account decided about.

An approval that follows from accepting an invitation SHALL be recorded as such, so that an account
admitted by vouching is distinguishable from one a member of staff looked at.

#### Scenario: Somebody let them in

- **WHEN** a member of staff approves an account
- **THEN** an entry records who approved it and which account

#### Scenario: A vouched admission is not a reviewed one

- **WHEN** an account is approved as a consequence of an invitation being accepted
- **THEN** the entry distinguishes it from a decision taken by staff
