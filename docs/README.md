# docs/ — for humans, not for AI

**This folder is written for people: infrastructure engineers, operators and users of the
admin console.** It is prose, diagrams and step-by-step instructions. It is allowed to
simplify, to repeat itself, and to lag slightly behind the code.

**It is NOT the authoritative specification.** The authoritative, machine-readable source of
truth for this service lives in [`../openspec/`](../openspec/):

| Question | Authoritative answer |
| --- | --- |
| What must the system do? | `openspec/specs/` and `openspec/changes/*/specs/` |
| Why was it built this way? | `openspec/changes/*/proposal.md` and `design.md` |
| What is left to build? | `openspec/changes/*/tasks.md` |
| How do I set it up and use it? | this folder |

**If you are an AI agent**: read `../openspec/` for requirements, design decisions and task
state. Do not treat anything in `docs/` as a requirement, and do not derive implementation
behaviour from it — where the two disagree, `openspec/` wins and `docs/` is the thing that
needs correcting. You may update `docs/` when asked, but never cite it as a source of truth.

---

# acmeservice

One small service that lets every Traefik load balancer we run obtain TLS certificates for
customer domains — without anybody handing us the keys to their DNS.

> **Status: pre-implementation.** This documents the service as designed. Nothing here
> is deployed yet. Configuration keys and screen layouts are as specified in
> `../openspec/changes/add-acme-alias-service/` and may shift slightly during
> implementation; the openspec artifacts are the source of truth.

## The problem in one page

Traefik gets its certificates from Let's Encrypt automatically. To prove we control a domain,
Let's Encrypt uses the **DNS-01 challenge**: it hands us a random string, and we
publish that string as a `TXT` record at a fixed name under the domain.

```
_acme-challenge.shop.example.com.   TXT   "gfj9Xq...Rg85nM"
```

The catch is that Traefik must be able to *write* into the DNS zone of every domain it
serves. DNS provider API tokens are almost always all-or-nothing: a token that can write the
challenge record can usually also redirect the customer's mail and repoint their website. We
would be collecting those tokens for every customer and storing them on our proxies.

### The fix: one permanent, harmless CNAME

DNS has an indirection record, the `CNAME`, meaning "this name is an alias for that
one — look there instead". Let's Encrypt follows it when looking up a challenge. So the customer
adds **one** record, once, and never touches it again:

```
_acme-challenge.shop.example.com.   CNAME   shop-example-com.acmeservice.cloudpowered.services.
```

That record grants exactly one power: whoever controls
`shop-example-com.acmeservice.cloudpowered.services` can answer ACME challenges for
`shop.example.com`. It cannot touch mail, cannot repoint the site, cannot read
anything. **acmeservice** is the thing that owns that zone and answers those
queries.

### What actually happens during a renewal

```
0.  once, by the customer:
    _acme-challenge.shop.example.com.  CNAME  shop-example-com.acmeservice.cloudpowered.services.

1.  traefik's ACME client resolves that CNAME itself and posts the target:
      POST /present {"fqdn": "shop-example-com.acmeservice.cloudpowered.services.",
                     "value": "gfj9Xq..."}          authenticated with the load balancer's API key
2.  acmeservice  ── stores the value, starts answering DNS with it ──▶ 200 OK
3.  Let's Encrypt asks public DNS for  _acme-challenge.shop.example.com  TXT
4.  example.com's nameserver answers:  CNAME shop-example-com.acmeservice.cloudpowered.services.
5.  Let's Encrypt follows the CNAME and asks us for that name's TXT
6.  acmeservice  ── answers  TXT "gfj9Xq..."  ────────▶  Let's Encrypt
7.  certificate issued.  traefik ── POST /cleanup ──▶  value deleted
```

> **Two spellings of step 1, both correct**
>
> Whether the client resolves the CNAME before calling us is a property of the client, not of the
> configuration. Traefik's lego does by default, and posts the target
> (`shop-example-com.acmeservice.cloudpowered.services.`). A client that does not follow
> CNAMEs posts the customer's name (`_acme-challenge.shop.example.com.`) instead. The API
> accepts **either**, resolves both to the same alias, and applies the same
> authorization — so a load balancer running something other than Traefik works unchanged.
>
> Because both are normal, the form we receive says nothing about whether the customer's CNAME is
> healthy. The authoritative check for that is
> [the DNS plan's verification](load-balancer-setup.md#step-4-onboard-a-domain), which resolves the record directly.

Steps 1 and 6 are why the service needs two very different things from infrastructure: an
ordinary HTTPS endpoint, *and* a genuinely public authoritative nameserver on port 53. What we
asked for and why — compute, addresses, the port 53 exposure and the risks that come with it — is
recorded in
[`../openspec/changes/archive/2026-08-21-add-acme-alias-service/proposal.md`](../openspec/changes/archive/2026-08-21-add-acme-alias-service/proposal.md)
and its `design.md`, which are authoritative and are not restated here.

## Where to start

1. **[DNS zone setup](dns-zone-setup.md)** — one-time delegation of
   `acmeservice.cloudpowered.services` from the parent zone, with the exact records and the
   verification commands. *For whoever owns the parent zone.*
2. **[Per-load-balancer setup](load-balancer-setup.md)** — registering a load balancer, minting its
   API key, the Traefik static configuration, and onboarding a domain end to end. *For LB / proxy
   operators.*
3. **[Install & configure](installation.md)** — deploying the service itself: database, migrations,
   the full environment variable reference, OIDC client, and the first administrator. *For whoever
   runs the service.*
4. **[Admin console guide](console-guide.md)** — using the web UI: projects, load balancers, domains
   and wildcards, the DNS plan, API keys, live challenges and the audit log. *For everyday users.*
5. **[Troubleshooting](troubleshooting.md)** — "certificates stopped renewing" and every other
   symptom, with the command that tells you which of the six moving parts broke. *For on-call.*

## Vocabulary

Four terms recur throughout these pages. They are worth getting straight now.

| Term | Meaning |
| --- | --- |
| **Project** | A customer engagement or one of our own products. The top of the inventory, and the unit that permissions are granted on. |
| **Load balancer** | One or more Traefik nodes fronting one environment for a project. The unit that holds node addresses and API keys, and the unit a certificate request is authorized against. |
| **Domain** | One name a load balancer answers for: a host (`shop.example.com`), an apex (`example.com`), or a wildcard (`*.example.com`). |
| **Alias** | The single label inside our zone that a customer's `_acme-challenge` CNAME points at — a label the service assigns, e.g. `k7m3q9x2r4tv8w1p`. Several domains can share one alias; see [wildcards](console-guide.md#wildcards). |

## What this service is not

- It is **not a general nameserver**. It serves one zone and answers only
  `TXT`, `SOA` and `NS`. Everything else in the zone is
  `NXDOMAIN`; everything outside it is `REFUSED`.
- It **does not write into customer zones**. It generates the exact list of records
  somebody at the other end must create, and can check whether they did.
- It **does not replace Traefik's certificate storage**. Certificates still live in
  Traefik's `acme.json`; this service only answers the challenge.
- It **does not issue certificates for anything not in the inventory**, no matter
  which credential is presented.
