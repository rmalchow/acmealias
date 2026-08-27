// Package webui embeds the compiled Angular consoles and serves them (design
// D9). `ng build console` and `ng build admin` write into dist/console and
// dist/admin, both embedded here, so the published image carries both surfaces
// inside the binary and needs no second artefact.
//
// TWO APPLICATIONS, ONE BINARY, CHOSEN BY HOSTNAME.
//
// The customer console and the staff console are separate Angular applications
// rather than one application with a staff section, and the reason is what does
// NOT get shipped: a customer's browser never downloads the staff bundle. The
// hostname decides which one answers, because the hostname is already the
// boundary everything else uses — the ingress allowlist, the identity provider's
// brand, and the OIDC client a session was established through.
//
// A request whose Host is not the configured staff name gets the customer
// console. That default is deliberate: an unset or mistyped staff host serves
// the customer surface to staff, which is a visible annoyance, rather than
// serving the staff surface to customers, which is not.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// The two build outputs. fs.Sub cannot fail here — both are literal directories
// go:embed already proved exist at compile time — so the errors are dropped
// rather than carried through every caller.
var (
	customerDist, _ = fs.Sub(embedded, "dist/console")
	staffDist, _    = fs.Sub(embedded, "dist/admin")
)

const indexFile = "index.html"

// hashed matches Angular's content-hashed output names (main-XZAOKVPC.js,
// styles-5INURTSO.css): a dash, at least eight hash characters, an extension.
// Only those get an immutable cache lifetime; a rebuild changes the name, so
// a year-old cached copy can never be the wrong one. Everything else
// (index.html, favicon.ico) keeps its stable name across builds and must not
// be cached that way.
var hashed = regexp.MustCompile(`-[A-Za-z0-9_]{8,}\.[A-Za-z0-9]+$`)

// Handler serves whichever console the request's Host asks for. staffHost is the
// hostname the staff console answers on; empty means this deployment has no staff
// surface and every request gets the customer console.
func Handler(staffHost string) http.Handler {
	staffHost = strings.ToLower(strings.TrimSpace(staffHost))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tree := customerDist
		if staffHost != "" && requestHost(r) == staffHost {
			tree = staffDist
		}
		serve(w, r, tree)
	})
}

// requestHost is the Host header without its port and lowercased, which is the
// form a configured hostname is compared against. A Host carrying no port is the
// common case and SplitHostPort would reject it, hence the fallback rather than
// an error.
func requestHost(r *http.Request) string {
	h := strings.ToLower(r.Host)
	if i := strings.LastIndex(h, ":"); i > -1 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return h
}

// serve answers from one build output: embedded files by path, and index.html for
// any unmatched navigation path so a deep link into an Angular route loads the
// application instead of a 404.
//
// Two carve-outs from the fallback. Anything under /api/ is a 404 here — those
// paths belong to the management API's JSON 404, and a fallback that swallowed
// them would turn a typo'd endpoint into an HTML page and a very confusing bug
// report (design D9). Anything whose last segment looks like a file (it has a
// dot) is also a 404: a missing script or image should fail as a missing script,
// not arrive as HTML with a 200.
func serve(w http.ResponseWriter, r *http.Request, dist fs.FS) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." {
		name = indexFile
	}
	if strings.HasPrefix(name, "api/") {
		http.NotFound(w, r)
		return
	}

	if st, err := fs.Stat(dist, name); err != nil || st.IsDir() {
		if name != indexFile && strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		name = indexFile
	}

	if hashed.MatchString(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeFileFS(w, r, dist, name)
}
