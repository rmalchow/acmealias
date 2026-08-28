# Design

Decisions are numbered `D1`, `D2`, … and referred to by number from `tasks.md`
and from code comments.

## D1. The check runs on load, and that is safe because it stores nothing

Opening a load balancer runs the verification for its domains.

The existing route is already a `GET` with no CSRF token, and its comment says
why: *"Verification is a read: it resolves names and stores nothing, so it is a
GET and needs no CSRF token."* Running it automatically changes when it happens,
not what it does.

**What it costs is real and bounded.** One lookup per record per domain, against
a resolver, on every page open. Three things keep that acceptable:

- `resolvercheck` already caches raw answers for twenty seconds, so a burst
  across the rows of one plan is one query per distinct name.
- A load balancer's domain list is the size a human reads on one screen. This is
  not an account-wide sweep — that would be a different feature with different
  costs, and it is out of scope.
- Nothing depends on the result, so a slow or failed lookup degrades the row and
  never the page.

**Rejected: checking on a schedule and storing the answer.** It turns an
observation into state, and stale state displayed as current is worse than no
answer. `health-checks` owns observation over time, and it owns it because doing
that properly needs history rather than a cache.

## D2. Five states, because "nothing there" and "the wrong thing" are different problems

The checker already distinguishes them; the console currently does not, and that
is the substance of this change.

| State | What is true | What the customer does about it |
| --- | --- | --- |
| **Not checked** | no resolver is configured, or the lookup is still in flight | nothing — we are not claiming anything |
| **Not configured** | the name does not resolve at all (`missing`) | publish the record |
| **Configured incorrectly** | it resolves, to something other than what we need (`incorrect`) | change the target of the record they already have |
| **Configured correctly** | it matches (`correct`) | nothing |
| **Could not check** | the lookup failed (`undetermined`) | usually nothing; it is our problem or the resolver's |

Collapsing the middle two is the mistake worth avoiding, because their fixes have
nothing in common. "Not configured" sends somebody to their DNS provider to add a
record. "Configured incorrectly" sends them to a record that already exists,
which they will otherwise not think to look at — they published something, so as
far as they are concerned the job is done.

**And in this product "incorrectly" has a specific, likely cause worth naming in
the help.** An alias is never re-labelled once issued, because its label lives in
a `CNAME` in somebody else's zone. But a domain that was deleted and added again
gets a *new* alias — so a customer who did that, and did not update their
delegation, now has a `CNAME` pointing at a target that no longer exists. It is
published, it is well-formed, and it will never validate. That is exactly
`incorrect`, and no generic "check your DNS settings" message would ever lead
them to it.

## D3. CAA is reported by what it permits, never as "configured / not configured"

`resolvercheck` models CAA with four outcomes of its own, and only one is a
problem:

| Outcome | What the customer is told | Tone |
| --- | --- | --- |
| `correct` | CAA authorises this service | good |
| `not-published` | No CAA restriction | **neutral — this is fine** |
| `blocking` | CAA blocks this service from issuing | **bad** |
| `undetermined` | CAA could not be checked | unknown |

Getting this wrong has a specific and nasty failure mode. `not-published` is by
far the most common state and is entirely correct; a chip calling it "not
configured" would show a warning to nearly every customer, and a warning nearly
everyone sees is a warning everyone learns to ignore. The one state that matters
— `blocking`, which prevents issuance no matter how perfect the delegation is —
would then be invisible in the noise it created.

This is why the existing `domainStatus` deliberately excludes CAA from a domain's
verdict. That exclusion was right for a single verdict. Per record type CAA can
be shown, provided it is shown as *what it says* rather than as *whether it
exists*.

Note that CAA therefore does NOT use the five states of D2. Its axis is
permission, not presence, and forcing the two onto one vocabulary is how
"not-published" would come to read as "not configured".

## D4. Without a resolver, the feature is absent rather than broken

`ACME_VERIFY_RESOLVER` unset currently makes the route answer `503`. Running on
load would put an error on every page for a deployment that never configured one.

So the console asks once and, on `503`, shows **not checked** with no alarm. A
deployment without a resolver has declined a feature; it is not misconfigured.

The distinction matters for the wording too: "not checked" must never be phrased
so that it reads as a fault in the customer's setup. We are the ones who did not
look.

## D5. The help is a modal, and it contains the customer's own record

A chip is a diagnosis. What a customer needs is the prescription, and the
prescription is specific to them: the exact name and the exact target, ready to
paste into whatever manages their DNS.

The modal holds, per state:

- **Not configured** — what to publish, with this domain's own challenge name and
  its own alias, not a placeholder.
- **Configured incorrectly** — what we found, what it should be, and the
  deleted-and-re-added explanation from D2, which is the cause a customer will
  not otherwise guess.
- **Blocking CAA** — what to add to their CAA set to authorise issuance. The fix
  is a CAA change, not a delegation one, and a modal that offered the delegation
  record here would send them to the wrong file.
- **Could not check** — that this is ours or the resolver's problem, not theirs.

**Rejected: a link to documentation.** The general instructions exist in `docs/`.
What is missing at the moment of confusion is the customer's own value, and a
page that cannot know their alias cannot supply it.

**Rejected: showing the fix inline in the row.** Five states times two record
types times every domain is a table nobody can read. The row says what is true;
the modal says what to do.
