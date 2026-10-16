# Example Compose stacks

Two self-contained deployments of the published image
`harbor.rand0m.me/public/acmealias`. Neither one handles TLS or runs a reverse
proxy. Each publishes plain HTTP on loopback, and you put the HTTPS proxy you
already run in front of it.

| | `internal-idp/` | `external-idp/` |
| --- | --- | --- |
| Sign-in | authentik, in the stack, configured from a blueprint | any OIDC provider you already run |
| Containers | postgres, redis, authentik server + worker, acmealias | postgres, acmealias |
| Self-registration | yes — authentik's sign-up page, with address confirmation | whatever your IdP allows |
| Team accounts | anyone signed in may create one (`ACME_ALLOW_ACCOUNT_CREATION=true`) | closed; create them with `adminctl` |
| HTTP ports to proxy | service `8080`, authentik `9000` | service `8080` |

`production/` is the deployment on vps-euc and stays the reference for it. These
are the same shape with the host-specific parts turned into `.env` values and
traefik taken out.

## Using one

```
cp -r examples/internal-idp /opt/acmealias     # or external-idp
cd /opt/acmealias
cp env.example .env && chmod 600 .env          # fill in every empty value
docker compose up -d
```

For `internal-idp`, sequence the **first** start. The authentik server and worker
both run migrations, and against an empty database they interleave and corrupt
the migration history:

```
docker compose up -d postgres
docker compose up -d authentik-server   # wait until `docker compose ps` says healthy, several minutes
docker compose up -d
```

## What your proxy has to do

- **Terminate TLS for every hostname.** The service sets `Secure` cookies, so on
  plain `http://` with a real hostname the browser discards the session and
  sign-in loops without an error. (`*.localhost` is the one exception browsers
  make, which is how the development stack gets away with http.)
- **Pass `Host` through unchanged**, and set `X-Forwarded-For` and
  `X-Forwarded-Proto: https`. authentik builds its issuer and redirect URLs from
  them. The service picks the customer or staff console from the Host header.
- **Block `/metrics` and `/api/admin/` on the public console hostname.** One
  listener serves everything, so this is a proxy rule or nothing.
- **Make the hostnames resolvable from inside the containers.** The service
  fetches OIDC discovery from the issuer's public `https://` URL at start-up, so
  for `internal-idp` it reaches authentik back through your proxy.

Both stacks pin their Compose network (`NETWORK_SUBNET`, default
`172.29.53.0/24`) and trust forwarding headers only from it. A proxy on the
same host arrives from that network's gateway. A proxy somewhere else needs
`ACME_TRUSTED_PROXY_CIDRS` changed to its address.

## DNS

Port 53 is published directly on `DNS_BIND`, over UDP and TCP. Use the host's
public address, not `0.0.0.0`: a wildcard collides with systemd-resolved on
`127.0.0.53` and is IPv4-only. To serve IPv6, add the two port lines again with
`"[<v6 address>]:53:53/udp"` and `/tcp`. Delegating the zone is covered in
[`docs/dns-zone-setup.md`](../docs/dns-zone-setup.md).

## After the first sign-in

Every user gets a personal account on first sign-in. For a shared team account
when self-service creation is closed:

```
docker compose run --rm acmealias adminctl account create \
  --slug infra --name "Infrastructure" --owner <idp-subject>
```

`--owner` is the `sub` claim, which you can read on `GET /api/v1/me` once that
person has signed in. More in [`docs/installation.md`](../docs/installation.md#6-first-sign-in).
