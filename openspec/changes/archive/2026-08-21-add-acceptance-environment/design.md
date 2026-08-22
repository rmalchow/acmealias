## Context

This change builds the end-to-end verification that `add-acme-alias-service` specified and did not
build. It adds no topology of its own, because the topology already exists twice over: it was
argued as **design decision D14 of `add-acme-alias-service`** (now
`openspec/changes/archive/2026-08-21-add-acme-alias-service/design.md`), and it was then *built*,
as the `acme` profile of `development/compose.yaml`.

**Read D14 first.** It is not summarised here on purpose. It holds the container diagram, the
pebble flags and why each one is set, the CoreDNS zone topology, the two-certificates trap in
pebble's management API, the Traefik pin and the version gap it leaves, the browser-less sign-in
sequence, and the one simplification that was consciously accepted (our zone is reached through a
forward in CoreDNS rather than by walking a real NS delegation from a root) together with what is
done instead. Restating any of that here would create a second copy to drift, and D14 is cited by
number from elsewhere in that document — D2, D6, D7, D13 and the testing section all point at it.

What this change adds on top of D14 is not a topology. It is the assertions, and one decision about
where they run.

## Decisions

### A1. The suite runs against the development stack; there is no second environment

D14 describes a Compose environment under `development/acceptance/` and the first draft of this
change had six tasks to build it. Those tasks are dropped. The suite runs against
`development/compose.yaml` — the stack a developer already has up — using its own front door:
`./scripts/dev.sh reset --yes`, then `./scripts/dev.sh up --acme`.

The reason is simply that the second rig would be a copy of the first. Concretely, here is the
would-be `development/acceptance/` on the left and what already exists on the right:

| the rig D14 describes | what `development/` already has |
| --- | --- |
| `acceptance/compose.yaml`, seven containers | `compose.yaml`: postgres, keycloak, coredns, api, web, traefik + pebble, `traefik-lb`, whoami under profile `acme` |
| `acceptance/pebble/` with `-dnsserver coredns:53`, `PEBBLE_VA_NOSLEEP=1`, `PEBBLE_WFE_NONCEREJECT=0` | the `pebble` service, with exactly those flags and those two variables |
| `acceptance/coredns/` zone files for `example.test` / `wild.test` | `development/dns/Corefile` + `zones/example.com.zone`, authoritative for the hijacked `example.com`, carrying the `traefik-lb` address records and the one shared `_acme-challenge` CNAME |
| `acceptance/traefik/` — stock image, `provider: httpreq`, `LEGO_CA_CERTIFICATES` | `traefik-lb` (`traefik:v3.1`), `HTTPREQ_ENDPOINT=http://api:8080/`, `LEGO_CA_CERTIFICATES=/pebble/minica.pem` extracted from the image by `up.sh` |
| `acceptance/seed.sh` via `adminctl` | `development/scripts/seed.sh` via `adminctl`, which also captures the one-time key secret into `data/acme-key.env` for `traefik-lb` |
| `acceptance/realm/acceptance-realm.json` | `development/realm/acmeservice-realm.json`: realm `acmeservice`, four groups, four users |
| a driver that orders a certificate | `development/scripts/issue.sh`: discards `acme.json`, restarts `traefik-lb`, waits on the *served* certificate, verifies against pebble's root **and** intermediate |

Nothing on the left is absent from the right. Building it anyway costs six tasks and buys a second
set of files that nobody exercises between acceptance runs.

And they would drift, in a specific and already-documented way. The alias label the customer must
CNAME to is *derived*: `dnsname.ChallengeName("*.demo.example.com")` gives
`_acme-challenge.demo.example.com`, and `SuggestLabel` of that gives `demo-example-com`. The zone
file carries a comment saying exactly this, and warning that if the seeded domain names change, the
label changes with them and issuance fails with an NXDOMAIN pointing nowhere near the cause. That
coupling between the seed and the zone is real, sharp, and survives only because a human runs into
it every day in the one stack they use. A second, unattended copy of the same coupling is a
landmine with nobody walking over it.

Rejected, in order of how tempting they were:

- **Build the rig as specified.** Above. Duplication that drifts, and no assertion gained: not one
  property this change asserts needs a container the `acme` profile does not already start.
- **Drop automated acceptance and keep `dev.sh issue` as the gate.** `issue.sh` really does drive
  the whole path, so this is not a strawman. It fails because it can only observe the *positive*
  case: a certificate that was issued says nothing about whether a key from another load balancer
  would have been refused, whether `/cleanup` fired, or whether the alias ends up empty rather than
  nonexistent. Half the value here is in the negative assertions, and none of them are visible in a
  certificate. (Note that this is a different question from *where* the suite runs — see A10, which
  does keep it off the pipeline.)
- **A hybrid: reuse the dev compose file but with an `acceptance` profile and separate zones.** A
  smaller version of the same mistake. The zones and the seed are precisely the parts that drift;
  duplicating only those duplicates only the dangerous half.
- **Keep the rig for CI and run the suite against the dev stack locally.** Two code paths to the
  same assertions, and the one that gates merges is the one nobody debugs interactively. If the CI
  path is different from the local path, a red pipeline is a research project.

The cost of this decision, stated plainly: the acceptance suite is now coupled to the development
stack, and editing a seeded domain name or a zone file can turn the pipeline red. That is the
coupling being asserted, not an accident — task 2.6 exists to catch exactly that edit — but it does
mean some acceptance failures are stack failures rather than service failures.

### A2. The specification is retargeted, not weakened

`specs/acceptance-harness/` keeps all nine of its original requirements and adds a tenth: the suite
is a one-command local gate and the pipeline declares no acceptance stage (A10). What changed in
the other nine is **where** each property is verified,
never **what** is verified: requirement text that said "the environment" now says "the development
stack", and the requirement that mandated a self-contained startable environment now mandates a
build-tagged suite that brings the existing stack up and down and defines no second copy of it.

Two requirements gained a scenario rather than losing one. The authorization requirement gained
"the source restriction is enforced in both directions" (A4), and the plan requirement gained "a
drifted zone fails the test". Both close holes where a test could have passed for the wrong reason.

Rejected: relaxing the harness requirements to match whatever the dev stack happens to make easy.
The scope reduction in this change comes entirely from not rebuilding infrastructure, and letting
it leak into the assertions would have converted a saving into a loss.

### A3. Fixtures come from the seed; the one exception is created through the API

The suite uses what `development/scripts/seed.sh` already creates and does not re-seed: project
`demo`, load balancer `demo/prod`, nodes `a`/`b`, domains `demo.example.com` and
`*.demo.example.com` sharing the alias `demo-example-com`, one API key, and the four realm users
`admin-user` / `config-admin-user` / `key-rotation-user` / `read-only-user`, all `devpass`.

Exactly one assertion needs something the seed does not create. Proving that a key belonging to
another load balancer is refused requires another load balancer, and there is one. The suite
creates it — and the CIDR-restricted key — through the service's own admin API with the
`admin-user` session, and removes them afterwards (task 2.4).

Rejected: extending `seed.sh`. It is a human-facing fixture whose output a developer reads on every
`dev.sh reset`, and a second load balancer that exists only so a test can be refused by it is
noise in that output and a puzzle for the next person who runs `adminctl loadbalancer list`.
Creating it through the API also exercises a code path — inventory creation under a real session —
that would otherwise only ever be driven by `adminctl`.

### A4. Boundary assertions are calibrated against what the API actually observes

The source-restriction test cannot assume the host's address. Requests from the test process reach
the challenge API through the dev Traefik overlay, so the peer address the service sees is the
overlay container's (`172.28.0.40`), not the host's. A CIDR chosen against the wrong address
produces a test that refuses everything and passes for the wrong reason, forever.

So the test asserts both directions — a CIDR containing the observed peer is accepted, one
excluding it is refused — and only then adds the forged `X-Forwarded-For` case on top. Same
instinct behind task 2.5's second assertion: a restart test that does not check the container
actually restarted decays silently into a duplicate of task 2.1.

### A5. The plan-versus-zone comparison is exact on alias records and explicitly excludes addresses

Task 2.6 compares `adminctl loadbalancer dns demo/prod` against `development/dns/zones/example.com.zone`
record for record — over the `_acme-challenge` alias records, exactly, in both directions.

The zone's **address** records are deliberately not comparable, and the zone file says so in a
comment: it points `demo` and `*.demo` at the `traefik-lb` container (`172.28.0.30`) because
something has to actually answer, while the generated plan prints the seeded node addresses
(203.0.113.10/.11 — TEST-NET-3, unroutable on purpose). Only the CNAME has to match for issuance to
work, and only the CNAME is what issuance validated against.

The spec therefore says the exclusion must be explicit and documented rather than silently omitted.
Rejected: asserting the address records equal anyway, which would assert something false; and
comparing "some subset" without saying which, which is how a test quietly stops covering anything.

### A6. The sign-in helper carries its own cookie jar, and the reason is written down

Go's `net/http/cookiejar` will not return a cookie marked `Secure` over a plain `http://` URL. This
service sets both the session cookie and the CSRF cookie `Secure` (`internal/oidcauth/session.go`,
`internal/oidcauth/csrf.go`), and the dev stack speaks plain HTTP on `acme-console.localhost`. A
browser is fine with that — `*.localhost` is a trustworthy origin under the Secure Contexts spec,
so browsers store and send `Secure` cookies there — but the standard library implements the plain
RFC 6265 rule and does not.

The symptom is a sign-in that looks like it worked, followed by a 401 with no cookie on it. Finding
that took hours with `curl`, so task 1.3 states it in full: the helper needs its own tiny
`http.CookieJar`, and the alternative — making the cookies non-`Secure` in development — is
rejected outright, because it would delete in dev the exact attribute production depends on and
that `session_test.go` and `csrf_test.go` assert.

### A7. Task 24.7 travels with the suite, not with the runbook

Task 24.7 of `add-acme-alias-service` — a rollout check for any load balancer running Traefik 2.11 —
is by subject matter a runbook paragraph. It lives here anyway, because it exists only as
compensation for a hole in *this* work: the supported floor is 2.11 (D2), the stack's `traefik-lb`
runs `traefik:v3.1`, and so 2.11 is supported and never exercised. The check — confirm the bundled
lego with the `go.mod` one-liner from D2, then watch one full present → validate → cleanup cycle on
that load balancer before moving the rest of its domains — is what stands in for the acceptance run
that does not happen.

Keeping the compensation next to the gap means whoever later widens the matrix is looking at task
3.3 while they do it. Left in the runbook group, it reads as unexplained operator folklore about an
old Traefik.

### A8. Group order is dependency order

Three groups: the harness, the assertions, the wiring. They are strictly ordered even though groups
1 and 2 touch the same tree, because every assertion is written against the harness's lifecycle,
trust pool and sign-in client, and because group 3 cannot honestly describe a gate in the `README`
before the gate passes.

### A9. The pointer repair is a task, not a footnote

`scripts/test-acceptance.sh` guards with `backend/acceptance does not exist yet (task group 23)`.
Group 23 exists nowhere. The guard and its pointer both go in task 3.1, in the same commit range
that makes the message obsolete anyway; the identical pointer in the `acceptance` job's comment
leaves with the job itself (A10). It is called out explicitly rather than left implied because a
stale pointer that survives the change that obsoletes it tends to survive forever.

### A10. CI does not run the suite; it lints, tests, builds and pushes

The `acceptance` stage and its job come out of `.gitlab-ci.yml`. The pipeline becomes
`lint → test → build → publish`, and the acceptance suite is run by a person with
`./scripts/test-acceptance.sh`.

The reasoning is about what merges should wait on. The job is not cheap: it needs a Docker daemon,
an image build from `Dockerfile.api`, an `npm ci` for the web container, a Keycloak boot and
several real ACME exchanges — minutes on every pipeline. Right now nobody is asking merges to block
on that signal, and a pipeline whose job is "lint, unit-test, build the image, push it on a tag" is
the pipeline that is wanted. Paying minutes per merge for a gate nobody is currently consulting is
the kind of cost that gets the whole stage disabled in a hurry six months from now, badly.

**The cost, stated plainly: a suite nobody runs automatically will rot.** Some assertion will start
failing for a reason unrelated to the service, nobody will notice for weeks, and the next person to
run it will find a wall of red and no idea which failure is real. There are two counterweights and
they are both weak on purpose, because a strong one would be the CI job:

1. It stays cheap to invoke — one command, no arguments, and `ACCEPTANCE_KEEP_STACK=1` for an
   iterating developer.
2. It is named in the `README` (task 3.3a), with its cost, so it is discoverable by someone who
   never reads `openspec/`.

And a third thing, which is why this is recorded as a decision rather than done quietly: task 3.2
requires a comment where the job used to be, saying the exclusion is deliberate. Without it, the
next reader finds a script with no caller and re-adds the job — or, worse, deletes the script.

Rejected: **keeping the job.** It is the version with teeth, and it is where this should end up
once someone wants merges gated on end-to-end issuance. Rejected for now on pipeline time, and on
the plain instruction that CI's job is build-and-push. Re-adding it is a one-block edit to
`.gitlab-ci.yml` — the script it would call is exactly the same one — so this decision is cheap to
reverse, which is the main thing that makes it acceptable.

Rejected: **a scheduled pipeline** running the suite nightly instead. A middle road that keeps some
automatic signal, and worth revisiting. Not taken here because it is still CI infrastructure to
own, with the added property that its failures arrive detached from the commit that caused them —
which is a worse debugging position than running it by hand.

## Risks / Trade-offs

- **Nothing enforces the suite.** The largest risk in the change, and it is A10's, not A1's: an
  unenforced suite decays, and the decay is invisible until someone runs it. Held only by the
  `README` paragraph and by how cheap it is to invoke.
- **Acceptance failures can be stack failures.** Editing a seeded domain, a zone file or a dev
  script can turn the suite red without the service having changed. This is the coupling the suite
  exists to catch (2.6 is literally that assertion), but it does mean the first question on a red
  run is "did anything under `development/` move?" — which the tasks say.
- **A run is minutes, not seconds.** A full reset plus several real issuances. Mitigated for the
  developer loop by `ACCEPTANCE_KEEP_STACK=1`, which reuses an already-running stack; a clean run is
  the honest one and stays the default.
- **The dev stack is now load-bearing for a gate.** It was previously a convenience that could be
  broken for an afternoon. Less so now — a real constraint on whoever edits
  `development/compose.yaml`, and an argument for the stack staying simple.
- **2.11 remains unexercised.** Unchanged from D14, and unchanged by the collapse — the stack pins
  `traefik:v3.1` for the same reason the rig would have pinned a 3.x tag. Task 3.3 is the standing
  compensation.
- **Two changes cite each other.** This one cites D14 and task 22.4 of `add-acme-alias-service`,
  which is archived; those references resolve into `openspec/changes/archive/`, where an archived
  change is expected to be found. Nothing dangles, but the reader follows one hop.
