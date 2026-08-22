package oidcauth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// flowCookieName carries state, nonce and the PKCE verifier across the
// redirect to the identity provider. It lives for flowCookieTTL and is
// cleared the moment the callback reads it.
const flowCookieName = "acme_oidc_flow"

// flowCookieTTL bounds how long a started sign-in may take to come back.
const flowCookieTTL = 10 * time.Minute

// ErrAuthFailed reports a sign-in that did not establish a session. It is
// deliberately coarse: the HTTP layer should not tell an attacker which of
// state, nonce, signature, issuer, audience or expiry was wrong. The wrapped
// error carries the detail for the log.
var ErrAuthFailed = errors.New("oidcauth: sign-in failed")

// flowState is what the short-lived cookie holds. The verifier is a secret
// (it proves this browser started the flow), hence HttpOnly.
type flowState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
}

// Identity is what a verified ID token yielded. It is the input to session
// creation and, in the next layer up, to role resolution.
type Identity struct {
	Subject     string
	DisplayName string
	Groups      []string
}

// Login starts a sign-in: it stores state, nonce and a PKCE verifier in a
// short-lived cookie and redirects the browser to the identity provider.
// Discovery happens here, on demand, not at startup.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) error {
	p, err := s.discover(r.Context())
	if err != nil {
		return err
	}

	fs := flowState{State: randomToken(), Nonce: randomToken(), Verifier: oauth2.GenerateVerifier()}
	raw, err := json.Marshal(fs)
	if err != nil {
		return fmt.Errorf("oidcauth: encode flow state: %w", err)
	}
	http.SetCookie(w, newCookie(flowCookieName, base64.RawURLEncoding.EncodeToString(raw), flowCookieTTL))

	url := s.oauth2Config(p).AuthCodeURL(fs.State,
		oidc.Nonce(fs.Nonce),
		oauth2.S256ChallengeOption(fs.Verifier),
	)
	http.Redirect(w, r, url, http.StatusFound)
	return nil
}

// Callback completes a sign-in: it checks state against the flow cookie,
// exchanges the code with the PKCE verifier, validates the ID token
// (signature, issuer, audience, expiry, nonce) and — only then — creates the
// session and sets its cookie. Every failure returns ErrAuthFailed and leaves
// no session behind.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) error {
	// The flow cookie is single-use whatever happens next.
	http.SetCookie(w, clearCookie(flowCookieName))

	id, err := s.verifyCallback(r)
	if err != nil {
		return err
	}

	sess, err := s.createSession(r.Context(), id)
	if err != nil {
		return err
	}
	http.SetCookie(w, newCookie(SessionCookieName, sess.ID, time.Until(sess.ExpiresAt)))
	http.Redirect(w, r, "/", http.StatusFound)
	return nil
}

// verifyCallback is everything up to, but not including, session creation:
// the part that can reject. Split out so it is testable without a database
// and so no code path can create a session before it has returned.
func (s *Service) verifyCallback(r *http.Request) (*Identity, error) {
	c, err := r.Cookie(flowCookieName)
	if err != nil {
		return nil, fmt.Errorf("%w: no flow cookie", ErrAuthFailed)
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed flow cookie: %v", ErrAuthFailed, err)
	}
	var fs flowState
	if err := json.Unmarshal(raw, &fs); err != nil {
		return nil, fmt.Errorf("%w: malformed flow cookie: %v", ErrAuthFailed, err)
	}

	if got := r.URL.Query().Get("state"); subtle.ConstantTimeCompare([]byte(got), []byte(fs.State)) != 1 {
		return nil, fmt.Errorf("%w: state mismatch", ErrAuthFailed)
	}
	if e := r.URL.Query().Get("error"); e != "" {
		return nil, fmt.Errorf("%w: provider returned %q", ErrAuthFailed, e)
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		return nil, fmt.Errorf("%w: no authorization code", ErrAuthFailed)
	}

	ctx := r.Context()
	p, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}

	tok, err := s.oauth2Config(p).Exchange(ctx, code, oauth2.VerifierOption(fs.Verifier))
	if err != nil {
		return nil, fmt.Errorf("%w: code exchange: %v", ErrAuthFailed, err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, fmt.Errorf("%w: response carried no id_token", ErrAuthFailed)
	}

	idTok, err := s.verifier(p).Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("%w: id token: %v", ErrAuthFailed, err)
	}
	if subtle.ConstantTimeCompare([]byte(idTok.Nonce), []byte(fs.Nonce)) != 1 {
		return nil, fmt.Errorf("%w: nonce mismatch", ErrAuthFailed)
	}

	var claims map[string]any
	if err := idTok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: id token claims: %v", ErrAuthFailed, err)
	}
	return &Identity{
		Subject:     idTok.Subject,
		DisplayName: displayName(claims, idTok.Subject),
		Groups:      Groups(claims, s.cfg.OIDCGroupClaim, s.cfg.OIDCGroupPrefix),
	}, nil
}

// createSession writes the session row. No provider token is stored: we never
// call the provider on the user's behalf, so there is nothing to spend one on,
// and storing one would turn this table into a credential store.
func (s *Service) createSession(ctx context.Context, id *Identity) (*model.Session, error) {
	now := time.Now().UTC()
	sess := &model.Session{
		ID:          randomToken(),
		Subject:     id.Subject,
		DisplayName: id.DisplayName,
		Groups:      model.StringArray(id.Groups),
		CreatedAt:   now,
		LastSeenAt:  now,
		ExpiresAt:   now.Add(s.cfg.SessionMaxLifetime),
	}
	// A single-account deployment selects it at sign-in (design P19), so the
	// internal edition never enters the "no account selected" state and renders
	// neither a switcher nor a selection screen. Constraint 1: no observable
	// change.
	//
	// A deployment with several accounts leaves the selection empty, and the
	// console shows the picker. Failing to resolve one is not fatal — the person
	// is signed in either way, and lands on the picker rather than on an error.
	if sc, err := store.ScopeForBootstrap(ctx, s.db); err == nil {
		id := sc.AccountID()
		sess.SelectedAccountID = &id
	}
	if err := store.CreateSession(ctx, s.db, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// displayName picks the friendliest label the token offers, falling back to
// the subject so the console never shows an empty user.
func displayName(claims map[string]any, subject string) string {
	for _, k := range []string{"name", "preferred_username", "email"} {
		if v, ok := claims[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return subject
}
