## Why

### What an alias label is, and where it ends up

This service answers ACME DNS-01 challenges on behalf of domains whose DNS we do not
administer. The mechanism is one `CNAME` record, published once by the customer, that points
the challenge name of their domain into a zone we *do* administer. The single label inside our
zone that the record points at is the **alias label**.

For `demo.example.com` today the customer publishes:

```
_acme-challenge.demo.example.com.  CNAME  demo-example-com.acmeservice.cloudpowered.services.
```

`demo-example-com` is the alias label. It is produced by `dnsname.SuggestLabel`, which lowercases
the challenge name and turns every character that is not a letter or digit into a hyphen —
`_acme-challenge.demo.example.com` → `demo-example-com`. The operator adding the domain is then
offered that label in a form field marked "Alias label (optional)" and may type something else
instead.

The important thing about the label is *where it lives*. It is not an internal identifier. It
appears in three places we do not control:

1. **In the customer's own zone**, in the `CNAME` record above. Anyone in the world can query
   `_acme-challenge.demo.example.com` and read it back.
2. **In our served zone**, as the name that answers the challenge TXT record. Our nameservers
   are authoritative and public; every label we serve is answerable to anyone who asks for it
   by name.
3. **In the certificate authority's resolver path**, which is the entire point.

### The leak

Because the label is derived from the domain by a fixed, public rule, anyone can compute it:
`shop.example.com` becomes `shop-example-com`, and
`shop-example-com.acmeservice.cloudpowered.services` either resolves or does not.

It is tempting to call that a domain-enumeration oracle. **It is not**, and the change should not
claim it is. Anyone curious whether `shop.example.com` uses this service can simply query
`_acme-challenge.shop.example.com` in the customer's OWN zone and read the delegation — that
record is world-readable by design; it is the entire mechanism. A predictable label in our zone
answers a question that the customer's zone already answers for free. Neither is a leak the
other creates.

What a predictable label actually costs is narrower, and worth being precise about:

- **In bulk, it is a dictionary attack waiting for a list.** One label is worth nothing, but a
  *set* of them is a customer list in lightly-obfuscated form. Labels leave the zone in more
  places than the zone: a DNS plan pasted into a support ticket, an exported zone snippet, a log
  shipped to a third party, a future zone that answers something it should not. A derived label
  discloses its domain immediately, and a hashed one falls to a wordlist in seconds. A random
  label discloses nothing in any of those cases, because there is no function from label to
  domain to invert.
- **Determinism collides.** Two parties adding `demo.example.com` derive the same label, and the
  label is the key the challenge store and the zone view are built on. This is a correctness
  problem, not a privacy one, and it is the reason the change would be worth making even if the
  privacy argument were worth nothing.

Two lesser problems ride along with it:

- **The label discloses the domain to whoever reads the CNAME.** A customer's zone is
  world-readable by design. The record tells any reader not only that the customer uses this
  service, but which domain of theirs it covers.
- **Two identical domains collide.** Two parties adding `demo.example.com` derive the same
  label. Today the inventory disambiguates with a numeric suffix (`demo-example-com-2`), which
  works but makes the collision itself visible: the presence of a `-2` tells the second party
  that someone got there first.

### Why the fix is a generated label and not a hashed one

The obvious-looking fix is to keep the label a pure function of the domain but make the function
one-way: `label = base32(sha256(challenge_name))[:16]`. It looks like it closes the leak,
because a human reading `k7m3q9x2r4tv8w1p` cannot see `shop.example.com` in it.

It buys less than it appears to. The hash is deterministic and the algorithm is not a secret, so
it does not make a label unpredictable — it makes it unreadable, and those are different
properties. Unreadable defeats someone who sees one label; it does not defeat someone who holds a
list of candidate domains and a CPU. Determinism also keeps the collision: two parties adding
`demo.example.com` still land on the same label, which is the correctness half of the problem and
the half a hash leaves entirely untouched.

The label must therefore be **unpredictable, not merely unreadable**: drawn from a random source
at creation time, so that there is no function from a domain to its label at all, and the only
way to learn a customer's label is to be told it — by us, in the console, or by the customer, in
their zone.

### Why the derived label is still worth having as an option

For a deployment that serves one organisation — our own fleet, our own operators, one company's
domains — the enumeration leak is not much of a leak, and the readable label is a genuine
operational asset. `demo-example-com` is self-describing in a log line, in the generated DNS
plan, and in the interaction history that records every time a certificate authority came to
look. An engineer paging through a week of challenge interactions at 03:00 can read the domain
straight off the row without joining anything.

So the derived label does not disappear. It stops being the default and becomes a deliberate
choice, made once per deployment by whoever configures it — not per domain by whoever happens to
be filling in the add-domain form.

## What Changes

- **The alias label becomes system configuration, not operator input.** A new setting,
  `ACME_ALIAS_USE_DOMAIN`, decides how the service labels the *next* alias it creates.
  `false`, the default, generates a random label. `true` derives the label from the challenge
  name exactly as `dnsname.SuggestLabel` does today. (`ACME_` is this project's prefix for every
  one of its own settings — the same convention as `ACME_ZONE`, `ACME_CHALLENGE_TTL` and the
  rest.)
- **The generated label is 16 characters of lowercase base32 — 80 bits of randomness** — for
  example `k7m3q9x2r4tv8w1p`. It is a valid DNS label by construction, and it is not derivable
  from the domain by anyone, including us.
- **The operator can no longer supply a label.** The `aliasLabel` field leaves the
  domain-create request body in `backend/internal/httpapi/admin_inventory.go`, the `aliasLabel`
  parameter leaves `inventory.CreateDomain`, the override branch leaves `inventory.resolveLabel`,
  the `--alias-label` flag leaves the CLI, and the "Alias label (optional)" input leaves the
  console's domain editor.
- **Existing aliases keep their labels forever.** Flipping the setting changes nothing that has
  already been published. See the design document, decision A3 — this is the property that makes
  the change safe to deploy.
- **A collision is retried, not refused.** A generated label that is already taken is discarded
  and another is drawn; a derived label that is already taken keeps today's numeric-suffix
  disambiguation. Either way the domain creation succeeds.
- **The label is documented where it is described**: `README.md`, `docs/console-guide.html` and
  `docs/load-balancer-setup.html`, the last of which currently tells operators to accept the
  proposed label or type their own.
- **Decision E6 of `add-accounts-and-public-edition` is reconciled.** It currently makes label
  style a difference in *behaviour between editions*. It becomes a difference in
  *configuration*, expressed by this setting, which is both simpler and lets the public edition
  ship without a second label code path.

## Impact

- Affected specs: `service-inventory` (one modified requirement)
- Affected code: `backend/internal/config/`, `backend/internal/dnsname/label.go`,
  `backend/internal/inventory/domain.go`, `backend/internal/httpapi/admin_inventory.go`,
  `backend/cmd/acmeservice/cli.go`, `backend/internal/adminctl/inventory.go`,
  `frontend/src/app/features/domains/domain-editor.component.ts`,
  `frontend/src/app/features/models.ts`
- Affected documents: `README.md`, `docs/console-guide.html`, `docs/load-balancer-setup.html`
- Affected changes: `add-accounts-and-public-edition`, decision E6 and its `service-inventory`
  delta
- **Not affected: any alias that already exists.** No migration, no backfill, no rewrite.
