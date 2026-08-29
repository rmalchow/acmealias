<!--
  Written after the work, like the change before it, and for the same reason: the exercise is what
  finds the drift. This pass turned up four contradicted requirements and one live compliance gap on
  the public site that nobody had noticed — recorded in the public-edition archive, not here.

  Every box is ticked. Where something was verified by a test, the test is named; where it was not,
  it says so.
-->

## 1. The switch

- [x] 1.1 Add `limits.monetized` / `ACME_MONETIZED`, default false, to the file schema and the runtime
      config, with the rejected alternative recorded on it
- [x] 1.2 Take it in `store.Usable` and `store.SuspensionReason` as a parameter, not a package
      variable, and thread it through the four call sites
- [x] 1.3 Cover both modes in `store.TestUsable`, including that a nil account is still unusable and
      that a staff suspension is deliberately not honoured unmonetized
- [x] 1.4 Ignore rather than refuse an amount in `createAccount`, since an older console still sends
      one; serve an empty list from `/checkout/amounts`
- [x] 1.5 Carry `monetized` on `/me`, because it is a fact about the deployment and the console needs
      it before an account is selected
- [x] 1.6 Take the flag in `store.CreateAccountWithOwner` — the floor whose premise is that zero means
      suspended — and in `adminctl account create`

## 2. The console, both ways

- [x] 2.1 Upgrade screen: Create rather than Buy now, no trolley, no Credits row, and the note's first
      clause conditional
- [x] 2.2 Checkout dialog: no amount, no not-live warning, and a title that says what the button does
- [x] 2.3 Settings: the whole Credits panel behind the flag, not the balance alone — a heading with a
      number and no way to change it is worse than no panel
- [x] 2.4 The shell's low-balance banner and the account picker's balance line
- [x] 2.5 Split the balance-warning test by mode, and assert that no `EUR` appears unmonetized

## 3. Roles

- [x] 3.1 `store.SetRole`, in place, with the duplicate reported as a conflict rather than a
      constraint violation
- [x] 3.2 Factor the owner and last-administrator guards into `guardLeavingUnrestricted`, shared with
      removal
- [x] 3.3 Refuse a change to one's own membership in the handler, compared by person; owner not exempt
- [x] 3.4 Make setting the role already held a no-op, since the console seeds its select from the
      current value
- [x] 3.5 `TestIntegrationNobodyChangesTheirOwnRole`, and extend the existing assign test to cover
      changing and re-setting

## 4. The roster and the invitation list

- [x] 4.1 Add `email` to the member view, from the address recorded at sign-in
- [x] 4.2 Render `Name (address)`, drop the identity-provider subject, and make the brackets
      conditional
- [x] 4.3 One role control for both states, disabled for the reader's own row with the reason
- [x] 4.4 Replace the empty invitation table with one line; label revoke as Cancel and confirm it

## 5. The address

- [x] 5.1 Stop requiring `email_verified`; rename the function to what it does and record on it both
      why authentik forces this and the condition under which it becomes wrong
- [x] 5.2 `TestEmailClaimIsTakenAsGiven` over verified, unverified, absent, normalized, missing and
      wrong-typed claims
- [x] 5.3 Verify end to end rather than by inference: real sign-ins populate `users.email`, the
      invitation joined to its user, and it was accepted

## 6. Approval

- [x] 6.1 Make the pending-approval policy read `is_active`, keeping the path only to choose the
      wording of the refusal
- [x] 6.2 Record the drift in the blueprint's design comment rather than quietly being right again
- [x] 6.3 Greet by name in all four mail templates and both halves of each, in both deployments,
      falling back to the username for a user who has none

## 7. The gate

- [x] 7.1 Backend, console and shared suites green
- [x] 7.2 Acceptance green, with the funding assertions reading `monetized` from `/me` rather than
      assuming, and the lapse skipped where nothing is charged
- [x] 7.3 Deployed to production with `ACME_MONETIZED=false` set in the compose environment — not in
      `.env` alone, which the app's service does not read
- [x] 7.4 NOT DONE, and named rather than left as an unticked box: the authentik email templates and
      the blueprint pointing at them are committed and NOT deployed. Production still sends
      authentik's stock templates. It restarts the identity provider and re-applies a blueprint, and
      it is waiting on a deliberate go-ahead.
