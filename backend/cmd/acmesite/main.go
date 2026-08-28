// Command acmesite serves the marketing site, and it exists so that image can
// be distroless.
//
// WHY NOT NGINX. Serving a directory of static files is the one job nginx is
// unarguably good at, and the image was `nginxinc/nginx-unprivileged:alpine` for
// exactly that reason. What it costs is a shell, a package manager, a config
// language and a CVE feed, all to answer four routes that never change — while
// the service image beside it is already `gcr.io/distroless/static` with one Go
// binary and nothing else. Two runtime baselines for one product is two things
// to patch and two ways to be wrong about what is installed.
//
// So the site is a Go binary with the files compiled into it, in the same
// module and the same toolchain as everything else here, and the image has no
// filesystem worth the name: no shell to exec into, nothing to read, nothing to
// upgrade but the binary.
//
// The routing rules below are the nginx config translated rather than
// reinvented, and the one that matters is the 404.
package main

import (
	"embed"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// The built site, copied here by docker/Dockerfile.site before the Go build.
//
// A committed dist/index.html keeps this resolvable on a clean checkout, the
// same trick internal/webui uses: `go:embed` is a COMPILE-TIME directive, so an
// empty directory is not a smaller binary, it is a build failure — and one that
// only appears for whoever builds without running site/build.sh first.
//
//go:embed all:dist
var embedded embed.FS

func main() {
	addr := os.Getenv("ACMESITE_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	docs, err := fs.Sub(embedded, "dist")
	if err != nil {
		log.Error("the site is not embedded", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: handler(docs),
		// Modest and explicit. A static server with no timeouts is a slowloris
		// target, and the defaults are none.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Info("serving the marketing site", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func handler(docs fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := resolve(docs, r.URL.Path)
		if name == "" {
			// THE 404 IS THE POINT, and it is why this is not the console's
			// handler. `internal/webui` falls back to index.html for any
			// unmatched path, correctly: an Angular application owns its routes
			// and the server cannot know them. This site is a handful of real
			// files at real paths, so the same fallback would render the
			// homepage for every typo and hide broken links from the people who
			// could fix them.
			http.NotFound(w, r)
			return
		}

		f, err := docs.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		// NOT http.FileServer, and not http.ServeFileFS either. Both apply
		// net/http's "directory index" convention: a request whose path ends in
		// `index.html` is answered with a 301 to `./`, which turned every page
		// of this site into a redirect once paths were resolved to their index
		// files. Serving the opened file directly skips that convention
		// entirely, which is right here — the mapping from URL to file is
		// decided above, by us.
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			http.Error(w, "unreadable", http.StatusInternalServerError)
			return
		}
		setCaching(w, name)
		// A zero ModTime means ServeContent emits no Last-Modified and no
		// If-Modified-Since handling, which is correct for an embedded file
		// system: every file's timestamp is the build's, identical across the
		// whole site, and therefore useless as a validator.
		http.ServeContent(w, r, path.Base(name), time.Time{}, rs)
	})
}

// resolve maps a request path onto a file in the embedded site, or "" when
// there is none.
//
// The rules are the whole routing table: an exact file wins; a directory is
// served by its index.html; everything else is a 404. `path.Clean` on a
// leading-slashed path is what keeps `../` out — it resolves the traversal
// before the name is ever used to open anything.
func resolve(docs fs.FS, urlPath string) string {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	info, err := fs.Stat(docs, name)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		name = path.Join(name, "index.html")
		if _, err := fs.Stat(docs, name); err != nil {
			return ""
		}
	}
	return name
}

// setCaching splits the site into the two kinds of file it actually has.
//
// Nothing here is content-addressed — there are no hashed filenames, because
// the build is a copy — so nothing may be cached the way a bundler's output is.
// HTML and CSS are edited and redeployed directly, and a cached copy is a
// change nobody sees; fonts and images change roughly never, so an hour keeps
// the page quick without stranding a logo for a day.
func setCaching(w http.ResponseWriter, name string) {
	switch path.Ext(name) {
	case ".html", ".css":
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	case ".woff", ".woff2", ".ttf", ".eot", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}
