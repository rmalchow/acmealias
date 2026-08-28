# Tasks

> **Read N2 first.** The alternative to this change is four separate projects — a cert-manager
> webhook, a Caddy libdns module, an acme.sh plugin, a Posh-ACME plugin — each with its own release
> cycle. This is one route.

## 1. The endpoint

- [ ] 1.1 Add `POST /update` beside `/present` in `internal/httpapi/challenge.go`, accepting `{"subdomain", "txt"}`
- [ ] 1.2 Make it a THIN TRANSLATION onto the existing publish handler, not a parallel implementation. If it ever needs logic of its own, that is the signal the two dialects have diverged (design N1)
- [ ] 1.3 Answer `200 {"txt": "<value>"}`, which is what the protocol specifies and what clients check
- [ ] 1.4 Resolve the alias by LABEL, the way `/present` resolves it by challenge name, and land both on the same authorization check (design N6)

## 2. Authentication

- [ ] 2.1 Accept `X-Api-User` and `X-Api-Key`, mapping them onto the key identifier and secret
- [ ] 2.2 Change NOTHING about the grant model underneath. The key reaches exactly the alias its grants name, and the network allowlist and expiry apply identically (design N5)
- [ ] 2.3 Test the property N6 names: a key that cannot publish for an alias by challenge name cannot publish for it by label either. Two spellings must not be two authorization surfaces

## 3. Registration, refused deliberately

- [ ] 3.1 Make `POST /register` REFUSE rather than 404. Clients will call it, and a 404 reads as "wrong URL" where the truth is "not how you get credentials here" (design N3)
- [ ] 3.2 Name the console in the refusal
- [ ] 3.3 Do NOT implement it behind API-key authentication either. Clients that call `/register` call it unauthenticated, so an authenticated one is a route that exists only to be able to say it exists (design N3)
- [ ] 3.4 Assert by test that no unauthenticated request in any publication protocol creates an account, a domain or an alias

## 4. The credentials file

- [ ] 4.1 Produce the JSON object clients read — `username`, `password`, `fulldomain`, `subdomain` — from the console, for one domain. The key it carries is a **PROJECT key** (`api-key-kinds`, now implemented): bound to the domain's project by a foreign key, holding that project's challenge answering and nothing else, and deleted with the project. That is the whole reason `api-key-kinds` sits before this change — a credentials file handing out a key assembled by hand from the grant grammar is the file somebody pastes into a Kubernetes Secret and never looks at again. Offer the address limit switch beside it where the project has nodes
- [ ] 4.2 Follow the show-once rule for the secret it contains, as the API key screen already does. See the open question
- [ ] 4.3 Make the format exactly what cert-manager's `accountSecretRef` expects, since that is the client with the least tolerance for a near-miss: a Kubernetes Secret is not somewhere a person notices a typo

## 5. What has no cleanup

- [ ] 5.1 Confirm a value published through `/update` is withdrawn by `ChallengeLifetime` and the sweeper, with no client signal (design N4)
- [ ] 5.2 Make sure the interaction view does not report such a publication as anomalous. A client that never withdraws is behaving exactly as its protocol specifies, and an outcome that reads as a fault would train somebody to ignore the column
- [ ] 5.3 Record in `ChallengeLifetime`'s comment that it is now a MECHANISM and not only a backstop, so somebody lowering it later understands they may be withdrawing a value a slow certificate authority has not yet asked for

## 6. Proving it against real clients

- [ ] 6.1 Drive the dev stack's Traefik through the acme-dns provider as well as `httpreq`, so both dialects issue a real certificate against pebble
- [ ] 6.2 Add a cert-manager or acme.sh exercise if it can be done without a Kubernetes cluster in the stack; if not, say so rather than claiming coverage
- [ ] 6.3 Check whether the two-TXT history acme-dns keeps is actually needed. It exists there for a wildcard and its base name validating at once, and our aliases already share one challenge name between a wildcard and its apex (design D6) — but verify against a client rather than reasoning about it

## 7. Saying so

- [ ] 7.1 **The marketing site ALREADY lists these clients as working.** `site/src/index.html`'s client strip names Traefik, cert-manager, certbot, acme.sh, lego, Caddy, win-acme, Posh-ACME and Certify The Web under "Works with" — seven of which need this change. That was written deliberately and ahead of the code, on the strength of this change sitting at 11, before the first customers. THIS TASK IS WHAT MAKES THE PAGE TRUE. Verify each name against a real client, and correct the page for any that does not work rather than leaving the claim standing
- [ ] 7.1a If this change slips past the first customers in the sequence, split that strip back into what works and what is coming BEFORE they arrive
- [ ] 7.2 Document both dialects, and be explicit that acme-dns clients need no `/register`
- [ ] 7.3 Update the OpenAPI document
