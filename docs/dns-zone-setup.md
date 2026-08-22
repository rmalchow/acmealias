> **docs/ is for humans, not for AI.** Authoritative specification: [`../openspec/`](../openspec/).

# DNS zone setup

The one-time delegation that makes this service authoritative. Done once, by whoever administers
the parent zone, before any customer is onboarded.

> **Do this before anyone depends on it**
>
> Publish and verify the delegation while there are no customer CNAME records pointing at us. A
> mistake at that point affects nothing. The same mistake after ten customers have been onboarded
> breaks all ten at once, and DNS caching means it stays broken for hours after you fix it.

## Step 0 — decide the names

Three names need choosing, and one rule governs them. Throughout this documentation the examples
use these values; substitute your own consistently.

| Purpose | Example value | Constraint |
| --- | --- | --- |
| The delegated zone | `acmeservice.cloudpowered.services` | A subdomain of a zone we already control. This is what customer CNAMEs will point into, so it appears in other people's zone files — keep it recognisable. |
| Challenge API hostname | `acme-api.cloudpowered.services` | **Must be outside the delegated zone.** |
| Admin console hostname | `acme-console.cloudpowered.services` | **Must be outside the delegated zone.** |

> **Why the HTTPS names must live outside the delegated zone**
>
> After delegation, this service is the final word on every name under
> `acmeservice.cloudpowered.services` — and it answers only `TXT`,
> `SOA` and `NS`. It has no `A` record capability at all. A name
> like `api.acmeservice.cloudpowered.services` would therefore resolve to nothing, and
> the failure looks like a broken proxy rather than a naming mistake.

## Step 1 — choose the nameserver names

A delegation says "ask these nameservers instead", by name. Where those names live decides whether
you also need **glue records**.

| Approach | Example | Glue needed? |
| --- | --- | --- |
| **Out-of-zone nameserver names** — *recommended* | `ns1.cloudpowered.services` | **No.** The name lives in the parent zone, which resolvers can already resolve normally. Nothing special to configure. |
| In-zone nameserver names | `ns1.acmeservice.cloudpowered.services` | **Yes.** Resolving the nameserver's address would require asking the nameserver you are trying to find. The parent zone must therefore publish address records for it — "glue" — and those must be kept in sync by hand forever. |

Use out-of-zone names. The glue variant buys nothing here and adds a record that silently rots when
the address changes.

> **How many nameservers?**
>
> Convention — and some registrar validators — expect at least two `NS` records. With a
> single instance, two names both resolving to the same address is honest but provides no
> redundancy. Publish two names anyway: it satisfies the validators, and the day a second instance
> exists you change one address record instead of touching the parent zone's delegation.

## Step 2 — add the records to the parent zone

The service is dual-stack — it has both an IPv4 and an IPv6 address, and both must serve the zone
identically. In `cloudpowered.services`, with the service at `203.0.113.53` and
`2001:db8::53`:

```
; --- delegation of the challenge zone ---
acmeservice.cloudpowered.services.   3600  IN  NS     ns1.cloudpowered.services.
acmeservice.cloudpowered.services.   3600  IN  NS     ns2.cloudpowered.services.

; --- addresses of those nameservers (ordinary records, not glue) ---
ns1.cloudpowered.services.           3600  IN  A      203.0.113.53
ns2.cloudpowered.services.           3600  IN  A      203.0.113.53
ns1.cloudpowered.services.           3600  IN  AAAA   2001:db8::53
ns2.cloudpowered.services.           3600  IN  AAAA   2001:db8::53

; --- the two HTTPS hostnames, pointing at our existing proxy ---
acme-api.cloudpowered.services.       3600  IN  A     198.51.100.20
acme-console.cloudpowered.services.   3600  IN  A     198.51.100.20
```

> **If the parent zone is DNSSEC-signed**
>
> Publish **no `DS` record** for
> `acmeservice.cloudpowered.services`. This service does not sign its zone, and a
> `DS` record pointing at an unsigned child makes every validating resolver — Let's
> Encrypt's included — treat all our answers as bogus. Some registrar interfaces add a DS record
> automatically when you create a delegation; check afterwards.

## Step 3 — configure the zone on the service

The service serves the zone's own housekeeping records from configuration. These must agree with
what you published in the parent, or resolvers will complain about a lame or inconsistent
delegation.

```
ACME_ZONE=acmeservice.cloudpowered.services
ACME_SOA_PRIMARY_NS=ns1.cloudpowered.services
ACME_SOA_HOSTMASTER=hostmaster@cloudpowered.services
ACME_NS_NAMES=ns1.cloudpowered.services,ns2.cloudpowered.services
ACME_SOA_MINIMUM=30s
ACME_CHALLENGE_TTL=30s
```

### Why the two TTLs are so short

| Setting | Governs | Why low |
| --- | --- | --- |
| `CHALLENGE_TTL` | How long a resolver may cache a challenge answer | A challenge lives about ninety seconds. Caching it longer than that only risks a stale value outliving the issuance it belonged to. |
| `SOA_MINIMUM` | **Negative** caching — how long "no such record" is remembered | This is the one that bites. A validator that asks a moment before Traefik publishes gets an empty answer, and with a large SOA minimum it will keep believing that for hours, failing the issuance long after the record exists. |

## Step 4 — verify

Run these in order. The first three work before the delegation exists, by asking the service
directly.

### Against the service itself

```
# 1. The zone apex must answer SOA authoritatively (look for "flags: qr aa")
dig @203.0.113.53 acmeservice.cloudpowered.services SOA +norecurse
dig @2001:db8::53 acmeservice.cloudpowered.services SOA +norecurse   # and over IPv6

# 2. The NS set must match what the parent zone publishes
dig @203.0.113.53 acmeservice.cloudpowered.services NS +norecurse

# 3. The same query over TCP — this is the check everyone forgets — both families
dig @203.0.113.53 +tcp acmeservice.cloudpowered.services SOA
dig @2001:db8::53 +tcp acmeservice.cloudpowered.services SOA

# 4. An unknown name inside the zone must be NXDOMAIN, with the SOA in AUTHORITY
dig @203.0.113.53 nothing-here.acmeservice.cloudpowered.services TXT

# 5. Anything outside the zone must be REFUSED — proves we are not an open resolver
dig @203.0.113.53 www.example.org A
```

| # | Expected | If you get something else |
| --- | --- | --- |
| 1 | `NOERROR`, `aa` flag set, one SOA in ANSWER — over **both** IPv4 and IPv6 | No answer at all → the service is not listening, or that address family is filtered. Missing `aa` → wrong zone configured. Works over one family but not the other → the service is not actually bound to both addresses, or only one made it into the parent zone's NS address records. |
| 2 | The two NS names, matching the parent exactly | A mismatch is a lame delegation. Fix the service configuration, not the parent. |
| 3 | Identical to #1, over both families | Timeout → TCP/53 is blocked for that family. Wildcard issuance will fail intermittently later. Fix this now. |
| 4 | `NXDOMAIN` with SOA in AUTHORITY | `REFUSED` → the zone name is misconfigured. |
| 5 | `REFUSED` | An actual answer means it is behaving as a resolver — stop and escalate, that is an abuse vector. |

### Through the public internet, after the delegation is published

```
# 6. Follow the delegation from the root — the authoritative end-to-end check
dig +trace acmeservice.cloudpowered.services SOA

# 7. Two independent public resolvers must agree
dig @1.1.1.1 acmeservice.cloudpowered.services NS
dig @8.8.8.8 acmeservice.cloudpowered.services NS

# 8. There must be NO DS record for the delegation
dig acmeservice.cloudpowered.services DS

# 9. Validating resolvers must not consider the zone bogus
dig @1.1.1.1 +dnssec acmeservice.cloudpowered.services SOA
```

| # | Expected |
| --- | --- |
| 6 | The trace ends at `ns1`/`ns2` and returns the SOA. If it stops at the parent, the delegation is not published or has not propagated. |
| 7 | Both return the same two NS names. A `SERVFAIL` here while step 1 works is almost always DNSSEC — see step 8. |
| 8 | `NOERROR` with an **empty** answer. Any DS record present must be removed. |
| 9 | `NOERROR`, and the `ad` flag *absent* — an insecure, unsigned delegation. `SERVFAIL` means a stale DS record is breaking validation. |

## What comes next

With the delegation live and verified, the zone is empty and answers `NXDOMAIN` for
every name in it — which is correct. Names appear as domains are added to the inventory, one alias
label each. Nothing further is ever needed in the parent zone.

Next: [register a load balancer and onboard its first domain](load-balancer-setup.md).

## Mistakes worth avoiding

| Mistake | Symptom |
| --- | --- |
| TCP/53 blocked, UDP open | Ordinary certificates work; wildcard certificates fail intermittently and inexplicably. |
| DS record published for an unsigned zone | `SERVFAIL` from validating resolvers only. Direct `dig` at the server looks perfect, which sends people down the wrong path for hours. |
| NS names in the service config differ from the parent | Works, but is a lame delegation; some validators and monitoring systems flag it, and it will confuse whoever debugs the next problem. |
| Large `SOA_MINIMUM` | Renewals fail with "record not found" long after the record was published. |
| API hostname inside the delegated zone | Traefik cannot resolve the endpoint at all; every issuance fails immediately with a connection error. |
| Address changed without a parent-zone change | Silent for hours, then total failure as caches expire. |

---

1 · DNS zone setup — [← Overview](README.md) · Next: [Per-load-balancer setup →](load-balancer-setup.md)
