package httpapi

import (
	"bufio"
	"bytes"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
)

// The published contract has to describe the API that is actually served
// (design P15, task 7.1b).
//
// A document nobody checks drifts, and a drifted contract is worse than none: a
// caller who programmed against it is now wrong in a way they cannot see, and
// the console — which is the first client and the demonstration that the
// contract is usable — would be quietly exempt from the very thing it is
// supposed to demonstrate.
//
// So this asserts both directions. A route with no operation is an endpoint
// nobody outside was told about; an operation with no route is a promise the
// service does not keep.

// documentPaths reads the operations out of the embedded document.
//
// This is a hand parser rather than a YAML library, and the reason is narrow
// enough to state: gopkg.in/yaml.v3 is in the module graph only as an indirect
// dependency, and promoting it to a direct one so that one test can read the
// keys of one map is a poor trade. The document is ours, its shape is fixed by
// this test, and the two things being read — path keys at one indent and method
// keys at the next — are unambiguous at that shape.
//
// If the document ever needs to be understood rather than enumerated, this
// should become a real parse and take the dependency.
func documentPaths(t *testing.T, doc []byte) map[string]map[string]bool {
	t.Helper()

	out := map[string]map[string]bool{}
	var current string
	inPaths := false

	sc := bufio.NewScanner(bytes.NewReader(doc))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "paths:") {
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		// A key at column zero ends the paths block.
		if line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		switch indent := len(line) - len(strings.TrimLeft(line, " ")); indent {
		case 2:
			if !strings.HasPrefix(trimmed, "/") {
				t.Fatalf("openapi.yaml: %q sits where a path key belongs", trimmed)
			}
			current = strings.TrimSuffix(trimmed, ":")
			out[current] = map[string]bool{}
		case 4:
			if current == "" {
				continue
			}
			key := strings.TrimSuffix(strings.SplitN(trimmed, ":", 2)[0], ":")
			if m := strings.ToUpper(key); isHTTPMethod(m) {
				out[current][m] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("openapi.yaml declares no paths — the parser or the document is wrong")
	}
	return out
}

// registeredPublicRoutes builds the API and reports the operations it serves,
// in the document's vocabulary.
//
// It registers against an unopened handle: RegisterAdmin needs one to be
// non-nil and never dials it, so the conformance check is a plain unit test
// that runs wherever the code compiles rather than only where a database does.
func registeredPublicRoutes(t *testing.T) map[string]map[string]bool {
	t.Helper()

	e := echo.New()
	err := RegisterAdmin(e, AdminOptions{
		DB:     &gorm.DB{},
		Guard:  &authz.Guard{Log: slog.New(slog.DiscardHandler)},
		Auth:   testConsoleAuth{},
		Config: config.Config{},
		Log:    slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("RegisterAdmin: %v", err)
	}

	out := map[string]map[string]bool{}
	for _, r := range e.Routes() {
		if !isHTTPMethod(r.Method) || !strings.HasPrefix(r.Path, "/api/v1/") {
			continue
		}
		switch {
		// Sign-in is a browser redirect dance, not an operation a program calls,
		// and it is deliberately outside the session guard the rest of the
		// document describes.
		case strings.HasPrefix(r.Path, "/api/v1/auth/"):
			continue
		// The catch-alls match no endpoint of their own.
		case strings.HasSuffix(r.Path, "*"):
			continue
		// The document cannot sensibly describe where it is served.
		case r.Path == "/api/v1/openapi.yaml":
			continue
		}

		path := documentForm(strings.TrimPrefix(r.Path, "/api/v1"))
		if out[path] == nil {
			out[path] = map[string]bool{}
		}
		out[path][r.Method] = true
	}
	return out
}

// documentForm rewrites Echo's `:param` into the document's `{param}`.
func documentForm(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func TestOpenAPIDescribesEveryPublicRoute(t *testing.T) {
	documented := documentPaths(t, openAPIDocument)
	served := registeredPublicRoutes(t)

	for path, methods := range served {
		ops, ok := documented[path]
		if !ok {
			t.Errorf("%s is served and appears nowhere in openapi.yaml.\n"+
				"An endpoint nobody outside was told about is either undocumented or should not exist.", path)
			continue
		}
		for m := range methods {
			if !ops[m] {
				t.Errorf("%s %s is served and openapi.yaml does not describe it", m, path)
			}
		}
	}
}

func TestOpenAPIPromisesNothingUnserved(t *testing.T) {
	documented := documentPaths(t, openAPIDocument)
	served := registeredPublicRoutes(t)

	for path, ops := range documented {
		methods, ok := served[path]
		if !ok {
			t.Errorf("openapi.yaml describes %s and nothing serves it.\n"+
				"A promise the service does not keep is worse than no promise: a caller cannot see that it is wrong.", path)
			continue
		}
		for m := range ops {
			if !methods[m] {
				t.Errorf("openapi.yaml describes %s %s and nothing serves it", m, path)
			}
		}
	}
}

// The document is served, unauthenticated, at a stable path. A contract nobody
// can read without credentials is not a published contract.
func TestOpenAPIIsServedWithoutCredentials(t *testing.T) {
	e := echo.New()
	if err := RegisterAdmin(e, AdminOptions{
		DB:     &gorm.DB{},
		Guard:  &authz.Guard{Log: slog.New(slog.DiscardHandler)},
		Auth:   testConsoleAuth{},
		Config: config.Config{},
		Log:    slog.New(slog.DiscardHandler),
	}); err != nil {
		t.Fatalf("RegisterAdmin: %v", err)
	}

	rec := get(t, e, http.MethodGet, "/api/v1/openapi.yaml")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/openapi.yaml: %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("openapi: 3.1.0")) {
		t.Fatal("the served document is not the document")
	}
}

// --- the reader ------------------------------------------------------------

// docsEcho builds the API on a bare Echo, the same way the tests above do, so
// the reader's routes are exercised through the real registration rather than
// against a hand-made router. It matters here because /api/swagger has to beat
// the /api/* catch-all RegisterAdmin installs, and that is a property of the
// router's preferences rather than of anything this package writes.
func docsEcho(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	if err := RegisterAdmin(e, AdminOptions{
		DB:     &gorm.DB{},
		Guard:  &authz.Guard{Log: slog.New(slog.DiscardHandler)},
		Auth:   testConsoleAuth{},
		Config: config.Config{},
		Log:    slog.New(slog.DiscardHandler),
	}); err != nil {
		t.Fatalf("RegisterAdmin: %v", err)
	}
	return e
}

// The reader is as public as the document it renders (design W4, task 6.1). A
// lock on the viewer standing beside an open door to the YAML protects nothing,
// and the audience is precisely the person who has no account yet.
func TestSwaggerPageIsServedWithoutCredentials(t *testing.T) {
	e := docsEcho(t)

	for _, p := range []string{"/api/swagger", "/api/swagger/"} {
		rec := get(t, e, http.MethodGet, p)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200 — the reader must not require a session, "+
				"and it must not lose to the /api/* catch-all", p, rec.Code)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("Scalar.createApiReference")) {
			t.Errorf("GET %s served something that is not the documentation page", p)
		}
	}
}

// The page renders the document that is already published, rather than a second
// copy of it (task 6.3). Two copies of a contract is how a contract drifts.
func TestSwaggerPageRendersTheServedDocument(t *testing.T) {
	page := string(get(t, docsEcho(t), http.MethodGet, "/api/swagger").Body.Bytes())

	if !strings.Contains(page, "url: '/api/v1/openapi.yaml'") {
		t.Error("the page does not point the renderer at /api/v1/openapi.yaml.\n" +
			"If it embeds its own copy of the document, the two can disagree and nothing will say so.")
	}
}

// docsReference matches every URL the page asks a browser to fetch or follow.
var docsReference = regexp.MustCompile(`(?:src|href)="([^"]+)"|url\("([^"]+)"\)`)

// Every asset the page needs is served by this service (design W2, task 6.2).
//
// This is the property most easily lost later, and it will not be lost
// deliberately: it goes when somebody adds an icon set, a font or an analytics
// snippet from a CDN because that is how one is normally added. On a deployment
// with no route to the internet the result is the one page in the product that
// renders blank — and that is the deployment most likely to have somebody
// reading documentation rather than guessing.
func TestSwaggerPageReachesNoOtherHost(t *testing.T) {
	e := docsEcho(t)
	page := get(t, e, http.MethodGet, "/api/swagger").Body.String()

	for _, m := range docsReference.FindAllStringSubmatch(page, -1) {
		ref := m[1]
		if ref == "" {
			ref = m[2]
		}
		if strings.Contains(ref, "://") || strings.HasPrefix(ref, "//") {
			t.Errorf("the documentation page references %q, which is somebody else's host.\n"+
				"The binary has to render this page with no route to the internet, and a CDN on the page "+
				"that documents this API also tells a third party who read it.", ref)
			continue
		}
		if !strings.HasPrefix(ref, "/api/swagger/") {
			// /brand/*, / and /api/v1/openapi.yaml are served by this binary
			// too — by internal/webui and by registerOpenAPI. They are checked
			// end to end in server_test.go, where the console is mounted.
			continue
		}
		if rec := get(t, e, http.MethodGet, ref); rec.Code != http.StatusOK {
			t.Errorf("the page asks for %s and this service answers %d", ref, rec.Code)
		}
	}

	// The renderer's own defaults reach out even when the page does not. Both of
	// these are switched off in the page rather than relied upon, and both are
	// asserted because a version bump could change either default under us.
	if !strings.Contains(page, "withDefaultFonts: false") {
		t.Error("withDefaultFonts is not turned off — Scalar's faces live on fonts.scalar.com")
	}
	if !strings.Contains(page, "proxyUrl: ''") {
		t.Error("proxyUrl is not pinned empty — a request proxy would send this service's callers, " +
			"and their session cookie, through api.scalar.com")
	}
}

// The renderer's own product is switched off (design W2).
//
// Scalar ships a Configure/Share/Deploy bar and an "Ask AI" chat, and BOTH turn
// themselves on by inspecting window.location.hostname: the toolbar's default is
// the string 'localhost', and the agent enables itself for localhost, 127.0.0.1
// and any name ending .test, .example, .invalid or .localhost. Every one of those
// describes the development stack this is worked on in, and both controls link to
// scalar.com services.
//
// A hostname check inside a vendored bundle is not where "no third party sees who
// reads our documentation" should rest, so the page says it. This asserts the page
// keeps saying it, because the assertion is cheap and the failure is invisible —
// nothing breaks, a Deploy button just appears on the shop window.
func TestSwaggerPageDoesNotOfferSomebodyElsesProduct(t *testing.T) {
	page := get(t, docsEcho(t), http.MethodGet, "/api/swagger").Body.String()

	if !strings.Contains(page, "showDeveloperTools: 'never'") {
		t.Error("the developer-tools bar is not switched off — it defaults to on for localhost " +
			"and links to editor.scalar.com, client.scalar.com and dashboard.scalar.com")
	}
	if !strings.Contains(page, "agent: { disabled: true }") {
		t.Error("the Ask AI agent is not switched off — it enables itself on localhost-like " +
			"hostnames and its chat talks to api.scalar.com")
	}
}
