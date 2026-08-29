## MODIFIED Requirements

### Requirement: Only a member of the account's unrestricted role may issue, revoke or re-send an invitation

The service SHALL require membership of the account's unrestricted role — the account's owner and its
administrators — to issue, revoke or re-send an invitation, and SHALL refuse the operation to any other
member whatever grants they hold.

Any such member SHALL be able to revoke or re-send an invitation, and the service SHALL NOT require
that they be the member who issued it. An invitation is the account's business rather than the
sender's, and the person who notices that an address is wrong is usually not the person who typed it.

#### Scenario: An unrestricted member issues an invitation

- **WHEN** a member of the unrestricted role issues an invitation
- **THEN** it is issued

#### Scenario: An administrator revokes an invitation somebody else sent

- **WHEN** a member of the unrestricted role revokes an invitation issued by another such member
- **THEN** it is revoked

#### Scenario: Any other member is refused

- **WHEN** a member who is not in the unrestricted role issues, revokes or re-sends an invitation
- **THEN** it is refused
