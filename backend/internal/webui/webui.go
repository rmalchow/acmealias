// Package webui embeds the compiled Angular console and serves it on the
// admin listener (design D9). `ng build admin` writes into dist/, which is
// embedded here, so the published image carries the console inside the
// binary and needs no second artefact.
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

// dist is the embedded tree rooted at the build output directory. fs.Sub
// cannot fail here — "dist" is a literal directory go:embed already proved
// exists at compile time — so the error is dropped rather than carried
// through every caller.
var dist, _ = fs.Sub(embedded, "dist")

const indexFile = "index.html"

// hashed matches Angular's content-hashed output names (main-XZAOKVPC.js,
// styles-5INURTSO.css): a dash, at least eight hash characters, an extension.
// Only those get an immutable cache lifetime; a rebuild changes the name, so
// a year-old cached copy can never be the wrong one. Everything else
// (index.html, favicon.ico) keeps its stable name across builds and must not
// be cached that way.
var hashed = regexp.MustCompile(`-[A-Za-z0-9_]{8,}\.[A-Za-z0-9]+$`)

// Handler serves the console: embedded files by path, and index.html for any
// unmatched navigation path so a deep link into an Angular route loads the
// application instead of a 404.
//
// Two carve-outs from the fallback. Anything under /api/ is a 404 here — the
// admin listener routes those to the management API's JSON 404, and a
// fallback that swallowed them would turn a typo'd endpoint into an HTML page
// and a very confusing bug report (design D9). Anything whose last segment
// looks like a file (it has a dot) is also a 404: a missing script or image
// should fail as a missing script, not arrive as HTML with a 200.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
}
