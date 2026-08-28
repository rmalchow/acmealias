## Why

### The contract is published; nothing renders it

`backend/internal/httpapi/openapi.go` embeds a 657-line hand-written OpenAPI document and serves it
at `/api/v1/openapi.yaml`, unauthenticated, on the reasoning that "a contract nobody can read without
credentials is not a published contract".

What a caller actually gets from that URL is 657 lines of YAML in a browser tab.

That is a published contract in the sense that a filing cabinet is a published archive. Somebody
evaluating whether to program against this service has to either read raw YAML top to bottom, or know
to paste it into a tool of their own — and the second is a step most people will not take before
deciding whether the API is worth their afternoon.

### The document is already the good half of the work

This is not a request to document the API. The document exists, it is hand-written *because* being
usable was the property under test, and a conformance test already fails when the console calls a
path the document does not describe.

So the gap is narrow and specific: **the last mile is a reader**, and the cost of not having one is
paid by exactly the audience the document was written for.

### Why it is not one `<script src>` away

The obvious implementation — an HTML page pointing at a CDN-hosted Swagger UI — is wrong here for a
reason this project has already committed to elsewhere. The console's stylesheet comment records it:
the compiled output "is embedded in the Go binary and has to render on a host with no route to the
internet." The fonts are vendored for that reason; a documentation page that goes blank on an
air-gapped deployment would be the one page in the product that does.

There is a second reason, which is about who is watching. A CDN reference on the page that documents
your API means a third party sees every developer who reads your docs, and can change what they are
shown. For a page whose entire job is to tell somebody what to send us, that is a poor place to
introduce an outside party.

## What Changes

- A browsable rendering of the existing document at **`/api/swagger`**, served from the binary with
  no outbound request.
- The renderer's assets vendored into the repository and embedded, the way the fonts and the console
  already are.
- The page reachable without credentials, matching the document it renders.
- A decision, recorded rather than defaulted, about whether the page may **send requests** — the
  "Try it out" affordance, which is genuinely useful and genuinely a loaded gun pointed at whatever
  account the reader's session has selected.
- The page **styled like the rest of the product** — the dark ground, the accent and Barlow the
  console and marketing site share, with the mark in the corner. This is not decoration on a
  developer tool: the documentation is one of the few pages a technical evaluator reads before
  deciding to build against the service, and a stock renderer's white page with its own blue reads as
  somebody else's page that happens to be hosted here. It is also what decides which renderer to use,
  since Swagger UI's colours are compiled in rather than exposed as variables.
- The document's `info.title`, currently `acmeservice management API`, corrected to the product's
  name. `acmeservice` is this repository's name; it is not what the service is called, and the title
  of the published contract is a poor place for the two to disagree.

## Capabilities

- `service-operations` — the contract is readable by a person, offline, without credentials

## Impact

- **`backend/internal/httpapi`** — one route, one embedded asset tree
- **Repository size** — a vendored renderer is on the order of megabytes; W3 is the decision about
  which one and what that costs
- **No API surface changes.** Nothing about the contract itself moves; this is a reader for a
  document that already exists and is already tested
