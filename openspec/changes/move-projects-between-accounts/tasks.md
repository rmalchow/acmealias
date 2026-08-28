# Tasks

> **The console already promises this**, in the future tense, on the screen somebody reads
> immediately before paying (`upgrade.component.ts`). Task 5.1 is what lets that sentence become
> present tense; until then it is a promise with a date attached to nothing.

## 1. The move itself

- [ ] 1.1 Add the store operation: one transaction, rewriting the materialized `account_id` on the project and on every row beneath it — `lb_nodes`, `aliases`, `domains`, and the project's keys (design M4)
- [ ] 1.2 Use `store.CrossAccount` for the WRITE ONLY. Authority is established first, under ordinary scopes; an escape that also decides who may move what is a hole in the shape of a feature (design M1)
- [ ] 1.3 ASSERT the end state inside the transaction: no row beneath the project may name the old account. RLS EMPTIES rather than fails, so an update run under the wrong scope matches rows until their account changes and then silently matches nothing — a half-moved project has its domains in one account and its nodes in another, with no error anywhere (design M4)
- [ ] 1.4 Do NOT change any alias label. The customer's published CNAME points at it, and re-labelling turns a move into work they must do, plus a window where nothing renews (design M3, `domain-activation` A3)
- [ ] 1.5 Note in `resolveLabel` that this change depends on alias labels being unique per DEPLOYMENT rather than per account. If that ever becomes per-account, the move breaks, and that comment is where somebody will find out

## 2. Authority on both sides

- [ ] 2.1 Require the right to remove the project from the source AND to create it in the destination (design M2)
- [ ] 2.2 Make the refusal NAME which side was lacking. There are two administrators here, and "you may not do that" sends somebody to argue with the wrong one
- [ ] 2.3 Test both asymmetric failures: source-only authority pushes an unsolicited liability into a stranger's account, since a team account pays per challenge; destination-only authority takes configuration out of an account the caller has no standing in
- [ ] 2.4 Apply the destination's project quota to what arrives, or the move is a way around it (design M5)
- [ ] 2.5 Do NOT give the source a reserved slot or any other special state. It simply has one fewer project

## 3. Audit

- [ ] 3.1 Record the move in BOTH accounts. One entry in one account is readable by exactly the people who did not need telling (design M6)
- [ ] 3.2 Name the same move, project and person in both, so either side can be joined to the other

## 4. The console

- [ ] 4.1 Add the control on the project, with a destination picker listing only accounts the person may act in
- [ ] 4.2 Say what will NOT happen — no DNS change, no interruption — because that is the fear the control provokes
- [ ] 4.3 Say what WILL: a domain that was free in a personal account starts costing in a team one, from the moment it moves

## 5. Closing the promise

- [ ] 5.1 Rewrite the note on `upgrade.component.ts` into the present tense. It currently says moving "is not built yet", deliberately, because that screen is where somebody spends money on the strength of what it says
- [ ] 5.2 Check the same sentence has not been copied to the marketing site while this was queued
