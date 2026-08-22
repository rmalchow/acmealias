# Tasks

> Ordered so each step compiles and its check runs on its own. Steps 1–2 add the new labelling
> without using it; step 3 switches the inventory over; steps 4–6 remove the operator input from
> the API, the CLI and the console; step 7 updates what the documents say.

## 1. The setting

- [x] 1.1 `backend/internal/config/config.go`: add `AliasUseDomain bool` read from
  `ACME_ALIAS_USE_DOMAIN`, defaulting to `false`, with a comment stating that it governs only
  aliases created afterwards (design A2, A3)
- [x] 1.2 `backend/internal/config/config_test.go`: unset defaults to `false`; `true` parses as
  `true`
- [x] Check: `cd backend && go test ./internal/config/`

## 2. The generator

- [x] 2.1 `backend/internal/dnsname/label.go`: add `GenerateLabel() (string, error)` — 16
  characters of lowercase base32 from `crypto/rand`, 80 bits (design A1); leave `SuggestLabel`
  exactly as it is
- [x] 2.2 `backend/internal/dnsname/label_test.go`: a generated label passes `ValidateLabel`; 1000
  labels are all 16 octets and all distinct; two labels for the same challenge name differ (the
  test that a hash would fail, design A1)
- [x] Check: `cd backend && go test ./internal/dnsname/`

## 3. The inventory chooses the label

- [x] 3.1 `backend/internal/inventory/domain.go`: drop the `aliasLabel` parameter from
  `CreateDomain` and the `supplied` parameter and override branch from `resolveLabel`;
  `resolveLabel` takes the setting and either generates (retrying up to 5 times on collision,
  then failing with an error naming the exhaustion) or derives with today's numeric suffix
  (design A4); update the doc comments, including the statement that an existing alias is reused
  as it is (design A3)
- [x] 3.2 `backend/internal/inventory/domain_test.go`: with the setting off, a created label is not
  derivable from the domain; with it on, `demo.example.com` yields `demo-example-com`; a
  pre-seeded alias colliding with the derived label yields `demo-example-com-2`; adding
  `*.example.com` after `example.com` reuses the existing alias and its label under either setting
- [x] Check: `cd backend && go test ./internal/inventory/`

## 4. The management API stops accepting a label

- [x] 4.1 `backend/internal/httpapi/admin_inventory.go` (~:501–510): remove `AliasLabel` from the
  domain-create request body and from the `inventory.CreateDomain` call; leave `AliasLabel` on
  every read model (`DomainView` and the interaction and plan views) untouched (design A5)
- [x] 4.2 `backend/internal/httpapi/admin_inventory_test.go`: a create request carrying
  `aliasLabel` is refused as an unknown field; a create without one succeeds and the response
  reports the label the service chose
- [x] Check: `cd backend && go test ./internal/httpapi/`

## 5. The command line stops accepting a label

- [x] 5.1 `backend/cmd/acmeservice/cli.go:189–191`: remove the `--alias-label` flag
- [x] 5.2 `backend/internal/adminctl/inventory.go:245–253`: drop the `aliasLabel` parameter of
  `DomainCreate`
- [x] Check: `cd backend && go build ./... && go test ./internal/adminctl/`

## 6. The console stops offering a label

- [x] 6.1 `frontend/src/app/features/domains/domain-editor.component.ts:50–54`: remove the "Alias
  label (optional)" form item, the `aliasLabel` field and its use in the submitted payload; keep
  the label in the domain list and detail views
- [x] 6.2 `frontend/src/app/features/models.ts`: remove `aliasLabel` from the domain-create request
  type only; the read models keep theirs
- [x] 6.3 `frontend/src/app/features/domains/domain-editor.component.spec.ts`: the form has no alias
  input and the submitted payload carries only the name
- [x] Check: `cd frontend && npm test -- --include='**/domain-editor.component.spec.ts'`

## 7. The documents say what the service now does

- [x] 7.1 `README.md`: in the CNAME model section, state that the alias label is generated and
  unguessable by default, that `ACME_ALIAS_USE_DOMAIN=true` derives it from the domain instead,
  and that the choice applies to aliases created afterwards — so it is worth making before
  onboarding domains
- [x] 7.2 `docs/load-balancer-setup.html:238`: replace "An alias label is proposed automatically;
  accept it unless you have a naming convention…" with a statement that the service assigns the
  label and it appears in the DNS plan; update the sample CNAME to a generated label
- [x] 7.3 `docs/console-guide.html` (~:168): the alias paragraph gains a sentence that the label is
  assigned by the service and cannot be edited, and any screenshot or field list mentioning the
  alias input is corrected
- [x] Check: `cd backend && go test ./... && cd ../frontend && npm test` (the acceptance suite in
  `backend/acceptance/` pins `demo-example-com`; it must set `ACME_ALIAS_USE_DOMAIN=true` or stop
  pinning the label)
