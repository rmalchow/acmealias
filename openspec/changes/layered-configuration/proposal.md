# Configuration in files that overlay, with placeholders that fall back

## What this changes, in one paragraph

The service is configured by forty environment variables named `ACME_*`, read
one at a time in `internal/config/config.go`, each with its default written in Go
beside the call that reads it. This adds a configuration **file** — YAML — that
can be layered: a base that describes what the service is, and overlays that
describe how this particular deployment differs. Values in those files may refer
to environment variables with a fallback, `${FOO:foobar}`, so a secret or a
per-environment endpoint is injected rather than committed. Every existing
`ACME_*` variable keeps working.

## Terms, before they are used

- An **overlay** is a second file whose values win over the first where they
  overlap, and are ignored where they do not. Layering base + overlay means a
  deployment states only its differences.
- A **deep merge** combines two nested structures key by key, descending into
  maps that both sides have, rather than replacing the whole map wholesale.
- A **placeholder** is `${NAME}` or `${NAME:default}` inside a file, replaced
  with the environment variable's value — or with the default when it is unset.
- **Pre-parse resolution** means placeholders are substituted in the file's text
  before the YAML is read, rather than in the values afterwards. Which of those
  two you pick changes the type of the result, and design G2 is about why.

## Why

### Forty variables is a shape that stopped fitting some time ago

`ACME_ZONE`, `ACME_SOA_PRIMARY_NS`, `ACME_SOA_HOSTMASTER`, `ACME_NS_NAMES`,
`ACME_DNS_LISTEN`, `ACME_LISTEN`, `ACME_DATABASE_URL`, `ACME_DB_ROLE`,
`ACME_TRUSTED_PROXY_CIDRS`, `ACME_VERIFY_RESOLVER`, five `ACME_OIDC_*`, four
`ACME_STAFF_*`, four `ACME_*_GROUP`, and the rest. Forty, in one flat namespace,
with no way to say that five of them are about one thing.

Three specific costs, and none of them is aesthetic:

**The effective configuration is not readable anywhere.** A default lives in Go,
in the `str("ACME_VERIFY_RESOLVER", "")` call that reads it. An operator asking
"what is this deployment actually doing" has to read `.env`, then read the source
to find what every unset variable falls back to, and hold both in their head.

**A deployment cannot state only its differences.** `development/.env` is a
hundred lines, most of which are identical to what production would want, and
`docs/installation.md` documents the same forty settings again in a table. The
two drift, and the drift is invisible until something is misconfigured.

**Structure cannot be expressed at all.** The staff plane's four settings and the
console's five are the same five ideas twice, and the flat namespace can only say
so by repeating the prefix. Anything genuinely nested — per-listener settings,
a list of trusted issuers with attributes — has no representation.

### Placeholders are what keep secrets out of the files

The moment configuration lives in a file, the question is what happens to the
database password. Committing it is wrong; keeping the whole file out of version
control loses the reason for having it.

`${ACME_DB_PASSWORD}` in a committed file answers both: the shape of the
configuration is in the repository and reviewable, the value is injected by
whatever manages secrets in that environment. The optional default,
`${ACME_LOG_LEVEL:info}`, then does the second job — it puts the default *in the
file*, where the operator reading the file can see it, rather than in the Go
source where they cannot.

### This has already been solved once, in a repository next door

`convotis-cloud/portal` has `backend/internal/config`: layered files, profile
overlays, `${VAR}` / `${VAR:default}` / `${VAR:-default}` expansion, environment
overlay mapped onto the struct schema, and source tracking so a service can log
which layers actually applied.

It is a good implementation, it is battle-tested in a stack with fourteen
services, and **this change adapts it rather than writing a second one**. What
that costs and what it buys is design G6.

## What Changes

- **A configuration file**, YAML, read at start-up: an optional base and any
  number of optional overlays, each stating only how it differs from the layer
  beneath it.
- **Placeholders in those files** — `${VAR}`, `${VAR:default}`, `${VAR:-default}`
  — resolved from the environment before the document is parsed, so a resolved
  value keeps its type.
- **Deep merge for structures, wholesale replacement for lists**, because a list
  is a whole answer and a merged one is an answer nobody wrote.
- **A configuration schema** with the grouping of design G7, which gives the
  console's and the staff plane's OIDC settings the same shape and gives the
  project quota of `personal-and-team-accounts` C13 somewhere to live.
- **Start-up reporting** of which layers applied and which configured files were
  absent.
- **No behavioural change for anyone**: the environment keeps the highest
  precedence, every `ACME_*` variable keeps its name and meaning, and a
  deployment that sets nothing else behaves exactly as it does today.

## Capabilities

- `service-operations` — how the service is configured and what it reports about
  it at start-up.

## Impact

- `internal/config` gains a loader and a schema; its `Load` signature does not
  change, so no caller moves in this change.
- `development/.env` shrinks to secrets and machine-specific addresses.
- `docs/installation.md`'s settings table becomes a description of the file, with
  each environment variable recorded as the equivalent override.

## What changes for an operator

Before:

```
# development/.env — 100 lines, flat, defaults invisible
ACME_OIDC_ISSUER=http://auth.localhost/application/o/acmeservice-console/
ACME_OIDC_CLIENT_ID=acmeservice-console
ACME_OIDC_CLIENT_SECRET=dev-console-secret
ACME_OIDC_REDIRECT_URL=http://console.localhost/api/v1/auth/callback
```

After:

```yaml
# config.yaml — committed, reviewable, defaults visible
oidc:
  issuer:       "${ACME_OIDC_ISSUER:http://auth.localhost/application/o/acmeservice-console/}"
  client_id:    "${ACME_OIDC_CLIENT_ID:acmeservice-console}"
  client_secret: "${ACME_OIDC_CLIENT_SECRET}"     # injected, never committed
  redirect_url: "${ACME_OIDC_REDIRECT_URL:http://console.localhost/api/v1/auth/callback}"
```

`ACME_OIDC_CLIENT_SECRET` is still an environment variable, because that is the
right place for a secret. The difference is that the other three now show their
defaults to whoever opens the file.

## What this does not change

- **Every existing `ACME_*` variable keeps working**, at the same precedence it
  has now. This is a migration, not a cutover, and a deployment that sets nothing
  but environment variables behaves exactly as it does today (design G5).
- **No new settings.** The forty that exist are the forty that exist. Giving them
  structure is this change; adding to them is not.
- **No secret management.** Placeholders read the environment. What puts a value
  into that environment — a file, a vault, a Kubernetes secret — is outside this.
