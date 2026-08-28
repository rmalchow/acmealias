# Tasks

> **Read N2 first.** The alternative to this change is four separate projects — a cert-manager
> webhook, a Caddy libdns module, an acme.sh plugin, a Posh-ACME plugin — each with its own release
> cycle. This is one route.

## 1. The endpoint

- [x] 1.1 Add `POST /update` beside `/present` in `internal/httpapi/challenge.go`, accepting `{"subdomain", "txt"}`
- [x] 1.2 Make it a THIN TRANSLATION onto the existing publish handler, not a parallel implementation. If it ever needs logic of its own, that is the signal the two dialects have diverged (design N1)
- [x] 1.3 Answer `200 {"txt": "<value>"}`, which is what the protocol specifies and what clients check
- [x] 1.4 Resolve the alias by LABEL, the way `/present` resolves it by challenge name, and land both on the same authorization check (design N6)

## 2. Authentication

- [x] 2.1 Accept `X-Api-User` and `X-Api-Key`, mapping them onto the key identifier and secret
- [x] 2.2 Change NOTHING about the grant model underneath. The key reaches exactly the alias its grants name, and the network allowlist and expiry apply identically (design N5)
- [x] 2.3 Test the property N6 names: a key that cannot publish for an alias by challenge name cannot publish for it by label either. Two spellings must not be two authorization surfaces

## 3. Registration, refused deliberately

- [x] 3.1 Make `POST /register` REFUSE rather than 404. Clients will call it, and a 404 reads as "wrong URL" where the truth is "not how you get credentials here" (design N3)
- [x] 3.2 Name the console in the refusal
- [x] 3.3 Do NOT implement it behind API-key authentication either. Clients that call `/register` call it unauthenticated, so an authenticated one is a route that exists only to be able to say it exists (design N3)
- [x] 3.4 Assert by test that no unauthenticated request in any publication protocol creates an account, a domain or an alias

## 4. The credentials file

- [x] 4.1 Produce the JSON object clients read — `username`, `password`, `fulldomain`, `subdomain` — from the console, for one domain. The key it carries is a **PROJECT key** (`api-key-kinds`, now implemented): bound to the domain's project by a foreign key, holding that project's challenge answering and nothing else, and deleted with the project. That is the whole reason `api-key-kinds` sits before this change — a credentials file handing out a key assembled by hand from the grant grammar is the file somebody pastes into a Kubernetes Secret and never looks at again. Offer the address limit switch beside it where the project has nodes
- [x] 4.2 Follow the show-once rule for the secret it contains, as the API key screen already does. See the open question
  - The OPEN QUESTION is answered by doing both: the file is shown once, in a modal with no Esc, no mask close and no corner X, released only by "I have stored these credentials somewhere safe" — and it is offered as text to copy rather than as a download, because a browser download of a secret lands in a Downloads folder nobody empties.
- [x] 4.3 Make the format exactly what cert-manager's `accountSecretRef` expects, since that is the client with the least tolerance for a near-miss: a Kubernetes Secret is not somewhere a person notices a typo
  - THERE IS NO SINGLE FORMAT, which the verification in 7.1 turned up and which this task assumed away. cert-manager, lego, Traefik and certbot-dns-acmedns read a JSON object keyed by DOMAIN; win-acme and Certify The Web read the four fields FLAT, in a file named for the domain; Caddy reads flat for one account; Posh-ACME reads a positional FOUR-ELEMENT ARRAY — subdomain, username, password, fulldomain — keyed by `_acme-challenge.<domain>`. The API answers with the four fields plus the domain, and the modal emits all three shapes behind a picker. Emitting one and calling it "the credentials file" would have been right for one client and silently wrong for the rest.

## 5. What has no cleanup

- [x] 5.1 Confirm a value published through `/update` is withdrawn by `ChallengeLifetime` and the sweeper, with no client signal (design N4)
- [x] 5.2 Make sure the interaction view does not report such a publication as anomalous. A client that never withdraws is behaving exactly as its protocol specifies, and an outcome that reads as a fault would train somebody to ignore the column
- [x] 5.3 Record in `ChallengeLifetime`'s comment that it is now a MECHANISM and not only a backstop, so somebody lowering it later understands they may be withdrawing a value a slow certificate authority has not yet asked for

## 6. Proving it against real clients

- [x] 6.1 Drive the dev stack's Traefik through the acme-dns provider as well as `httpreq`, so both dialects issue a real certificate against pebble
  - DONE AND RUN. traefik-lb now carries two resolvers in one process — `pebble` over httpreq for `demo.example.com` + `*.demo.example.com`, and `pebble-acmedns` over acme-dns for a new `alt.example.com` — from ONE API key and one challenge API. Both issued against pebble and both verified against pebble's own chain; the log shows `fqdn_form: acme-dns` publishing and, correctly, never withdrawing. `issue.sh` now fails if the acme-dns half did not run, so a regression cannot pass as a green run.
- [x] 6.2 Add a cert-manager or acme.sh exercise if it can be done without a Kubernetes cluster in the stack; if not, say so rather than claiming coverage
  - acme.sh CAN be done without a cluster and now is: `issue.sh` runs `neilpang/acme.sh --issue --dns dns_acmedns` as a throwaway container on the stack's own network, and it issued a real certificate. It is worth more than a second lego test, because acme.sh shares no code with lego AND it is the only client that GREPS the response body for the quoted value — a server answering 200 with an empty body would pass every other client and fail this one. **cert-manager genuinely cannot be exercised here**: its solver is an in-cluster controller. What stands in for it is that it reads the same domain-keyed credentials file lego does, which the traefik half exercises, and that its code never calls `/register` at all (verified against `pkg/issuer/acme/dns/acmedns/acmedns.go`).
- [x] 6.3 Check whether the two-TXT history acme-dns keeps is actually needed. It exists there for a wildcard and its base name validating at once, and our aliases already share one challenge name between a wildcard and its apex (design D6) — but verify against a client rather than reasoning about it
  - NOT NEEDED, and verified against source rather than reasoned about. acme-dns keeps exactly two values (`SELECT ... LIMIT 2`, and each update overwrites the older of two pre-seeded rows) for the wildcard-plus-apex case; our aliases already share one challenge name between those two, and we keep every distinct unexpired value, which is a SUPERSET. No client depends on the eviction: lego, cert-manager and libdns all implement cleanup as an explicit no-op, and nothing reads the record set back. Two things to keep an eye on, recorded rather than fixed: an unbounded set could grow the TXT RRset until answers truncate, and expiry is entirely ours because no acme-dns client ever withdraws.

## 7. Saying so

- [x] 7.1 **The marketing site ALREADY lists these clients as working.** `site/src/index.html`'s client strip names Traefik, cert-manager, certbot, acme.sh, lego, Caddy, win-acme, Posh-ACME and Certify The Web under "Works with" — seven of which need this change. That was written deliberately and ahead of the code, on the strength of this change sitting at 11, before the first customers. THIS TASK IS WHAT MAKES THE PAGE TRUE. Verify each name against a real client, and correct the page for any that does not work rather than leaving the claim standing
  - VERIFIED CLIENT BY CLIENT, against each project's own source rather than its marketing, and **two of the nine did not survive unqualified**. `certbot` ships no acme-dns authenticator at all — it needs a third-party plugin, and only `certbot-dns-acmedns` refrains from calling `/register`; the panel had promised "the console gives you the script", a script we do not ship. `Caddy` has no acmedns module in the stock binary; it is an xcaddy build of `caddy-dns/acmedns`. Both are now stated on the page. The other seven are built in.
  - AND ONE THING TRUE OF ALL NINE that the page did not say: they work only because the credentials file is placed BEFORE the first run. Left alone, lego, Traefik, acme.sh, win-acme, Posh-ACME and Certify The Web all try to register when they find no credentials, and this service refuses that deliberately. That is now the second lede of the section, above the tabs, rather than buried in one of them.
  - Two response-shape constraints found in client source and now recorded on the page and in the code: acme.sh GREPS the body for the quoted value (an empty 200 breaks it and nothing else), and libdns — Caddy — requires status exactly 200, not 201 or 204.
- [x] 7.1a If this change slips past the first customers in the sequence, split that strip back into what works and what is coming BEFORE they arrive
  - Not applicable: it did not slip. The strip is now accurate rather than split.
- [x] 7.2 Document both dialects, and be explicit that acme-dns clients need no `/register`
- [x] 7.3 Update the OpenAPI document

## What this change did not anticipate

- [x] **`POST /register` and `POST /update` had to be unprefixed.** The design left the paths
  implicit and `/acme/httpreq` set a precedent for a namespace. There is no namespace available:
  cert-manager's `acmeDNS` solver, acme.sh, Posh-ACME, win-acme and lego's provider all take a
  HOST and append `/update` themselves, with no base-path setting anywhere. A prefixed route would
  have been a route no client can reach.
- [x] **`X-Api-Key` is the same header this service already used, meaning something else.** acme-dns
  sends `X-Api-User: <username>` and `X-Api-Key: <password>` — two headers, each half of a pair —
  while our own third transport sends `X-API-Key: <identifier>.<secret>`, one header carrying both.
  HTTP header names are case-insensitive, so those are the SAME HEADER. The presence of
  `X-Api-User` is what tells them apart, unambiguously: nothing else sends it, and every existing
  caller keeps working byte for byte.
- [x] **The interaction history would have called every correct acme-dns renewal abandoned.** The
  sweep closes a publication that was never withdrawn and marks it `abandoned`, which is right for
  a client that vanished and exactly wrong for a protocol with no withdrawal operation. The
  distinction is now made once, in `store.AbandonStaleInteractions`, in the same statement that
  closes the row — so both places that derive an outcome from the flag keep working unchanged, and
  an acme-dns row reads as `validated` when it was queried and `unqueried` when it was not.
- [x] **A previous change broke the development seed, and this change found it.** `api-key-kinds`
  renamed the CLI's "created api key" to "created project api key"; `seed.sh` parses the identifier
  out of that line with sed, because the secret is printed exactly once. The parse silently
  produced nothing, the seed died having minted a credential nobody could ever use, and the stack
  came up with an empty key file. The human fixed the parser to match on the quoted identifier
  instead — the durable half — and `adminctl/key.go` now says above that line that something parses
  it.
