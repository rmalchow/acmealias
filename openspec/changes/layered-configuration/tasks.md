# Tasks

> Every task here preserves existing behaviour. A deployment configured entirely
> by `ACME_*` variables must behave identically at the end of this change as it
> does at the beginning — that is the property that makes this a migration rather
> than a cutover (design G1), and task 5.1 is what proves it.

## 1. The loader

- [x] 1.1 Adapt `convotis-cloud/portal/backend/internal/config` into `internal/config/load.go`, keeping its structure and its comments. It is adapted rather than rewritten because the interesting parts — pre-parse resolution, schema-aware environment mapping, the escape sequence — are not obvious, and a second implementation rediscovers them by getting them wrong (G6)
- [x] 1.2 Drop the shared `APP_` prefix and the per-service prefix layering. One binary, one prefix, `ACME_` (G6)
- [x] 1.3 Keep `Source` tracking, and log the layers at start-up: which files were read, which were absent, which prefixes were overlaid. "Why is this deployment behaving like that" is otherwise answered by guessing (G6)
- [x] 1.4 Placeholders resolve BEFORE the YAML is parsed, so `port: ${ACME_PORT:8080}` stays an integer rather than becoming a string (G2)
- [x] 1.5 Support `${VAR}`, `${VAR:default}` and `${VAR:-default}`, with `$${` escaping to a literal `${` — a configuration file that cannot express a dollar-brace cannot configure a service whose password contains one (G2)
- [x] 1.6 Deep merge for maps; a list REPLACES the list beneath it. Test the nameserver case explicitly: a base of two and an overlay of one yields one, not three (G3)
- [x] 1.7 A missing overlay file is not an error. A base file that was explicitly configured and is absent IS one (G4)

## 2. The schema

- [x] 2.1 Define the configuration struct with the grouping of design G7 — `zone`, `listeners`, `database`, `oidc`, `staff`, `challenge`, `limits`, `log`
- [x] 2.2 Express `staff.oidc` with the same shape as `oidc`, since they are the same five ideas twice and the flat namespace could only say so by rhyming prefixes (G7)
- [x] 2.3 Give `limits.projects_per_personal_account` a home. `personal-and-team-accounts` C13 requires the quota to be configuration and this is where it lives (G7)
- [x] 2.4 Map every one of the forty `ACME_*` variables onto a path in the schema, BY SCHEMA rather than by splitting on underscores: `ACME_SOA_PRIMARY_NS` could split four ways and only one is a path that exists (G5)
- [x] 2.5 Rename nothing. The forty keep their names and meanings; a later change may introduce better ones, and this one earns that right by breaking nothing (G5)

## 3. The shipped files

- [x] 3.1 Write `config.yaml` — the base, committed, with every default visible in it rather than in a Go call the operator cannot see
- [x] 3.2 Quote every placeholder value. A substituted secret containing `: ` produces either invalid YAML or valid YAML meaning something else, and quoting is the convention that avoids it (G2)
- [x] 3.3 Never put a secret in a committed file. `client_secret` and the database password are `${...}` with no default, so an unset one fails loudly rather than starting with a blank password
- [x] 3.4 Add the development overlay and reduce `development/.env` to what genuinely differs — secrets and machine-specific addresses

## 4. Wiring

- [x] 4.1 Replace the body of the existing `config.Load` with the new loader while keeping its signature, so no caller changes in this task
- [x] 4.2 Keep the environment at the TOP of the precedence order, above the files (G1). A container platform's `env:` block is the last word, and a mechanism that let a committed file beat it would be fighting the platform
- [x] 4.3 Update `docs/installation.md`: the settings table becomes a description of the file, with the environment variables recorded as the equivalent override

## 5. Proving nothing moved

- [x] 5.1 THE LOAD-BEARING TEST: with no configuration file present, a deployment setting only `ACME_*` variables produces byte-identical configuration to what it produces today. Snapshot the current struct for a representative environment and assert equality against it (G1)
- [x] 5.2 Test the precedence order in both directions: a file value beats a default, an environment variable beats a file value
- [x] 5.3 Test that an absent overlay changes nothing and an absent-but-configured base file fails to start (G4)
- [x] 5.4 Test placeholder typing: an integer placeholder resolves to an integer, a boolean to a boolean, a list to a list (G2)
- [ ] 5.5 Run the acceptance suite. It exercises the real start-up path, which is the only place the loader's failure modes actually matter
