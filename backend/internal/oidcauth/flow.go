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
	// RawIDToken is the encoded ID token exactly as the provider issued it,
	// carried only so the session row can hold it for logout. Nothing else
	// reads it: every claim this service acts on has already been verified and
	// extracted into the fields around it.
	RawIDToken  string
	// Email is the address the provider says this person controls, and it is
	// EMPTY unless the provider also said it had verified it.
	//
	// That condition is the whole of it. An invitation is bound to an address
	// (account-invitations V2), and the only thing that makes the binding real
	// is that "this user's address is alice@corp.com" was PROVEN rather than
	// typed. An unverified `email` claim is a string somebody chose about
	// themselves; accepting it here would let anybody who can register claim
	// any address and walk into any account they had been shown an invitation
	// to.
	Email string
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
		RawIDToken:  rawID,
		DisplayName: displayName(claims, idTok.Subject),
		Groups:      Groups(claims, s.cfg.OIDCGroupClaim, s.cfg.OIDCGroupPrefix),
		Email:       verifiedEmail(claims),
	}, nil
}

// verifiedEmail reads the `email` claim, and ONLY when `email_verified` is
// true.
//
// The two claims are separate in OIDC for exactly this reason and the second is
// routinely ignored, which is how "bound to an address" quietly becomes "bound
// to a string the user typed". A provider that omits email_verified is treated
// as not having verified anything: absent is not true.
//
// The value is lower-cased and trimmed here so that it matches invitations,
// which are stored normalized. A comparison that normalizes one side only is a
// comparison that fails on `Alice@corp.com`.
func verifiedEmail(claims map[string]any) string {
	verified, _ := claims["email_verified"].(bool)
	if !verified {
		return ""
	}
	addr, _ := claims["email"].(string)
	return strings.ToLower(strings.TrimSpace(addr))
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
	// Held for the logout call and nothing else — see model.Session.LogoutHint.
	if id.RawIDToken != "" {
		hint := id.RawIDToken
		sess.LogoutHint = &hint
	}
	// A new session selects nothing, and that is the front door rather than an
	// edge case (design P19, task 2.2).
	//
	// It used to select the deployment's only account here, which is why the
	// "no account selected" state has never actually been reached in practice —
	// the least-exercised path in the console is now the most-travelled one.
	// What makes that acceptable is that the picker it leads to is never empty:
	// every user has a personal account (design C1), so "no account selected"
	// means "you have not yet said which" and never "you have nothing".
	//
	// Which is materialized right here, and this is the only place it happens
	// (task 3.1). There is no create-personal-account route and no form: a user
	// has one because they are a user, and sign-in is the one moment "this
	// person exists" becomes true from the service's point of view. Doing it
	// before the session row is written means the picker the session lands on
	// cannot be empty even for the very first request of the very first
	// sign-in — the alternative, materializing lazily when /accounts is asked,
	// puts the creation on a GET and leaves a window where it has not happened.
	//
	// It is idempotent, so this is a read for everyone but the first-time
	// caller. It is NOT fatal-free: a sign-in that could not give somebody
	// anywhere to work is a sign-in into an empty console, and failing loudly
	// here is better than a session that appears to work and shows nothing.
	//
	// The verified address travels with it. It is what an invitation is bound to
	// (account-invitations V2), and sign-in is the only moment the service hears
	// the provider say it — so a user row whose email is empty is a person the
	// provider has told us nothing verified about, and no invitation will match
	// them.
	user, err := store.FindOrCreateUser(ctx, s.db, sess.Subject, sess.DisplayName, id.Email)
	if err != nil {
		return nil, err
	}
	if _, err := store.EnsurePersonalAccount(ctx, s.db, user); err != nil {
		return nil, err
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
