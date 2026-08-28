# Tasks

> The document already exists, is hand-written, is served at `/api/v1/openapi.yaml`, and has a
> conformance test that fails when the console calls a path it does not describe. This change adds a
> READER. Do not regenerate the document, and do not replace it with a generated one — `openapi.go`
> records why it is written by hand, and that reasoning is not in scope here.

## 1. The renderer, vendored

- [ ] 1.0 SETTLE W3 FIRST — Scalar or Swagger UI. It is the one open call, and W7's styling requirement is what decides it: Swagger UI's colours are compiled in rather than exposed as custom properties, so matching the product means a long override sheet against internal class names, which is the approach `brand.less` already rejected once for ng-zorro. Scalar themes through variables. The trade is brand fit against a stranger's familiarity
- [ ] 1.1 Vendor the chosen renderer's distribution beside `backend/internal/httpapi/openapi.yaml`, PINNED, with the version recorded where somebody upgrading will read it (design W3)
- [ ] 1.2 Embed it with `go:embed`, the same mechanism the document already uses. No build step, nothing new to install
- [ ] 1.3 Do NOT reference a CDN. The binary has to render on a host with no route to the internet — the fonts are vendored for exactly this reason — and a documentation page is a poor place to introduce a third party who sees every developer who reads it (design W2)
- [ ] 1.4 Note the binary size before and after in the change, so ~1.5 MB is a stated cost rather than one somebody discovers

## 2. The route

- [ ] 2.1 Serve the page at `/api/swagger`, registered beside `registerOpenAPI` in `openapi.go` — the two are one feature, and somebody deleting the document should be looking at what renders it (design W1)
- [ ] 2.2 NOT under `/api/v1`. The document is versioned because it describes version 1; a viewer is not, and versioning it would mean a second route serving identical HTML (design W1)
- [ ] 2.3 Serve it unauthenticated, matching the document. A lock on the viewer beside an open door to the YAML protects nothing (design W4)
- [ ] 2.4 Point it at the existing `/api/v1/openapi.yaml` rather than embedding a second copy of the document

## 3. Try it out, and knowing what you are about to do

- [ ] 3.1 Leave request sending ENABLED (design W5). Read operations are most of why somebody prefers this to reading YAML, and refusing all of them to prevent the rare destructive one is the trade made backwards
- [ ] 3.2 Name the currently selected account on the page, before anything can be sent. This is what the page owes a reader: the API already enforces every grant it enforces for any other client, so the risk is not authority but SITUATIONAL AWARENESS, which a bare Swagger UI gives nowhere
- [ ] 3.3 Say plainly that requests act on real data. Nothing about a documentation page's framing suggests production, and the document describes `DELETE`
- [ ] 3.4 Verify a request sent from the page carries the session and is refused exactly as any other client would be — the page must not become a path around anything

## 4. Making it look like the product

- [ ] 4.0 Take the palette from the console's own `--ac-*` tokens rather than retyping the greens. A documentation page with hard-coded colours is a fourth surface to update the next time one moves (design W7)
- [ ] 4.1a The ground and the type first — the dark palette and Barlow, with the monospace face for payloads. This is most of the work
- [ ] 4.1b One accent for links and active states, BUT leave the HTTP method colours semantically distinct. `GET`, `POST` and `DELETE` reading as three different things matters more than any of them matching the brand
- [ ] 4.1c The mark and wordmark in the corner, rendered as `ShellComponent` and the marketing site render them, so the page is visibly part of the product rather than adjacent to it
- [ ] 4.1d If W3 lands on Swagger UI anyway, say in the change that the match is partial and stops at the ground and the accent — rather than promising a fit that renderer cannot deliver

## 5. The title

- [ ] 5.1 Correct `info.title` in `openapi.yaml`: it reads `acmeservice management API`, and `acmeservice` is this repository's directory name rather than the product's (design W6)
- [ ] 5.2 Check the rest of the document's prose for the same substitution while there

## 6. Verifying

- [ ] 6.1 Assert the page answers 200 unauthenticated
- [ ] 6.2 Assert every asset it requests is served by this service — the offline property is the one most easily lost later, by somebody adding a font or an icon set without noticing (design W2)
- [ ] 6.3 Assert the document it loads is the one served at `/api/v1/openapi.yaml`, so the two cannot drift
