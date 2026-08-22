## Why

### The service answers one question about one kind of name

`internal/dnsserver` is an authoritative nameserver, but a deliberately narrow one. Its package
documentation describes the whole of it:

> *"It answers TXT for alias labels, SOA and NS at the apex, and refuses everything else; it never
> recurses and never touches the database on the query path."*

That narrowness is a feature of the challenge product. A customer publishes one `CNAME` — say
`_acme-challenge.shop.example.com` pointing at `k4m2p9.acme.example.net` — and from then on we can
answer the one question a certificate authority asks about that name. We hold no other record for
them, and their real DNS stays wherever it already is.

Offering DNS as a service means answering *every* question about a name: the `A` record that decides
where the traffic goes, the `MX` that decides where the mail goes, the `NS` that delegates a subtree,
the `TXT` that carries SPF and DKIM. The customer delegates their zone to us and we become the
authority for all of it.

### The grants written in `tenancy-and-permissions` describe objects that do not exist yet

That change specifies a permission whose object identifier is a DNS selector:

```
[dns_record, {zone: example.com, names: *.staging.example.com, types: {A, AAAA}}, write]
```

Nothing in the system currently stores a zone or a recordset for that to name. This change creates
them, and it must create them in a particular way for those grants to be legitimate at all.

The grant model assumes **we hold the authoritative data**. If a satellite were the authority and we
merely orchestrated it, a grant naming `zone: example.com` would be describing an object we do not
own and cannot enforce access to — the grant would be advisory, and the real authority would be
whoever could reach the satellite. So the direction of authority is not an implementation preference:
the centre is authoritative, satellites are replicas, and `satellite-sync` already establishes exactly
that flow.

### The blast radius changes completely, which is why the permission model came first

`tenancy-and-permissions` argued this and this change is where it becomes real. The worst a stolen
API key can do today is publish a TXT under a label we generated in a zone we own. Once we are
authoritative for `example.com`, the same credential can:

- repoint `www.example.com` to an attacker's address;
- change `MX` so that mail — including password-reset mail for every other service the customer uses —
  is delivered elsewhere;
- add `NS` delegating a subtree away entirely.

There is no second gate. We are the authority, so whatever we serve is true as far as the internet is
concerned. This change is safe to build only because the per-zone, per-name, per-type grants already
exist to constrain it.

## What Changes

### Zones and recordsets become inventory

A **zone** is a name we are authoritative for, belonging to a project, and therefore owned by an
account through the ownership chain `tenancy-and-permissions` establishes. A **recordset** is every
record sharing one name and one type within a zone — `www.example.com/A` holding `203.0.113.10` and
`203.0.113.11` is one recordset with two values, which is how DNS itself thinks about it and how
every sane API models it.

Modelling recordsets rather than individual records matters for a reason that is not aesthetic:
replacing the addresses behind `www.example.com` must be one atomic operation. As individual rows it
is a delete and two inserts, and the window between them is a window where the name resolves to
nothing.

### The satellite's DNS backend is an interface, and in-process is the first implementation

A satellite must get records into something that answers queries. That something is chosen by an
interface with two implementations that matter:

**In-process, using `miekg/dns`.** This is not a placeholder. The dependency is already in `go.mod`,
`internal/dnsserver` already answers authoritative queries with it, and `internal/zoneview` already
holds the answerable state as a snapshot replaced wholesale. Extending that view from "alias labels
and their TXT values" to "recordsets by name and type" is the natural shape of the code that exists.
It has no external process to supervise, no configuration file to render, no reload to sequence, and
no second failure mode. For the fleet we run ourselves, it is the right answer and may remain so
indefinitely.

**RFC 2136 dynamic update with TSIG.** The important second implementation, and the reason is that it
is a **protocol rather than a product**. "Orchestrate any DNS server" sounds like it means writing a
driver per vendor; it does not, because BIND, Knot and PowerDNS all already speak dynamic update.
Supporting one standard covers the servers a customer already runs, and does so without us learning
each vendor's API.

Further options, with what each would actually require:

| Server | How it is reconfigured at runtime | Fit |
| --- | --- | --- |
| In-process (`miekg/dns`) | Our own memory | Best; already built here |
| Any server speaking RFC 2136 | Dynamic update over the wire, authenticated with TSIG | Best generic answer; one protocol, many servers |
| PowerDNS Authoritative | Native HTTP API for zones and records, or writes to its SQL backend | Very common for this pattern; two integration paths |
| Knot DNS | Control socket via `knotc`, plus dynamic update | Fast, clean dynamic configuration |
| CoreDNS | Plugin-backed store such as etcd or redis, or file reload | Go and familiar, but reload semantics differ per plugin |
| NSD / BIND with zone files | Rewrite the file and reload | Reload-oriented rather than dynamic; the worst fit |

### DNSSEC is deferred, and the pipeline is shaped so it can be added centrally

Signing is not in this change. But *where* signing would happen determines what a compromised
satellite can do, and that has to be designed for now rather than discovered later.

If satellites signed locally they would each hold zone keys, and one compromised satellite could
forge any answer for any customer — a mass hijack. If signing happens centrally and satellites
receive records already signed, a compromised satellite can withhold answers or serve stale ones but
**cannot forge them**: a mass outage instead of a mass hijack. That is a large enough difference that
the record distribution path is shaped to carry signatures alongside records from the start, even
while nothing produces them.

This connects back to a point `satellite-sync` raises and defers. A satellite holding every tenant's
zone data is not primarily a disclosure problem, because DNS records are public data by
construction — the entire purpose of publishing a record is that strangers can read it. The threat is
**write**: a satellite serving answers nobody authorized. Central signing is the eventual answer to
that, and shaping for it now is what keeps the answer available.

### Not in scope

- **DNSSEC itself** — key management, signing, `DS` publication, key rollover. Shaped for, not built.
- **Health-checked records and failover.** A recordset whose contents depend on check results is
  `health-checks`.
- **Zone transfer (AXFR/IXFR) as a distribution mechanism.** `satellite-sync` distributes state; DNS
  zone transfer is not used between centre and satellite.
- **Registrar integration.** The customer delegates their zone to us; we do not buy or manage domain
  registrations.
- **Secondary DNS for zones whose primary is elsewhere.** We are the primary or we are not involved.

### What we considered and rejected

**Storing individual records rather than recordsets.** Simpler rows, and it matches how a naive API
reads. Rejected because it makes the common operation — replace the addresses behind a name —
non-atomic, and because every consumer of the data (the answering path, the distribution stream, the
permission check) immediately regroups records by name and type anyway. Storing them the way they are
used removes the regrouping and the atomicity problem together.

**Making satellites authoritative and treating the centre as an orchestrator.** It sounds more
resilient, and it is how zone-file-based deployments work. Rejected because it invalidates the
permission model: a grant naming `zone: example.com` can only be enforced by whoever holds the
authoritative copy, and if that is the satellite then our grants are advisory. It also makes "what
does this zone actually contain" a question with several possible answers.

**Pushing to the customer's existing DNS provider instead of serving the zone ourselves.** Rejected in
`satellite-sync` and rejected again here for the same reason — it requires storing a credential per
customer equivalent to full control of their domain — and additionally because a service that cannot
answer for a name cannot health-check it, cannot fail over it, and has nothing to meter.

**A vendor-specific driver per DNS server.** Rejected in favour of RFC 2136. A driver per vendor is a
combinatorial maintenance burden whose payoff is supporting servers that already speak a standard we
could support once.

## Capabilities

### New Capabilities

- `dns-zones`: zones and recordsets as inventory owned through a project, the operations that change
  them, the validation that keeps a zone servable, and the atomicity of a recordset change.
- `dns-serving`: answering authoritative DNS for a delegated zone — the satellite backend as an
  interface, the in-process and dynamic-update implementations, and the distribution of records to
  satellites in a form that can later carry signatures.

### Modified Capabilities

- `challenge-dns-server`: the responder answers for many zones rather than for one configured zone,
  and for record types beyond the challenge TXT, while keeping its refusal to recurse and its refusal
  of out-of-zone names.

## Impact

- **New schema.** `zones` (owned by a project), `recordsets` (name, type, TTL, values), and a change
  sequence over them feeding the distribution stream.
- **`internal/zoneview`.** Generalised from alias labels with TXT values to recordsets keyed by name
  and type. The snapshot-replaced-wholesale structure, the read-time expiry and the fail-safe policy
  are unchanged.
- **`internal/dnsserver`.** Answers from the generalised view, for several zones, across record types.
  Its refusal to recurse and to answer out-of-zone names is unchanged.
- **New `internal/dnsbackend`.** The satellite backend interface, with the in-process implementation
  and the RFC 2136 implementation behind it.
- **`satellite-sync`'s stream.** Carries recordsets as well as challenge values, and carries a slot for
  signatures that nothing yet fills.
- **The permission check.** `dns_record` selectors from `tenancy-and-permissions` are evaluated here
  for the first time, against real zones and real names.
- **The console.** Zone and recordset editing is new.

## Dependencies

This change depends on `tenancy-and-permissions` and `satellite-sync`, and assumes from them:

- **The `dns_record` selector** — zone, name pattern with a leading wildcard matching one or more
  labels and excluding the apex, and a set of record types — exists in the grant grammar and is
  enforced. Without it this change hands out mail-interception capability with no way to constrain it.
- **Ownership through a project** to an account, materialized as an `account_id` column whose
  agreement with its parent is enforced by composite foreign keys, so a zone cannot belong to one
  account and its project to another.
- **The distribution stream** — satellite-initiated, position-resumable, snapshot-or-delta — exists
  and can carry a second kind of state.
- **The fail-safe policy** that a satellite keeps serving what it last knew when the centre is
  unreachable, which for real records is the difference between a control-plane blip and a customer
  outage.
