# identity-and-roles Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
### Requirement: Humans authenticate through OpenID Connect

The service SHALL authenticate console users through an OpenID Connect authorization code flow
with PKCE, performed by the server. It SHALL validate the issuer, audience, expiry and nonce of
the identity token against the provider's published keys. It SHALL NOT provide local password
accounts.

Tokens obtained from the provider SHALL NOT be exposed to the browser.

#### Scenario: Unauthenticated request is directed to sign in

- **WHEN** an unauthenticated request is made to the management API
- **THEN** the service responds `401 Unauthorized` and indicates where to begin sign-in

#### Scenario: Successful sign-in establishes a session

- **WHEN** the provider redirects back with a valid authorization code and matching state
- **THEN** the service establishes a session and the user's identity is readable from the management API

#### Scenario: Invalid identity token is rejected

- **WHEN** the identity token fails signature, issuer, audience, expiry or nonce validation
- **THEN** sign-in fails and no session is established

### Requirement: Sessions are server-side, revocable and time-bounded

The service SHALL represent a session as server-side state referenced by an opaque,
cryptographically random identifier in a cookie marked `HttpOnly`, `Secure` and `SameSite`.
Sessions SHALL expire after a configured idle timeout and a configured absolute lifetime, and
SHALL be revocable.

#### Scenario: Sign-out ends the session

- **WHEN** a user signs out
- **THEN** the session is destroyed and subsequent requests presenting its cookie are unauthenticated

#### Scenario: Expired session is rejected

- **WHEN** a session exceeds its idle timeout or absolute lifetime
- **THEN** requests presenting it are unauthenticated

### Requirement: An administrator can end another person's sessions

The service SHALL allow a user holding `ADMIN` to enumerate the sessions that are currently
valid, to revoke an individual session, and to revoke every session belonging to one subject.
Revocation SHALL take effect on the next request presenting the revoked session, and SHALL NOT
affect any session it was not applied to.

Enumeration SHALL report, per session, the subject it belongs to, when it was established, when
it was last used and when it expires. It SHALL NOT expose the session identifier in a form that
could be used to authenticate as that session.

#### Scenario: Administrator revokes one session

- **WHEN** an `ADMIN` revokes a session belonging to another user
- **THEN** the next request presenting that session is unauthenticated

#### Scenario: Other sessions are untouched

- **WHEN** one session is revoked
- **THEN** sessions belonging to other subjects remain valid

#### Scenario: All sessions for one subject are revoked together

- **WHEN** an `ADMIN` revokes every session of one subject
- **THEN** no session belonging to that subject remains valid

#### Scenario: Only an administrator may revoke

- **WHEN** a user not holding `ADMIN` attempts to enumerate or revoke sessions
- **THEN** the request is refused

### Requirement: State-changing management requests are protected against cross-site request forgery

The service SHALL require a token bound to the session, submitted in a request header, on every
state-changing management request, and SHALL reject requests without it.

#### Scenario: Request without the token is rejected

- **WHEN** a state-changing management request is made with a valid session cookie but no matching token header
- **THEN** the service responds with a client error and performs no change

### Requirement: Role changes take effect on sign-in, and sessions are the immediate lever

A change to a user's group membership SHALL take effect at their next sign-in, and SHALL be
bounded by the configured absolute session lifetime, because group membership is read from the
identity token when a session is established. A change to the group-to-role configuration SHALL
take effect when the service is restarted.

The service SHALL allow an `ADMIN` to revoke a session, and revocation SHALL take effect on the
next request made with it.

#### Scenario: Group removal does not end an existing session

- **WHEN** a user is removed from a mapped group while holding an active session
- **THEN** that session retains the role until it is revoked or expires

#### Scenario: Revocation is immediate

- **WHEN** an administrator revokes a session
- **THEN** the next request presenting it is unauthenticated

#### Scenario: Exposure is bounded by the absolute lifetime

- **WHEN** a session reaches the configured absolute lifetime
- **THEN** it is no longer usable and the user's roles are re-derived at their next sign-in

### Requirement: Privileged actions are recorded

The service SHALL record every creation, modification and deletion of inventory, API keys and
sessions — including the revocation of a session by an administrator — with the acting identity,
the affected object, the action and the time. Records SHALL NOT contain key secrets or tokens.

#### Scenario: Key creation is attributable

- **WHEN** a user creates an API key
- **THEN** an audit record names that user, the key identifier and the load balancer
- **AND** the record does not contain the secret

#### Scenario: Session revocation is attributable

- **WHEN** an `ADMIN` revokes another user's session
- **THEN** an audit record names the administrator, the subject whose session was ended, and the time

### Requirement: Administering roles is itself a permission

The service SHALL require a permission to create, edit or delete a role, and to change a person's
memberships. That permission SHALL be grantable like any other, and SHALL confer authority only
within the account holding it.

A subject administering roles SHALL be bound by the rule that it may grant only what it holds.

#### Scenario: Role administration is refused without the permission

- **WHEN** a member without the role-administration permission creates or edits a role
- **THEN** the request is refused

#### Scenario: Role administration does not cross accounts

- **WHEN** a member holding the role-administration permission in one account edits a role of another
- **THEN** the request is refused

#### Scenario: A role cannot be given authority its author lacks

- **WHEN** a member holding the role-administration permission adds a grant it does not itself hold
- **THEN** the request is refused

### Requirement: Seeded roles reproduce the previous authority exactly

When the schema is migrated the service SHALL create, in the single account, one role for each of
the four previously fixed roles, bearing the same name and holding grants conferring the same
authority the permission matrix conferred before the migration.

#### Scenario: A seeded role confers what its predecessor conferred

- **WHEN** an operator whose groups map to a seeded role performs an operation that role permitted before the migration
- **THEN** the operation is permitted

#### Scenario: A seeded role confers nothing more

- **WHEN** that operator performs an operation the role did not permit before the migration
- **THEN** the request is refused

#### Scenario: Seeded roles are ordinary records

- **WHEN** a member holding the role-administration permission inspects the seeded roles
- **THEN** they are listed with their grants, and all but the unrestricted role may be edited

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

### Requirement: An API key is of one of two kinds

An API key SHALL be either a project key, bound to one project, or a system key, holding a role.

Both kinds SHALL be authorized by the same mechanism as any other subject. Neither SHALL introduce a
decision path of its own.

#### Scenario: A project key reaches its project

- **WHEN** a project key is presented for an operation on its own project
- **THEN** it is permitted, as its kind allows

#### Scenario: A project key reaches nothing else

- **WHEN** a project key is presented for an operation on another project
- **THEN** it is refused

#### Scenario: A system key acts as its role

- **WHEN** a system key is presented for an operation
- **THEN** it is permitted exactly where a member holding the same role would be

### Requirement: A project key does not outlive its project

Deleting a project SHALL delete the keys bound to it.

#### Scenario: The project goes and takes its keys

- **WHEN** a project holding keys is deleted
- **THEN** those keys no longer exist and no longer authenticate

### Requirement: A system key exists only where roles do

A system key SHALL NOT be creatable in a personal account.

The refusal SHALL report that such a key cannot exist there, distinguishably from a refusal of
permission.

#### Scenario: A personal account has no system keys

- **WHEN** a system key is requested in a personal account
- **THEN** it is refused as impossible rather than as forbidden

#### Scenario: A personal account still has project keys

- **WHEN** a project key is created in a personal account
- **THEN** it is created and works

### Requirement: A system key's role cannot be deleted from under it

Deleting a role held by a system key SHALL be refused.

A system key whose role is absent SHALL authorize nothing.

#### Scenario: The role is defended

- **WHEN** a role held by a system key is deleted
- **THEN** the deletion is refused and names what holds it

#### Scenario: An absent role grants nothing

- **WHEN** a system key's role cannot be resolved
- **THEN** the key authorizes nothing

### Requirement: A person may register themselves

The service's identity provider SHALL offer an unauthenticated person a way to create their own user,
reachable from the sign-in form.

The address given SHALL be verified before that person can authenticate, and the verification SHALL be
proof of control of the address rather than a declaration of it.

#### Scenario: A stranger can begin

- **WHEN** an unauthenticated person reaches the sign-in form
- **THEN** they are offered a way to register

#### Scenario: An unverified address does not authenticate

- **WHEN** a person completes registration without proving control of the address
- **THEN** they cannot authenticate

### Requirement: A self-registered person cannot authenticate until they are approved

Where approval is required, a person who registers themselves SHALL NOT be able to authenticate until
somebody approves them.

An unapproved person SHALL be told that their registration is awaiting review. They SHALL NOT be given
a refusal indistinguishable from a wrong password.

#### Scenario: A registered person waits, and knows it

- **WHEN** an unapproved person attempts to sign in
- **THEN** they are refused, and are told their registration is under review

#### Scenario: Approval admits them

- **WHEN** they are approved and sign in
- **THEN** they hold their own account and are an ordinary customer

### Requirement: The service holds nothing for a person who has not been admitted

No account SHALL be created for a person who has not been approved.

#### Scenario: Registration writes nothing to the service

- **WHEN** a person registers themselves while approval is required
- **THEN** the service holds no account for them until they first sign in

### Requirement: Whether approval is required is configurable, and is applied when a person registers

Whether a self-registered person requires approval SHALL be a matter of configuration, applied when
they register.

Changing it SHALL NOT change whether a person who has already registered may authenticate.

#### Scenario: Opening the door does not release those behind it

- **WHEN** approval ceases to be required
- **THEN** people who registered while it was required still cannot authenticate until approved

#### Scenario: Closing the door does not lock out those already admitted

- **WHEN** approval becomes required
- **THEN** everybody who could already authenticate still can

### Requirement: An invitation confirms an address; it does not admit a person

A person who becomes a user by following an invitation link SHALL have the invited address recorded as
confirmed without a confirmation message being sent.

That person SHALL NOT thereby be exempt from approval: while approval is required, they SHALL NOT be
able to authenticate until approved.

The service SHALL NOT provide a second enrolment path that creates users already approved.

#### Scenario: An invited person's address needs no confirmation

- **WHEN** a person registers by following an invitation link
- **THEN** the invited address is recorded as confirmed and no confirmation message is sent

#### Scenario: An invited person still waits

- **WHEN** a person registers by following an invitation link while approval is required
- **THEN** they cannot authenticate until approved

#### Scenario: There is one enrolment path

- **WHEN** the enrolment paths are enumerated
- **THEN** none of them creates a user that is approved on creation while approval is required

### Requirement: An invitation link proves control of the address it was sent to

The service SHALL treat following an invitation link as proof of control of the address the invitation
names, equivalent to following a confirmation token sent to that address.

A user created through an invitation SHALL have the invited address recorded as confirmed, and the
service SHALL NOT require a further confirmation of that address.

Registration reached by any other means SHALL be unchanged and SHALL still require confirmation.

#### Scenario: Signing up through an invitation needs no confirmation

- **WHEN** a person with no user follows an invitation link and registers
- **THEN** the user is created with the invited address confirmed, and no confirmation message is sent

#### Scenario: The new user proceeds directly to the offer

- **WHEN** a user has been created through an invitation link
- **THEN** they are asked whether to accept the invitation, without an intervening confirmation step

#### Scenario: Ordinary registration is unaffected

- **WHEN** a person registers without an invitation
- **THEN** the address is unconfirmed and confirmation is required as before

#### Scenario: The proof extends to no other address

- **WHEN** a user is created through an invitation link
- **THEN** only the invited address is recorded as confirmed

#### Scenario: Confirming the address does not admit the person

- **WHEN** a user is created through an invitation link while registrations require approval
- **THEN** the address is confirmed and the user still cannot authenticate until approved

### Requirement: A membership may hold no role, and confers nothing while it does

The service SHALL permit a membership that names no role.

A subject whose memberships name no role SHALL hold no grant and SHALL be unrestricted in no account,
and every authorization decision for it SHALL be a refusal.

Such a membership SHALL still make its holder a member: the account SHALL be selectable by them and
SHALL appear among the accounts they belong to.

A role SHALL be assignable to such a membership afterwards, after which it confers what that role
confers.

#### Scenario: A roleless member is authorised for nothing

- **WHEN** a member holding no role attempts any account-scoped operation
- **THEN** it is refused, and no object of the account is returned to them

#### Scenario: A roleless member is still a member

- **WHEN** a member holding no role lists the accounts they belong to
- **THEN** the account appears, and they may select it

#### Scenario: Assigning a role takes effect

- **WHEN** a role is assigned to a member who held none
- **THEN** the member holds what that role confers

