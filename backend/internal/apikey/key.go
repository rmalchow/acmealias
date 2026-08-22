// Package apikey generates and verifies the API keys load balancers use to
// authenticate challenge API requests (design D3).
//
// A key is a public identifier plus a secret. Only sha256(secret) is stored; the
// secret is returned exactly once, at creation, and is not recoverable afterwards.
// Verification is a lookup by identifier followed by a constant-time digest
// comparison — deliberately not bcrypt, which exists to slow down guessing at
// low-entropy human passwords and buys nothing against 256 random bits.
package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// ErrInvalid is what every failed verification returns. Callers MUST render the
// same response for all of them: the wrapped text names the real reason for the
// log only, because a distinguishable response tells an attacker holding a stolen
// key whether the key itself is still good.
var ErrInvalid = errors.New("invalid api key")

// New builds an unsaved key with a freshly generated identifier and secret, and
// returns the secret alongside it. The caller fills in LoadBalancerID, Note, ExpiresAt
// and Networks, persists the key with store.CreateAPIKey, and shows the returned
// secret to the operator once.
//
// ponytail: the identifier is 4 random bytes ("k_" + 8 hex, per design D3), so
// identifiers collide by birthday around ~65k keys; the unique index on
// api_keys.identifier turns that into a create error rather than a silent
// overwrite. Widen the identifier if key counts ever approach that.
func New() (*model.APIKey, string, error) {
	var idBytes [4]byte
	var secretBytes [32]byte // 256 bits, comfortably above the 128-bit floor
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, "", fmt.Errorf("apikey: generate identifier: %w", err)
	}
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return nil, "", fmt.Errorf("apikey: generate secret: %w", err)
	}

	secret := base64.RawURLEncoding.EncodeToString(secretBytes[:])
	digest := sha256.Sum256([]byte(secret))

	return &model.APIKey{
		Identifier:    "k_" + hex.EncodeToString(idBytes[:]),
		SecretHash:    digest[:],
		DisplayPrefix: secret[:6],
	}, secret, nil
}

// Lookup finds a key by its public identifier, with Networks populated. It
// returns an error when no such key exists. store.FindAPIKeyByIdentifier has this
// shape; taking it as a function keeps verification testable without a database.
type Lookup func(ctx context.Context, identifier string) (*model.APIKey, error)

// dummyDigest stands in for a real key's digest when the identifier is unknown,
// so the unknown and wrong-secret paths run the same comparison and an attacker
// cannot enumerate valid identifiers by response time. Its preimage is unknown to
// anyone: it is drawn from crypto/rand at startup, so no secret can match it.
var dummyDigest = func() []byte {
	var b [sha256.Size]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("apikey: crypto/rand unavailable: " + err.Error())
	}
	return b[:]
}()

// Verify resolves an identifier/secret pair to its key. It returns ErrInvalid for
// an unknown identifier, a wrong secret, and an expired or disabled key alike;
// a deleted key is simply an unknown identifier.
func Verify(ctx context.Context, lookup Lookup, identifier, secret string, now time.Time) (*model.APIKey, error) {
	k, err := lookup(ctx, identifier)

	// Always compare, even when the lookup failed, so both paths cost the same.
	want := dummyDigest
	if err == nil && k != nil {
		want = k.SecretHash
	}
	got := sha256.Sum256([]byte(secret))
	ok := subtle.ConstantTimeCompare(got[:], want) == 1

	switch {
	case err != nil || k == nil:
		return nil, fmt.Errorf("%w: unknown identifier %q", ErrInvalid, identifier)
	case !ok:
		return nil, fmt.Errorf("%w: wrong secret for %q", ErrInvalid, identifier)
	case k.DisabledAt != nil && !k.DisabledAt.After(now):
		return nil, fmt.Errorf("%w: key %q was revoked at %s", ErrInvalid, identifier, k.DisabledAt.UTC())
	case k.ExpiresAt != nil && !k.ExpiresAt.After(now):
		return nil, fmt.Errorf("%w: key %q expired at %s", ErrInvalid, identifier, k.ExpiresAt.UTC())
	}
	return k, nil
}
