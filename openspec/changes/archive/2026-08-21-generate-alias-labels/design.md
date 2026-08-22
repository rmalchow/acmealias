# Design — alias labels become system configuration

## Context

`design.md` decision **D4** of the archived `add-acme-alias-service` change introduced the
inventory: projects own load balancers, load balancers own domains, and **aliases are their own
entity keyed by challenge name**. That keying is not cosmetic. A certificate for
`*.example.com` is proved at the challenge name `_acme-challenge.example.com` — the `*.` label is
stripped, not included — which is byte-for-byte the challenge name of the apex `example.com`. So
`example.com` and `*.example.com`, the commonest wildcard setup there is, are two domain rows
that must share one alias row and one customer-side `CNAME`.

The alias row is:

```sql
create table aliases (
  id              bigserial primary key,
  load_balancer_id bigint not null references load_balancers(id) on delete restrict,
  challenge_fqdn  text not null unique,   -- '_acme-challenge.example.com'
  label           text not null unique,   -- 'example-com'   <-- this change is about this column
  created_at      timestamptz not null default now()
);
```

D4 also settled how `label` is filled: "The operator is offered a generated label (`example-com`
from `example.com`, disambiguated with a suffix on collision) and may override it, because a
human-readable label is what makes a zone file or a log line legible months later."

This change revisits exactly that sentence and nothing else in D4. The table, the keying, the
one-owner-per-alias rule and the wildcard validation are untouched.

## Goals

- Make a label unpredictable rather than merely unreadable, for the reasons in `proposal.md`: no one outside the service may
  compute a customer's alias label from their domain name.
- Keep the readable derived label available to deployments that want it, as one setting.
- Change nothing about aliases that already exist.
- Remove the label from operator input entirely, so there is one code path per setting value
  rather than one per setting value plus an override.

## Non-goals

- Rotating or re-labelling existing aliases. There is no rotation feature here, and adding one
  would need a publish-both-then-retire dance that nothing has asked for.
- Hiding the *number* of aliases we serve. A zone transfer is refused and the labels are
  unguessable, but this change does not claim to make the zone's size secret.
- Changing the challenge name, the `CNAME` target's zone, or anything the customer publishes
  other than which label the record points at.

## Decisions

### A1. The default label is randomly generated: 16 characters of lowercase base32

A new alias created with `ACME_ALIAS_USE_DOMAIN=false` — the default — is labelled with 16
characters drawn from the lowercase base32 alphabet, sourced from `crypto/rand`. Sixteen
characters at five bits each is **80 bits** of entropy, for example:

```
_acme-challenge.demo.example.com.  CNAME  k7m3q9x2r4tv8w1p.acmeservice.cloudpowered.services.
```

Base32 rather than base64 because a DNS label is case-insensitive and permits only letters,
digits and hyphens: base64's mixed case and `+`/`/` are both disqualifying, whereas lowercase
base32 lands inside the label alphabet with no escaping and no post-processing. Sixteen
characters rather than more because the label is copied by hand into a customer's zone file
often enough that length is a real cost, and 80 bits is far beyond guessing: an attacker
spraying our nameservers at a million queries a second would need on the order of 10¹⁷ years to
stumble onto one live label.

The label is also, by construction, always valid — it can never begin or end with a hyphen,
never exceed 63 octets, and never be empty — so the generator needs none of the trimming and
fallback logic `SuggestLabel` carries.

**Rejected: a hash of the challenge name**, e.g. `base32(sha256(challenge))[:16]`. This is the
alternative that looks equivalent and is not, so it is recorded here in full. A hash is
*deterministic*: the same domain always produces the same label, and the hash function is not a
secret. An attacker who wants to know whether `shop.example.com` is a customer computes
`sha256("_acme-challenge.shop.example.com")`, base32-encodes it, truncates to 16 characters, and
queries that name in our zone. An answer means yes; `NXDOMAIN` means no. That is the identical
the predictability this change exists to remove, kept intact by a mechanism whose unreadable output makes it
look closed. Determinism is the defect, not readability. A keyed hash (HMAC with a
deployment-held secret) would resist it, but then the label depends on a secret that must be
backed up, rotated and never lost — a key whose compromise silently re-opens enumeration for
every domain at once — in exchange for a property (reproducing a label from a domain) that
nothing in this service needs, since the label is stored in the row anyway.

**Rejected: a database sequence** (`alias-1`, `alias-2`, …). Unguessable it is not — it is the
most guessable thing possible — and it publishes our customer count to anyone who adds a domain.

### A2. `ACME_ALIAS_USE_DOMAIN=true` restores the derived label

Set to `true`, an alias created from then on is labelled exactly as today:
`_acme-challenge.demo.example.com` → `demo-example-com`, via the existing
`dnsname.SuggestLabel`. The default is `false`.

The setting is a plain boolean with the project's `ACME_` prefix, alongside `ACME_ZONE`,
`ACME_CHALLENGE_TTL`, `ACME_CAA_ISSUER` and every other setting the service owns. It is read
once at startup like the rest of `internal/config`, and it is not a per-request or per-load-balancer
option — see A5.

The reason to keep it is operational legibility, and it is worth stating concretely. With
derived labels, a challenge interaction row reads:

```
14:02:03  demo-example-com  present  203.0.113.10  ok
```

and an engineer knows which customer that is without leaving the line. With generated labels the
same row reads:

```
14:02:03  k7m3q9x2r4tv8w1p  present  203.0.113.10  ok
```

and answering "whose is that?" costs a lookup. For a deployment serving one organisation, where
the zone is not a list of *customers* but a list of *our own domains*, that trade is often the
wrong way round, and the setting lets that deployment choose.

### A3. An existing alias is never re-labelled

**The setting decides how the next alias is labelled. It never touches one that exists.**

This is the property an operator most needs to be able to trust, so it is stated as a rule rather
than left as an implementation consequence: `resolveLabel` is called only on the path where a new
alias row is created. A deployment that flips `ACME_ALIAS_USE_DOMAIN` and restarts keeps every
label it has already published, unchanged, forever.

The reason is not tidiness. The label is one half of a record that lives in **someone else's
zone**, published by a customer who is not watching us. If a restart rewrote
`demo-example-com` to `k7m3q9x2r4tv8w1p`, the customer's record

```
_acme-challenge.demo.example.com.  CNAME  demo-example-com.acmeservice.cloudpowered.services.
```

would point at a name that no longer answers. Nothing breaks visibly at first — the record is
consulted only during a certificate renewal — and then, up to sixty days later, every renewal
for that domain fails at once, and keeps failing until the customer is told to republish. A
configuration change that arms a silent failure two months out is exactly the kind of change an
operator must be able to rule out, so the specification says outright that it cannot happen.

The same rule already applies within a single setting value, and for the same reason: when a
domain is added whose challenge name already has an alias — the `example.com` then
`*.example.com` case — that alias is reused as it is. `CreateDomain` already does this; this
change only makes the guarantee explicit.

A worked example, one deployment over time:

| Step | Setting | Action | Resulting label |
| --- | --- | --- | --- |
| 1 | `true` | add `demo.example.com` | `demo-example-com` |
| 2 | `true` | add `*.demo.example.com` | reuses `demo-example-com` (same challenge name) |
| 3 | → `false`, restart | — | `demo-example-com` **unchanged** |
| 4 | `false` | add `shop.example.com` | `x4n2vq7btm93kd5r` |
| 5 | `false` | add `*.shop.example.com` | reuses `x4n2vq7btm93kd5r` |

Steps 3 and 5 are the whole of A3.

### A4. Labels stay globally unique, so a generated label is still checked at insert

Randomness is not uniqueness. `aliases.label` carries a `unique` constraint, that constraint
stays, and the service still resolves collisions rather than trusting the entropy.

- **Generated label taken** — draw another and retry, up to 5 attempts, then fail the request
  with an error naming the exhaustion. At 80 bits the first retry is already vanishingly
  unlikely (with a million aliases the chance of any collision at all is about one in 2·10¹²), so
  five attempts is generous; a run of five is a broken random source, not bad luck, and failing
  loudly is the right answer to that.
- **Derived label taken** — keep today's behaviour: append `-2`, `-3`, … until free, truncating
  the base so the result stays within 63 octets.

Uniqueness is checked at insert time, inside the same transaction that creates the alias and the
domain, so that two concurrent creations cannot both pass a pre-check and then both insert. The
present implementation loads every alias and tests the candidate against a set in memory —

```go
// ponytail: every alias is loaded to test the label for collisions.
// Ceiling: O(aliases) per domain creation ...
```

— which is fine at this service's scale and is not what this change is for; the unique constraint
is what actually enforces the invariant, and a violation from it is retried the same way. The
upgrade path noted in that comment (a `FindAliasByLabel` query) stands unchanged.

**Rejected: dropping the uniqueness check because 80 bits makes it unnecessary.** The label is a
primary lookup key for the zone view and the challenge store. A collision would not be a cosmetic
clash; it would let one load balancer's challenge writes appear under another's name. The check
costs nothing on a path that runs once per domain, and the constraint has to exist regardless.

### A5. The label is not operator input, anywhere

Every entry point loses its label parameter, rather than defaulting it:

| Surface | Today | After |
| --- | --- | --- |
| `POST` domain-create body (`admin_inventory.go` ~:510) | `{"name": "...", "aliasLabel": "..."}` | `{"name": "..."}` |
| `inventory.CreateDomain` | `(…, name, aliasLabel string)` | `(…, name string)` |
| `inventory.resolveLabel` | `(…, challenge, supplied string)` | `(…, challenge string)` |
| CLI `domain create` | `--alias-label` | flag removed |
| Console domain editor | "Alias label (optional)" input | removed |

The label stays in every *read* model — `DomainView.aliasLabel`, the interaction history, the DNS
plan — because operators need to see it constantly. Only the write path loses it.

Removing the parameter rather than ignoring it is deliberate: the compiler then enumerates the
call sites, and an operator who scripts against the API gets a rejected field rather than a
silently discarded one. `aliasLabel` in a create request is refused as an unknown field, so a
script that has been setting it learns that it stopped working the first time it runs, instead of
the first time someone reads a zone file and wonders why the label is wrong.

**Rejected: keeping the override "for emergencies".** The override is what makes the guarantee in
A1 conditional — a service that promises unguessable labels but lets a caller set
`shop-example-com` promises nothing, since the leak returns for exactly the domains someone
decided to be helpful about. There is no emergency this solves: an alias that must have a
different label can be recreated, which is a documented, visible operation with a known
consequence (the customer republishes), rather than a quiet field on a form.

### A6. Reconciling decision E6 of `add-accounts-and-public-edition`

E6 of that change currently reads, in outline: the *public edition* uses opaque random labels;
the *internal edition* keeps derived labels; "this is the one place where the two editions
genuinely differ in behaviour rather than in policy."

That framing is superseded. The difference is real but it is not a property of the edition — it
is a property of the deployment, and a deployment expresses those through configuration. Binding
it to `ACME_EDITION` would mean an internal deployment that wants unguessable labels (a perfectly
reasonable want, since even one organisation's domain list is not public information) has no way
to ask for them, and the public edition carries a second label code path that exists only to be
never used.

E6 therefore becomes: labels are governed by `ACME_ALIAS_USE_DOMAIN`, which defaults to `false`;
a public deployment leaves it at the default, and must, because its zone is a list of mutually
untrusting customers. The reasoning E6 records — collision between accounts, and enumeration of
the customer list — is unchanged and is the reasoning behind the default. What goes is the
edition-specific *mechanism*.

The public edition's own requirement that the label not be *chosen by the customer* is satisfied
a fortiori: after A5 it cannot be chosen by anyone.

## Risks

- **A deployment that wants readable labels does not set the flag and gets random ones.** The
  cost is legibility, not breakage, and the fix is one setting — but note that aliases created
  before the flag is set keep their random labels (A3). Operators should decide before onboarding
  domains, and the README says so.
- **Operators who scripted `aliasLabel` see their scripts rejected.** Intentional (A5), and
  visible immediately rather than silently.
- **A random source failure would surface as five collisions and a failed request.** Loud, which
  is the intent, but it does mean domain creation depends on `crypto/rand` succeeding — as does
  every other security-relevant part of this service already.
