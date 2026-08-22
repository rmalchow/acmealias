## Context

The service is already an authoritative nameserver, for exactly one zone and effectively one record
type. `internal/dnsserver` answers TXT for alias labels, SOA and NS at the apex, and refuses
everything else. `internal/zoneview` holds what it answers from, as a `map[string][]store.ChallengeValue`
replaced wholesale on each refresh.

This change generalises both: many zones, all record types, records that belong to a customer rather
than to our challenge mechanism. The answering architecture — in-memory snapshot, no database on the
query path, replaced wholesale, expiry applied at read time — is kept because it is already correct
for a much harder problem than the one it currently solves.

Terms used throughout, defined once:

- **Zone** — a name we are authoritative for, because its owner delegated it to us by pointing their
  registrar's `NS` records at our nameservers. `example.com` is a zone; `www.example.com` is a name
  within it.
- **Recordset** — every record sharing one owner name and one type within a zone. `www.example.com/A`
  is one recordset; if it holds `203.0.113.10` and `203.0.113.11` it has two values and one TTL.
- **Apex** — the zone's own name, `example.com` itself. Special because `SOA` and `NS` live there and
  because `CNAME` may not.
- **Backend** — whatever a satellite actually gets records into so that queries are answered: our own
  memory, or an external nameserver.
- **Dynamic update** — RFC 2136, the standard protocol for changing a zone's contents over the wire,
  authenticated with a shared key (TSIG).

Constraints that shaped everything below:

1. **The centre is authoritative.** Anything else invalidates the `dns_record` grants.
2. **A recordset change is atomic.** There must be no instant at which a name resolves to nothing
   because it is being edited.
3. **A satellite must keep answering when the centre is unreachable** — inherited from `satellite-sync`
   and much more consequential here, since these are the records a customer's traffic depends on.
4. **The pipeline must be able to carry signatures later** without rework of the distribution path.

## Goals / Non-Goals

**Goals:**

- Zones and recordsets as owned inventory, with the existing permission model constraining them.
- One answering path shared between challenge TXT and customer records.
- A backend interface whose first implementation is the code that already exists and whose second is a
  standard protocol rather than a vendor API.
- A distribution format that can carry signatures without being redesigned.

**Non-Goals:**

- DNSSEC signing, key management or rollover. Refusing to accept a zone that is *already* signed is
  in scope and is R8 — it is a pre-flight check, not an implementation of DNSSEC.
- Health-checked recordsets and failover.
- Zone transfer between centre and satellite.
- Registrar integration, or secondary DNS for a zone whose primary is elsewhere.

## Decisions

### R1. The recordset is the unit of storage, not the record

```
zones      (id, account_id, project_id, name, soa_*, ttl_default)
recordsets (id, account_id, zone_id, name, type, ttl, values[])
```

`values` is a list because DNS says it is: several `A` records at one name are one RRset, they are
answered together, and they share a TTL. Modelling them as separate rows would mean regrouping them
on the answering path, on the distribution path and in the permission check — all three of which ask
"what is at this name, of this type".

Constraint 2 is the decisive argument. Replacing the two addresses behind `www.example.com` with two
different ones is one `UPDATE` of a values list. As individual rows it is two deletes and two inserts,
and the intermediate state — however brief, however transactional in the database — becomes a real
intermediate state on a satellite the moment it is distributed as separate changes.

`account_id` is materialized on both tables and constrained by the composite foreign keys
`tenancy-and-permissions` establishes, so a recordset cannot disagree with its zone about who owns it.

**Rejected: individual records with an ordinal.** Closer to a naive REST resource model, and it makes
"delete one value" a smaller operation. Rejected on atomicity, and because every reader regroups.

### R2. Validation is what keeps a zone servable, and it belongs at write time

A zone with contradictory records is not an error the DNS protocol reports; it is a zone that behaves
strangely and intermittently. The rules that must be enforced when a recordset is written:

- **`CNAME` is exclusive.** A name with a `CNAME` may hold no other type. This is the single most
  common way a hand-edited zone breaks, and it breaks it for every resolver differently.
- **No `CNAME` at the apex.** `example.com` must hold `SOA` and `NS`, so it cannot hold `CNAME`.
- **The apex `SOA` and `NS` are ours to maintain**, not the customer's to edit. A customer who deletes
  their `NS` records de-delegates their own zone from the servers that hold it.
- **Names must be within the zone.** A recordset named `www.other.com` in zone `example.com` is not a
  typo to be stored; it is unanswerable.
- **Type-specific syntax.** An `MX` needs a preference and a name; an `A` needs an IPv4 address. A
  malformed value that reaches the answering path is either a `SERVFAIL` or a wire-format error.

Enforcing these at write time rather than at serve time means an invalid zone cannot be stored, and
therefore cannot be distributed, and therefore cannot be discovered by a satellite at query time when
there is nothing useful to do about it.

### R3. The permission check happens against the recordset, and it is the first real use of the selector

`tenancy-and-permissions` specifies the `dns_record` selector and its matching rules; this is where
they are evaluated. A write to `web.staging.example.com/A` is checked against each of the subject's
grants: the zone must match, the name must match the pattern with `*` standing for one or more labels
and never the apex, and `A` must be in the grant's type set.

Two consequences worth stating because they will look like bugs otherwise:

- A grant of `*.staging.example.com` does **not** permit writing `staging.example.com`. That is the
  specified apex-exclusion, and the remedy is a second grant naming the apex explicitly.
- A grant of `*.example.com` **does** permit writing `staging.example.com`, because the apex a pattern
  excludes is only its own. Subtree grants nest, which is what makes the containment test in the
  subset rule tractable.

Deleting a recordset is a write to it, and is checked the same way. Reading a recordset is a separate
action, because a zone's contents are not secret to the internet but are secret to other tenants.

### R4. The backend is an interface; in-process first, dynamic update second

```go
type Backend interface {
    Apply(ctx context.Context, zone string, change Change) error
    Snapshot(ctx context.Context, zone string, rrsets []RRset) error
}
```

**In-process** is `zoneview` generalised. The view's key becomes `(name, type)` rather than a bare
label, its value becomes a recordset rather than a list of challenge values, and everything
structural — snapshot replaced wholesale under a write lock, read under a shared lock, expiry applied
at read time, previous snapshot kept when a load fails — is untouched. `Apply` mutates the map;
`Snapshot` replaces it. There is no external process, no rendered configuration, no reload to
sequence, and no second thing that can be down.

**RFC 2136 dynamic update** is the generic implementation. `Apply` becomes an update message
authenticated with TSIG; `Snapshot` becomes a sequence of deletes and adds, or an update with
prerequisites. It is chosen over vendor APIs because it is a protocol: the servers a customer already
runs — BIND, Knot, PowerDNS — already speak it, so one implementation covers all of them and covers
the ones we have not heard of.

The interface is deliberately small and deliberately expressed in terms of whole recordsets, because
that is the only vocabulary every backend shares. A backend that supports finer-grained operations
gains nothing from being asked to, and one that does not would have to emulate them.

**Rejected: a driver per DNS server** (a PowerDNS driver, a Knot driver, a CoreDNS driver). Each is
straightforward alone; together they are a matrix that must be tested against several versions of
several products, to support servers that already implement a standard we could implement once.
PowerDNS's HTTP API and Knot's control socket remain available as later additions where dynamic
update is genuinely insufficient, not as the first answer.

### R5. Distribution carries recordsets and a signature slot that nothing fills

`satellite-sync` distributes state as positioned events over a satellite-initiated stream. This change
adds recordsets to what is carried, in a shape that includes a place for signatures:

```
event: rrset
data: {"zone":"example.com","name":"www.example.com","type":"A","ttl":300,
       "values":["203.0.113.10","203.0.113.11"],"sig":null}
```

`sig` is always null in this change. It exists because adding a field to a distribution format that
satellites parse is a coordinated upgrade of the fleet, and doing it now costs a null and doing it
later costs a migration during which two formats are in flight.

The reason it must be *central* signing, when it arrives, is the threat model. A satellite holds every
tenant's zone data, which is not primarily a disclosure problem — DNS records are public data by
construction, and the point of publishing one is that strangers read it. The problem is a compromised
satellite serving answers nobody authorized. If keys live at the centre and satellites receive signed
material, a compromised satellite can withhold or serve stale answers but cannot forge: **mass outage
rather than mass hijack.** If satellites signed locally, every satellite would be a key holder and one
compromise would forge for everyone.

### R6. The apex is ours, the rest is theirs

`SOA` and `NS` at the apex are generated and maintained by the service from the fleet's identity, not
edited by the customer. The serial advances on every change to the zone.

This is not paternalism. A customer who edits their apex `NS` to remove one of our nameservers has
de-delegated part of their own zone, and the failure is intermittent and resolver-dependent, which is
the worst kind. The `SOA` minimum governs negative caching, which is exactly the mechanism
`metering-and-credits` relies on when it argues for `REFUSED` over `NXDOMAIN`.

What a customer needs — delegating a subtree to their own nameservers — is `NS` *below* the apex, which
is theirs to write, subject to a grant permitting the `NS` type.

### R8. A zone that is already signed is refused at delegation time

DNSSEC is out of scope, and that has one consequence that must be handled here rather than later,
because it is the worst failure this design can produce and the customer causes it by doing exactly
what the documentation tells them to.

The sequence: a customer's zone `example.com` is DNSSEC-signed today, wherever it is hosted. Signing
means their registrar publishes a `DS` record in the parent zone — for `example.com`, in `.com` —
which is a fingerprint of the key their current provider signs with. Every validating resolver, which
today is most of them, reads that `DS`, learns "answers for `example.com` must carry a signature that
matches this key", and refuses any answer that does not.

They then delegate the zone to us. We are not signing anything. So we answer correctly, unsigned, and
every validating resolver on the internet compares our unsigned answer against the `DS` still sitting
in `.com`, concludes the zone has been tampered with, and returns `SERVFAIL`. Not a wrong answer — no
answer. Web, mail and everything else for that domain is hard-down for the majority of the internet,
within one TTL, and the customer's own resolver may well be one of the minority that still works,
which makes it maddening to diagnose.

**The decision: when a zone is added, query the parent for a `DS` record at that name, and refuse the
zone if one exists**, with a message naming the cause and the remedy — remove the `DS` at your
registrar, wait for the parent's TTL to expire, then add the zone here.

Three things worth being explicit about:

- **This is not DNSSEC work.** It is one query for one record type against the same resolver
  machinery this change already needs for delegation verification, plus a refusal. It does not touch
  keys, signing, `NSEC`, or rollover, all of which remain firmly out of scope.
- **It fails closed on doubt, not open.** If the parent cannot be reached or the query times out, the
  zone is *not* accepted with a shrug; the customer is told to retry, because a false negative here
  costs them their domain and a false positive costs them a minute.
- **It has to be re-checked, not only checked once.** A customer can add a zone here first and add a
  `DS` at their registrar afterwards — enabling DNSSEC at a provider they have not finished leaving.
  So the check also runs periodically for served zones, and a `DS` appearing on a zone we serve
  unsigned raises an alert on the zone rather than silently continuing.

**Rejected: warning instead of refusing.** A warning in a console someone has already closed does not
prevent the outage, and the outage is total.

**Rejected: accepting the zone and serving `REFUSED` until the `DS` is gone.** Indistinguishable from
a broken delegation, and it makes us the visible cause of an outage we were trying to prevent.

**Rejected: deferring the check until DNSSEC signing exists.** The check is what makes deferring
DNSSEC *safe*. Shipping the product without it means the first customer with a signed zone discovers
the gap by taking their own domain off the internet.

### R9. The fleet's nameserver names are load-bearing, and their registration is a single point of failure

R6 generates apex `NS` records "from the fleet's identity". That identity — the hostnames customers
put in their registrar's delegation — is the most irreversible decision in this change, and it is not
a naming preference.

Once a hundred zones are delegated to `ns1.example.net` and `ns2.example.net`, those names are
permanent in a way nothing else here is. Changing them means a coordinated change at a hundred
registrars, performed by a hundred customers, each of whom must be persuaded to do it, with the old
names answering throughout. There is no migration that a small operator completes.

What follows is a constraint rather than a name:

- **At least two nameserver names**, which every registry requires anyway.
- **Spread across at least two parent domains, in at least two different top-level domains,
  registered at two different registrars.** The failure this prevents is one expired registration,
  one registrar dispute, one registry-level suspension, or one compromised registrar account taking
  *every customer's DNS down at once* — because every delegation points into the same parent. It is
  the only mitigation available to an operator who cannot run their own anycast, and it costs the
  price of a second domain.
- **Registry lock and auto-renew on both**, with renewal reminders held by a person, not only by the
  registrar's mail.
- **Glue records** at the parent for any nameserver name that lives inside a domain we ourselves
  serve, since otherwise resolving the nameserver requires asking the nameserver.

The concrete names are not decided in this change. Every name appearing in these documents —
`acme.example.net`, `ns1.example.net` — is illustrative, and the eventual names are configuration
read by the apex generator in R6, not constants.

**Rejected: one domain with `ns1`/`ns2`/`ns3` under it.** The common arrangement, and the reason it is
common is that large operators mitigate the shared-parent risk with contracts and registry locks and
staff. At this size the second domain is cheaper than the mitigation.

### R7. One answering path for challenge TXT and customer records

The challenge alias labels do not get a separate mechanism. They become recordsets in the served
challenge zone: name `k4m2p9.acme.example.net`, type `TXT`, values the active challenge values, TTL
the configured challenge TTL. `internal/dnsserver` answers them by the same lookup it uses for
`www.example.com/A`.

This keeps one implementation of the things that are easy to get wrong — the difference between a name
that does not exist (`NXDOMAIN`) and a name that exists with no records of the requested type
(`NOERROR` with the `SOA` in authority), which `zoneview` was built to preserve, and which now has to
hold across many zones and many types.

Challenge values keep their per-value expiry, applied at read time. Ordinary recordsets have no
expiry; they persist until changed.

## Risks / Trade-offs

- **A credential with a broad grant can intercept mail** → the type constraint in the selector exists
  precisely for this; a grant of `{A, AAAA}` cannot write `MX` or `NS`. This change's safety rests on
  `tenancy-and-permissions` being in place first.
- **An invalid zone is stored and discovered at query time** → validation at write time (R2); an
  invalid recordset cannot be stored, so it cannot be distributed.
- **A recordset change is observed half-applied** → whole-recordset operations end to end (R1, R4); the
  distribution event carries the complete new state, never a mutation of part of it.
- **A compromised satellite forges answers** → not solved here, and stated rather than implied: it is
  solved by central signing, for which R5 shapes the pipeline.
- **The in-process backend becomes a bottleneck for large zones** → the view is a map replaced
  wholesale; a very large zone makes each snapshot expensive. Delta application is the mitigation and
  is already how the stream works; snapshot remains the correctness fallback.
- **RFC 2136 is refused or partially supported by some server** → it is the second implementation, not
  the first; the fleet we run uses in-process, and dynamic update is for customers who ask.
- **The apex being ours frustrates a customer with an unusual requirement** → `NS` below the apex is
  theirs, which covers delegation; the apex records that are genuinely theirs to choose (TTL) are
  configurable.

## Migration Plan

1. Add `zones` and `recordsets` with materialized `account_id` and composite foreign keys.
2. Add write-time validation (R2) with the `CNAME` exclusivity and apex rules.
3. Generalise `internal/zoneview` from labels-to-TXT to recordsets keyed by name and type; keep the
   snapshot, locking, fail-safe and read-time expiry behaviour.
4. Generalise `internal/dnsserver` to serve several zones and all record types, keeping its refusal to
   recurse and to answer out-of-zone names.
5. Represent challenge aliases as recordsets in the served zone (R7); confirm the challenge acceptance
   suite is unchanged.
6. Add `internal/dnsbackend` with the in-process implementation.
7. Extend the distribution stream with `rrset` events carrying a null `sig`.
8. Add the RFC 2136 implementation behind the same interface.
9. Add zone and recordset routes, gated by the `dns_record` selector, and the console screens.

**Rollback**: steps 6–9 are additive. Step 3 and 4 are the risky ones and are gated by the existing
challenge acceptance suite, which must pass unchanged before anything is delegated to the new path.

## Open Questions

- **What is the default TTL for a customer recordset?** `internal/inventory/plan.go` already defines
  `DefaultTTL = 300` for the records a customer publishes in *their* zone, with a comment noting it has
  nothing to do with the challenge TTL. Whether zones we serve should share that number or have their
  own is unresolved — and `health-checks` will want a lower one for health-checked names.
- **How many recordsets may a zone hold, and how large may a values list be?** Both are quota
  questions that interact with `metering-and-credits`, and both are also protection against a zone
  that cannot be distributed in one event.
- **Does a customer import an existing zone, and how?** Zone-file import is the obvious onboarding
  path and is not specified here.
- **How often is the `DS` re-check run for served zones (R8), and what does it do beyond alerting?**
  Daily is the obvious cadence. Whether a `DS` appearing on a served zone should eventually suspend
  the zone — trading a deliberate outage for an accidental one — is deliberately left open until
  there is an operator to answer the alert.
- **Where does the challenge zone's own delegation live** once the fleet serves many zones — is it a
  zone row like any other, or configuration? Treating it as a row is more uniform and makes it
  visible in a way configuration is not.
