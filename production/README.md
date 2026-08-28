# Production deployment

What runs on `vps-euc` at `/docker/acme`, kept here because a deployment that
exists only on the server is a deployment nobody can review, diff or rebuild.

```
docker-compose.yml          the stack
env.example                 every key .env needs, with the values stripped
postgres-init/              role and database bootstrap, run ONCE on a fresh cluster
authentik/blueprints/       the identity provider's configuration as code
traefik/acme.yml            the ingress, copied to /docker/traefik/config/config/
```

## Deploying

1. `mkdir -p /docker/acme && cd /docker/acme`
2. Copy `docker-compose.yml`, `postgres-init/`, `authentik/` here.
3. `cp env.example .env`, fill every value, `chmod 600 .env`.
4. Copy `traefik/acme.yml` to `/docker/traefik/config/config/` — the file
   provider picks it up without a restart.
5. `docker compose up -d postgres` and wait for healthy, so the init scripts
   run against an empty cluster with nothing racing them.
6. `docker compose up -d authentik-server` and WAIT for it to finish migrating
   before starting anything else. See the note below.
7. `docker compose up -d`.

## Two things that will bite

**Start the authentik SERVER alone, then the worker.** Both run Django
migrations on boot, and against an empty database they interleave and produce
`InconsistentMigrationHistory: migration X is applied before its dependency Y`.
The only cure is dropping the authentik database and starting over, so it is
much cheaper to sequence the first boot than to recover from it. It takes
several minutes and several hundred migrations; the server is `unhealthy` and
answers 500 the whole time, which is normal and not a fault.

**The init scripts run ONCE**, on a fresh data directory, and `docker-entrypoint`
abandons the whole sequence at the first failure. A script that fails silently
takes every later script with it — which is how a deployment ends up with an app
role but no identity-provider role, and an authentik that logs
`password authentication failed` for a role that was never created.

## What is NOT here

`.env` itself, and it never should be — it holds the database password, the
authentik secret key, two OIDC client secrets and the mail password.
