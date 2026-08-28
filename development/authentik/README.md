# The identity provider, and the approval gate in front of it

The blueprint next door (`blueprints/acmeservice.yaml`) is the authority on what exists; this
file is the operator's half — the two procedures that are actions somebody takes rather than
objects a blueprint declares.

The specification is `openspec/changes/self-service-signup/`; where the two disagree, that wins.

## Approving somebody

A person who registers themselves at `console.localhost` is created **inactive**, and is
therefore unable to authenticate at all. Approving them is one action:

1. Open authentik's admin interface at <http://auth.localhost/if/admin/>.
2. Go to **Directory → Users**, and open the folder **`users/pending-approval`**.
3. Open the person, tick **Active**, save.

The folder is the queue, and nobody built it: the enrolment flow puts people there and
authentik renders user paths as a tree. It contains only people whose **address has been
proved** — somebody who registered and never clicked the confirmation link stays in
`users/pending-verification`, and approving one of those admits an address that may not
exist. That ordering is the whole reason the flow writes the user twice.

**Refusing somebody** is leaving them alone, or deleting the user. Neither needs anything
from us. Whether a refused person may register again is the identity blocklist's question
(`staff-plane`), not this one.

**What a reviewer has to go on is short** — an address, when it registered, that it is
confirmed — and the shortness is honest rather than a limitation of the screen. There is
nothing else known about a stranger, and a richer interface would only invite somebody to
look for signal that is not there.

**We promise twelve hours.** That is not manners. An invitation expires fourteen days after
it is *issued*, and that clock runs while its invitee waits for us: somebody invited on the
1st who registers on the 1st and is approved on the 16th signs in to find the invitation they
registered in order to accept has expired. Any response time longer than fourteen days is a
promise to break invitations.

## Turning the gate off

The gate is one key — `create_users_as_inactive` on the enrolment flow's
`acmeservice-enrollment-user-write` stage. Clearing it stops the flow creating inactive
users.

**It does not activate the people it already created.** That asymmetry is right in one
direction and a trap in the other:

- Turning the gate **on** cannot lock anybody out. Existing users keep `is_active`, and a
  setting somebody tightens must never suspend the customers already using the product.
- Turning it **off** leaves the backlog exactly where it is, waiting for a gate that no
  longer exists.

So switching it off is two steps, and the second is not optional:

1. Clear `create_users_as_inactive` in `blueprints/acmeservice.yaml` and apply the blueprint.
2. **Activate everybody still held in `users/pending-approval`**, deliberately, looking at
   them.

The alternative — making the flow retroactive, so that clearing the key released the backlog
— was rejected. A config change that silently admits a batch of real people, attributed to
nobody, at a moment when nobody is looking at them, is not an improvement on remembering.

## Applying a blueprint change immediately

Blueprints are applied by the **worker**, not the server, and the scheduled apply hides its
errors. To apply now and read them:

```
docker exec acmeservice-dev-authentik-worker-1 ak apply_blueprint /blueprints/custom/acmeservice.yaml
```

Entries apply **in order**. A `!Find` for an object defined further down the file resolves to
null and fails with "This field may not be null" — naming the field, not the ordering.

## A known defect: an unapproved person is not told why

Design S3 requires that somebody who cannot sign in yet is told so, rather than being given a
refusal indistinguishable from a wrong password. **authentik 2024.10 cannot do that while
`is_active` is the gate**, and this is a diagnosis rather than an omission — see
`openspec/changes/self-service-signup/tasks.md` task 2.2, which records what was measured and
what the two ways out cost.

The end of the enrolment flow does say it, clearly, which is where most people meet it.
