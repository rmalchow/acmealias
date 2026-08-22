// Package oidcauth signs console users in through OpenID Connect and keeps
// their sessions server-side (design D10).
//
// The flow is authorization code with PKCE run entirely on the server — a
// "backend for frontend". No provider token, access, refresh or ID, ever
// reaches the browser: it holds only the opaque session cookie this package
// issues, and the session row it names holds subject, display name and the
// group list as it stood at sign-in, nothing else.
//
// The package exposes plain net/http-shaped functions so the HTTP layer can
// call them from whatever router it uses; it registers no routes itself.
package oidcauth

import (
	"context"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
)

// Service holds the configuration, the database handle and the lazily
// discovered provider. Construct it with New; it makes no network call until
// the first sign-in.
type Service struct {
	cfg config.Config
	db  *gorm.DB

	mu   sync.Mutex
	prov *oidc.Provider
}

// New returns a Service. It does not dial the identity provider — see
// discover.
func New(cfg config.Config, db *gorm.DB) *Service {
	return &Service{cfg: cfg, db: db}
}

// discover fetches the provider's OpenID configuration, once, on first use.
//
// Deliberately not at startup: an identity provider down for maintenance must
// not stop the DNS server and the challenge API — surfaces that have nothing
// to do with human sign-in — from coming up. Config validation only checks
// that the issuer parses as a URL.
//
// A failed discovery is not cached, so the next sign-in retries it.
//
// ponytail: one mutex, so concurrent first sign-ins queue behind a single
// discovery call. That costs at most one round trip on a cold process; if it
// ever matters, replace with a singleflight keyed on the issuer.
func (s *Service) discover(ctx context.Context) (*oidc.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prov != nil {
		return s.prov, nil
	}
	p, err := oidc.NewProvider(ctx, s.cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidcauth: discover %q: %w", s.cfg.OIDCIssuer, err)
	}
	s.prov = p
	return s.prov, nil
}

// oauth2Config builds the exchange configuration for a discovered provider.
func (s *Service) oauth2Config(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.cfg.OIDCClientID,
		ClientSecret: s.cfg.OIDCClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  s.cfg.OIDCRedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// verifier validates ID tokens against the provider's JWKS for our client id:
// signature, issuer, audience and expiry. The nonce is checked separately in
// Callback, because only the caller knows which nonce it sent.
func (s *Service) verifier(p *oidc.Provider) *oidc.IDTokenVerifier {
	return p.Verifier(&oidc.Config{ClientID: s.cfg.OIDCClientID})
}
