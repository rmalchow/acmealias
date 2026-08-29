## Context

Four pieces of work shipped in one session and this is the change written after them, which is
deliberate rather than lazy: the last change written this way found two requirements the code had
quietly contradicted, and writing this one found four more, plus a live compliance gap on the public
site that nobody had noticed.

The four are related more tightly than they look. Three of them are about the same question — *what
does this deployment actually do* — asked of money, of a role, and of an address; and each had an
answer hardcoded somewhere that had stopped being true.

## Goals / Non-Goals

**Goals:**

- Make "does this deployment charge" one setting that every downstream rule reads, so that turning it
  on is the whole of starting to charge.
- Let an account change somebody's role without dismantling their membership.
- Make an invitation reach the person it was sent to.
- State, in the specs, what the deployment now does, so the repository stops disagreeing with
  production.

**Non-Goals:**

- Building payment. `public-edition` group 4 specified it and this change turns it off instead.
- Protective limits. They are more necessary now, not less, and they are carried forward in that
  change's archive rather than smuggled in here.
- Changing what a role means, who may hold several, or how grants are evaluated.

## Decisions

### One flag, read in one function, passed as a parameter

`store.Usable(acc, monetized)` is the single place the usability rule lives (design C3, C6), so the
flag goes there and nowhere else. It is a PARAMETER rather than a package variable: a formula that
reads hidden state is a formula nobody can test a row against, and the alternative is a global whose
value depends on whether something called a setter first. Threading it costs four call sites.

Rejected: creating team accounts with an enormous balance. Two lines rather than this, and it writes
a fiction into the column on the day the answer is meant to change — every account created while the
service was free carries a nine-million-euro balance somebody must unpick before the first real
charge, and the credit check goes on running, refusing anybody whose fiction runs out. A switch says
what is true, which is that nobody is charged yet.

The sweep for other gates found two floors beneath the handler, and the second is what surfaced the
bug in front of a user pressing Create: `store.CreateAccountWithOwner` refused a zero balance because
"0 leaves it suspended from its first instant", a premise true only where a balance suspends
anything. `adminctl account create` had the same floor. Background workers — certificate observation,
the activation poller, the sweeper — have no funding gate at all and never did; named here because a
sweep that finds nothing should say where it looked.

### Role changes: relax the write, keep the guards, add one that is not about data

`AssignRole` carried `WHERE role_id IS NULL`, which made assignment the last step of an invitation and
nothing else. What that clause was accidentally providing is two real protections, and they are kept
explicitly: the owner stays in the unrestricted role, and the last administrator stays one — read with
`SELECT … FOR UPDATE` inside the transaction, because without the lock two concurrent changes each see
two administrators, each concludes one will remain, and together they demote both. Those two are now
shared with removal, since demoting the last administrator and removing them strand an account
identically.

The third refusal is different in kind and therefore lives somewhere else. "Nobody changes their own
role" is about who is asking, the store never learns who is asking, and so it is enforced in the
handler. It is compared by PERSON and not by membership row: memberships are per-role, one person can
hold several, and editing "some other row that happens to be mine" alters my own authority just as
surely.

### Trust the address, and write down what makes it trustworthy

Requiring `email_verified` was correct reasoning against a provider that answers the question.
authentik does not: its stock scope mapping returns `"email_verified": False` hardcoded for every
user, including one whose address it proved by emailing a link. So the check never separated verified
from unverified — it discarded every address there is, `users.email` was NULL for the entire
deployment, and no invitation could match anybody.

The claim is accepted now, and the justification is upstream of the claim: nobody reaches this
service without an authentik account, and authentik's enrolment confirms the address before the
account can be used. The condition under which this stops being safe — a federated source whose
addresses this deployment never confirmed — is written on the function and in the spec, because it is
the kind of thing that is otherwise rediscovered by incident.

### Approval is the box that says approval

Not strictly part of the same story, but found by the same walk. The enrolment blueprint says the
gate is `create_users_as_inactive` "AND NOTHING ELSE" and that approving somebody is ticking Active.
The deny stage gated on the directory path, nothing moves an approved user out of that folder, and so
approving changed nothing anyone could feel. The policy reads `is_active` now; the path is kept only
to choose the wording, since a deny firing on every inactive user would tell somebody an
administrator had disabled that their registration was under review.

## Risks / Trade-offs

- **The paid path stops being exercised end to end.** → The acceptance suite reads `monetized` from
  `/me` and asserts accordingly rather than assuming, so it is honest either way; what is not running
  while the switch is off is the proof that a lapsed account refuses a member with `402`.
  `ACME_MONETIZED=1` restores it and the predicate is unit-tested in both modes. This is a real loss
  and the mitigation is a setting, not a fix.
- **Nothing bounds an account now.** → The balance was the only limit that existed, and it now limits
  nobody. Protective limits were already specified and unbuilt; deferring payment made them load
  bearing. Carried in the `public-edition` archive.
- **An approved user stays in the reviewer's queue folder.** → Nothing prunes it. Named on the policy;
  the fix, if it matters, is a second `user_write` on approval rather than another policy reading
  another path.
- **Accepting the email claim is weaker than verifying it.** → True, and it is weaker than nothing was
  before only in appearance: the previous behaviour was not "verify", it was "discard". The
  substantive protection moved from a claim nobody sets to an enrolment flow that actually runs.

## Migration Plan

No data migration and no schema change. `ACME_MONETIZED` defaults to false, so a deployment that says
nothing gets the unmonetized behaviour; production sets it explicitly in its compose environment
rather than relying on that, because a reader of the file should not have to know the default to know
what the deployment does with money.

Turning it on is the whole of starting to charge: the console screens, the funding predicate and the
creation floor all read the same flag, and the credit rows, top-ups and audit entries were kept
throughout.

Rollback is the previous image plus the previous value of the flag.

## Open Questions

- Should an approved user be moved out of `users/pending-approval` automatically? It makes the queue
  correct and adds a write to a flow that is already long.
- Should the roster show which of several memberships a person holds, now that a role can be changed
  in place? Today it lists one row per membership and says nothing about the fact that they are the
  same person.
