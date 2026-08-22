> **docs/ is for humans, not for AI.** Authoritative specification: [`../openspec/`](../openspec/).

# Troubleshooting

A symptom-driven guide for on-call: get from what you are seeing to which of the six moving parts
broke, fast.

> **This is (almost) never urgent**
>
> A certificate is renewed at two-thirds of its lifetime — roughly 30 days before expiry — and the
> ACME client retries for days after that. A page about "certificates failing" is reporting the start
> of a slow process, not an outage in progress. Take the few minutes to diagnose properly rather than
> guessing under pressure; a wrong fix applied fast is how a one-alias problem becomes a whole-fleet
> one.

Six things can break, and almost every symptom below traces back to exactly one of them: Traefik's
configuration or credential, the challenge API, our datastore, our DNS server, the customer's CNAME,
or the network path on port 53. The three commands below narrow it down before you read a single
log line.

## First, three commands

Run these against the domain and load balancer you were paged about. Together they answer "is it us, is
it them, or is it the network" in under a minute.

| Command | Tells you |
| --- | --- |
| `dig @<service-ip> <zone> SOA +norecurse` | Is our DNS server alive and authoritative at all? No answer means the process is down or UDP/53 is filtered on the way to it — nothing downstream can possibly be working. |
| `dig +short _acme-challenge.<domain> CNAME` | Did the customer's delegation survive? Compare the answer against the alias recorded for that domain in the console's DNS plan. Empty means missing or not propagated; present-but-different means someone edited the zone. |
| `curl -si -u "<keyid>:<secret>" -X POST https://acme-api.<domain>/present -d '{}'` | `400` means the credential and the source address were both accepted and only the empty body was rejected — the path from Traefik to the challenge API is fine. `401` means it is not: bad key, revoked key, or a source address outside the key's restriction. |

```
# example, using the values from the other pages in this doc set
dig @203.0.113.53 acmeservice.cloudpowered.services SOA +norecurse
dig +short _acme-challenge.shop.example.com CNAME
curl -si -u "k_7f3a91c4:$ACME_KEY" -X POST https://acme-api.cloudpowered.services/present -d '{}'
```

## Symptom → cause

| Symptom | Usual cause |
| --- | --- |
| Certificates just stopped renewing everywhere | Usually a [port 53 firewall change](#port-53-firewall-change), or [the delegation itself](#delegation-broke) broke. |
| Works for normal domains, fails for wildcards, intermittently | [TCP/53 blocked](#tcp53-blocked) — the truncation-retry case. This is the classic. |
| `SERVFAIL` from public resolvers, but `dig` at the server works | A [DS record on an unsigned delegation](#ds-record-on-an-unsigned-delegation). |
| Traefik logs `401` from acmeservice | A [key revoked or expired, or a source address outside the key's CIDR restriction](#401-from-acmeservice) — both look identical on purpose. |
| Traefik logs `403` | The [domain is not in the inventory](#403-from-acmeservice) for that load balancer. |
| Traefik logs `400` | [`HTTPREQ_MODE=RAW` is set](#400-from-acmeservice), or a malformed request body. |
| Traefik cannot connect to the endpoint at all | The [API hostname was put inside the delegated zone](#traefik-cannot-connect-at-all) (our DNS serves no `A` records), or a firewall rule. |
| Issuance hangs in the propagation check | [The delegation is not live yet](#issuance-hangs-in-the-propagation-check), or Traefik's `dnsChallenge.resolvers` points at a split-horizon internal resolver. |
| A value appears in Live challenges and never disappears | [Validation failed after publish](#value-stuck-in-live-challenges) — the interesting logs are Traefik-side, not ours. |
| CA reports no TXT record, but our logs show a successful publish | [`LEGO_DISABLE_CNAME_SUPPORT` is set](#ca-sees-no-txt-record-despite-a-logged-successful-publish), or the customer's CNAME points at the wrong alias. |
| Console: signed in, but no permissions | The [groups claim is missing or path-style](#console-signed-in-but-no-permissions), or the person is in none of the four configured `ACME_*_GROUP` groups — the refusal page lists the groups it saw. |
| Service refuses to start | Configuration [validation names the offending setting](#service-refuses-to-start); the special case is an API key with a CIDR restriction while `ACME_TRUSTED_PROXY_CIDRS` is empty — it fails closed on purpose. |

## Detail, per cause

### Port 53 firewall change

Confirm with the first of the three commands above. If the SOA query times out from outside the
network but works when run from the service host itself, something between the internet and the
service started dropping UDP/53, TCP/53, or both. This is almost always a firewall or security-group
change, not the service — it has no state that would cause this on its own. Fix the rule: both
UDP/53 and TCP/53 must stay open to the entire internet, because Let's Encrypt validates from
several vantage points whose addresses are not a published, stable list. See
[install & configure, before you start](installation.md#before-you-start).

### Delegation broke

Confirm with `dig +trace <zone> SOA`. If the trace stops at the parent zone instead
of reaching our nameservers, the `NS` records in the parent were changed or removed.
Compare against what the service itself is configured to serve
(`ACME_NS_NAMES`) — a mismatch anywhere is a lame delegation. See
[DNS zone setup, step 2](dns-zone-setup.md#step-2-add-the-records-to-the-parent-zone).

### TCP/53 blocked

Confirm with `dig @<service-ip> +tcp <zone> SOA` — if this times out while the
plain UDP query succeeds, that is the whole diagnosis. A certificate covering both
`example.com` and `*.example.com` needs two TXT values at one name; the answer
exceeds what fits in a single UDP datagram, the server sets the truncation flag, and the client
retries over TCP. Block TCP/53 and that retry never lands, so wildcard issuance fails while ordinary,
single-value issuance keeps working — the exact "intermittent, only wildcards" symptom. Fix: open
TCP/53 alongside UDP/53, see
[install & configure, before you start](installation.md#before-you-start).

### DS record on an unsigned delegation

Confirm with `dig acmeservice.cloudpowered.services DS` — any answer at all is the bug,
since this service does not sign its zone. A validating resolver treats a DS record pointing at an
unsigned child as proof the zone should be signed; when it is not, every answer is bogus and the
resolver returns `SERVFAIL`. A plain `dig` straight at the server looks
perfect because it does not validate, which is what sends people down the wrong path for hours. Fix:
remove the DS record from the parent zone — some registrar UIs add one automatically when a
delegation is created, so check after every change to it. See
[DNS zone setup](dns-zone-setup.md#step-2-add-the-records-to-the-parent-zone).

### 401 from acmeservice

Confirm with the credential-test `curl` from an LB node itself (not from a laptop — the
source address matters). A `401` covers two causes that are indistinguishable from the
response alone, by design — the API does not reveal which, since telling an attacker "the key is
fine, only the address is wrong" is itself useful information to leak:

- The key was revoked, expired, or never matched this load balancer.
- The request's source address falls outside the key's CIDR restriction — commonly because the
  node egresses through NAT and presents a different address than expected.

Check the audit log for a recent revocation or restriction change first — it is faster than guessing.
If nothing changed there, create a temporary unrestricted key, retry, and read the source address
the service actually saw from the audit log; compare it against the restriction on the real key. See
[per-load-balancer setup, step 2](load-balancer-setup.md#step-2-mint-the-api-key).

### 403 from acmeservice

Authorization is a positive allowlist: a key can publish only for domains explicitly entered against
its own load balancer. A `403` means the FQDN in the request is not in that load balancer's domain list —
check the load balancer's **Domains** screen, or the audit log for a recent removal. This is
usually benign and just means a domain someone added to Traefik was never added to the inventory.

### 400 from acmeservice

Two causes: `HTTPREQ_MODE=RAW` set in Traefik's environment, which changes the request
body to a shape this service does not implement — it must be unset, see
[per-load-balancer setup, step 3](load-balancer-setup.md#step-3-configure-traefik). Or, less commonly, a malformed body
from something other than Traefik talking to the endpoint. The credential-test command in
[the three commands](#first-three-commands) deliberately sends an empty body and expects exactly this
`400` — that is the "everything except the body is fine" signal, not a fault.

### Traefik cannot connect at all

Two causes, and the first is a naming mistake that is easy to make once and painful to unwind: the
challenge API hostname (`acme-api.cloudpowered.services`) was placed *inside* the
delegated zone instead of the parent. This service answers only `TXT`, `SOA`
and `NS` for its own zone — it has no ability to serve an `A` record, so the
hostname simply does not resolve and Traefik cannot even open a connection. Confirm with
`dig acme-api.cloudpowered.services A`; if that comes back empty or `NXDOMAIN`
where you expected the proxy's address, this is it. See
[why the HTTPS names must stay outside the delegated
zone](dns-zone-setup.md#step-0-decide-the-names). The other cause is a firewall rule blocking the LB node's address from reaching the proxy
on `tcp/443` — check that the node's egress address is in the allowed source list.

### Issuance hangs in the propagation check

Two causes. If this is a brand-new domain, the delegation may simply not be live yet or not
propagated — onboarding a domain before [the zone delegation](dns-zone-setup.md) is
published and verified produces exactly this symptom, a hang rather than a fast failure. Confirm
with `dig +short _acme-challenge.<domain> CNAME` from outside the network.
If the delegation is fine, check Traefik's static configuration:
`dnsChallenge.resolvers` must point at public resolvers, not an internal one. An internal
resolver with a split-horizon view of our domains can see a different (or no) answer than the CA
will see when it validates. See [per-load-balancer setup, step 3](load-balancer-setup.md#step-3-configure-traefik).

### Value stuck in Live challenges

The publish succeeded — the value is live and answerable — but nothing withdrew it, which means
validation failed somewhere after that point. This service's job ended at "answer correctly when
asked"; the failure is downstream, on Traefik's side. Pull the Traefik logs for that node around the
time the value first appeared (shown in the console) and look for the actual CA error. The service's
own sweep will eventually clear a stale value (`ACME_CHALLENGE_LIFETIME`, default
`1h`), but that is just housekeeping, not the fix.

### CA sees no TXT record despite a logged successful publish

Start with the load balancer's [Recent interactions](console-guide.md#recent-interactions)
panel and switch on **"Never queried only"** — a publication the CA never queried is
this exact symptom, and the panel turns "renewals are failing somewhere" into a list of domains to
go and fix before you dig any further.

Two causes. First, check Traefik's environment for `LEGO_DISABLE_CNAME_SUPPORT=true` (or,
on older builds, a missing `LEGO_EXPERIMENTAL_CNAME_SUPPORT=true`) — CNAME-following is
the entire mechanism this service relies on, and without it Traefik's own validator, and the CA,
look up the TXT name directly instead of following the customer's CNAME to ours, and find nothing.
See [per-load-balancer setup, step 3](load-balancer-setup.md#step-3-configure-traefik). Second, if CNAME support is
correctly enabled, check where the customer's CNAME actually points:
`dig +short _acme-challenge.<domain> CNAME` and compare the target against the alias
shown in that domain's DNS plan in the console — a typo'd or stale target sends the query to a
different alias than the one this service is answering for that domain.

### Console: signed in but no permissions

The refusal page lists the IdP groups it saw for that sign-in — start there. Either the
**groups claim** is missing from the ID token entirely (check
`ACME_OIDC_GROUPS_CLAIM` matches what the IdP actually emits, and that the IdP's mapper
is configured to add it to the ID token), or it is present but path-style
(`/parent/child`) when the configured group names are plain — see
`ACME_OIDC_GROUP_PREFIX` and the "Full group path" setting in
[install & configure, step 2](installation.md#2-identity-provider). If the claim looks right and
the groups shown are the expected ones, none of them simply match any of the four
`ACME_*_GROUP` values in `.env` yet; an administrator needs to either add the
person's group to one of those variables (edit `.env`, restart) or add them to a
Keycloak group that is already mapped.

### Service refuses to start

Configuration is validated before any listener opens, and the process exits non-zero naming the
offending setting — read the last line of the startup log first, not the deployment tooling's
generic failure. One case is deliberate rather than a bug: the service refuses to start if any API
key carries a source-address restriction while `ACME_TRUSTED_PROXY_CIDRS` is empty. An
address restriction is only meaningful if the service can trust the forwarded client address, and
with no trusted proxy configured it cannot — it fails closed instead of silently trusting whatever
the caller claims. See
[install & configure, the `TRUSTED_PROXY_CIDRS`
warning](installation.md#3-configuration).

## Escalate with this information

If the cause is not one of the above, or the fix does not stick, escalate with:

- [ ] Which domain, and which alias it maps to
- [ ] Which project and load balancer
- [ ] The key identifier involved (the display prefix, never the secret)
- [ ] Timestamps of the failing renewal attempts
- [ ] Output of the three commands from the top of this page
- [ ] Relevant audit log entries around the time the symptom started

---

5 · Troubleshooting — [← Admin console guide](console-guide.md)
