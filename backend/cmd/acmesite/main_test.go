package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The site ships three links to the console, and they are written as the
// DEVELOPMENT console because that is where the development stack serves it.
// Production shipped them verbatim for as long as production existed: the Sign
// in button in the header, the Sign up button at the foot of the page, and the
// one on the about page all sent a visitor to a host on their own machine.
//
// These tests are about the substitution that fixes it, and about the two ways
// it may not fail quietly.

func siteWithLinks() fs.FS {
	return fstest.MapFS{
		"index.html":       {Data: []byte(`<a href="http://console.localhost/api/v1/auth/login">Sign in</a>` + "\n" + `<a href="http://console.localhost/api/v1/auth/login">Sign up</a>`)},
		"about/index.html": {Data: []byte(`<a href="http://console.localhost/api/v1/auth/login">Sign in</a>`)},
		"styles.css":       {Data: []byte(`/* http://console.localhost is not a link */`)},
	}
}

func TestTheConsoleOriginIsSubstitutedInEveryPage(t *testing.T) {
	got, err := consoleRewrites(siteWithLinks(), "https://console.acmealias.com")
	if err != nil {
		t.Fatalf("rewriting: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rewrote %d files, want the two HTML ones: %v", len(got), keys(got))
	}
	if _, ok := got["styles.css"]; ok {
		t.Error("the stylesheet was rewritten; only HTML carries these links, and a CSS file " +
			"mentioning the host is a comment rather than a destination")
	}
	for name, body := range got {
		if strings.Contains(string(body), devConsoleOrigin) {
			t.Errorf("%s still points at %s after rewriting", name, devConsoleOrigin)
		}
		if !strings.Contains(string(body), "https://console.acmealias.com/api/v1/auth/login") {
			t.Errorf("%s does not carry the substituted link:\n%s", name, body)
		}
	}
}

// Unset is development, where the literal in the markup is already correct.
func TestAnUnsetOriginRewritesNothing(t *testing.T) {
	got, err := consoleRewrites(siteWithLinks(), "")
	if err != nil || got != nil {
		t.Fatalf("an unset origin produced %v, %v; it must leave the site exactly as built", got, err)
	}
}

// A trailing slash would otherwise produce `https://console.example.com//api/…`,
// which most servers answer and some redirect. Accepted and normalised, because
// a value copied out of a browser's address bar carries one.
func TestATrailingSlashIsAccepted(t *testing.T) {
	got, err := consoleRewrites(siteWithLinks(), "https://console.acmealias.com/")
	if err != nil {
		t.Fatalf("rewriting: %v", err)
	}
	if !strings.Contains(string(got["index.html"]), "https://console.acmealias.com/api/v1/auth/login") {
		t.Errorf("the trailing slash was not normalised:\n%s", got["index.html"])
	}
}

func TestAnUnusableOriginIsRefused(t *testing.T) {
	for _, origin := range []string{"console.acmealias.com", "/console", "https://console.acmealias.com/deep"} {
		if _, err := consoleRewrites(siteWithLinks(), origin); err == nil {
			t.Errorf("%q was accepted; it is not a scheme-and-host origin, and the link it "+
				"produces is broken in a way nobody sees until a visitor presses it", origin)
		}
	}
}

// The failure this exists for: somebody edits the markup, the constant stops
// matching, and the site ships pointing every visitor at a development host.
// Starting is the last moment anybody can be told.
func TestAnOriginThatMatchesNothingRefusesToStart(t *testing.T) {
	empty := fstest.MapFS{"index.html": {Data: []byte(`<a href="/somewhere-else">Sign in</a>`)}}
	if _, err := consoleRewrites(empty, "https://console.acmealias.com"); err == nil {
		t.Fatal("a site containing no development console link was accepted; the substitution " +
			"silently did nothing, which is how the links got shipped wrong in the first place")
	}
}

// And the rewritten body is what is actually served, with the response otherwise
// unchanged.
func TestTheRewrittenPageIsWhatIsServed(t *testing.T) {
	docs := siteWithLinks()
	rewrites, err := consoleRewrites(docs, "https://console.acmealias.com")
	if err != nil {
		t.Fatalf("rewriting: %v", err)
	}
	rec := httptest.NewRecorder()
	handler(docs, rewrites).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / answered %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), devConsoleOrigin) {
		t.Errorf("the served homepage still points at %s:\n%s", devConsoleOrigin, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache, must-revalidate" {
		t.Errorf("Cache-Control is %q; a rewritten page is still HTML and must not be cached "+
			"differently from one served straight out of the binary", got)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
