# Build order

Eight changes are proposed and none is implemented. They have dependencies on each other, they are
built by one person, and several of them exist only to serve customers who do not exist yet. This
file records the order they are built in and — more usefully — what has to become true before the
later ones are worth starting.

It is not a schedule. It is a sequence with triggers.

## The order

1. **`tenancy-and-permissions`** — the floor. Everything else assumes accounts, anchored grants,
   `Scope`, and the database-enforced isolation of P20. It is also the only change that is
   substantially cheaper to do now than later, because it rewrites the shape of every table while the
   only data in them is ours.
2. **`account-invitations`** — the entry path. While `public-edition`'s self-registration switch is
   off, an invitation is the only way a person becomes a user, so this is what onboarding the first
   customers actually runs on.
3. **First customers, on challenge answering alone.** No satellites, no zones, no checks. Worth
   pausing here deliberately rather than treating it as a milestone passed on the way to something
   else: it is the whole product for a customer who wants certificates and keeps their DNS where it
   is, and its worst outage is invisible. A renewal has about thirty days of slack, so a service
   interruption of a few hours costs nobody anything. Nothing else in this sequence is that forgiving.
4. **`satellite-sync`** — answering has to be separable from deciding before there is anything worth
   answering in more than one place.
5. **`dns-records`** — zones and recordsets, the delegation-signer pre-flight of R8, and the
   nameserver-naming constraint of R9. From here on a mistake takes a customer's domain off the
   internet, which is the step change this sequence exists to make visible.
6. **`health-checks`** — needs satellites to probe from and records to steer.
7. **`metering-and-credits`** — needs something to meter. Deferred this far because credits are
   granted by hand while the customers are people we know, and because the billable shapes only
   settle once checks and zones exist.
8. **`public-edition`** — the residue: self-registration, payment, abuse limits, the marketing site.

`staff-plane` sits outside the numbering. Its suspension button (S10) and blocklists (S12) are needed
**before the first customer we do not know personally**, wherever in the sequence that falls. Its
support-access machinery can follow later.

## Triggers, not dates

- **Satellites and zones** wait until a customer wants DNS hosted, not until the specs feel finished.
- **`metering-and-credits`** waits until manual credit grants stop being reasonable — which is a
  count of customers, not a date.
- **`public-edition`'s registration switch** and payment integration wait until strangers arrive.
  Both are already built and inert by then (U9), so opening them is configuration.
- **`staff-plane`'s suspension path** must exist before the first stranger, because discovering it is
  missing during a live phishing incident is the wrong time to write it.

## What this sequence is protecting against

One person can hold one change in their head at a time. Specs four through eight will rot if they are
written and then left for a year, so the value of the order is that each change is implemented while
its reasoning is still recent — and that the two changes with the largest irreversible surface, the
data model and the nameserver names, are decided first and last respectively.
