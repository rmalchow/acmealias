package apikey

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

var errNotFound = errors.New("not found")

// fixture builds a saved-shaped key plus its plaintext secret, applying opts.
func fixture(t *testing.T, opts ...func(*model.APIKey)) (*model.APIKey, string) {
	t.Helper()
	k, secret, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	k.ID = model.NewID()
	for _, o := range opts {
		o(k)
	}
	return k, secret
}

// lookupOf returns a Lookup serving exactly the given keys.
func lookupOf(keys ...*model.APIKey) Lookup {
	return func(_ context.Context, id string) (*model.APIKey, error) {
		for _, k := range keys {
			if k.Identifier == id {
				return k, nil
			}
		}
		return nil, errNotFound
	}
}

func TestNewShape(t *testing.T) {
	k, secret, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Identifier, "k_") || len(k.Identifier) != 10 {
		t.Errorf("identifier %q is not k_ + 8 hex", k.Identifier)
	}
	raw, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("secret is not base64url: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("secret carries %d bytes of entropy, want 32", len(raw))
	}
	if want := sha256.Sum256([]byte(secret)); string(k.SecretHash) != string(want[:]) {
		t.Error("stored hash is not sha256 of the secret")
	}
	if k.DisplayPrefix != secret[:6] {
		t.Errorf("display prefix %q, want %q", k.DisplayPrefix, secret[:6])
	}
	// The stored form must not be usable as the secret itself.
	if strings.Contains(string(k.SecretHash), secret) {
		t.Error("stored hash contains the secret")
	}

	// Two keys must not collide.
	k2, secret2, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if k2.Identifier == k.Identifier || secret2 == secret {
		t.Error("New is not producing fresh randomness")
	}
}

func TestVerify(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	valid, validSecret := fixture(t)
	expired, expiredSecret := fixture(t, func(k *model.APIKey) { k.ExpiresAt = &past })
	notYetExpired, notYetSecret := fixture(t, func(k *model.APIKey) { k.ExpiresAt = &future })
	revoked, revokedSecret := fixture(t, func(k *model.APIKey) { k.DisabledAt = &past })

	lookup := lookupOf(valid, expired, notYetExpired, revoked)

	cases := []struct {
		name       string
		identifier string
		secret     string
		wantOK     bool
	}{
		{"valid", valid.Identifier, validSecret, true},
		{"unexpired", notYetExpired.Identifier, notYetSecret, true},
		{"unknown identifier", "k_00000000", validSecret, false},
		{"wrong secret", valid.Identifier, "not-the-secret", false},
		{"empty secret", valid.Identifier, "", false},
		{"expired", expired.Identifier, expiredSecret, false},
		{"revoked", revoked.Identifier, revokedSecret, false},
		{"another key's secret", valid.Identifier, revokedSecret, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, err := Verify(t.Context(), lookup, tc.identifier, tc.secret, now)
			switch {
			case tc.wantOK && err != nil:
				t.Fatalf("want success, got %v", err)
			case tc.wantOK && k == nil:
				t.Fatal("success returned a nil key")
			case !tc.wantOK && err == nil:
				t.Fatal("want failure, got success")
			case !tc.wantOK && !errors.Is(err, ErrInvalid):
				t.Fatalf("error %v does not wrap ErrInvalid", err)
			}
		})
	}
}

// TestVerifyComparesOnUnknownIdentifier pins the property that makes the two
// failure paths cost the same: the digest comparison happens even when the
// lookup found nothing.
func TestVerifyComparesOnUnknownIdentifier(t *testing.T) {
	// A lookup that always fails must still not accept the dummy digest's
	// (unknown) preimage, and must not short-circuit before comparing.
	compared := false
	lookup := func(context.Context, string) (*model.APIKey, error) {
		compared = true
		return nil, errNotFound
	}
	if _, err := Verify(t.Context(), lookup, "k_deadbeef", "whatever", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
	if !compared {
		t.Fatal("lookup was not consulted")
	}
	if len(dummyDigest) != sha256.Size {
		t.Fatalf("dummy digest is %d bytes, want %d", len(dummyDigest), sha256.Size)
	}
}

// serve runs one request through Middleware and a handler that reports 200.
func serve(t *testing.T, req *http.Request, trusted []string, keys ...*model.APIKey) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.Logger.SetOutput(io.Discard)
	ext, err := IPExtractor(trusted)
	if err != nil {
		t.Fatalf("IPExtractor: %v", err)
	}
	e.IPExtractor = ext
	e.Use(Middleware(Options{
		Lookup: lookupOf(keys...),
		Logger: slog.New(slog.DiscardHandler),
	}))
	e.GET("/", func(c echo.Context) error {
		if FromContext(c) == nil {
			t.Error("handler reached without a key in the context")
		}
		return c.NoContent(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func request(t *testing.T, peer string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = net.JoinHostPort(peer, "54321") // brackets the IPv6 cases
	return req
}

func TestMiddlewareTransports(t *testing.T) {
	k, secret := fixture(t)
	token := k.Identifier + "." + secret

	cases := []struct {
		name string
		set  func(*http.Request)
		want int
	}{
		{"basic", func(r *http.Request) { r.SetBasicAuth(k.Identifier, secret) }, http.StatusOK},
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }, http.StatusOK},
		{"x-api-key", func(r *http.Request) { r.Header.Set("X-API-Key", token) }, http.StatusOK},
		{"no credentials", func(*http.Request) {}, http.StatusUnauthorized},
		{"basic wrong secret", func(r *http.Request) { r.SetBasicAuth(k.Identifier, "nope") }, http.StatusUnauthorized},
		{"bearer without secret", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+k.Identifier) }, http.StatusUnauthorized},
		{"x-api-key without secret", func(r *http.Request) { r.Header.Set("X-API-Key", secret) }, http.StatusUnauthorized},
		{"unknown scheme", func(r *http.Request) { r.Header.Set("Authorization", "Token "+token) }, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := request(t, "198.51.100.9")
			tc.set(req)
			if got := serve(t, req, nil, k).Code; got != tc.want {
				t.Fatalf("status %d, want %d", got, tc.want)
			}
		})
	}
}

// TestMiddlewareRejectionsAreIndistinguishable is the enumeration defence: an
// attacker must not learn from the response whether an identifier exists, or
// whether a valid key was merely used from the wrong address.
func TestMiddlewareRejectionsAreIndistinguishable(t *testing.T) {
	restricted, restrictedSecret := fixture(t, func(k *model.APIKey) {
		k.Networks = []model.APIKeyNetwork{{CIDR: "203.0.113.0/24"}}
	})

	body := func(name, id, secret, peer string) string {
		t.Helper()
		req := request(t, peer)
		req.SetBasicAuth(id, secret)
		rec := serve(t, req, []string{"192.0.2.1/32"}, restricted)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", name, rec.Code)
		}
		return rec.Header().Get("WWW-Authenticate") + "\n" + rec.Body.String()
	}

	unknown := body("unknown identifier", "k_00000000", restrictedSecret, "203.0.113.5")
	wrongSecret := body("wrong secret", restricted.Identifier, "wrong", "203.0.113.5")
	wrongAddr := body("out-of-range address", restricted.Identifier, restrictedSecret, "198.51.100.9")

	if unknown != wrongSecret {
		t.Errorf("unknown identifier and wrong secret differ:\n%q\n%q", unknown, wrongSecret)
	}
	if unknown != wrongAddr {
		t.Errorf("unknown identifier and out-of-range address differ:\n%q\n%q", unknown, wrongAddr)
	}
}

func TestMiddlewareSourceAddressRestriction(t *testing.T) {
	const proxy = "192.0.2.1"
	trusted := []string{proxy + "/32"}

	cases := []struct {
		name    string
		cidrs   []string
		peer    string
		xff     string
		want    int
		explain string
	}{
		{"unrestricted key from anywhere", nil, "198.51.100.9", "", http.StatusOK,
			"no CIDR blocks means unrestricted"},
		{"in range, direct peer", []string{"198.51.100.0/24"}, "198.51.100.9", "", http.StatusOK, ""},
		{"out of range, direct peer", []string{"203.0.113.0/24"}, "198.51.100.9", "", http.StatusUnauthorized, ""},
		{"in range via trusted proxy", []string{"203.0.113.0/24"}, proxy, "203.0.113.5", http.StatusOK,
			"the proxy is trusted, so its X-Forwarded-For is believed"},
		{"out of range via trusted proxy", []string{"203.0.113.0/24"}, proxy, "198.51.100.9", http.StatusUnauthorized, ""},
		{"spoofed header from untrusted peer", []string{"203.0.113.0/24"}, "198.51.100.9", "203.0.113.5",
			http.StatusUnauthorized, "the header is ignored and the peer address is judged"},
		{"ipv6 in range", []string{"2001:db8::/32"}, "2001:db8::1", "", http.StatusOK, ""},
		{"ipv6 peer against ipv4 block", []string{"203.0.113.0/24"}, "2001:db8::1", "", http.StatusUnauthorized, ""},
		{"malformed block fails closed", []string{"not-a-cidr"}, "203.0.113.5", "", http.StatusUnauthorized, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, secret := fixture(t, func(k *model.APIKey) {
				for _, c := range tc.cidrs {
					k.Networks = append(k.Networks, model.APIKeyNetwork{CIDR: c})
				}
			})
			req := request(t, tc.peer)
			req.SetBasicAuth(k.Identifier, secret)
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := serve(t, req, trusted, k).Code; got != tc.want {
				t.Fatalf("status %d, want %d (%s)", got, tc.want, tc.explain)
			}
		})
	}
}

func TestIPExtractorRejectsMalformedTrustedCIDR(t *testing.T) {
	if _, err := IPExtractor([]string{"192.0.2.0/24", "bogus"}); err == nil {
		t.Fatal("want an error naming the bad block")
	} else if !strings.Contains(err.Error(), "ACME_TRUSTED_PROXY_CIDRS") {
		t.Fatalf("error %q does not name the setting", err)
	}
}

func TestLastUsedThrottles(t *testing.T) {
	var mu sync.Mutex
	writes := map[model.ID]int{}
	count := func(id model.ID) int {
		mu.Lock()
		defer mu.Unlock()
		return writes[id]
	}
	l := newLastUsed(func(_ context.Context, id model.ID) error {
		mu.Lock()
		defer mu.Unlock()
		writes[id]++
		return nil
	}, slog.New(slog.DiscardHandler))

	first, second := model.NewID(), model.NewID()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Touch(context.Background(), first)
			l.Touch(context.Background(), second)
		}()
	}
	wg.Wait()

	if count(first) != 1 || count(second) != 1 {
		t.Fatalf("wrote %d/%d times for two keys, want exactly one each within the interval",
			count(first), count(second))
	}

	// Pretending the last write was long ago must let the next one through.
	l.mu.Lock()
	l.seen[first] = time.Now().Add(-2 * touchInterval)
	l.mu.Unlock()
	l.Touch(context.Background(), first)
	if count(first) != 2 {
		t.Fatalf("wrote %d times for the first key after the interval elapsed, want 2", count(first))
	}
}

// A failing write must not be fatal: last_used_at is display bookkeeping.
func TestLastUsedSwallowsWriteErrors(t *testing.T) {
	l := newLastUsed(func(context.Context, model.ID) error { return errNotFound }, slog.New(slog.DiscardHandler))
	l.Touch(context.Background(), model.NewID()) // must not panic
}
