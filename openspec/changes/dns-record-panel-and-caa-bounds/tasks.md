<!--
  EVERY BOX IS TICKED, and the change was written after the work rather than before it.

  The implementation landed first, driven by a conversation rather than by this list. Writing the
  tasks now is not bookkeeping theatre: the list is what the specs and the design get checked
  against, and reconstructing it surfaced two requirements the code had quietly contradicted —
  service-inventory's "CAA verification distinguishes absent from conflicting" and admin-console's
  "No CAA record is not a warning", both of which asserted the opposite of what now ships.

  Where a task was verified by a test, the test is named. Where it was not, it says so.
-->

## 1. The CAA verdict

- [x] 1.1 Add `CAAUnconstrained` to `resolvercheck.CAAStatus` and rewrite the doc comment to say why
      absence is now the worst outcome rather than the best (`internal/resolvercheck/caa.go`)
- [x] 1.2 Parse RFC 8657 parameters: `hasAccountURI` walks the `;`-separated tail after the authority
      identifier, so an authority merely spelled like the parameter constrains nothing
- [x] 1.3 `pinned` requires an account on EVERY authorization, and reports an empty tag list as not
      pinned — the case that reads backwards next to `authorizes`, which reports it as permitting
- [x] 1.4 Populate `Found` in every state that has published data, not only `CAABlocking`, so the
      console has something to print in its second column
- [x] 1.5 Populate `Found` on a CORRECT `CheckCNAME` too, for the same reason
      (`internal/resolvercheck/check.go`)
- [x] 1.6 Extend `TestCheckCAA` with pinned, half-pinned, empty-parameter and
      authority-named-like-a-parameter fixtures, and assert `Found` is never empty where there is
      rdata

## 2. The record we recommend

- [x] 2.1 Render `accounturi=YOUR-ACME-ACCOUNT-URL` on every line, behind an exported
      `inventory.AccountURIPlaceholder`, and say in the comment why a plausible URL would be worse
      than a placeholder
- [x] 2.2 Export `inventory.CAALines` and DELETE the second copy in `internal/httpapi`, which its own
      `ponytail:` comment had already flagged — two renderers would now print two different records
- [x] 2.3 Add `caaName` to the domain view, since rdata with no owner name is not a publishable
      record and the console cannot derive it without the public suffix list
- [x] 2.4 Add `CAALines` to the `TestEveryOperationAuthorizes` exempt list with its reason: pure
      formatting, touches no object
- [x] 2.5 Update `TestExpectationsShareOneDelegation` and the API-level assertion in `admin_test.go`,
      the second of which only runs with a database and so had been passing by skipping

## 3. Checking on demand, per record type

- [x] 3.1 Add `?records=delegation|caa` to the verification route, filtering the QUESTIONS in
      `groupForChecks` rather than the answers — an unrecognised value is ignored rather than refused
- [x] 3.2 Leave the un-asked record type in the response reading `not-checked`, so a partial answer
      can be merged rather than replacing what is on screen
- [x] 3.3 Cover both in tests: `TestGroupForChecksAsksOnlyWhatWasRequested` and
      `TestDomainChecksKeepsRowsForRecordsNotAsked`
- [x] 3.4 Stop the automatic pass in `ngOnChanges`, and say in the comment that this reverses design
      D1 and why the argument for D1 no longer holds
- [x] 3.5 Stop re-checking after a domain is added or deleted — rows are keyed by name, so a new
      domain simply has no answer
- [x] 3.6 Simplify `runCheck`: every run is a press now, so every run spins its button and every
      failure earns a banner

## 4. The DNS records panel

- [x] 4.1 New `DnsRecordsPanelComponent`: two tables, delegation grouped by challenge name and CAA by
      registrable name, rendered from the project read with no lookup
- [x] 4.2 Print whole zone-file lines — owner, TTL, type, rdata — with the trailing dot on the CNAME
      target, and list the domains each row covers
- [x] 4.3 Treat `not-checked` as the absence of an answer, which is what lets each table report
      "checked" independently and collapses correctly onto a deployment with no resolver
- [x] 4.4 `mergeChecks` in the parent: a kinded pass replaces only its own half per domain
- [x] 4.5 Regrade `caaChip` — red / red / amber / green — and add `unconstrained` to `CAAStatus`
- [x] 4.6 Update `domain-status.spec.ts`, which asserted the old verdict in two tests
- [x] 4.7 Spec the panel: records before any check, one row for a wildcard and its base, the
      placeholder, the colours, no CAA table without a configured issuer, no delegation row for a
      domain whose alias is missing
- [x] 4.8 Compact the sections: headings at the panel heading's own size, explanations behind a help
      hint, columns in twelfths at 3 / 6 / 3

## 5. Alias labels

- [x] 5.1 `GenerateLabel` draws 6 bytes and encodes with padding SUPPRESSED — `StdEncoding` would
      append `=`, which is not legal in a DNS label
- [x] 5.2 Assert the length and the absence of padding in `TestGenerateLabelIsValidAndUnpredictable`
- [x] 5.3 `store.LabelTaken` matches the constraint name, so the challenge-name conflict is not
      retried into a loop; covered by `TestLabelTaken`
- [x] 5.4 Retry the insert in `CreateDomain` through `resolveLabel`, which covers both label modes,
      clearing the ids the rolled-back attempt minted
- [x] 5.5 NOT VERIFIED BY A TEST: the concurrent insert itself. `TestLabelTaken` covers the predicate
      and the retry path is exercised only by inspection — a real race needs two transactions against
      a database, which is an integration test on the model of
      `TestIntegrationPersonalAccountRace`. Worth adding; not added here.

## 6. Collapsible info boxes

- [x] 6.1 New shared `InfoBoxComponent`: heading is the control, body arrives as a `TemplateRef` so a
      collapsed box instantiates nothing, `aria-expanded` on the toggle
- [x] 6.2 Persist only the CLOSED boxes under `acme.infobox.<key>`, every access wrapped because
      reading storage throws rather than returning null in some contexts
- [x] 6.3 Convert all six info boxes across both consoles; the checkout dialog's "Payment is not
      live" warning is deliberately NOT converted, because its own comment requires it to be read
      before the form
- [x] 6.4 Spec it, including the storage-throws case and per-key independence

## 7. Saying it out loud

- [x] 7.1 Rewrite step 4 of the marketing site's setup sequence to state what publishing the
      delegation hands us, and print the record that bounds it
- [x] 7.2 Correct the two places that claimed the delegation was the only DNS change a customer ever
      makes, which step 4 has made false
- [x] 7.3 Name us explicitly in the console's account-URL info box: a complete record here is what
      stops US obtaining certificates for these names
- [x] 7.4 Give the box a worked `jq` command for Traefik, verified against a real `acme.json`

## 8. The gate

- [x] 8.1 `go test ./...` green
- [x] 8.2 Frontend suites green
- [x] 8.3 `./scripts/test-acceptance.sh -count=1` green
- [x] 8.4 Update the two specs the implementation contradicted, in `admin-console` and
      `service-inventory`, rather than leaving the repository asserting the opposite of what ships

## 9. Found on the way, fixed here

- [x] 9.1 THE SHARED LIBRARY'S TESTS NEVER RAN. `projects/shared` had no test target, so its five
      spec files were compiled for types and never executed. Proven with a deliberately failing
      assertion that passed. Added a `shared` project to `angular.json` with a `tsconfig.spec.json`;
      45 previously dead tests now run.
- [x] 9.2 One of them was stale and failing: `auth.interceptor.spec.ts` asserted that a 401 with no
      session navigates to sign-in, which the interceptor deliberately stopped doing. Updated to
      assert the intended behaviour, which the interceptor's own comment explains at length.
- [x] 9.3 `development/scripts/status.sh` reported "schema not migrated yet" against a fully migrated
      database, because its counts query still selected from `load_balancers` — a table the
      `collapse-project-and-loadbalancer` change removed.
- [x] 9.4 The dev stack configured no `caa_issuers`, so the entire CAA half of this panel was
      invisible to anybody working on it locally. Set to `letsencrypt.org`, which is what production
      uses, so a dev console prints the record production prints.
