# Design

Decisions are numbered `W1`, `W2`, … and referred to by number from `tasks.md`.

## W1. The path is `/api/swagger`, and it sits beside the document rather than under `/api/v1`

`/api/v1/openapi.yaml` is *in* the versioned API because it describes version 1. The reader is not:
it is a tool for looking at whatever the current document says, and versioning a viewer would mean
`/api/v2/swagger` eventually serving the same HTML from a second route for no reason.

So `/api/swagger`, as asked, which also reads as what it is — a human entrance next to a machine one.

The route is registered beside `registerOpenAPI` in `openapi.go`, because the two are one feature:
somebody deleting the document should be looking directly at the thing that renders it.

## W2. The assets are vendored and embedded, not fetched

**Rejected: `<script src="https://unpkg.com/swagger-ui-dist/...">`.** It is four lines and it is wrong
here twice over.

**The binary has to work with no route to the internet.** This is already a settled property, not a
new constraint invented for this change — `projects/shared/src/theme/globals.css` vendors the fonts
because "the compiled output is embedded in the Go binary and has to render on a host with no route
to the internet." A documentation page that renders on the developer's laptop and shows a blank white
box on an air-gapped deployment would be the single page in the product that behaves that way, and it
would fail in the exact deployment most likely to have somebody reading documentation rather than
guessing.

**And a CDN on this page is a third party in an unusually bad place.** The page's whole job is to tell
somebody what to send us — including, if W5 goes that way, from a session that can act on their
account. An outside origin that sees every developer who reads our docs, and can change what they are
shown, is a strange thing to add to that particular page.

The assets go in the repository next to `openapi.yaml` and are embedded with the same `go:embed` the
document already uses. There is no build step to add and nothing new to install.

## W3. Which renderer — and W7's styling requirement is what actually decides it

The request said "swagger docs", so Swagger UI is the default and an alternative has to beat it. The
requirement that the page **look like the rest of the product** (W7) is what gives one the chance.

| | Swagger UI | Scalar | Redoc |
| --- | --- | --- | --- |
| Vendored size | ~1.5 MB | ~0.5–1 MB | ~1 MB |
| Sends requests | yes | yes | no |
| Recognised on sight | universally | not yet | widely |
| **Themeable** | **fights you** | CSS custom properties, dark theme shipped | limited but clean |

**Swagger UI is hard to restyle, and that is a specific claim rather than a taste.** Its markup is a
deep tree of generated class names, its colours are compiled into a stylesheet rather than exposed as
custom properties, and its logo and several controls are inline SVG with baked-in fills. Restyling it
means a long override sheet written against internal class names — which is precisely the "override
layer" the console's theme work already rejected once, for the reason recorded in
`projects/shared/src/theme/brand.less`: an override sheet only reaches what somebody has already
looked at, and everything else keeps arriving in the original colours the day it is first seen.

Doing that here would produce Swagger UI wearing our accent on the parts somebody checked, and its own
blue-grey everywhere else — on a page whose entire purpose is to be the first impression a technical
evaluator forms.

**So: Scalar, unless the name matters more than the fit.** It themes through CSS custom properties, is
built to be embedded, ships a dark theme to start from, and is smaller. What it costs is recognition:
a developer who has seen a hundred Swagger UIs knows where everything is, and does not know that about
Scalar.

**This is the one genuinely open call in the change**, and it is open because it trades a property we
can measure (brand fit) against one we cannot (a stranger's familiarity). The recommendation is
Scalar, on the grounds that "looks like it belongs to this product" is a first-impression property and
"looks like every other API" is not an advantage when the goal is to look like a commercial offering.
If the answer is Swagger UI regardless, W7's expectations drop to a dark ground and the accent, and
the change should say so rather than promise a match it cannot deliver.

Whichever is chosen, the version is pinned and recorded, so "why is our docs page three years old" has
an answer and an upgrade is a deliberate act.

## W4. Unauthenticated, exactly like the document

The reader is as public as what it renders. `openapi.go` already argues this for the YAML — "there is
nothing in it that is not already discoverable by anyone the API answers" — and a viewer that required
a session while the document behind it did not would be a lock on a door standing next to an open one.

This does mean the page is reachable on the customer hostname by anyone. That is the intent: somebody
deciding whether to build against this service should not need an account to find out what it can do.

## W5. "Try it out" is armed, and that is a decision rather than a default

Swagger UI ships a **Try it out** button that sends a real request from the reader's browser. Same
origin, so the session cookie goes with it.

The tension is real and both sides are worth stating:

- **For:** it is most of why a developer prefers this over reading YAML. Being able to fire
  `GET /accounts/{id}/projects` and see the actual shape of your own data answers in ten seconds what
  the document answers in ten minutes. And it is genuinely *your* data — the session is yours and the
  API enforces exactly what it enforces for any other client, so nothing is reachable here that
  `curl` would not reach with the same cookie.
- **Against:** the document also describes `DELETE`. A reader who is signed in, has an account
  selected, and is exploring rather than working can delete a project from a documentation page. The
  affordance that makes reading pleasant makes destroying easy, and nothing about the page's framing
  says "this is production".

**Decision: leave it enabled, and make the page say which account it will act in.** Disabling it
throws away the feature's main value to prevent a mistake the API already guards — every destructive
operation requires the same grant it requires anywhere, and a reader who can delete a project from
this page could delete it from the console two clicks away. What the page owes them is not a
restriction but *situational awareness*, which the console gives everywhere else and a bare Swagger UI
gives nowhere: the account currently selected, named on the page, before anything is sent.

**Rejected: pointing the page at a sandbox account.** There isn't one, inventing one is a change of
its own, and a sandbox whose data does not look like yours answers a different question from the one
being asked.

**Rejected: disabling it and telling people to use `curl`.** It is the read operations people
actually try, and refusing all of them to prevent the rare destructive one is the trade made
backwards.

## W6. The title is wrong, and the published contract is the worst place for that

`info.title` is `acmeservice management API`. `acmeservice` is this repository's directory name. The
product is AcmeAlias — the marketing site says so, the console's wordmark says so since the branding
work, and this document is the artefact a stranger reads *first*.

It is corrected here rather than in a tidying pass because this change is what puts that string in
front of a person in 32-point type at the top of a page.

## W7. The page is styled like the product, because it is a shop window

The documentation is not an internal tool. It is one of the few pages a technical evaluator reads
before deciding whether to build against this service, and it sits on the same hostname as a console
and a marketing site that were given a deliberate identity — dark ground, one accent, Barlow, the mark
in the corner. A stock renderer's white page with its own blue reads as a page belonging to somebody
else that happens to be hosted here.

What "styled" means concretely, in the order the value drops off:

1. **The ground and the type.** The dark palette the console and the marketing site share, and Barlow
   for prose with the monospace face for payloads. This alone does most of the work.
2. **One accent**, the product's green, wherever the renderer would otherwise use its own colour for
   method badges, links and active states — with the caveat that HTTP method colours are semantic and
   should stay distinguishable: `GET`, `POST` and `DELETE` reading as three different things matters
   more than any of them matching the brand.
3. **The bar.** The mark and wordmark in the corner, exactly as `ShellComponent` and the marketing
   site render them, so the page is visibly part of the product rather than adjacent to it.

The tokens come from the same place the console's do rather than being retyped: the theme emits
`--ac-*` custom properties, and a documentation page that hard-codes the greens is a fourth surface to
update the next time one of them moves.

**Rejected: a long override sheet against a renderer's internal class names.** This is exactly the
approach `brand.less` rejected for ng-zorro, and the reason transfers whole — an override layer only
reaches components somebody has already looked at, and everything else arrives in the original
colours the day it first appears. W3 is the consequence: pick a renderer that themes through
variables, rather than one that has to be fought.

## Risks / Trade-offs

- **Binary size.** Half a megabyte to one and a half, depending on W3, stated plainly rather than
  discovered.
- **Choosing fit over familiarity** (W3). A developer who knows Swagger UI by heart has to look around
  a Scalar page for a minute. That is the cost of the recommendation, and it is a real one.
- **A vendored renderer goes stale.** Pinned and recorded, so it is visible rather than silent.
- **An armed Try-it-out on a public page.** Bounded by the same authorization as every other client
  and mitigated by naming the account (W5), not by removing the feature.

## Migration Plan

1. Settle W3 — the renderer, which W7 is what actually decides.
2. Vendor it, pinned, beside `openapi.yaml`.
3. Add the route and the page.
4. Theme it from the console's own `--ac-*` tokens rather than retyped colours.
5. Name the selected account on the page.
6. Fix `info.title`.

Rollback is deleting the route; the document is untouched and keeps serving.

## Open Questions

- **Whether the staff API deserves the same treatment**, served only on the staff hostname. It has no
  published document today, and `P15`'s reasoning for keeping the two documents apart applies to
  viewers as well.
- **Whether the marketing site should link to it.** It is the most persuasive page for a technical
  evaluator and it is currently reachable only by knowing the URL.
