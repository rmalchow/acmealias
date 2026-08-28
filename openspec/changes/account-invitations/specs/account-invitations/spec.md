## ADDED Requirements

### Requirement: An invitation offers a membership to an email address

The service SHALL represent an invitation as an offer of membership in one account, naming one role of
that account and one email address, issued by one user.

An invitation SHALL NOT create a membership by itself. A membership SHALL exist only once the invitation
has been accepted.

Email addresses SHALL be stored and compared in a normalized form, so that addresses differing only in
case or surrounding whitespace are one address.

#### Scenario: An invitation names an account, a role and an address

- **WHEN** an invitation is issued
- **THEN** it records the account, the role offered, the invited address and the issuing user

#### Scenario: Issuing an invitation creates no membership

- **WHEN** an invitation has been issued and not yet accepted
- **THEN** the invited address holds no membership of that account

#### Scenario: Addresses differing only in case are one address

- **WHEN** an invitation is issued to an address that differs from an outstanding one only in letter case
- **THEN** they are treated as the same address

### Requirement: Only a member of the account's unrestricted role may issue, revoke or re-send an invitation

The service SHALL require membership of the account's unrestricted role to issue, revoke or re-send an
invitation, and SHALL refuse the operation to any other member whatever grants they hold.

Because that role confers every authority within its account, the service SHALL NOT additionally check
the role an invitation offers against the issuer's own authority.

#### Scenario: An unrestricted member issues an invitation

- **WHEN** a member of the unrestricted role issues an invitation
- **THEN** it is issued

#### Scenario: Any other member is refused

- **WHEN** a member who is not in the unrestricted role issues, revokes or re-sends an invitation
- **THEN** the request is refused, whatever grants that member holds

#### Scenario: Any role of the account may be offered

- **WHEN** a member of the unrestricted role issues an invitation naming any role of that account
- **THEN** it is issued, and no further comparison against the issuer's authority is made

### Requirement: An invitation carries a token that expires, is usable once and is not guessable

The service SHALL address an invitation by a token that expires, is usable once, and is not guessable.

The service SHALL store only a cryptographic hash of the token, and SHALL NOT retain the token in a form
usable to accept the invitation.

Expiry SHALL be evaluated when the token is resolved, and SHALL NOT depend on a scheduled process having
run.

#### Scenario: The stored form is not usable

- **WHEN** the stored representation of an invitation is read directly from the datastore
- **THEN** it does not contain the token in a form that could be used to accept the invitation

#### Scenario: An expired token is refused

- **WHEN** a token is resolved after its expiry has passed
- **THEN** it is refused, whether or not any scheduled process has run

#### Scenario: A consumed token is refused

- **WHEN** a token is resolved after the invitation has been accepted
- **THEN** it is refused

### Requirement: Issuing an invitation does not reveal whether the address has a user

The service SHALL respond identically to an invitation issued to an address that already has a user and
to one that does not.

#### Scenario: The response does not differ

- **WHEN** an invitation is issued to an address that has a user, and to one that does not
- **THEN** the two responses are identical in status, body and timing characteristics

### Requirement: Only a user with the invited address may accept

The service SHALL permit acceptance only by an authenticated user whose verified address is the address
the invitation names, and SHALL refuse acceptance by any other authenticated user.

The service SHALL NOT provide a means of adding the invited address to a user that does not already hold
it. Where no user holds the invited address, the only path SHALL be to create one.

A refusal SHALL identify the invited address only in a partially masked form.

#### Scenario: The invited user accepts

- **WHEN** a user whose verified address is the invited address accepts
- **THEN** the membership is created

#### Scenario: Another authenticated user is refused

- **WHEN** a user whose verified address is not the invited address accepts
- **THEN** the request is refused and no membership is created

#### Scenario: A forwarded link confers nothing

- **WHEN** the link is opened and accepted by a user who received it from the invited recipient
- **THEN** the request is refused

#### Scenario: The invited address is not disclosed on refusal

- **WHEN** acceptance is refused because the authenticated user is not the invited one
- **THEN** the response identifies the invited address only in a partially masked form

#### Scenario: There is no path to claim the address

- **WHEN** the acceptance flow is searched for an operation adding the invited address to the signed-in user
- **THEN** no such operation exists

### Requirement: Acceptance is explicit, and is the only thing that consumes the token

The service SHALL require an explicit acceptance, distinct from resolving the token, before a membership
is created.

Resolving a token SHALL NOT consume it, alter the invitation, or create anything.

#### Scenario: Opening the link changes nothing

- **WHEN** a token is resolved but not accepted
- **THEN** the invitation remains outstanding and can still be accepted afterwards

#### Scenario: Repeated resolution is harmless

- **WHEN** a token is resolved several times without acceptance
- **THEN** it remains valid

#### Scenario: Acceptance consumes the token

- **WHEN** an invitation is accepted
- **THEN** the token no longer resolves

### Requirement: Resolving and accepting an invitation require no account to be selected

The service SHALL permit an authenticated session with no account selected to resolve an invitation token
and to accept the invitation, and SHALL NOT require the session to name an account for either.

Resolving a token SHALL report the name of the account and the name of the role offered, and nothing
further about the account.

#### Scenario: A user belonging to nothing can accept

- **WHEN** a session with no account selected accepts an invitation
- **THEN** the membership is created

#### Scenario: Resolution reports what the offer is

- **WHEN** a token is resolved
- **THEN** the response names the account and the role offered

#### Scenario: Resolution discloses nothing else about the account

- **WHEN** a token is resolved by a session holding no membership of that account
- **THEN** no inventory, member or billing information of that account is returned

### Requirement: Accepting selects the account that was joined

The service SHALL make the newly joined account the session's selected account upon acceptance,
evaluating the membership as it would for any other selection.

#### Scenario: The session continues in the joined account

- **WHEN** a session with no account selected accepts an invitation
- **THEN** the joined account becomes its selected account and account-scoped operations are available

### Requirement: An invitation naming a role that no longer exists fails closed

The service SHALL refuse acceptance of an invitation whose role no longer exists, and SHALL NOT
substitute another role or recreate the named one.

The service SHALL make such an invitation distinguishable in the account's roster so that a member can
see why it failed and issue a replacement.

#### Scenario: Acceptance is refused

- **WHEN** the role an outstanding invitation names has been deleted and the invitation is accepted
- **THEN** the request is refused and no membership is created

#### Scenario: No role is substituted

- **WHEN** acceptance fails because the role no longer exists
- **THEN** no membership is created with any other role

#### Scenario: The account can see the failure

- **WHEN** a member of the account reads its outstanding invitations
- **THEN** an invitation whose role no longer exists is distinguishable from one that is still valid

### Requirement: One invitation is outstanding per address per account

The service SHALL permit at most one outstanding invitation for a given address in a given account.

Re-sending an invitation SHALL issue a new token and SHALL immediately invalidate the previous one.
Re-sending MAY change the role offered.

#### Scenario: A second invitation is not issued alongside the first

- **WHEN** an invitation is issued to an address that already has an outstanding invitation in that account
- **THEN** no second outstanding invitation exists for that address in that account

#### Scenario: Re-sending invalidates the earlier token

- **WHEN** an invitation is re-sent
- **THEN** the previously issued token no longer resolves and the newly issued one does

#### Scenario: Re-sending may amend the offer

- **WHEN** an invitation is re-sent naming a different role of the same account
- **THEN** acceptance of the new token creates a membership with the newly named role

### Requirement: An invitation may be revoked, and stops working immediately

The service SHALL allow an outstanding invitation to be revoked, after which its token SHALL NOT resolve.

A revoked invitation SHALL be retained as a record of what was offered.

#### Scenario: A revoked token stops working

- **WHEN** an invitation is revoked
- **THEN** its token no longer resolves and it cannot be accepted

#### Scenario: A revoked invitation leaves the outstanding list

- **WHEN** an invitation is revoked
- **THEN** it no longer appears among the account's outstanding invitations

### Requirement: An address that already holds a membership is refused at issue

The service SHALL refuse to issue an invitation to an address whose user already holds a membership of
that account, and SHALL refuse it when the invitation is issued rather than when it is accepted.

#### Scenario: Inviting an existing member is refused immediately

- **WHEN** an invitation is issued to the address of a user who is already a member of that account
- **THEN** the request is refused and no message is sent

### Requirement: An accepted invitation cannot create a second membership

The service SHALL refuse acceptance where the accepting user already holds a membership of that account,
and SHALL NOT create a second membership or alter the existing one.

#### Scenario: Acceptance by an existing member is refused

- **WHEN** a user who has become a member since being invited accepts the invitation
- **THEN** the request is refused and the existing membership is unchanged

### Requirement: An expired invitation reports its state and offers no self-service renewal

The service SHALL report, on resolution of an expired token, that the invitation has expired and name the
account that issued it, and SHALL NOT allow the recipient to renew or extend it.

#### Scenario: An expired link explains itself

- **WHEN** an expired token is resolved
- **THEN** the response reports the invitation as expired and names the account

#### Scenario: The recipient cannot renew

- **WHEN** an expired token is resolved
- **THEN** no operation renewing or extending the invitation is offered

### Requirement: Issuing invitations is rate limited per account, per address and per invitation

The service SHALL limit the rate at which invitations are issued, by account, by invited address across
all accounts, and by re-sends of one invitation.

A refusal SHALL state that a limit was reached and when the operation may be retried, and SHALL be
recorded.

#### Scenario: An account cannot send without limit

- **WHEN** one account issues invitations beyond its permitted rate
- **THEN** further issues are refused until the rate permits them

#### Scenario: One address cannot be targeted from many accounts

- **WHEN** invitations to one address are issued from several accounts beyond the permitted rate for that address
- **THEN** further issues to that address are refused

#### Scenario: Re-sending is limited

- **WHEN** one invitation is re-sent beyond the permitted rate
- **THEN** further re-sends are refused

#### Scenario: A refusal says when to retry

- **WHEN** an issue is refused because a limit was reached
- **THEN** the response states that a limit was reached and when the operation may be retried

### Requirement: Invitations belong to their account and do not outlive it

The service SHALL remove an account's outstanding invitations when the account is removed, and their
tokens SHALL cease to resolve.

#### Scenario: Deleting an account ends its invitations

- **WHEN** an account holding outstanding invitations is deleted
- **THEN** their tokens no longer resolve

### Requirement: A blocked address is refused when the invitation is accepted

The service SHALL check the invited address against the identity blocklist at acceptance, and SHALL
refuse acceptance while the address is blocked. Issuing an invitation SHALL NOT be refused on that
basis, so that the outcome is reported to the account rather than silently withheld.

#### Scenario: Acceptance by a blocked address is refused

- **WHEN** an invitation to a blocked address is accepted
- **THEN** acceptance is refused, no membership is created, and the token is not consumed

#### Scenario: The account is told the address cannot be used

- **WHEN** an invitation whose address is blocked remains outstanding
- **THEN** the account's outstanding list reports that the address cannot accept

#### Scenario: Issuing is not refused for a blocked address

- **WHEN** an invitation is issued to a blocked address
- **THEN** issuing succeeds and the refusal happens at acceptance

### Requirement: An invitation expires fourteen days after it is issued

An invitation SHALL expire fourteen days after it is issued.

#### Scenario: An invitation outlives a fortnight's absence

- **WHEN** a person opens an invitation thirteen days after it was issued
- **THEN** it is outstanding and may be accepted

#### Scenario: An invitation does not outlive the fortnight

- **WHEN** a person opens an invitation fifteen days after it was issued
- **THEN** it is reported as expired

### Requirement: A signed-in person sees the invitations issued to their address

The service SHALL show a person, on authenticating, every outstanding invitation issued to their
address, whichever account issued it, without requiring the invitation's token.

Accepting an invitation by this route SHALL be governed by the same rule as accepting it by its token:
the person SHALL be authenticated as the invited address.

#### Scenario: The mail never arrived

- **WHEN** a person authenticates as an address that holds an outstanding invitation and has not opened
  the message
- **THEN** the invitation is presented to them, and may be accepted

#### Scenario: Someone else's invitation is not presented

- **WHEN** a person authenticates as an address that holds no outstanding invitation
- **THEN** no invitation is presented, whatever invitations exist for other addresses

#### Scenario: The two routes reach the same invitation

- **WHEN** an invitation is accepted without its token by the invited address
- **THEN** the membership created is the one the invitation offered, and the token no longer resolves

### Requirement: An invitation may be declined, and declining is an answer

The service SHALL let the invited address decline an invitation.

Declining SHALL consume the invitation, remove it from the account's outstanding list, and be
distinguishable from an invitation that expired unanswered.

Declining SHALL NOT prevent the account from issuing a further invitation to the same address.

#### Scenario: A decline is visible as a decline

- **WHEN** an invited person declines
- **THEN** the account sees that the invitation was declined, rather than seeing it expire

#### Scenario: A declined invitation is not resurrectable

- **WHEN** the token of a declined invitation is resolved
- **THEN** it does not offer a membership

#### Scenario: Declining is not a block

- **WHEN** an account invites an address that previously declined
- **THEN** the invitation is issued
