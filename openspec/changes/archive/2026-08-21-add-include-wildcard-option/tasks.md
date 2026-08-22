# Tasks

> Ordered so each step compiles and its check runs on its own. Step 1 puts the pairing in the
> inventory, where every entry point shares it; steps 2–3 expose it on the API and the CLI;
> step 4 puts the control in the console; step 5 updates what the documents say.

## 1. The inventory creates the pair

- [x] 1.1 `backend/internal/dnsname/wildcard.go`: add `Counterpart(name string) string` — the
  wildcard of a plain name, the base name of a wildcard (`example.com` ⇄ `*.example.com`,
  `shop.example.com` ⇄ `*.shop.example.com`). Derivation only; the caller validates the result
  (design B2, B3)
- [x] 1.2 `backend/internal/dnsname/wildcard_test.go`: both directions round-trip; the
  counterpart of `co.uk` is `*.co.uk` and is refused by `ValidateDomain`; a 252-octet name's
  counterpart is refused as over-length
- [x] Check: `cd backend && go test ./internal/dnsname/`
- [x] 1.3 `backend/internal/inventory/domain.go`: add an `includeCounterpart bool` parameter to
  `CreateDomain`. Split the existing pre-write checks (validate, duplicate name, alias
  ownership, coverage warnings) into a helper run once per name, run it for both names before
  any write, and write both domain rows and the shared alias in the one existing transaction.
  Return both domains from `DomainCreation` — a `Domains []*model.Domain` field, or a second
  field — so callers can audit each (design B1, B3)
- [x] 1.4 `backend/internal/inventory/domain_test.go`: `example.com` with the flag yields two
  domains, one alias, one challenge name; `*.example.com` with the flag yields the apex and no
  second wildcard; `co.uk` with the flag is refused naming `*.co.uk` and leaves the `domains`
  table empty; a counterpart whose alias belongs to another load balancer refuses the whole
  request; the flag off creates exactly one row
- [x] Check: `cd backend && go test ./internal/inventory/`

## 2. The management API accepts the flag

- [x] 2.1 `backend/internal/httpapi/admin_inventory.go`, `createDomain` (~:495–545): add
  `IncludeWildcard bool \`json:"includeWildcard"\`` to the request body — the decoder uses
  `DisallowUnknownFields`, so the field must be declared to be accepted — pass it to
  `inventory.CreateDomain`, and emit an `ActionDomainCreate` event per created domain plus the
  alias event when the alias is new. Report both domains in the response
- [x] 2.2 `backend/internal/httpapi/admin_inventory_test.go`: a create with
  `"includeWildcard": true` returns both domains and one alias label; the same request for
  `co.uk` returns a validation error naming `*.co.uk` and a subsequent list shows no domains; a
  create without the field creates one domain; the audit trail holds two domain-create entries
  for a successful pair
- [x] Check: `cd backend && go test ./internal/httpapi/`

## 3. The command line accepts the flag

- [x] 3.1 `backend/cmd/acmeservice/cli.go:185–190`: add `--include-wildcard` (default `false`)
  to `domain create`, and update the usage line at `:49`
- [x] 3.2 `backend/internal/adminctl/inventory.go:245–280`: take the flag in `DomainCreate`, pass
  it through, emit one `ActionDomainCreate` event per created domain, and print each created
  domain above the single CNAME record
- [x] Check: `cd backend && go build ./... && go test ./internal/adminctl/`

## 4. The console offers the control

- [x] 4.1 `frontend/src/app/features/models.ts`: add optional `includeWildcard?: boolean` to the
  domain-create request type; the domain-created response type carries the created domains
- [x] 4.2 `frontend/src/app/features/domains/domain-editor.component.ts`: add an unticked
  `nz-checkbox` below the name field whose label is computed from the typed name — "Also add the
  wildcard `*.example.com`" for a plain name, "Also add the apex `example.com`" for a wildcard —
  with a note that the wildcard covers every host one label under the domain and needs its own
  address records. Send `includeWildcard` only when it is on. Keep the existing warning-keeps-
  the-dialog-open behaviour (design B2, B4)
- [x] 4.3 `frontend/src/app/features/domains/domain-editor.component.spec.ts`: the checkbox is
  unticked on open; the label names `*.example.com` for `example.com` and `example.com` for
  `*.example.com`; submitting untouched posts `{name}` with no `includeWildcard`; ticking it
  posts `{name, includeWildcard: true}`; a 400 leaves the dialog open with the server's message
  and reports nothing created
- [x] Check: `cd frontend && npm test -- --include='**/domain-editor.component.spec.ts'`

## 5. The documents say the pair is one action

- [x] 5.1 `docs/load-balancer-setup.md:288–293`: replace "Add the apex as a second domain if it
  is served" with the one-action option, and note that the pair adds address records for the
  wildcard to the DNS plan the customer must publish (design B4 risk)
- [x] 5.2 `docs/console-guide.md` (~:89 table and ~:295 onboarding steps): the Add domain
  dialog's checkbox, what it adds, and that it is off by default and why
- [x] 5.3 `README.md:40–46` "Wildcard rules": one sentence that the apex and its wildcard can be
  added in one action and that it is opt-in
- [x] Check: `cd backend && go test ./... && cd ../frontend && npm test`
