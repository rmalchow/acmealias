package oidcauth

import (
	"encoding/base64"
	"errors"
	"net/http"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// assertCookieAttrs checks the attributes design D10 requires of every cookie
// this package sets: HttpOnly, Secure, SameSite=Lax and host-only.
func assertCookieAttrs(t *testing.T, c *http.Cookie) {
	t.Helper()
	if !c.HttpOnly {
		t.Errorf("%s: HttpOnly is not set", c.Name)
	}
	if !c.Secure {
		t.Errorf("%s: Secure is not set", c.Name)
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("%s: SameSite = %v, want Lax", c.Name, c.SameSite)
	}
	if c.Domain != "" {
		t.Errorf("%s: Domain = %q, want host-only", c.Name, c.Domain)
	}
	if c.Path != "/" {
		t.Errorf("%s: Path = %q, want /", c.Name, c.Path)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	assertCookieAttrs(t, newCookie(SessionCookieName, "value", time.Hour))

	cleared := clearCookie(SessionCookieName)
	assertCookieAttrs(t, cleared)
	if cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Errorf("clearCookie = %+v, want an empty value and a negative MaxAge", cleared)
	}
}

func TestRandomTokenIsUnpredictable(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		tok := randomToken()
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("token %q is not URL-safe base64: %v", tok, err)
		}
		if len(raw) != 32 {
			t.Fatalf("token carries %d bytes, want 32 (256 bits)", len(raw))
		}
		if seen[tok] {
			t.Fatalf("token %q repeated", tok)
		}
		seen[tok] = true
	}
}

func TestCheckSession(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	revoked := now.Add(-time.Minute)
	const idle = time.Hour

	live := func() *model.Session {
		return &model.Session{
			ID:         "s1",
			CreatedAt:  now.Add(-2 * time.Hour),
			LastSeenAt: now.Add(-time.Minute),
			ExpiresAt:  now.Add(6 * time.Hour),
		}
	}

	cases := map[string]struct {
		mutate  func(*model.Session)
		wantErr bool
	}{
		"live":                   {mutate: func(*model.Session) {}},
		"revoked":                {mutate: func(s *model.Session) { s.RevokedAt = &revoked }, wantErr: true},
		"past absolute lifetime": {mutate: func(s *model.Session) { s.ExpiresAt = now.Add(-time.Second) }, wantErr: true},
		"expiring exactly now":   {mutate: func(s *model.Session) { s.ExpiresAt = now }, wantErr: true},
		"idle too long":          {mutate: func(s *model.Session) { s.LastSeenAt = now.Add(-idle - time.Second) }, wantErr: true},
		"idle just short of the timeout": {mutate: func(s *model.Session) {
			s.LastSeenAt = now.Add(-idle + time.Second)
		}},
		// A session may be inside its idle window and still be past its
		// absolute lifetime: the two limits are independent.
		"active but past absolute lifetime": {mutate: func(s *model.Session) {
			s.LastSeenAt = now
			s.ExpiresAt = now.Add(-time.Second)
		}, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := live()
			tc.mutate(s)
			err := checkSession(s, idle, now)
			if tc.wantErr && !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("err = %v, want ErrSessionInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}
