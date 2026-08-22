## Why

### What this service does, and why nothing small can prove it works

This repository builds **acmeservice**: an authoritative DNS server plus a small HTTP API, whose
whole reason to exist is to answer one question asked by a certificate authority. When a Traefik
load balancer wants a TLS certificate for `shop.example.com`, it does not prove ownership by
serving a file — it proves it by publishing a secret value in DNS, at the name
`_acme-challenge.shop.example.com`, and letting the certificate authority look it up. That is the
**DNS-01 challenge**.

The customer's own DNS is not ours to write into. So instead the customer publishes, once, a
permanent `CNAME` — an alias record — at `_acme-challenge.shop.example.com` pointing into a zone
we do control. From then on the certificate authority's lookup follows that alias into our server,
and our server answers with whatever value the load balancer just published over the HTTP API.
That indirection is the product.

Six independent things have to line up for one certificate to be issued:

1. Traefik's bundled ACME client (**lego**) has to call our `/present` endpoint with the right
   name and value, authenticating with an API key scoped to one load balancer.
2. Our HTTP API has to accept it, decide the key is allowed to speak for that name, and write the
   value to PostgreSQL.
3. Our DNS server has to start answering that name, immediately, with the value it just stored —
   including two values at once for the wildcard case, where `demo.example.com` and
   `*.demo.example.com` share a single challenge name.
4. Lego's own **propagation check** — it polls DNS itself before telling the CA to go look — has
   to follow the customer's `CNAME` rather than giving up at the alias. Whether it does depends on
   the lego version, not the Traefik version, which is why the supported floor is stated the way
   it is (`design.md` D2 of `add-acme-alias-service`).
5. The certificate authority's validation server has to resolve the same name, through the public
   DNS hierarchy, follow the same alias, and find the same value.
6. Traefik has to call `/cleanup` afterwards, and our store has to end up with no leftover rows
   while the alias still resolves as an existing-but-empty name rather than a non-existent one.

Every one of those steps is covered by a unit test in `add-acme-alias-service`, and none of the
unit tests can fail in the way that actually breaks a customer. A unit test asserts that our DNS
server returns two TXT records when the store holds two values. It cannot assert that lego, having
followed a CNAME into our zone, saw both of them and then let issuance proceed. **The bugs live in
the seams**, and this change puts a load on the seams.

### The environment already exists. The assertions do not.

When this change was first written, none of that machinery existed and the plan was to build a
seven-container rig under `development/acceptance/`: its own compose file, its own pebble, its own
CoreDNS zones, its own Traefik, its own seed script, its own Keycloak realm.

Since then, `development/compose.yaml` was built and it *is* that rig. Its default profile brings
up postgres, keycloak, coredns, the api under `air`, the Angular dev server and a dev Traefik. Its
`acme` profile adds **pebble** — Let's Encrypt's own test server, which speaks the genuine ACME
protocol and performs genuine DNS-01 validation but issues from a throwaway CA with no rate limits
— a customer-like **`traefik-lb`** (the stock released image, configuration only, `provider:
httpreq` pointed at our challenge API), and a `whoami` behind it. **CoreDNS** plays the entire rest
of the world's DNS: it is authoritative for `example.com` (reserved by RFC 2606, so hijacking it
collides with nothing real) and holds the customer side of the arrangement — the address records
pointing at `traefik-lb` and the `_acme-challenge` CNAME delegating into our zone.

Four commands drive it. `./scripts/dev.sh reset --yes` rebuilds the whole thing from empty;
`up --acme` adds the fake CA and the customer-like load balancer; `seed` loads the fixtures through
`adminctl` — project `demo`, load balancer `demo/prod`, the domains `demo.example.com` and
`*.demo.example.com` sharing one alias, an API key, with the four Keycloak users imported from the
realm export; and `issue` throws away the stored certificate and drives a **real DNS-01 issuance
end to end**, present → propagation check → validate → cleanup, then fetches the site through the
new certificate verifying against pebble's own chain.

So the topology in D14 is not missing. What is missing is that `dev.sh issue` ends by printing an
issuer line and a certificate for a human to look at. **Nothing fails a build.** No script asserts
that both TXT values coexisted at the shared name, that `/cleanup` actually fired, that a key from
another load balancer is refused, that a restart mid-issuance survives, that the DNS plan we hand a
customer matches the zone that actually validated, or that a `READ_ONLY` user is refused what a
`CONFIG_ADMIN` user is allowed. Those assertions are the deliverable.

Meanwhile `.gitlab-ci.yml` **already has an `acceptance` stage**, and it already runs
`scripts/test-acceptance.sh`, and that script already exists — and dies:

```
[ -d backend/acceptance ] || die "backend/acceptance does not exist yet (task group 23)"
```

A stage that invokes a script whose only job is to explain that the thing it invokes was never
built. Both halves of that get fixed, but not in the way an earlier draft assumed. The script gets
something real to run; **the CI stage goes away.** For now the pipeline's job is to lint,
unit-test, build the image and push it on a tag, and the acceptance suite is a gate a person runs —
`./scripts/test-acceptance.sh` — not one the pipeline waits on. That is a decision with a cost, and
it is recorded as design A10 so that nobody later reads the missing stage as an oversight and
"fixes" it back.

### What a run will assert

What a person checks by hand today, done by a Go test behind an `acceptance` build tag:
`https://demo.example.com/` returns 200 and its certificate chains to pebble's chain with the
expected name on it; the wildcard and the apex are covered by one certificate with two TXT values
answerable at one challenge name simultaneously; `/present` and `/cleanup` both fired and nothing
stays answerable afterwards; a key belonging to another load balancer is refused and a
CIDR-restricted key is enforced, including against a forged `X-Forwarded-For`; restarting the api
container between publish and validation still yields a certificate; the DNS plan we generate for
an operator matches the CoreDNS zone the issuance validated against, record for record; and all
four seeded users' roles resolve through the real Keycloak, with the `CONFIG_ADMIN`/`KEY_ROTATION`
boundary pinned in both directions.

The trust chain is verified, never skipped — which matters more than it sounds, because Traefik's
fallback when issuance fails is to serve a self-signed `TRAEFIK DEFAULT CERT` of its own. A suite
with `InsecureSkipVerify` would pass green against a stack where no certificate was ever issued.

Sign-in is exercised without a browser, over HTTP, with a cookie jar — the whole OIDC path
(discovery, PKCE, ID-token verification against real JWKS, the groups claim in the shape the mapper
produces, the session cookie, CSRF) is security-relevant and nothing else in the suite touches it.

### Why this is a change of its own

`add-acme-alias-service` is the change that built this service, and it specified this verification
as one of its deliverables. Everything else in it got built — 138 tasks, nine capabilities, a
working service with a console. The acceptance suite did not: `backend/acceptance/` does not exist.

Archiving that change with those tasks still open would have done two dishonest things at once: it
would have filed unbuilt work under "archive", where finished work goes, and it would have
published an `acceptance-harness` baseline into `openspec/specs/` stating as current fact that this
repository verifies end-to-end issuance, when it does not. A spec baseline that describes software
nobody wrote is worse than an empty one, because the next reader has no way to tell which parts of
it are true. So the unbuilt work moved out and kept its own change — this one — and stays visible
in `openspec/changes/` as what it is: specified, agreed, not yet built.

### What we considered and rejected

- **Build the separate `development/acceptance/` rig as originally specified.** Six tasks: a second
  compose file, a second pebble, second CoreDNS zone files, a second Traefik configuration, a
  second seed script, a second Keycloak realm export. Rejected as duplication that drifts. Every
  one of those has a counterpart in `development/` that is *already* exercised daily by whoever is
  developing the service, and a second copy is a copy that nobody runs between acceptance runs and
  that silently diverges — the alias label, the seeded domain names and the CoreDNS zone are
  coupled in a way that already burns people (the zone file carries a comment explaining exactly
  that coupling). Two rigs means two chances to get it wrong and one of them is unattended. It also
  doubled the work with no assertion gained: not one of the properties above needs a container the
  `acme` profile does not already start. See design A1.
- **Drop automated acceptance entirely and keep `dev.sh issue` as the gate.** Tempting, because
  `issue.sh` genuinely does drive the whole path and prints a readable result. Rejected because it
  asserts none of the negative cases — an authorization refusal cannot be eyeballed from a
  certificate that was issued anyway — and because "a human looked at it" is not a thing a later
  reader can re-run. A scripted suite that a person invokes is still a gate; an issuance a person
  squints at is not.
- **Keep the `acceptance` job in CI once the suite exists.** The original plan, and the reason the
  stage was declared in the first place. Rejected *for now*: the job needs a Docker daemon and a
  full stack boot — image build, `npm ci`, Keycloak, a real ACME exchange — which is minutes on
  every pipeline for a signal nobody is currently asking merges to block on. The pipeline stays
  fast and stays what the user wants it to be: lint, test, build, push. The cost is real and stated
  in design A10 — a suite nobody runs automatically will rot — and the mitigation is that running
  it stays one command and is named in the `README`.
- **Weaken the requirements to a smaller suite** — say, dropping Keycloak or the mid-issuance
  restart — so a cheaper version lands sooner. Rejected: the sign-in path and the restart are the
  two properties nothing else in the test suite touches at all, which makes them the most valuable
  assertions in the set, not the most negotiable. The scope reduction in this change comes entirely
  from *not rebuilding infrastructure that exists*, not from asserting less.
- **Delete the `acceptance-harness` capability and re-specify it later.** Rejected because the
  requirements are good and were argued for once already. Deleting them would lose the reasoning
  and guarantee it gets re-derived worse. They are retargeted here, not re-opened.
- **Mark the tasks done and file the gap as a known issue.** Rejected outright. A checkbox that
  says a thing was built when it was not is the one failure mode this whole process exists to
  prevent.

## What Changes

Nothing about the running system. When implemented, this change adds test-only files that no
production build includes, and repairs two stale comments.

- **A suite, not an environment.** `backend/acceptance/` — a handful of Go files behind an
  `acceptance` build tag — drives the *existing* development stack via `./scripts/dev.sh` and
  asserts the properties above. The six tasks that would have created `development/acceptance/` are
  dropped; nothing under `development/` is copied, forked or duplicated.
- **Fixtures come from the existing seed.** `development/scripts/seed.sh` already creates
  everything the suite needs but one thing: there is a single load balancer, so the
  cross-load-balancer refusal has no second one to be refused against. The suite creates that
  second load balancer and the CIDR-restricted key through the service's own API and removes them
  afterwards, rather than adding test-only clutter to a fixture humans use every day.
- **One command, run by a person.** `scripts/test-acceptance.sh` runs the tagged suite instead of
  dying, and loses the guard whose `die` message still points at "group 23" — a task group that
  after the split exists nowhere.
- **The CI acceptance stage is removed.** The `acceptance` stage and its job come out of
  `.gitlab-ci.yml`, leaving `lint → test → build → publish`. A comment where the job used to be
  says why, so the gap reads as a decision rather than a deletion someone should undo.
- **The suite is documented in the `README`.** Which, with no pipeline running it, is the only
  thing standing between the suite and being forgotten.
- **One rollout note.** The runbook check for a load balancer running the supported Traefik floor
  (2.11), which neither the dev stack nor CI exercises, travels with the suite that has the gap.

**Left where it is:** design decision **D14** of `add-acme-alias-service` — the container diagram,
the pebble flags, the CoreDNS topology, the two-certificates trap, the Traefik pin and the one
consciously accepted simplification. It is cited by number from D2, D6, D7, D13 and that document's
testing section; renumbering or copying it would break those citations or create a second copy to
drift. This change cites it.

### Not in scope

- Any change to the service itself. If the suite finds a bug, fixing it is a separate change; this
  one builds the instrument, not the repairs.
- Any change to `development/compose.yaml`, the CoreDNS zones, the realm export or the dev scripts.
  If the suite needs something the stack cannot do, that is a finding to raise, not a fork to make.
- A Traefik version matrix. One 3.x image, deliberately (D14), with the resulting gap covered by
  the rollout check in task 3.3 rather than by a second CI job.
- A browser-driven test of the admin console. The sign-in flow is exercised over HTTP with a cookie
  jar; the console's own behaviour is covered by frontend unit tests.
- DNSSEC, zone transfers, or a full root-to-leaf resolution chain. D14 records the last of these as
  the one accepted simplification and what is done instead.

## Capabilities

### New Capabilities

- `acceptance-harness`: the properties an acceptance run must assert end to end — real ACME
  issuance against a throwaway CA through a stand-in public DNS, a verified fake trust chain, the
  wildcard invariant, the authorization boundaries, durability and cleanup, plan-versus-zone
  agreement, and sign-in and role enforcement against a real identity provider — and the
  requirement that the suite run against the repository's development stack rather than a second
  copy of it.

### Modified Capabilities

None.

## Impact

**New code.** `backend/acceptance/`: `main_test.go`, `trust.go`, `signin.go` and seven test files,
all behind an `acceptance` build tag that keeps them out of `go build ./...` and every ordinary
`go test` run. Nothing is compiled into the service binary or the published image.

**Changed code.** `scripts/test-acceptance.sh` (runs the suite, loses its guard),
`.gitlab-ci.yml` (the `acceptance` stage and job removed, a comment left in their place), and two
paragraphs in `README.md`.

**Not changed.** `development/` in its entirety. That is the point of the change.

**New build requirements.** None, and one fewer than before: with the acceptance job gone, the
pipeline no longer needs the Docker socket for anything but `docker build`/`docker push`, which
`build:` and `publish:` already do.

**Runtime and infrastructure.** Unchanged. Nothing here ships.

**CI time.** Unchanged, or slightly better — the pipeline loses a stage rather than gaining
minutes. The cost lands on whoever runs the suite by hand: a full reset plus several real
issuances. `TestMain` supports reusing an already-running stack (`ACCEPTANCE_KEEP_STACK=1`) so an
iterating developer does not pay a rebuild per run.

**The suite is unenforced.** Nothing fails if it is never run, and nothing notices if it rots.
That is accepted for now (design A10); the counterweights are that it stays one command and that
the `README` names it.

**Process.** With these tasks out of `add-acme-alias-service`, that change is honestly complete and
archivable, which creates the `openspec/specs/` baseline and unblocks every later change carrying a
`MODIFIED` delta.

**Risk.** The suite is now coupled to the dev stack: renaming a seeded domain, editing the CoreDNS
zone or changing a dev script can turn the acceptance stage red. That is a feature — that coupling
is exactly the class of drift a second rig would have hidden — but it means the failure will
sometimes be in the stack rather than in the service, and the tasks say where to look.
