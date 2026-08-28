# Design

Decisions are numbered `G1`, `G2`, … and referred to by number from `tasks.md`
and from code comments.

## G1. Files are the primary source; the environment stays a valid one

Precedence, lowest to highest:

```
  1. defaults declared in Go        the struct's zero-ish values
  2. the base file                  config.yaml
  3. overlay files, in order        config-<profile>.yaml
  4. environment variables          ACME_*
```

The environment sitting on top rather than underneath is deliberate, and it is
what makes this a migration rather than a cutover. A deployment configured
entirely by `ACME_*` today keeps behaving identically after this lands, because
its variables still win over everything the files say. Nothing has to move on the
day the mechanism arrives.

It also matches how a container platform actually injects configuration: a
Kubernetes `env:` block or a compose `environment:` is the last word, and a
mechanism that made a committed file beat it would be fighting the platform.

## G2. Placeholders resolve BEFORE the YAML is parsed, and the reason is types

`${VAR}` and `${VAR:default}` are substituted in the file's bytes, and the result
is then parsed as YAML.

The alternative — parse first, then walk the decoded values substituting into
strings — is more obviously safe and is wrong in a way that shows up immediately:

```yaml
port: ${ACME_PORT:8080}
```

Resolved pre-parse, YAML sees `port: 8080` and the value is an integer. Resolved
post-parse, YAML first sees the string `${ACME_PORT:8080}`, and whatever is
substituted into it stays a **string** — so `port` is `"8080"` and every consumer
either coerces or fails. The same applies to booleans, durations and lists.

The cost is that a substituted value can break the document: a password
containing `: ` will produce invalid YAML or, worse, valid YAML meaning something
else. That is real, and the mitigation is that it fails loudly at parse time
rather than silently — plus the convention of quoting placeholder values in the
files, which the shipped files follow.

`$${NOT_A_VAR}` escapes to a literal `${NOT_A_VAR}`, because a configuration file
that cannot express a dollar-brace is a configuration file that cannot configure
a service whose passwords contain one.

## G3. Maps merge deeply. Lists replace, wholesale

Two structures combine key by key, descending into maps present on both sides. A
list on the overlay replaces the list underneath it entirely.

**Lists replace because a list is a whole answer, not a set of contributions.**
Consider the nameserver names:

```yaml
# config.yaml
ns_names: [ns1.acmealias.com, ns2.acmealias.com]

# config-staging.yaml
ns_names: [ns1.staging.acmealias.com]
```

Merging would give three nameservers, one of which is production's, and nobody
wrote that list. The same reasoning applies with more force to
`trusted_proxy_cidrs`: a merged allowlist is an allowlist somebody widened by
accident, and the accident is invisible in both files.

The cost is that appending to a list means restating it. That is the right trade
for lists this short, and the failure mode of the alternative is a security
control that silently grew.

## G4. A missing overlay is not an error; a missing base file is

An overlay names a way this deployment differs. A deployment that does not differ
that way has no such file, and demanding one would make every optional overlay a
required stub.

The base file is different: if it was explicitly configured and is absent,
something is wrong with the deployment and starting anyway would run on defaults
nobody chose. Absent *and* unconfigured is fine — that is the environment-only
deployment of G1.

## G5. Every `ACME_*` variable keeps working, and the mapping is by schema

Environment keys map onto the configuration structure rather than by splitting on
underscores and hoping. `ACME_OIDC_CLIENT_ID` reaches `oidc.client_id` because
the loader knows `oidc` is a node and `client_id` is a field in it — not because
it guessed where to split a string that contains three underscores.

That distinction is what makes a flat namespace survive contact with a nested
one. `ACME_SOA_PRIMARY_NS` could split four ways; only one of them is a path that
exists.

**Nothing is renamed in this change.** The forty variables keep their names and
their meanings. A later change may introduce better ones; this one earns the
right to by not breaking anything.

## G6. Adapted from the portal, not shared with it, and not rewritten

`convotis-cloud/portal/backend/internal/config` already implements this. It is
adapted into this repository rather than depended on.

**Rejected: a shared module both repositories import.** Cleaner in principle, and
it means a release process, a version to bump and a coordination step between two
repositories in different groups — for a file that changes twice a year. The
coupling costs more than the duplication.

**Rejected: writing a second implementation.** The interesting parts of this are
not obvious: pre-parse resolution (G2), schema-aware environment mapping (G5),
and the escape sequence. Rediscovering them is how the second implementation gets
the type bug that the first one has a comment about.

**What is dropped in the adaptation**, because this service is one binary rather
than fourteen: the shared `APP_` prefix that lets settings apply across services,
and the per-service prefix layering beneath it. One prefix, `ACME_`.

**What is kept and is easy to undervalue**: `Source` tracking. The loader reports
which files it read, which were absent and which prefixes it overlaid, and the
service logs that at start-up. The question "why is this deployment behaving like
that" is otherwise answered by guessing which overlay applied.

## G7. What the file looks like is decided here, not left to whoever migrates first

A structure invented setting-by-setting during a migration is a structure nobody
designed. The grouping is:

```yaml
zone:      { name, soa_primary_ns, soa_hostmaster, ns_names }
listeners: { dns, http }
database:  { url, role, password }
oidc:      { issuer, client_id, client_secret, redirect_url, groups_claim, group_prefix }
staff:     { host, oidc: { client_id, client_secret, redirect_url } }
challenge: { trusted_proxy_cidrs, verify_resolver, alias_use_domain }
limits:    { projects_per_personal_account }
log:       { level }
```

Two things this makes visible that the flat namespace could not. The console's
OIDC settings and the staff plane's are **the same shape**, which is now
expressed by `staff.oidc` mirroring `oidc` rather than by two prefixes that
happen to rhyme. And `limits` has somewhere to live — `personal-and-team-accounts`
C13 needs the project quota to be configuration, and this is where it goes.
