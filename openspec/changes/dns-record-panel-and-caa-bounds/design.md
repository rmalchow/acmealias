## Context

Two things have to be understood before any decision here makes sense, and neither is obvious from
the code.

**What a delegation hands us.** The ACME protocol offers a certificate authority several ways to
check that you control a name. The one this service exists for is **DNS-01**: the authority looks up
a `TXT` record at `_acme-challenge.<your name>` and checks it against a value it gave your client.
A customer of this service publishes one `CNAME` — `_acme-challenge.shop.example.com` pointing at
`k7f2q.edgedns.cloud`, a label in a zone we publish — and from then on the authority's lookup lands
on us and we answer it. That is the whole product, and the customer never gives us a credential to
their DNS.

It follows that we can answer that question whenever we like, for anyone. If we ordered a
certificate for `shop.example.com` from Let's Encrypt, the validation would consult
`_acme-challenge.shop.example.com`, follow the customer's `CNAME` into our zone, and read whatever
we put there. And because `dnsname.ChallengeName` strips a leading `*.` before forming the name,
`*.example.com` and `example.com` validate at the identical `_acme-challenge.example.com` — so a
delegation for a name is also a delegation for its wildcard.

**What bounds it.** A **CAA record** (Certification Authority Authorization, RFC 8659) is a DNS
record at a domain saying which authorities may issue for it. RFC 8657 adds parameters to that
record, one of which is `accounturi`:

```
example.com.  300  IN  CAA  0 issue "letsencrypt.org; accounturi=https://acme-v02.api.letsencrypt.org/acme/acct/3680831095"
```

That says: only Let's Encrypt may issue for `example.com`, and only for orders from ACME account
3680831095. Our order would come from a different account and Let's Encrypt refuses it — at the
authority, not here, so nothing this service does or fails to do changes the outcome. Without the
parameter, `0 issue "letsencrypt.org"` authorises *every* account at Let's Encrypt, and ours is one
of them.

The console previously graded CAA on whether issuance would succeed, which made the absence of any
record the best outcome (a green dot reading "no restriction — nothing to do"). For a customer who
has delegated their challenge name to us, that sentence is the opposite of the truth.

Constraints that shaped what follows: an account URI is minted by the authority for the customer's
own ACME client and lives in that client's state, so this service cannot know it. The public suffix
list is what separates `example.com` from `example.co.uk`, and it lives in the backend. And the
project screen already had a check that ran on every open, at one DNS query per name against a
resolver we do not own.

## Goals / Non-Goals

**Goals:**

- Print the exact record each name needs, without asking DNS anything, so a customer setting a
  domain up is never waiting on a lookup to learn what to publish.
- Grade CAA by how much issuance it leaves open, and say plainly — in the console and on the
  marketing site — that an unbounded name is one this service could obtain a certificate for.
- Query DNS only when somebody asks, and only for what they asked about.
- Shorten the alias label, which a customer types into a DNS form by hand.

**Non-Goals:**

- Enforcing anything. Every verdict here is advice; the authority enforces CAA and this service has
  no say in it. Refusing to answer challenges for a name whose CAA looks wrong would break customers
  for a diagnosis we are not entitled to make.
- Knowing whose ACME account a published `accounturi` names. Nothing in DNS says so, and a console
  that claimed otherwise would assert what it cannot see.
- `validationmethods` and CAA tree-walking. Both are real gaps, both are named in the proposal, and
  both are their own change.
- Removing the recorded re-check on the Domains panel. It records what it saw and brings a domain
  online; it is a different act from looking, and it keeps its own button.

## Decisions

### Grade CAA on four states, with absence as the worst

Rejected: keeping `correct`/`not-published`/`blocking` and adding a warning elsewhere. That leaves
the dot — the thing anybody actually reads — saying "nothing to do" on the least bounded domain.

Rejected: collapsing absent and blocking into one red "fix your CAA". They call for different work.
Absent means *publish this record*; blocking means *change the record you have*, usually because
somebody set it years ago for an authority the customer has since left. Two states, two colours the
same, two sentences.

Chosen: `not-published` (red), `blocking` (red), `unconstrained` (amber), `correct` (green).
`unconstrained` is where most customers who have thought about CAA at all will land, and it is a
real finding with a cheap fix — a parameter on a line they already publish.

The pinning test is *every* authorisation, not any: a set that pins wildcard issuance and leaves
plain issuance open to the whole authority is still a name anybody holding the delegation can get a
certificate for, and reporting the better half would be reporting the wrong half.

`hasAccountURI` walks the parameters after the first `;` rather than searching the whole value,
because `0 issue "accounturi.example"` names an authority that merely spells like the parameter and
constrains nothing.

### Recommend the record that bounds us, not the minimum that works

`inventory.CAALines` now renders `accounturi=YOUR-ACME-ACCOUNT-URL` on every line. The placeholder is
upper case and hyphenated so it cannot be mistaken for a value; printing a plausible URL would be
actively dangerous, since a customer who published someone else's account URI would lock themselves
out of their own domain.

This forced a cleanup that was already flagged in the code: `internal/httpapi` carried a second copy
of the renderer, with a `ponytail:` comment saying the right fix was to export the first and delete
the second. That duplication was survivable while both produced `0 issue "letsencrypt.org"`. It
stopped being survivable here, because the two would have shown one customer two different records
for one name — one that bounds the delegation and one that does not. Exported as
`inventory.CAALines`; the copy is gone.

### The records come from the project read; the check is a separate, per-table act

The panel renders entirely from the domains the screen already has. `caaName` is added to the domain
view for this: rdata with no owner name is not a record anybody can publish, and the console cannot
derive it without shipping the public suffix list to a browser to recompute a name the service
already knows.

This reverses design D1 of `domain-setup-feedback`, which had the screen check on open. D1's
argument was that a customer with a mistyped `CNAME` would otherwise read "never validated" and
learn nothing. That argument is answered rather than overruled: the screen now shows the exact line
to publish without asking anybody, so the empty screen D1 was written about no longer exists — and
with it goes the case for spending a query per name, on someone else's recursor, for a person who
did not ask.

Two buttons, one per table, because they are two questions asked at two moments: the delegation is
published once and checked once, and CAA is a decision people come back to. The route takes
`?records=delegation|caa` and asks *less* — filtering the answer client-side would have been simpler
and a lie, since the queries would still have gone out and the second button would have silently
doubled the screen's DNS traffic.

Merging is what makes two buttons safe. A partial pass still returns every domain with the
un-asked record reading `not-checked`, because the service builds its answer from what a domain
*needs* rather than from what was asked — so taking the response whole would blank the column
somebody checked a minute ago. `mergeChecks` replaces only the half that was asked for. The console
treats `not-checked` as the absence of an answer, which also collapses correctly onto the case where
the deployment configured no resolver: nobody looked, and the row says so.

### 48-bit alias labels, with the retry moved to where the race actually is

Ten characters instead of sixteen. The label is typed into a DNS provider's form by hand, read back
off a screen to verify, and quoted in support threads; six characters that buy nothing cost a
transposition each time. Guessing a label is worth nothing without the matching delegation, and at
2^48 the birthday probability that any two of a hundred thousand aliases collide is about one in
56 million.

Base32 with padding suppressed is load-bearing, not a detail: `StdEncoding` on six bytes appends six
`=` characters, which are not legal in a DNS label.

The existing collision handling checked a snapshot of every label before inserting, which cannot see
a label another transaction is inserting right now. The database's unique index can. So the insert
now retries on `aliases_label_key`, and `store.LabelTaken` matches the constraint name rather than
SQLSTATE 23505 alone — `aliases_challenge_fqdn_key` means somebody else owns that challenge name,
which is a genuine refusal that a retry would turn into an infinite loop of the same error.

### Collapsible info boxes, remembered per browser

The body is passed as a `TemplateRef` rather than projected with `<ng-content>`, so a collapsed box
instantiates no markup at all. Open by default: a box collapsed on first sight is a box nobody
opens, and some of these carry things a customer has to read once. `localStorage` holds only the
closed ones, so changing the default later does not have to migrate anything, and every access is
wrapped because reading it *throws* in some contexts rather than returning null.

## Risks / Trade-offs

- **Amber and red on domains that were green yesterday, for most customers.** → This is the point of
  the change rather than a side effect, and the copy is written so the amber state reads as a real
  fix rather than a nag. Nothing is broken in that state and the tooltip says so.
- **A green dot that is not the customer's own account.** → We cannot tell whose an account URI is.
  The label says "bound to one account" and never "your account", and the spec forbids the stronger
  claim.
- **Dots are grey until somebody presses a button, where they used to be coloured on arrival.** →
  The row that used to be coloured on arrival now carries the record it needs instead, which is more
  actionable than a colour. The button is on the table it fills.
- **`validationmethods` still discarded, so a set authorising `http-01` only reads green.** → Named
  in the proposal and left for its own change. It is a narrower failure than the one being fixed
  here.
- **A shorter label is a smaller keyspace.** → Its secrecy was never load-bearing; it is a name in a
  zone anybody can query, and the enumeration property the randomness protects (that a label cannot
  be *computed* from a customer's domain) is unaffected by length.

## Migration Plan

No data migration. Existing alias labels keep their sixteen characters — the spec already requires
that a label never changes, because it is published in a record we do not control — and new ones are
ten. Nothing reads a label's length.

The `records` query parameter is additive and its absence means "everything", so an older console,
`curl`, or the recorded re-check behave exactly as before. `caaName` is a new field on the domain
read; a client that does not know it ignores it.

Rollback is a redeploy of the previous image. The only durable trace this change leaves is the
`acme.infobox.*` keys in a viewer's own browser, which are inert if the code that reads them is gone.

## Open Questions

- Should a domain whose CAA reads `blocking` be refused at `/present` rather than served? It would
  be a stronger signal and it would also break a customer mid-renewal for a diagnosis we may be
  wrong about — the tree-walk gap alone makes false blockings possible. Left alone deliberately;
  worth revisiting once the lookup walks.
- Should the console offer to check every project's CAA on a schedule and mail on a change from
  green? That is a monitoring feature with a mail budget, not a panel.
