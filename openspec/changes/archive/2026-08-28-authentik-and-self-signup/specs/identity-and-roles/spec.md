## ADDED Requirements

### Requirement: A self-registered user holds their personal account and nothing else

Where the identity provider permits a person to create their own account, the resulting user SHALL
hold their own personal account and SHALL hold membership of no other account.

They SHALL hold no group that any role is configured for, and membership of any other account SHALL
become possible only when it is written through the service's own mechanisms — never through a change
of group membership made at the identity provider alone.

This is the observable form of the rule that tenancy is never derived from a directory: an identity
provider can create a person, and cannot place that person inside anybody else's account.

#### Scenario: Self-registration produces a working user with nobody else's data

- **WHEN** a person registers themselves at the identity provider and signs in
- **THEN** authentication succeeds, they hold their personal account, and they hold membership of no
  other account

#### Scenario: A directory group does not confer membership of an account

- **WHEN** a self-registered user is added to a group at the identity provider that no role is configured for
- **THEN** they gain membership of no account beyond their own personal one

#### Scenario: A person who signs themselves up is not shown a refusal

- **WHEN** a self-registered user reaches the product for the first time
- **THEN** they are placed in their personal account rather than told they are associated with nothing
