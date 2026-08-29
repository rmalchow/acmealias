## Why

Four pieces of work landed in one session, and the repository now says the opposite of what runs in
production in at least three places. Writing them up is not bookkeeping: the last time a change was
written after the fact it surfaced two requirements the code had quietly contradicted, and the same
exercise here found four more.

**The console asked for money it cannot take.** "Buy now", an amount in EUR, a balance in Settings, a
warning below 25 EUR, and a dialog explaining that payment is not live — a complete purchase flow in
front of a service with no payment provider behind it (that is `public-edition` group 4, and none of
its eight tasks is done). Underneath, `store.Usable` refused any team account whose balance was zero,
so an account nobody had been asked to pay for was suspended from its first instant.

**Roles could be given but never changed.** `store.AssignRole` carried `WHERE role_id IS NULL` and a
comment saying that re-pointing a membership was "a remove and an add". Moving somebody from
read-only to operator therefore meant taking them out of the account and putting them back — which
destroys the membership row and its history, and is refused outright for the last holder of the
unrestricted role, so the one person most likely to need a change was the one person who could not
have one.

**No user had an email address, so no invitation could ever be shown.** An invitation binds to an
address (`account-invitations` V2), and `users.email` was NULL for every account in the deployment,
including accounts that had signed in dozens of times. The cause was a claim check that could never
pass: authentik's stock `email` scope mapping returns `"email_verified": False` hardcoded, for every
user, including one whose address authentik itself proved by emailing a link and waiting for the
click. So the check never distinguished verified addresses from unverified ones — it discarded all of
them, and the invitation screens had never had anything to display.

**And approving a person did nothing.** The enrolment blueprint says, in capitals, that the gate is
`create_users_as_inactive` "AND NOTHING ELSE" and that "APPROVING SOMEBODY IS TICKING `Active` ON
THEIR USER". The deny stage that gives an unapproved person a specific message gated on the user's
directory PATH instead, and nothing moves an approved user out of that folder — so a person could be
ticked Active and still be told their registration was under review, with the deciding value on a
screen nobody thinks to open.

## What Changes

- **A monetization switch.** `ACME_MONETIZED` (`limits.monetized`, default **false**) decides whether
  a balance gates anything. Off: a team account is created without a payment, works at a balance of
  zero, and the console shows no price, no balance and no Buy anywhere. The credit machinery is
  untouched — balances stored, top-ups recorded, the audit trail carrying them, both console paths
  kept behind the flag — so turning it on is the whole of starting to charge.
- **BREAKING (reported behaviour):** a team account with a zero balance is usable where the
  deployment is not monetized. `store.Usable` takes the flag, and it is the only place the rule
  lives.
- **A role can be changed in place**, whether the membership held one or not, subject to three
  refusals: the owner keeps the unrestricted role, the last administrator stays one (read with
  `SELECT … FOR UPDATE`, as removal already does), and a duplicate membership is a conflict.
- **Nobody changes their own role.** A separation rule rather than a stranding guard, enforced where
  the identity of the caller is known, and compared by user rather than by membership row.
- **The roster shows `Name (address)`** and no longer prints the identity-provider subject.
- **An empty invitation list is one line**, not a table of headings; cancelling is confirmed, and any
  administrator may cancel rather than only the sender — which was already true of the service and is
  now true of the console.
- **The email claim is taken as given.** What makes the address trustworthy is authentik's enrolment,
  which confirms it before the account can be used — not a claim the provider hardcodes to false.
- **Approval reads `is_active`**, with the directory path kept only to choose the wording of the
  refusal.

## Capabilities

### New Capabilities

None. Every item changes how an existing capability behaves.

### Modified Capabilities

- `accounts-and-tenancy`: whether a balance gates an account becomes a property of the deployment
  rather than an invariant; creating a team account no longer requires a payment where nothing is
  charged.
- `identity-and-roles`: a role may be changed and not only assigned; nobody may change their own; the
  address a person authenticates with is recorded from the provider, and what makes it trustworthy is
  named.
- `account-invitations`: an administrator other than the sender may cancel a pending invitation, and
  the console states an empty list rather than rendering one.
- `admin-console`: no price, balance or purchase is shown where the deployment charges nothing; the
  member roster identifies people by name and address.

## Impact

- **Backend.** `internal/config` gains the flag; `internal/store` threads it through `Usable`,
  `SuspensionReason` and `CreateAccountWithOwner`, and gains `SetRole` plus the shared
  `guardLeavingUnrestricted`; `internal/httpapi` gains the self-guard, the `email` field on the member
  view and `monetized` on `/me`; `internal/oidcauth` stops requiring `email_verified`;
  `internal/adminctl` stops requiring an amount.
- **Console.** The upgrade screen, the checkout dialog, Settings, the account picker, the shell's
  low-balance banner, and the roles and members panel.
- **Identity provider.** The development blueprint's approval policy, and the greeting in all four
  mail templates in both deployments.
- **Acceptance.** The funding assertions read `monetized` from `/me` rather than assuming, so the
  suite is honest against a deployment configured either way. While the switch is off, the end-to-end
  proof that a lapsed account refuses a member with `402` does not run; `ACME_MONETIZED=1` restores
  it, and the predicate itself is unit-tested in both modes.
- **Not addressed.** Payment itself (`public-edition` group 4) and the protective limits
  (`public-edition` group 3) remain unbuilt, and the second matters more now that nobody pays: a
  limit that holds whether or not anybody has paid is the only thing bounding what one account can do
  to a shared resource.
