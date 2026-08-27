-- Suspension by staff, which paying does not lift (design C6).
--
-- There are two suspensions and they must not be able to lift each other. Running
-- out of credit is one, and it is not a column: it is `credits = 0`, which is the
-- whole of C3. Abuse is the other, and it needs state of its own precisely
-- BECAUSE it must survive a checkout — a customer who has been stopped for abuse
-- and then buys 25 EUR of credit must still be stopped, and if the only state
-- were the balance they would not be.
--
-- The column lands here rather than with `staff-plane`, which will own the
-- routes that set it, because usability is the conjunction of both clauses and a
-- conjunction with one operand missing is not the conjunction — it is the other
-- clause wearing its name. store.Usable is written once, against both, and the
-- staff plane later supplies the writer rather than the meaning.
--
-- The column carries no `kind = 'team'` clause, unlike the credits constraint:
-- abuse is a property of what somebody did rather than of what they paid, so a
-- personal account can be flagged. Note that store.Usable, transcribing C6's
-- formula literally, short-circuits on `kind = personal` and therefore does not
-- yet ACT on the flag for one — see the note there. The column is honest about
-- what staff recorded either way, which is the half that has to be right before
-- `staff-plane` supplies the writer.

set local app.cross_account = 'on';

alter table accounts
  add column staff_suspended boolean not null default false;

-- Not indexed. It is read one account at a time, on the row the request already
-- has in hand, and never used to select a working set; an index on a boolean
-- that is false for every row would be read by nothing.

reset app.cross_account;
