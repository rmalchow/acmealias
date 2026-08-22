> **There is no second environment to build.** An earlier draft of this change opened with six
> tasks standing up `development/acceptance/` — its own compose file, its own pebble, its own
> CoreDNS zones, its own Traefik, its own seed script and its own Keycloak realm. That group is
> gone. `development/compose.yaml` already is that rig: postgres, keycloak, coredns, api, web and a
> dev Traefik by default, plus pebble, a customer-like `traefik-lb` and `whoami` under the `acme`
> profile. `./scripts/dev.sh reset --yes` rebuilds it from empty, `up --acme` adds the fake CA,
> `seed` loads the fixtures and `issue` already drives a real DNS-01 issuance end to end. See
> design A1.
>
> What is genuinely missing is the **assertions**. `dev.sh issue` prints a certificate and a human
> decides whether it looks right; no script fails a build. Everything below is the suite that does,
> running against the stack that already exists.
>
> Fixtures come from `development/scripts/seed.sh` and are not re-created: project `demo`, load
> balancer `demo/prod`, nodes `a`/`b` (203.0.113.10, .11), domains `demo.example.com` and
> `*.demo.example.com` sharing the alias label `demo-example-com`, one API key, and the four
> Keycloak users `admin-user`, `config-admin-user`, `key-rotation-user`, `read-only-user` — all
> password `devpass`, realm `acmeservice`. The zone `example.com` is hijacked by the stack's
> CoreDNS from `development/dns/zones/example.com.zone`. Exactly one task needs a fixture the seed
> does not create, and says so where it needs it (2.4).
>
> Group order is dependency order: the harness (group 1) is what every assertion in group 2 is
> written against, and group 3 cannot honestly describe a gate in the `README` before the gate
> passes. Note that group 3 **removes** the `acceptance` stage from `.gitlab-ci.yml` rather than
> making it work — the suite is a local gate for now, deliberately (design A10). Every task names
> the files it produces and ends in a runnable check.

## 1. The harness <!-- complexity: opus | touches: backend/acceptance -->

- [x] 1.1 `backend/acceptance/main_test.go`, `//go:build acceptance`, package `acceptance`:
      `TestMain` brings the existing development stack up from empty with `./scripts/dev.sh reset
      --yes` followed by `./scripts/dev.sh up --acme`, waits for the stack to be usable, runs the
      suite, and tears it down with `./scripts/dev.sh down` (leave `destroy` to the operator — a
      failed run's state is the only debugging material there is). Shell out to the scripts rather
      than reimplementing compose orchestration in Go; they already handle the pebble CA
      extraction, the migration and the key capture, and a second orchestrator would drift from
      them. The two calls are in that order for a reason worth a comment: `reset.sh` runs
      `up.sh` *without* `--acme`, so the fake CA and `traefik-lb` do not exist until the second
      call — and `seed.sh` must have run first anyway, because it is what writes
      `development/data/acme-key.env`, the API key `traefik-lb` authenticates to `/present` with.
      Honour an opt-out env var (e.g. `ACCEPTANCE_KEEP_STACK=1`) that skips both reset and
      teardown, so an iterating developer is not paying a full rebuild per run.
      **Check:** `go test -tags acceptance ./acceptance/... -run TestMain` (or the smallest test in
      the package) brings the stack from `./scripts/dev.sh destroy --yes` to green and back down.

- [x] 1.2 `backend/acceptance/trust.go`, same build tag: fetch pebble's chain from its management
      API on the host at `https://127.0.0.1:15000` into an `x509.CertPool`, and expose the pool to
      the suite. **Both** `/roots/0` **and** `/intermediates/0` are required — pebble issues from
      an intermediate, so a pool holding only the root does not verify a leaf and the failure reads
      as an unrelated "unknown authority". `development/scripts/issue.sh` already concatenates both
      for `curl --cacert`; this is the Go equivalent. A third certificate is in play and conflating
      them is the classic trap here: `pebble.minica.pem` (which `up.sh` copies out of the image
      into `development/data/pebble/`) secures **pebble's own management API** and is what lets a
      client *reach* `:15000` at all, while the chain fetched *from* `:15000` is what issued
      certificates are verified against. Neither substitutes for the other. Never
      `InsecureSkipVerify` on the verification path; fetching from the management endpoint itself
      may skip verification (`issue.sh` uses `curl -sk`) but must be commented as deliberate.
      **Check:** a test asserting the pool holds two certificates and that `x509.Certificate.Verify`
      of the leaf served on `https://demo.example.com` succeeds against it.

- [x] 1.3 `backend/acceptance/signin.go`, same build tag: a browser-less sign-in helper that
      completes the authorization-code flow against the stack's real Keycloak and returns an
      `*http.Client` holding the session, plus the CSRF token for state-changing calls. The
      sequence is `GET http://acme-console.localhost/api/v1/auth/login` → 302 to Keycloak → parse
      the `action` URL out of the login form → POST username and password → follow back through
      `/api/v1/auth/callback` → session cookie set. The only parsing is one `action` attribute.
      **The trap, which cost hours to find with `curl` and must not be rediscovered:** Go's
      `net/http/cookiejar` will not **return** a cookie marked `Secure` over a plain `http://` URL,
      and this service sets both the session cookie (`oidcauth/session.go`) and the CSRF cookie
      (`oidcauth/csrf.go`) `Secure`, while the dev stack speaks plain HTTP on
      `acme-console.localhost`. A browser accepts this because `*.localhost` is a *trustworthy
      origin* per the Secure Contexts spec; Go's default jar implements the plain RFC 6265 rule and
      does not. The symptom is a sign-in that appears to succeed and a following request that is a
      401 with no cookie on it. So the helper needs its own `http.CookieJar` implementation — a map
      keyed by host with the `Secure` check dropped is enough, and a `ponytail:` comment should say
      that is what it is and why. Do **not** work around it by making the cookies non-`Secure` in
      dev; that would delete the property the production path relies on.
      **Check:** a test that signs in as `admin-user` / `devpass` and gets a 200 with the expected
      username from `GET /api/v1/me` — and, as a regression pin on the jar, asserts the request
      actually carried the session cookie.

## 2. The assertions <!-- complexity: opus | touches: backend/acceptance -->

- [x] 2.1 `backend/acceptance/cert_test.go`: force a genuine issuance for the seeded domain the way
      `development/scripts/issue.sh` does — discard `development/data/traefik_lb/acme.json` and
      restart `traefik-lb`, because Traefik keeps a valid certificate for ~90 days and a restart
      alone proves nothing — then `GET https://demo.example.com/` through `traefik-lb`'s published
      `:443` (dial 127.0.0.1, set `ServerName`), assert 200, and assert the served chain verifies
      against the pool from 1.2 with `demo.example.com` among its SANs. Verifying is the whole
      point: Traefik's fallback when issuance fails is its own self-signed `TRAEFIK DEFAULT CERT`,
      so a suite that skipped verification would pass green against a stack where no certificate
      was ever issued.
      **Check:** the test is red against a stack whose `traefik-lb` never obtained a certificate
      (e.g. with the API key removed from `development/data/acme-key.env`) and green otherwise.

- [x] 2.2 `backend/acceptance/wildcard_test.go`: assert the issued certificate covers **both**
      `demo.example.com` and `*.demo.example.com`, and that during validation both challenge values
      were answerable at the single shared name `_acme-challenge.demo.example.com` at the same
      time. Two domains, one alias, one CNAME — the invariant the whole design turns on (D6). Query
      the service's own authoritative listener directly for the TXT set while the order is in
      flight; the host-published port is `8153` (`development/.env` binds the listener on `:8053`
      inside the container).
      **Check:** the test names both SANs and fails if fewer than two TXT values are seen at the
      shared name.

- [x] 2.3 `backend/acceptance/lifecycle_test.go`: assert `/present` and `/cleanup` were **both**
      called during the issuance — the api container logs `challenge_publish` and
      `challenge_withdraw`, which `issue.sh` already greps, and the metrics endpoint counts them —
      and that afterwards nothing remains answerable: no challenge rows in the store, and a query
      for the alias returns `NOERROR` with an empty answer rather than `NXDOMAIN`. The
      empty-versus-nonexistent distinction is D7 and is the half of this that a naive
      implementation gets wrong.
      **Check:** the test fails if the alias answers `NXDOMAIN` after a completed issuance.

- [x] 2.4 `backend/acceptance/authz_test.go`: two refusals.
      (a) A valid API key belonging to a **different** load balancer cannot publish a challenge for
      `demo.example.com`, and nothing becomes answerable afterwards.
      (b) A key restricted to a CIDR is enforced: refused from a disallowed address, and still
      refused when the request carries a forged `X-Forwarded-For` naming an allowed one.
      **This is the one task needing a fixture `seed.sh` does not create** — there is exactly one
      load balancer (`demo/prod`) and one unrestricted key in the seeded stack. The test creates
      the second load balancer and both keys itself, through the admin API with the `admin-user`
      session from 1.3, and cleans up after itself; `seed.sh` is a human-facing fixture for the dev
      loop and should not grow test-only clutter (design A3). Calibrate (b) against what the API
      actually observes rather than against an assumed address: requests from the host reach the
      challenge API through the dev Traefik overlay at `172.28.0.40`, so the peer address is the
      overlay's, not the host's. Assert **both** directions — a CIDR containing the observed peer
      is accepted, one excluding it is refused — so the test cannot pass merely because everything
      is refused.
      **Check:** each refusal asserts the specific status the API returns, and (a) additionally
      queries DNS to confirm no value appeared.

- [x] 2.5 `backend/acceptance/restart_test.go`: restart the `api` container between the moment a
      challenge is published and the moment it is validated, and assert the certificate is still
      issued. This is the durability claim (D6) and the reason challenges live in PostgreSQL rather
      than in memory at all.
      **Check:** the test asserts the api container's start time moved *and* that issuance
      completed — without the first assertion it silently degrades into 2.1.

- [x] 2.6 `backend/acceptance/plan_test.go`: run `acmeservice adminctl loadbalancer dns demo/prod`
      inside the api container and compare it, record for record, against the checked-in zone
      `development/dns/zones/example.com.zone` that CoreDNS is serving. Scope the comparison to the
      `_acme-challenge` alias records and make it exact in both directions: every challenge record
      the plan emits must exist in the zone and vice versa — for the seeded inventory that is the
      single `_acme-challenge.demo.example.com CNAME demo-example-com.dev.acmeservice.internal.`,
      the one record issuance actually validated against. The zone's **address** records are
      deliberately not comparable and the zone file says so in a comment: it points
      `demo`/`*.demo` at the `traefik-lb` container (`172.28.0.30`) while the plan prints the
      seeded node addresses (203.0.113.10/.11, TEST-NET-3 and unroutable on purpose). Asserting
      those equal would assert a falsehood; the test must state that exclusion in a comment rather
      than quietly diffing a subset.
      **Check:** the test fails if the alias label in the zone file is edited to anything other
      than what the plan derives.

- [x] 2.7 `backend/acceptance/identity_test.go`, using the client from 1.3: for each of the four
      seeded users — `admin-user`, `config-admin-user`, `key-rotation-user`, `read-only-user`, all
      `devpass` — `GET /api/v1/me` reports that user's expected groups (`/acme-admin`,
      `/acme-config-admin`, `/acme-key-rotation`, `/acme-read-only`) and the role its group maps
      to. Then the enforcement, through the management API: `read-only-user` is refused (403)
      creating a domain; `config-admin-user` succeeds at the identical call; `key-rotation-user` is
      refused creating a domain but **succeeds** creating an API key. That last pair is what
      actually pins the `CONFIG_ADMIN`/`KEY_ROTATION` boundary — the other two would still pass
      under a naive "any role may write" implementation. Table-drive it over the four users; clean
      up anything created.
      **Check:** all four users are covered by name and the key-rotation pair is asserted in both
      directions.

- [x] 2.8 `backend/acceptance/interactions_test.go`: after the issuance forced in 2.1, assert the
      durable interaction history it left behind. Sign in as `admin-user` (1.3) and read
      `GET /api/v1/loadbalancers/{demo/prod}/interactions`: exactly **two** interactions for the
      shared alias `demo-example-com` — one per authorization, apex and wildcard — both with a
      non-zero query count credited from the **single** TXT query that answered both values, and
      both ending with outcome `validated`. This is the acceptance half of the overlap-attribution
      rule (`specs/challenge-history/spec.md`, *"One query serves several concurrent
      authorizations"*), and the unit tests cannot reach it: they exercise the SQL overlap predicate
      against hand-written rows, not a real lego client publishing two values at one name and a real
      resolver querying it once. Assert the count is credited to **both** rows rather than summing
      them — a naive implementation that credits only the most recent interaction passes any
      assertion phrased as a total. Moved here from
      `rebuild-console-and-add-interaction-history` task 10.3, which owned the assertion but not the
      harness.
      **Check:** the test fails if either interaction has a zero query count, and fails if the
      number of interactions for the alias is anything other than two.

## 3. The local gate, and taking the suite out of CI <!-- complexity: sonnet | touches: scripts/test-acceptance.sh, .gitlab-ci.yml, README.md -->

- [x] 3.1 `scripts/test-acceptance.sh`: run the tagged suite against the development stack —
      `cd backend && go test -tags acceptance ./acceptance/...`, with the stack lifecycle owned by
      `TestMain` (1.1). Keep the existing `need go`, `need docker` and daemon-reachability guards,
      and pass `"$@"` through so `-run` and `-v` reach `go test`. Remove the
      `[ -d backend/acceptance ] || die "backend/acceptance does not exist yet (task group 23)"`
      guard along with its stale `group 23` pointer — the directory exists after group 1, and
      "group 23" names a task group that exists nowhere. Update the header comment to cite
      `add-acceptance-environment` and D14, and to say in one line that this script is the gate: it
      is run by hand, not by the pipeline (see 3.2).
      **Check:** `scripts/test-acceptance.sh` exits non-zero when any assertion in group 2 fails
      and zero when they pass; `grep -rn "group 23" scripts/` returns nothing.

- [x] 3.2 `.gitlab-ci.yml`: **remove** the `acceptance` stage from the `stages:` list and the
      `acceptance:` job entirely, leaving `lint → test → build → publish`. CI's job for now is to
      lint, unit-test, build the image and push it on a tag — nothing more. Say so in a comment
      where the job used to be, in the same plain terms as the existing D13 comment on `test-go`
      (which already explains why the integration tests are skipped in CI): **the acceptance suite
      exists and is runnable — `./scripts/test-acceptance.sh` — but is a local gate on purpose.**
      It needs a Docker daemon and a full stack boot (`Dockerfile.api` built, `npm ci` for the web
      container, Keycloak up, a real ACME exchange), which is minutes per pipeline for a signal
      nobody is currently asking merges to wait on. This is a decision, not an oversight; wiring it
      back in is a deliberate later choice (design A10). Do not re-add the job on the assumption
      that its absence is a mistake.
      **Check:** the pipeline for the commit that lands this runs exactly four stages and is green,
      and `grep -n acceptance .gitlab-ci.yml` returns only the explanatory comment.

- [x] 3.3 `README.md`, two paragraphs.
      (a) The acceptance suite: what it asserts, that it runs against the development stack, that
      it is run with `./scripts/test-acceptance.sh` (roughly: a full reset plus several real
      issuances, so minutes not seconds), that `ACCEPTANCE_KEEP_STACK=1` reuses a running stack,
      and that CI does not run it. This paragraph is the only thing standing between the suite and
      being forgotten — it is the discoverability half of design A10.
      (b) A rollout check for any load balancer running Traefik 2.11 — the supported floor (D2),
      which neither the dev stack (`traefik:v3.1` for `traefik-lb`) nor anything else exercises
      (D14) — namely to confirm its bundled lego with the `go.mod` one-liner from D2, and to watch
      one full present → validate → cleanup cycle on that load balancer before moving the rest of
      its domains.
      **Check:** (a) names the script, the cost and the CI exclusion; (b) names the version, the
      one-liner and the cycle to watch.
