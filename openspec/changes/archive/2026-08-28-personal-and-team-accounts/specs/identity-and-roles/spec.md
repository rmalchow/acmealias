<!-- ADDED, not MODIFIED: there is no requirement of this name in the main spec
     to modify. The global role vocabulary this was written against was removed
     by `tenancy-and-permissions`, so what remains is a new statement about which
     account kinds hold roles at all. -->

## ADDED Requirements

### Requirement: Roles are held by people only in team accounts

Roles and memberships SHALL exist only in team accounts. A personal account SHALL
hold no roles, no memberships and no invitations, and requests for them SHALL be
refused rather than answered with an empty collection, so that a client can tell
"there are none" from "there cannot be any".

The grant model SHALL remain in force in every account, including personal ones,
where it scopes API keys. The subset rule SHALL apply to a key's grants
regardless of the account's kind.

#### Scenario: A personal account has no roles

- **WHEN** roles or members are requested for a personal account
- **THEN** the request is refused, and the refusal distinguishes this from an empty account

#### Scenario: Keys are still scoped in a personal account

- **WHEN** an API key is created in a personal account with grants narrower than its creator holds
- **THEN** it is created and its grants are enforced

#### Scenario: The subset rule does not depend on the account kind

- **WHEN** an API key is created in a personal account with grants exceeding its creator's
- **THEN** it is refused

## REMOVED Requirements

### Requirement: Group claims are mapped to roles by configuration

**Reason**: The internal edition is removed. Roles are no longer derived from
identity-provider group claims by configuration; they are rows inside a team
account. Membership becomes exclusively what the service wrote, which is the
absolute form of the rule `tenancy-and-permissions` P18 previously stated with an
exception for this mechanism.

**Migration**: The four roles the mapping named become the default role set seeded
into each new team account, with the same names and the same grants, editable by
that account's owner. The `ACME_*_GROUP` settings are removed.

<!-- "Roles are global and fixed" was listed here for removal as well. It had
     already been removed by `tenancy-and-permissions`, which archived first and
     replaced the global role vocabulary with per-account roles holding grants.
     The block is dropped rather than kept, because a REMOVED delta naming a
     requirement that is already gone aborts the archive. -->
