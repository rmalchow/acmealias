package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
)

// The public API's contract, served publicly (design P15), and the page that
// renders it for a person (design W1).
//
// One document, for the public surface only. The staff tree is a separate route
// tree with a separate authorization plane precisely so that a staff-only
// operation cannot be one forgotten annotation away from appearing here.
//
// It is hand-written rather than generated. A generator would guarantee the
// document matches the handlers and would guarantee nothing about whether the
// contract is one anyone would want to program against — and the console is the
// first client, so the second property is the one under test. What keeps it
// honest instead is the conformance test: every path the console calls has to
// appear here, or the test fails.
//
//go:embed openapi.yaml
var openAPIDocument []byte

// The reader's assets: one HTML page, one vendored renderer, four faces.
//
// VENDORED AND PINNED, NOT FETCHED (design W2). The renderer is Scalar's
// standalone browser bundle, version 1.66.1, taken verbatim from
// https://registry.npmjs.org/@scalar/api-reference/-/api-reference-1.66.1.tgz
// (dist/browser/standalone.js). The version is in the FILE NAME rather than only
// in this comment, so an upgrade is a rename that shows up in a diff and in `ls`
// rather than a silent overwrite — and so "why is our docs page three years old"
// has an answer somebody can read without a manifest.
//
// To re-vendor: download that tarball at the new version, copy
// dist/browser/standalone.js to docs/scalar-api-reference-<version>.js, delete
// the old one, and update the <script src> in docs/index.html. There is no build
// step and nothing to install; that is the point.
//
// WHY SCALAR RATHER THAN SWAGGER UI. The change asked for "swagger docs" and
// design W3 recorded a recommendation against the obvious answer: the page has
// to look like the product (W7), Swagger UI's colours are compiled into its
// stylesheet rather than exposed as custom properties, and matching the brand
// would therefore mean a long override sheet written against its internal class
// names. That is exactly the approach brand.less already rejected once for
// ng-zorro, and for a reason that transfers whole — an override layer reaches
// only what somebody has already looked at, and everything else arrives in the
// original colours the day it is first seen. Scalar themes through
// `--scalar-*` custom properties, which is why docs/index.html's theming is
// fourteen declarations and not a stylesheet. What it costs is recognition: a
// developer who knows Swagger UI by heart has to look around for a minute.
//
// WHAT IT COSTS IN BYTES. The bundle is 3.8 MB unminified-but-minified — four
// times design W3's ~0.5–1 MB estimate, which was wrong and is corrected here
// rather than left to be discovered. Measured: the binary goes from 38.4 MB to
// 42.3 MB, about 10%. It is embedded uncompressed because go:embed stores what
// it is given; serving it gzipped would save ~3 MB of binary at the cost of a
// second code path for the client that does not ask for gzip, and that trade was
// declined at this size.
//
//go:embed docs
var docsAssets embed.FS

// docsContentTypes is explicit rather than left to mime.TypeByExtension, because
// Go's built-in table has no entry for .woff2: it would be sniffed to
// application/octet-stream. Browsers load fonts regardless of the type they
// arrive with, so this is tidiness rather than a fix — but a font served as an
// octet stream is the kind of thing that looks like the bug when something else
// is the bug.
var docsContentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".woff2": "font/woff2",
}

// registerOpenAPI serves the document at a stable, unauthenticated path, and the
// page that renders it beside it.
//
// Unauthenticated deliberately: a contract nobody can read without credentials
// is not a published contract, and there is nothing in it that is not already
// discoverable by anyone the API answers. The reader inherits that (design W4) —
// a lock on the viewer standing beside an open door to the YAML protects
// nothing.
//
// The two are registered together because they are one feature: somebody
// deleting the document should be looking directly at the thing that renders it.
//
// The path is /api/swagger and NOT /api/v1/swagger (design W1). The document is
// versioned because it describes version 1; a viewer is not, and versioning it
// would mean /api/v2/swagger eventually serving byte-identical HTML from a
// second route.
func registerOpenAPI(e *echo.Echo) {
	e.GET("/api/v1/openapi.yaml", func(c echo.Context) error {
		return c.Blob(http.StatusOK, "application/yaml", openAPIDocument)
	})

	// Both spellings answer with the page rather than one redirecting to the
	// other: /api/swagger is the path a person types and the one the change
	// names, and a 301 to a trailing slash is a wart in a URL somebody is going
	// to paste into a README.
	e.GET("/api/swagger", docsPage)
	e.GET("/api/swagger/*", docsPage)
}

// docsPage answers with one of the embedded assets, or with the page itself for
// the bare path.
//
// The asset names are joined under docs/ after path.Clean and a check that the
// result did not climb out — the standard traversal guard, kept explicit because
// this is the only handler in the package serving a file named by the caller.
func docsPage(c echo.Context) error {
	name := strings.TrimPrefix(path.Clean("/"+c.Param("*")), "/")
	if name == "" || name == "." {
		name = "index.html"
	}

	body, err := fs.ReadFile(docsAssets, path.Join("docs", name))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such documentation asset")
	}

	ct, ok := docsContentTypes[path.Ext(name)]
	if !ok {
		ct = "application/octet-stream"
	}

	// The renderer's file name carries its version, so a cached copy can never be
	// the wrong one; the page and the faces keep stable names and must not be
	// cached that way. Same rule, and the same reasoning, as internal/webui.
	if strings.HasPrefix(name, "scalar-") {
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Response().Header().Set("Cache-Control", "no-store")
	}
	return c.Blob(http.StatusOK, ct, body)
}
