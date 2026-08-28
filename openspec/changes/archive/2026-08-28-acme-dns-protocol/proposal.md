## Why

### We built acme-dns and gave it a different API

The product's model is: publish one permanent `CNAME` from `_acme-challenge.yours` into a zone we
run, and update the TXT there over HTTP when a certificate authority asks.

That is, precisely and not approximately, what **acme-dns** does. It is an existing, widely deployed
open-source server with the same design, and — crucially — **its HTTP API has become the de facto
standard for exactly this pattern.**

We implemented lego's `httpreq` protocol instead. That was a reasonable choice and it covers lego and
Traefik. It also means every other ACME client needs an adapter written by us, one at a time,
forever.

### One endpoint unlocks nearly every client

acme-dns's own documentation lists the clients that already speak its API:

> acme.sh, Certify The Web, cert-manager, Lego, Posh-ACME, Sewer, Traefik, and Windows ACME Simple
> (WACS). Certbot integration is available through dedicated authentication hooks.

**cert-manager's support is built in** — it ships an `acmeDNS` DNS-01 solver taking a `host` and an
`accountSecretRef`, with no webhook to deploy:

```yaml
solvers:
- dns01:
    acmeDNS:
      host: https://acme.example.com
      accountSecretRef:
        name: acme-dns
        key: acmedns.json
```

So the comparison is stark. Writing native adapters means a cert-manager webhook (a deployable Go
apiserver extension), a Caddy libdns module, an acme.sh shell plugin, a Posh-ACME PowerShell plugin —
four projects, four release cycles, four things to keep working. Speaking acme-dns's `/update`
instead is **one HTTP endpoint** and all of them work on the day it ships.

### The API is small

```
POST /update
X-Api-User: <uuid>
X-Api-Key:  <password>

{"subdomain": "8e5700ea-...", "txt": "<validation value>"}
→ 200 {"txt": "<validation value>"}
```

That is the whole of what a client does at renewal time.

## What Changes

- The challenge API additionally speaks the **acme-dns `/update`** protocol, alongside the existing
  `httpreq` `/present` and `/cleanup`.
- The console can produce the credentials file a client expects — the JSON object of
  `username` / `password` / `fulldomain` / `subdomain` that cert-manager, acme.sh and the rest read.
- **`POST /register` is deliberately NOT implemented as acme-dns defines it.** In acme-dns it is
  unauthenticated and creates an account and a subdomain on demand; here accounts are bought, domains
  are added in the console, and an unauthenticated endpoint that mints inventory is the one shape
  this product's whole tenancy model exists to prevent. Registration is a console action that hands
  back the same credentials file.
- The semantic difference acme-dns has and we do not — **no cleanup** — is handled and recorded
  rather than papered over.

## Capabilities

- `challenge-answering` — the service answers to a second, widely-implemented publication protocol

## Impact

- **`backend/internal/httpapi/challenge.go`** — one more route and an authentication path
- **The console** — a way to get the credentials file for a domain
- **The marketing site** — the supported-client list stops being "Traefik, and adapters are being
  written" and becomes most of the ecosystem
- **No schema change.** An acme-dns update is a `/present` with different spelling
