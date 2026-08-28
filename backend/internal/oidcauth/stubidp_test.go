package oidcauth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
)

// stubIDP is an OpenID provider reduced to the three things a sign-in touches:
// discovery, JWKS, and the token endpoint. Signing tokens by hand (RS256 is
// twenty lines of crypto/rsa) keeps a JOSE library out of the module for the
// sake of tests, and lets a test mint a token that is wrong in exactly one way.
type stubIDP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	token string // the id_token the token endpoint hands back next

	// noEndSession makes discovery omit end_session_endpoint, which is how a
	// provider says it has not implemented RP-initiated logout.
	noEndSession bool
}

const stubClientID = "acme-console"

func newStubIDP(t *testing.T) *stubIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &stubIDP{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]any{
			"issuer":                                idp.srv.URL,
			"authorization_endpoint":                idp.srv.URL + "/auth",
			"token_endpoint":                        idp.srv.URL + "/token",
			"jwks_uri":                              idp.srv.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}
		if !idp.noEndSession {
			doc["end_session_endpoint"] = idp.srv.URL + "/end-session"
		}
		writeJSON(w, doc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []any{map[string]any{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": "test",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   "AQAB",
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"access_token": "stub-access-token",
			"token_type":   "Bearer",
			"expires_in":   300,
			"id_token":     idp.token,
		})
	})

	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

// sign mints an RS256 JWT over claims, defaulting the ones a valid token
// needs so a test only states what it wants to be wrong.
func (idp *stubIDP) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	full := map[string]any{
		"iss": idp.srv.URL,
		"aud": stubClientID,
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range claims {
		full[k] = v
	}

	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"})
	body, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// service returns a Service pointed at the stub, with a nil database handle:
// every test that uses it expects the sign-in to be rejected before any
// session could be written, and a nil handle turns a mistake there into a
// panic rather than a silent pass.
func (idp *stubIDP) service() *Service {
	return New(config.Config{
		OIDCIssuer:       idp.srv.URL,
		OIDCClientID:     stubClientID,
		OIDCClientSecret: "secret",
		OIDCRedirectURL:  "https://console.example.com/api/v1/auth/callback",
		OIDCGroupClaim:   "groups",
		OIDCGroupPrefix:  "/acme/",

		PostLogoutRedirectURL: "https://www.example.com/",

		SessionIdleTimeout: time.Hour,
		SessionMaxLifetime: 8 * time.Hour,
	}, nil)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
