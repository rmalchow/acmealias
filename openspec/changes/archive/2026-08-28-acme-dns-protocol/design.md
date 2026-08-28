# Design

Decisions are numbered `N1`, `N2`, … and referred to by number from `tasks.md`.

## N1. Speaking acme-dns is not writing an adapter — it is admitting what we built

The temptation is to file this under "integrations" and weigh it against the others. That
understates it.

acme-dns is a server whose design is the same as this product's: a permanent `CNAME` from the
customer's zone into one we run, and an HTTP call that sets the TXT when a certificate authority
asks. We did not adopt a competitor's protocol; **we independently arrived at their architecture and
then chose a different spelling for the one call it needs.**

Which means the work is not translation. `POST /update` with `{"subdomain", "txt"}` maps onto our
`/present` with `{"fqdn", "value"}` because the two servers do the same thing. There is no impedance
to absorb, no concept in their protocol we lack, and no concept in ours their clients cannot express.

## N2. The arithmetic, which is the whole argument

| Route | Work | Unlocks |
| --- | --- | --- |
| Native adapters | a cert-manager **webhook** (a deployable Go apiserver extension), a Caddy **libdns module**, an acme.sh **shell plugin**, a Posh-ACME **PowerShell plugin** | one client each |
| acme-dns `/update` | **one HTTP endpoint** | acme.sh, Certify The Web, cert-manager, lego, Posh-ACME, Sewer, Traefik, WACS, and certbot via hooks |

The webhook is the one that settles it. cert-manager is how most Kubernetes clusters get
certificates, and its DNS-01 webhook ABI means a separate binary, a separate Helm chart, a separate
release cycle, and an apiserver extension to keep working across cert-manager versions — **or** a
`host:` line in an Issuer, because cert-manager ships an `acmeDNS` solver already.

**Four projects to maintain forever, against one route.** Nothing else in this change needs arguing.

## N3. `/register` is not implemented, and that is a tenancy decision rather than an omission

acme-dns's `POST /register` is **unauthenticated**. Anybody may call it, and it creates an account
and a subdomain and returns credentials.

That is right for acme-dns, which is a single-purpose daemon somebody runs for themselves. It is
exactly wrong here: this service has accounts that are bought, quotas that bound them, domains added
deliberately, and an activation gate before any of it answers. An unauthenticated endpoint that mints
inventory is the precise shape `tenancy-and-permissions` and `personal-and-team-accounts` exist to
prevent, and adding one for protocol fidelity would hand away the whole model to satisfy a client
that does not need it.

**And clients do not need it.** Registration in acme-dns is a one-time bootstrap whose output is a
credentials file; renewal only ever calls `/update`. So the console produces that file — the same
JSON of `username`, `password`, `fulldomain`, `subdomain` that cert-manager's `accountSecretRef`,
acme.sh's config and the rest already read — and every client works without ever having called
`/register`.

**Rejected: implementing `/register` behind our API-key authentication.** It would be a
protocol-shaped endpoint that no client can use, because clients that call `/register` call it
unauthenticated. It is a route that exists only to be able to say it exists.

## N4. There is no cleanup in acme-dns, and pretending otherwise would be worse

Our `httpreq` dialect has `/present` and `/cleanup`: a client tells us to publish and later tells us
to withdraw. acme-dns has only `/update`. A client speaking it **never tells us it is finished.**

So the withdrawal has to come from somewhere else, and it already does: `ChallengeLifetime` bounds
how long a published value lives, and the sweeper removes what has passed it. A value published
through `/update` is withdrawn on that timer rather than on a signal.

Two things follow and both should be stated rather than discovered:

- **The observability changes shape.** `challenge_interactions` records a publication seen through to
  its end, and one of its outcomes distinguishes a value that was withdrawn from one that is still
  live. A client that never withdraws makes that distinction less sharp, and the interaction view
  must not report an acme-dns publication as anomalous merely for behaving exactly as its protocol
  specifies.
- **A shorter lifetime is not a free tightening any more.** With `httpreq`, a value is normally gone
  seconds after validation and the lifetime is a backstop. With `/update` it is the only mechanism,
  so lowering it risks withdrawing a value a slow certificate authority has not yet asked for.

**Rejected: inventing a `/cleanup` extension for acme-dns clients.** No client would call it. An
extension nobody implements is a maintenance burden that buys a diagram.

## N5. Authentication maps onto what a key already is

acme-dns sends `X-Api-User` and `X-Api-Key`. Our keys are an identifier and a secret. The mapping is
direct, and the grant model underneath does not change: a key presented on `/update` reaches exactly
the alias its grants name, and the subset rule and the network allowlist apply identically.

The credentials file the console produces therefore carries a key that is **already scoped**, which
is a genuine improvement on acme-dns's own model, where a registration's credentials are all-powerful
over that registration.

## N6. `subdomain` is our alias label

acme-dns's `subdomain` is the label under its zone that the customer's `CNAME` points at, and
`fulldomain` is that label plus the zone. We have both already: the alias label, and the challenge
FQDN it answers.

So `{"subdomain": "shop-example-com", "txt": "..."}` resolves the same alias `/present` resolves from
`{"fqdn": "_acme-challenge.shop.example.com."}` — one addressed by our label, the other by the
customer's name. Both must land on the same authorization check, and the test that matters is that a
key which cannot publish for an alias by one spelling cannot publish for it by the other.

## Risks / Trade-offs

- **Two dialects for one operation** is two code paths to keep honest. Mitigated by making
  `/update` a thin translation onto the same handler rather than a parallel implementation — if it
  ever needs its own logic, that is the signal something has diverged.
- **No withdrawal signal** (N4), accepted as inherent to the protocol.
- **Clients will assume other acme-dns behaviour.** They may expect `/register`, or the two-TXT
  history acme-dns keeps. The answer to `/register` has to be a refusal that explains where to get
  credentials, not a 404.

## Migration Plan

1. Add `POST /update` beside `/present`, translating and delegating.
2. Map the headers onto key authentication.
3. Make `/register` refuse with a message naming the console.
4. Produce the credentials file in the console.
5. Update the site's supported-client list, and the documentation.

No schema change and no migration. Rollback is removing the route.

## Open Questions

- **Whether to also speak RFC 2136 with TSIG.** It is the other universal option — certbot, lego,
  cert-manager and acme.sh all support it — and we already run an authoritative DNS server, so the
  listener exists. It is a much larger piece of work (DNS UPDATE parsing, TSIG key management) and
  acme-dns reaches more clients for far less, so it is not proposed here. It is the obvious second
  move if a customer needs it.
- **Whether the credentials file should be downloadable or shown once.** It contains a key secret,
  and `personal-and-team-accounts` already established that a secret is shown exactly once.
- **Whether to keep the two most recent TXT values**, as acme-dns does. It exists there for a
  wildcard and its base name validating at once; our aliases already share one challenge name between
  a wildcard and its apex (design D6), which may make it unnecessary — but that needs checking
  against a real client rather than reasoning.
