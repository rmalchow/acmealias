> **docs/ is for humans, not for AI.** Authoritative specification: [`../openspec/`](../openspec/).

# Per-load-balancer setup

Registering a load balancer, giving it a credential, configuring Traefik, and onboarding the
first domain. Repeat per load balancer; the domain part repeats per domain.

> **Traefik 2.11 or newer is required — but check the bundled lego, not the version**
>
> CNAME following in the ACME client is the entire mechanism this service depends on, and it comes
> from [lego](https://go-acme.github.io/lego/), the library Traefik embeds — not from
> Traefik itself. The real requirement is that the bundled lego follows CNAMEs *by default*,
> which every release since lego 4.9 does.
>
> **Version numbers do not order the way you would expect.** Traefik 2.11 bundles lego
> 5.2.2, while Traefik 3.0 bundles lego 4.17.4 — the older major line ships the newer ACME client. So
> "upgrade Traefik" is not automatically "get newer CNAME behaviour". Both satisfy the requirement;
> the point is that the version number is not what to reason about.
>
> To check a specific build:
>
> ```
> docker run --rm traefik:v2.11 version
> # then, for the bundled lego of that tag:
> curl -s https://raw.githubusercontent.com/traefik/traefik/v2.11/go.mod | grep go-acme/lego
> ```
>
> Anything from lego 4.9 onwards is fine. Older builds required an opt-in
> (`LEGO_EXPERIMENTAL_CNAME_SUPPORT=true`) that no longer exists under that name.

> **Prerequisites**
>
> The [zone delegation](dns-zone-setup.md) must be published and verified, and the
> service must be [running](installation.md), before any of this will work. Onboarding a
> domain before the delegation exists produces a confusing symptom: issuance hangs in the propagation
> check instead of failing quickly.

## Why the load balancer is the unit

Everything here hangs off the **load balancer** rather than the individual node, because the
load balancer is what actually answers for a set of domains. Its nodes share one credential, and that
credential can publish challenges for exactly the domains recorded against that load balancer — nothing
else, regardless of what is asked for.

```
project  acme-shop
└── load balancer  eu-prod      nodes 203.0.113.10, 203.0.113.11
    ├── api key  k_7f3a91c4     restricted to those two addresses
    └── domains
        ├── shop.example.com          ─┐
        ├── example.com               ─┤ 3 domains
        └── *.example.com             ─┘ 2 challenge names, 2 CNAMEs
```

## Step 1 — register the load balancer

1. **Create the project**

   In the console, **Projects → New project**. A project is a customer engagement or one
   of our own products; it is also the unit that permissions are granted on, so it is worth matching
   however teams are already organised.

2. **Create the load balancer**

   Inside the project, **New load balancer**. Give it a slug that matches how the environment
   is referred to elsewhere — `eu-prod`, `eu-staging`.

3. **Add the nodes and their public addresses**

   These addresses are used for two things: they are what the customer is asked to point the
   domain's `A`/`AAAA` records at, and they are offered as the default source restriction on API
   keys. Once a domain's certificate observation switch is on (see
   [step 4](#step-4-onboard-a-domain)), they are also the addresses this service connects to on
   port 443 to read what is actually being served. Get them right — a wrong address here produces
   a certificate on a host nobody reaches.

The same can be done from the command line on the service host, which is the route to use when
bootstrapping before anyone has console access. The stack runs under Docker Compose, so each command
is prefixed with `docker compose exec` from `/opt/acmeservice` — the doubled
name is the Compose service followed by the binary inside it:

```
docker compose exec acmeservice \
  acmeservice adminctl project create --slug acme-shop --name "Acme Shop"
```

Shown below without the prefix for readability:

```
acmeservice adminctl project create --slug acme-shop --name "Acme Shop"
acmeservice adminctl loadbalancer create --project acme-shop --slug eu-prod --name "EU production"
acmeservice adminctl node add --loadbalancer acme-shop/eu-prod --label a --ipv4 203.0.113.10
acmeservice adminctl node add --loadbalancer acme-shop/eu-prod --label b --ipv4 203.0.113.11
```

## Step 2 — mint the API key

1. **Create it**

   On the load balancer page, **API keys → Create key**. Name it after the load balancer so an audit
   entry is readable a year from now.

2. **Restrict it to the load balancer's addresses**

   The CIDR editor offers the load balancer's node addresses as one-click defaults —
   `203.0.113.10/32` and `203.0.113.11/32`. Accept them unless the nodes egress
   from a different address than they ingress on, in which case use the egress addresses.

   > **Check the egress address, not the ingress address**
   >
   > The restriction is evaluated against the address the request *arrives from*. A node behind
   > outbound NAT will present the NAT address, not its own. If in doubt, create the key unrestricted,
   > make one request, and read the observed address from the audit log — then add the restriction.

3. **Store the secret immediately**

   It is displayed exactly once and cannot be recovered. Put it straight into wherever that load balancer's
   secrets live. If you lose it, delete the key and create another — there is no recovery path, by
   design.

## Step 3 — configure Traefik

Identical on every node of the load balancer.

### Static configuration

```
# traefik.yml
certificatesResolvers:
  acmesvc:
    acme:
      email: ops@cloudpowered.services
      storage: /data/acme.json
      dnsChallenge:
        provider: httpreq
        resolvers:
          - "1.1.1.1:53"
          - "8.8.8.8:53"
```

> **Point `resolvers` at public resolvers**
>
> Before handing off to the CA, Traefik checks for itself that the record is visible. If it asks an
> internal resolver with a split-horizon view of our domains, it may see a different answer from the
> one the CA will see — or none at all. Public resolvers are what the CA effectively uses.

### Environment

```
HTTPREQ_ENDPOINT=https://acme-api.cloudpowered.services/
HTTPREQ_USERNAME=k_7f3a91c4          # the key identifier
HTTPREQ_PASSWORD=<the secret, shown once>
HTTPREQ_HTTP_TIMEOUT=30
```

| Setting | Requirement |
| --- | --- |
| `HTTPREQ_ENDPOINT` | Trailing slash included. Traefik appends `present` and `cleanup` itself. |
| `HTTPREQ_MODE` | **Must not be set.** Setting it to `RAW` changes the request body to a shape this service deliberately does not implement; every publish will return `400`. |
| `LEGO_DISABLE_CNAME_SUPPORT` | **Must not be set to `true`.** CNAME following is the entire mechanism. On current Traefik it is on by default; on older builds the opt-in was `LEGO_EXPERIMENTAL_CNAME_SUPPORT=true`. If issuance fails with the CA reporting no TXT record while our logs show a successful publish, check this first. |

### Compose fragment

```
services:
  traefik:
    image: traefik:v3
    environment:
      HTTPREQ_ENDPOINT: "https://acme-api.cloudpowered.services/"
      HTTPREQ_USERNAME: "k_7f3a91c4"
      HTTPREQ_PASSWORD: "${ACME_KEY}"
    volumes:
      - ./traefik.yml:/etc/traefik/traefik.yml:ro
      - traefik-acme:/data
```

> **Each node runs ACME independently — that is fine**
>
> Unless the load balancer shares its `acme.json`, each node obtains its own certificates and
> each will publish its own challenge value. Those values coexist at the same name, which this service
> supports by design, and each node cleans up only its own. No coordination is needed. It does mean
> a certificate per node per domain, and CA rate-limit consumption multiplied by the node count, which
> matters only if a load balancer serves hundreds of domains.

## Step 4 — onboard a domain

1. **Add the domain to the load balancer**

   On the load balancer page, **Domains → Add domain**. Enter the exact name the certificate
   will cover: `shop.example.com`, `example.com`, or `*.example.com`.
   The service assigns the alias label itself — there is nothing to fill in and nothing to choose —
   and the label it assigned appears on the domain's `CNAME` dot below.

2. **Read off the `CNAME` to publish**

   The domain's own row shows the one `_acme-challenge` CNAME it needs. Click the `CNAME` dot and
   copy it — it names this domain's alias exactly, and prints the same record in every state,
   including once it is already correct.

   ```
   _acme-challenge.shop.example.com.  300  IN  CNAME  k7m3q9x2r4tv8w1p.acmeservice.cloudpowered.services.
   ```

   The address records the customer needs (`shop.example.com. IN A 203.0.113.10`, and so on for
   every node) are not generated for you: the customer already told *us* those addresses when the
   load balancer's nodes were registered, so there is nothing this service can check about them
   that the customer does not already know — send those the way you would send any other DNS
   change request.

   The label in the CNAME is generated and unguessable, which is what stops anyone outside from
   computing it and asking our nameservers whether a given domain is a customer. Copy it from the
   dot rather than typing it. (A deployment run with `ACME_ALIAS_USE_DOMAIN=true` shows a
   readable `shop-example-com` here instead; see
   [Installation](installation.md).)

   The repository's own development stack runs the default too: `./scripts/dev.sh seed` reads the
   generated label back out of `acmeservice adminctl loadbalancer dns demo/prod` and writes it into
   the fake internet's `example.com` zone for you, so every reset issues against a different label.
   To see the current one: `dig @127.0.0.1 -p 8053 _acme-challenge.demo.example.com CNAME +short`.

3. **Send it to whoever administers that zone**

   This is the only manual, external step in the whole process, and the one that takes days rather
   than seconds. Send the snippet as-is; it is deliberately in a form a DNS administrator can paste.

   Worth saying explicitly in the request: the CNAME is permanent, it is added once and never
   changed, and it grants only the ability to answer certificate challenges for that one name.

4. **Verify, rather than believe**

   The domain's `CNAME` dot checks itself whenever you open the load balancer, and turns green
   once it resolves to this domain's alias — red means something resolves but not the alias we
   need (with what was found instead), grey means it could not be checked or has not been checked
   yet. Do not proceed until it is green — everything downstream depends on it, and a wrong CNAME
   is far commoner than a missing one. Click the dot for the exact record this domain needs.

   Watch the CAA dot too: red means a CAA record set names some other authority, and issuance is
   refused however right the delegation is. Green with no dot's tooltip naming an issuer is the
   normal "no restriction" state and needs nothing.

   **Check now**, beside the domain, re-runs the same lookup immediately rather than waiting for
   the poller.

   The same check from a shell:

   ```
   dig +short _acme-challenge.shop.example.com CNAME
   # expected: the target shown on the CNAME dot, e.g.
   #           k7m3q9x2r4tv8w1p.acmeservice.cloudpowered.services.
   ```

5. **Point the router at the resolver**

   Only now change Traefik's dynamic configuration. Traefik picks a certificate resolver per router,
   so this affects exactly one domain and nothing else.

   ```
   http:
     routers:
       shop:
         rule: "Host(`shop.example.com`)"
         service: shop-backend
         tls:
           certResolver: acmesvc
   ```

6. **Watch one full cycle**

   The **Live challenges** screen shows the value appear and disappear. In the audit log
   you should see a publish followed by a withdrawal a minute or two later, and Traefik should report
   the certificate obtained. If the value appears and never disappears, the issuance failed after
   publishing — go to [troubleshooting](troubleshooting.md).

## Wildcards

A wildcard is worth pushing for whenever a customer has more than two or three hostnames, because
it collapses the manual part of onboarding almost to nothing: **one CNAME covers every host
under the domain**, instead of one CNAME per host.

The reason is a detail of the ACME protocol. The challenge name for `*.example.com` is
`_acme-challenge.example.com` — the `*.` is stripped, not included. So the
wildcard, the apex, and any future hostname you would otherwise have added separately all validate
at one name, behind one CNAME.

> **Traefik does not infer a wildcard from a routing rule**
>
> A router with ``Host(`*.example.com`)`` will *not* request a wildcard certificate.
> Wildcards must be declared explicitly with `tls.domains`:
>
> ```
>       tls:
>         certResolver: acmesvc
>         domains:
>           - main: "example.com"
>             sans:
>               - "*.example.com"
> ```
>
> That configuration requests one certificate covering both names, which means the CA issues two
> authorizations validated at the *same* challenge name, and both TXT values must be present at
> once. This service handles that; a naive DNS-01 provider that replaces rather than adds does not,
> which is why the case is worth knowing about.

Two limits to state when proposing a wildcard to a customer, because both surprise people:

- `*.example.com` does **not** cover `example.com` itself. It needs its own domain row if it is
  served — tick **Also add the apex/wildcard** in the Add domain dialog (or pass
  `--include-wildcard` to `adminctl domain create`) and one action creates both rows against the
  one alias they share. The option is off unless you tick it.
- `*.example.com` does **not** cover `a.b.example.com`. A
  wildcard matches exactly one label.

Adding the pair means telling the customer for one more name, and deliberately so: a wildcard
needs its own address records — one A and one AAAA per node, for `*.example.com` as well as for
`example.com` — because a DNS wildcard is a record in the customer's zone, not an inheritance
rule. Without them the certificate is issued and `shop.example.com` still resolves nowhere. The
`_acme-challenge` CNAME does *not* double: both names validate at the same challenge name, so the
`CNAME` dot on either row prints the same one record, and the customer publishes one delegation
for both.

Tell the customer what the wildcard address record does before they publish it: it answers for
every unregistered name in the zone, so it can shadow a mail host or a subdomain served by
somebody else entirely. That is why the option is opt-in rather than the default.

## Rolling back

Move the router back to its previous certificate resolver. That is the whole procedure, and it is
never urgent: certificates already issued are files in Traefik's store and stay valid for their full
lifetime regardless of what this service does.

The customer's CNAME can be left in place — it is inert while unused, and leaving it means a
future retry needs no further contact with them.

## Checklist per load balancer

- [ ] Project, load balancer and every node address registered
- [ ] API key created, restricted to the nodes' egress addresses, secret stored
- [ ] Traefik static config on every node: `httpreq` provider, public resolvers
- [ ] `HTTPREQ_MODE` unset, CNAME support not disabled
- [ ] Challenge API reachable from every node (`curl -u` returns 400, not 401 — see below)
- [ ] First domain added, `CNAME` sent, verification green
- [ ] One router switched over and a full present → issue → cleanup cycle observed

> **Quick credential test**
>
> From an LB node, with an empty body:
>
> ```
> curl -si -u "k_7f3a91c4:$ACME_KEY" \
>      -X POST https://acme-api.cloudpowered.services/present -d '{}'
> ```
>
> `400` means the credential and the source address were both accepted and only the body
> was rejected — which is the result you want. `401` means the key is wrong, disabled, or
> the source address is outside its restriction.

---

2 · Per-load-balancer setup — [← DNS zone setup](dns-zone-setup.md) · Next: [Install & configure →](installation.md)
