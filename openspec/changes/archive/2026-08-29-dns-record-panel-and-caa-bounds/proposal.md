## Why

A customer of this service publishes one `CNAME` — from `_acme-challenge` under a name they own,
into a zone we publish — and from that moment we can answer the question a certificate authority
asks about that name. That is the product. It also means we could ask an authority for a
certificate for their name ourselves, and for its wildcard, because a wildcard and its base share
one challenge name: `*.example.com` and `example.com` are both validated at
`_acme-challenge.example.com`. Every hosted responder for the DNS-01 challenge method (the ACME
challenge that is answered by publishing a DNS record, as opposed to serving a file over HTTP) holds
that same power over the names delegated to it. It is not a defect we can engineer away — it is what
the customer is buying — and until now the console said nothing about it.

Worse, it said the opposite. A **CAA record** — the DNS record type that names which certificate
authorities may issue for a domain — was reported by whether issuance would succeed. On that scale
the absence of any CAA record is perfect: no record means every authority may issue, so *our*
authority may issue, so the console painted a green dot and the words "no restriction — nothing to
do". For a customer who has just handed us their challenge name, "any authority may issue" includes
us, and telling them there was nothing to do was telling them the reverse of what was true.

There is exactly one record that bounds it: RFC 8657's `accounturi` parameter on a CAA entry, which
pins issuance to a single ACME account. An order from any other account — ours included — is refused
by the authority itself, whatever this service does. Nothing in the console mentioned it, and the
console never printed the CAA record a customer would have to publish to get it.

Separately, the screen had lost the ability to print a record at all. A DNS plan panel existed and
was deleted (change `domain-diagnostics`, decision G2) because most of it restated the domain table
above it. Deleting it was right; what went with it was the only thing it did that nothing else does,
which is show the exact line to publish. And the check that colours those rows ran on every screen
open, spending a DNS query per name against somebody else's recursive resolver on behalf of a person
who had not asked for one.

## What Changes

- **A DNS records panel returns to the project screen**, for two record types and no others: the
  delegation `CNAME` per challenge name, and the CAA set per registrable name (the name one label
  below the public suffix — `example.com` for `shop.example.com` — which is where CAA is read from,
  since CAA is inherited and the nearest ancestor wins). No address records, no zone-file download,
  no verdict column duplicating the dots above.
- **Every record is printed as a complete zone-file line** — owner name, TTL, type and rdata — from
  the project read alone, before any lookup happens. Each row names the domains it covers, because
  one record often serves several.
- **BREAKING (reported behaviour): CAA is regraded by how much issuance it leaves open**, replacing
  the old "does issuance work" scale. No CAA published is red; a set that excludes our configured
  authority stays red but distinct, since one asks the customer to publish a record and the other to
  change one; a set naming the authority with no `accounturi` is amber; a set pinned to an account is
  green. Green never claims the account is the customer's, which is not observable from DNS.
- **The recommended CAA rdata now carries `accounturi`**, with an unmistakable placeholder for the
  value, since an account URL is minted by the authority for the customer's own ACME client and is
  not visible to this service. An info box under the table says where to read the real one, with a
  worked `jq` command for Traefik.
- **BREAKING (reported behaviour): the screen no longer queries DNS on its own.** A check happens
  when somebody presses a button, and there is one button per table — checking CAA sends no `CNAME`
  queries, because the route now takes a `records` parameter and asks less rather than answering
  less.
- **Alias labels shorten from 80 bits to 48** — sixteen lowercase characters to ten — and collisions
  are now also retried when the database's unique index refuses the insert, not only when a
  pre-insert snapshot says the label is taken.
- **Info boxes across both consoles collapse to their heading**, and each remembers whether it is
  open in that browser.
- **The marketing site's setup sequence states the same thing** in step 4, which becomes the step
  that says plainly what publishing the delegation hands us and what record takes it back.

## Capabilities

### New Capabilities

None. Everything here changes how existing capabilities behave rather than adding a new area of
the system.

### Modified Capabilities

- `admin-console`: CAA is reported by how much issuance it leaves open rather than by whether a
  record exists; the project screen states its DNS records as complete records without querying
  anything and checks them only on request, per record type.
- `service-inventory`: the entropy floor for a generated alias label drops from 80 bits to 48, and
  a label collision must be resolved even when it is detected only at write time — while a conflict
  over the challenge name itself stays a refusal and must not be retried.

## Impact

- **Backend.** `internal/resolvercheck` gains a CAA state and an `accounturi` parser;
  `internal/inventory` renders the recommended rdata and exports the one renderer (a second copy in
  `internal/httpapi` is deleted, since the two would now show a customer two different records);
  `internal/httpapi` gains a `records` query parameter on the verification route and a `caaName`
  field on the domain read; `internal/dnsname` shortens the label; `internal/store` gains a
  predicate that tells the alias label's unique index apart from the challenge name's.
- **Console.** A new DNS records panel, a new shared collapsible info box, the CAA chip's colours,
  and the project screen's check lifecycle.
- **Marketing site.** Step 4 of the setup sequence, and the two sentences elsewhere that claimed the
  delegation was the only DNS record a customer ever publishes.
- **Not addressed here**, and worth naming because they are adjacent and real: the CAA check still
  discards `validationmethods`, so a set authorising our authority for `http-01` only reads green
  while every order through this service fails; and the lookup asks only the registrable name rather
  than walking up from the fully-qualified name, so a CAA set published at a subdomain is invisible
  to us and visible to the authority.
