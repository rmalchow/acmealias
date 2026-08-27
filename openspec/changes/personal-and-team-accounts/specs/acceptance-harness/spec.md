## MODIFIED Requirements

### Requirement: The acceptance suite exercises sign-in and role enforcement against a real identity provider

The suite SHALL complete the authorization code flow against the stack's identity provider over
HTTP, without a browser, for each seeded user, and SHALL use the resulting session to assert that
authority is held per account: a signed-in user with no account selected holds no role and no
permission, and a user holds a role because they are a member of an account with that role. The
suite SHALL assert that a group claim grants nothing. The suite SHALL NOT weaken the service's own
cookie attributes in order to complete the flow.

#### Scenario: Sign-in establishes a session and selects nothing

- **WHEN** a seeded user completes the sign-in flow
- **THEN** a session is established, no account is selected, and the identity endpoint reports the user's groups with no roles and no permissions

#### Scenario: A role is held in one account and nowhere else

- **WHEN** that user selects an account they are a member of
- **THEN** the identity endpoint reports the role their membership names, and reports it only while that account is selected

#### Scenario: A read-only user is refused a change

- **WHEN** the user holding the `READ_ONLY` role in that account attempts to create a domain
- **THEN** the request is refused

#### Scenario: A configuration administrator is allowed the same change

- **WHEN** the user holding `CONFIG_ADMIN` attempts the same creation
- **THEN** the request succeeds

#### Scenario: Key rotation is refused the inventory but allowed a key

- **WHEN** the user holding `KEY_ROTATION` attempts to create a domain and then to create an API key
- **THEN** the domain creation is refused and the key creation succeeds

## ADDED Requirements

### Requirement: The acceptance suite walks both kinds of account end to end

The suite SHALL assert, against the running stack, that a user who has just signed in is offered
somewhere to work without having created anything, and that both kinds of account behave as
specified: a personal account is usable immediately and refuses a roster, and a team account is
created by paying, holds other people, stops when its balance reaches zero and works again when it
is credited.

#### Scenario: The picker is never empty

- **WHEN** any user signs in
- **THEN** the accounts they may act in include their personal account, so that "no account selected" means they have not said which and never that they have nothing

#### Scenario: The personal arc

- **WHEN** a user selects their personal account, creates a project, a load balancer and a domain, and publishes a challenge with a key created there
- **THEN** every step succeeds, the challenge value is answerable at the alias and is withdrawn again, while roles and members in that account are refused

#### Scenario: The team arc

- **WHEN** a user buys a team account, invites another user into it, the balance reaches zero, and the account is then credited again
- **THEN** the invited user and the challenge API are both refused with payment required while the balance is zero, the owner can still reach the account to pay, and both work again afterwards without a new session or a new key

#### Scenario: A quota is not a permission

- **WHEN** a personal account is filled to its project limit and one more is created
- **THEN** the refusal says there is no room rather than that the caller lacks permission
