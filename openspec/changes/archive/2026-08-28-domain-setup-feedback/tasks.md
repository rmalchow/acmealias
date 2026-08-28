# Tasks

## 1. The check, per domain

- [x] 1.1 Shape the verification result per domain rather than per plan record, so a row can be answered independently — one alias is issued per challenge name, and a wildcard shares one with its apex, so a single verdict for the load balancer would be wrong for every domain but the first that failed
- [x] 1.2 Report each record type separately for a domain: the delegation, and CAA. Do NOT reduce them to one verdict (D2)
- [x] 1.3 Keep the route a read that stores nothing. It is a `GET` without CSRF for that reason, and running it automatically changes when it happens, not what it does (D1)
- [x] 1.4 Answer `not checked` rather than `503` when no resolver is configured, so a deployment that declined the feature is not told it is broken (D4)

## 2. The console

- [x] 2.1 Run the check when the load balancer screen opens, per domain, without anybody pressing anything (D1)
- [x] 2.2 Replace the single tag per row with one chip per record type, stating what is true rather than whether something exists
- [x] 2.3 CAA chips say what CAA SAYS: authorises us / no restriction / **blocks us** / could not check. Never "not configured" — `not-published` is the commonest state and is entirely correct, and a warning almost everyone sees is one everybody learns to ignore (D2)
- [x] 2.4 Render FIVE delegation states, not three: **not checked**, **not configured**, **configured incorrectly**, **configured correctly**, **could not check** (D2). The checker already distinguishes all of them; the console is what collapses them
- [x] 2.5 Never merge "not configured" with "configured incorrectly". Their fixes have nothing in common: one sends somebody to add a record, the other to a record they already published and therefore believe is done (D2)
- [x] 2.5a "Not checked" must never read as a fault in the customer's setup. We are the ones who did not look (D4)
- [x] 2.6 A help control beside the chips opens a modal (D5)
- [x] 2.7 The modal carries, per state: what it means in one sentence, and the exact record to publish WITH THIS DOMAIN'S ALIAS rather than a placeholder. The general instructions are in `docs/`; what is missing at the moment of confusion is the customer's own value (D5)
- [x] 2.8 For `blocking`, the modal explains authorising an issuer instead — the fix is a CAA change, not a delegation one, and offering the delegation record here would send them to the wrong file (D5)
- [x] 2.8a For **configured incorrectly**, the modal shows WHAT WAS FOUND alongside what it should be, and names the likely cause: a domain deleted and re-added gets a NEW alias, so a delegation left pointing at the old one is published, well-formed, and will never validate. No generic "check your DNS" message leads anybody there (D2, D5)
- [x] 2.8b For **could not check**, say it is ours or the resolver's problem, not theirs (D5)
- [x] 2.9 Keep the manual "Run verification" control for the plan as a whole. It answers a different question — the whole plan including addresses — and re-running on demand is still worth having
- [x] 2.10 A slow or failed lookup degrades its row and never the page (D1)

## 3. Proving it

- [x] 3.1 Test the per-domain mapping where a wildcard and its apex share one alias: both rows read from the one delegation result rather than one of them reading "absent"
- [x] 3.2 Test each CAA outcome renders its own sentence, and that `not-published` is NOT presented as a problem (D2)
- [x] 3.3 Test that an unconfigured resolver yields `not checked` on every row and no error banner (D4)
- [x] 3.4 Acceptance: a domain whose delegation is published reads as published, and one whose delegation is absent says so — against the fake internet, which already serves exactly this
