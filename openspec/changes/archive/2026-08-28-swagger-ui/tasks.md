# Tasks

> The document already exists, is hand-written, is served at `/api/v1/openapi.yaml`, and has a
> conformance test that fails when the console calls a path it does not describe. This change adds a
> READER. Do not regenerate the document, and do not replace it with a generated one — `openapi.go`
> records why it is written by hand, and that reasoning is not in scope here.

## 1. The renderer, vendored

- [x] 1.0 SETTLE W3 FIRST — Scalar or Swagger UI. It is the one open call, and W7's styling requirement is what decides it: Swagger UI's colours are compiled in rather than exposed as custom properties, so matching the product means a long override sheet against internal class names, which is the approach `brand.less` already rejected once for ng-zorro. Scalar themes through variables. The trade is brand fit against a stranger's familiarity
  - SETTLED: **Scalar**, acting on W3's recommendation rather than on a new argument. Stated here so the human can reverse it cheaply: reversing means vendoring `swagger-ui-dist` instead, deleting `docs/index.html`'s `--scalar-*` block, and dropping W7's promise to the ground and the accent per 4.1d.
- [x] 1.1 Vendor the chosen renderer's distribution beside `backend/internal/httpapi/openapi.yaml`, PINNED, with the version recorded where somebody upgrading will read it (design W3)
- [x] 1.2 Embed it with `go:embed`, the same mechanism the document already uses. No build step, nothing new to install
- [x] 1.3 Do NOT reference a CDN. The binary has to render on a host with no route to the internet — the fonts are vendored for exactly this reason — and a documentation page is a poor place to introduce a third party who sees every developer who reads it (design W2)
- [x] 1.4 Note the binary size before and after in the change, so ~1.5 MB is a stated cost rather than one somebody discovers
  - MEASURED: 38,437,679 → 42,243,257 bytes (+3.63 MiB, +9.9%). The vendored bundle is 3.8 MB, which is FOUR TIMES design W3's ~0.5–1 MB estimate for Scalar; the estimate was wrong and is corrected in `openapi.go` rather than left to be discovered.

## 2. The route

- [x] 2.1 Serve the page at `/api/swagger`, registered beside `registerOpenAPI` in `openapi.go` — the two are one feature, and somebody deleting the document should be looking at what renders it (design W1)
- [x] 2.2 NOT under `/api/v1`. The document is versioned because it describes version 1; a viewer is not, and versioning it would mean a second route serving identical HTML (design W1)
- [x] 2.3 Serve it unauthenticated, matching the document. A lock on the viewer beside an open door to the YAML protects nothing (design W4)
- [x] 2.4 Point it at the existing `/api/v1/openapi.yaml` rather than embedding a second copy of the document

## 3. Try it out, and knowing what you are about to do

- [x] 3.1 Leave request sending ENABLED (design W5). Read operations are most of why somebody prefers this to reading YAML, and refusing all of them to prevent the rare destructive one is the trade made backwards
- [x] 3.2 Name the currently selected account on the page, before anything can be sent. This is what the page owes a reader: the API already enforces every grant it enforces for any other client, so the risk is not authority but SITUATIONAL AWARENESS, which a bare Swagger UI gives nowhere
- [x] 3.3 Say plainly that requests act on real data. Nothing about a documentation page's framing suggests production, and the document describes `DELETE`
- [x] 3.4 Verify a request sent from the page carries the session and is refused exactly as any other client would be — the page must not become a path around anything

## 4. Making it look like the product

- [x] 4.0 Take the palette from the console's own `--ac-*` tokens rather than retyping the greens. A documentation page with hard-coded colours is a fourth surface to update the next time one moves (design W7)
  - NOT MET AS WRITTEN, and the reason is structural: the console emits `--ac-*` from `brand.less` at BUILD time into a content-hashed stylesheet, and this page is served by Go with no build step (task 1.2) and no way to name that file. So `docs/index.html` carries a fourth copy of the palette — six values, in one block, under the same token names — alongside the copies `site/src/styles.css` and `development/authentik/branding/custom.css` already admit to. It is stated in the file, exactly as those two state it of themselves.
- [x] 4.1a The ground and the type first — the dark palette and Barlow, with the monospace face for payloads. This is most of the work
- [x] 4.1b One accent for links and active states, BUT leave the HTTP method colours semantically distinct. `GET`, `POST` and `DELETE` reading as three different things matters more than any of them matching the brand
- [x] 4.1c The mark and wordmark in the corner, rendered as `ShellComponent` and the marketing site render them, so the page is visibly part of the product rather than adjacent to it
- [x] 4.1d If W3 lands on Swagger UI anyway, say in the change that the match is partial and stops at the ground and the accent — rather than promising a fit that renderer cannot deliver
  - Not applicable: W3 landed on Scalar, and the match is not partial.

## 5. The title

- [x] 5.1 Correct `info.title` in `openapi.yaml`: it reads `acmeservice management API`, and `acmeservice` is this repository's directory name rather than the product's (design W6)
- [x] 5.2 Check the rest of the document's prose for the same substitution while there

## 6. Verifying

- [x] 6.1 Assert the page answers 200 unauthenticated
- [x] 6.2 Assert every asset it requests is served by this service — the offline property is the one most easily lost later, by somebody adding a font or an icon set without noticing (design W2)
- [x] 6.3 Assert the document it loads is the one served at `/api/v1/openapi.yaml`, so the two cannot drift
