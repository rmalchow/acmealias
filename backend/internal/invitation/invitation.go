// Package invitation holds the parts of an account invitation that are not
// database access: the token, the address normalization the whole flow compares
// on, and the mask a refusal shows.
//
// # What an invitation is, and what it deliberately is not
//
// An invitation offers membership of ONE ACCOUNT to ONE EMAIL ADDRESS. It names
// no role. Accepting makes the invitee a member holding nothing, and an
// administrator assigns a role afterwards (account-invitations V6) — so
// "you are in" and "here is what you may do" are two decisions, made at the two
// different moments somebody is actually able to make them.
//
// # It is bound to the address, and there is one way that quietly stops being true
//
// Only a user whose verified address IS the invited address may accept. A
// forwarded link confers nothing.
//
// The subtlety worth stating before anybody extends this: HOLDING A LINK SENT TO
// alice@corp.com IS PROOF OF CONTROL OF alice@corp.com. It is exactly the proof
// an email confirmation token provides, and this flow relies on that to skip a
// confirmation mail when the invitee has no user yet.
//
// Which means that if the service ever offered "add this invited address to the
// user I am currently signed in as", anybody forwarded the message could attach
// the address to their own user and then accept as its owner. Bound would have
// become bearer with two extra clicks, and it would look correct in every test
// asserting "the accepting user's address matches the invitation". There is no
// such operation, and adding one is the one change to this flow that must not be
// made.
//
// # The token addresses the row; it is not the authority
//
// Resolving a token renders the offer and consumes nothing. Only accepting (or
// declining) consumes it. That is not fastidiousness: links in mail are opened by
// things that are not people — security gateways follow every URL to scan it,
// clients generate previews — and if arrival consumed the token a meaningful
// share of invitations would be dead before their invitee saw them, presenting
// as a mail bug rather than as a decision.
//
// So a leaked link is not a membership. It is an opportunity to attempt an
// acceptance that will be refused unless the attempter can authenticate as the
// invited address.
package invitation

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// Lifetime is how long an invitation stands.
//
// FOURTEEN DAYS, and seven was the first proposal. Seven was rejected for
// exactly the case that matters: the person being invited is usually the one
// with the least free attention, so the invitation arrives the day they go on
// holiday and comes back to a dead link — costing an administrator's time as
// well as theirs. Fourteen survives a holiday, a conference week and a full
// inbox, and is still short enough that a message sitting in an archive is not a
// live credential months later.
//
// IT IS ALSO, NOW, PARTLY A COMMITMENT ABOUT US. An invited person waits for
// approval like anybody else (account-invitations V13), and this clock does not
// pause while they wait. An approval that takes longer than this expires an
// invitation belonging to somebody who did everything right. Lengthening this
// number to hide that would trade a security parameter for an operational one;
// the answer is to approve people faster.
const Lifetime = 14 * 24 * time.Hour

// ErrInvalid is what every failed resolution returns.
//
// Callers MUST render the same response for all of them — unknown, expired,
// revoked, declined, already accepted. The wrapped text names the real reason for
// the log only. A distinguishable response turns the endpoint into an oracle
// answering "was this token ever real", which is the one question the token's
// unguessability is meant to make unanswerable.
var ErrInvalid = errors.New("invitation: not valid")

// NewToken returns a fresh token and the only form of it that is ever stored.
//
// 256 bits from crypto/rand, and SHA-256 rather than a memory-hard hash — the
// same argument internal/apikey makes at length: bcrypt and its relatives exist
// to slow guessing at low-entropy human passwords, and buy nothing against a
// value nobody is going to guess.
//
// The token has no public identifier half, unlike an API key. There is nothing
// to look a row up BY except the hash, which is what HashToken is for.
func NewToken() (token string, hash []byte, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", nil, fmt.Errorf("invitation: generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	return token, HashToken(token), nil
}

// HashToken is the stored form. Never store, log or return the token itself: a
// datastore read must not yield a usable credential.
func HashToken(token string) []byte {
	d := sha256.Sum256([]byte(token))
	return d[:]
}

// TokenMatches compares a candidate token against a stored hash in constant
// time.
//
// The row is found by an indexed equality on the hash, so this comparison is
// arguably redundant today. It is here because the day somebody changes that
// lookup — adds a prefix column, widens it to a range scan, caches it — the
// comparison is what stops the change from also introducing a timing side
// channel, and nobody making that change would think to add it.
func TokenMatches(token string, storedHash []byte) bool {
	got := HashToken(token)
	return subtle.ConstantTimeCompare(got, storedHash) == 1
}

// NormalizeEmail lower-cases and trims an address, and refuses one that is not
// an address.
//
// Normalizing the WHOLE address, local part included, is a deliberate departure
// from the letter of RFC 5321, which permits a case-sensitive local part.
// Essentially no mail system implements one, and treating Alice@corp.com and
// alice@corp.com as two people would produce two invitations, two pending rows
// that the one-outstanding-per-address index cannot see are the same, and one
// confused customer.
//
// A display name is stripped: `Alice <alice@corp.com>` normalizes to
// `alice@corp.com`. Somebody pasting from a mail client should not end up with an
// invitation nothing can match.
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%w: no address given", ErrInvalidAddress)
	}
	a, err := mail.ParseAddress(s)
	if err != nil {
		return "", fmt.Errorf("%w: %q is not an email address", ErrInvalidAddress, s)
	}
	return strings.ToLower(strings.TrimSpace(a.Address)), nil
}

// ErrInvalidAddress is a bad address at the point somebody typed it — a
// different failure from ErrInvalid, which is about a token, and one the caller
// SHOULD report in detail because the person reading it is the person who made
// the typo.
var ErrInvalidAddress = errors.New("invitation: not an email address")

// Mask renders an address for a refusal: `alice@corp.com` becomes
// `a***e@corp.com`.
//
// Somebody holding a forwarded link should not learn the full address from the
// message telling them they may not use it. What survives is enough for the
// legitimate recipient — signed in as the wrong one of their own identities — to
// recognise which address is wanted, which is the entire purpose of showing
// anything at all.
//
// The domain is NOT masked. It is what makes the hint useful, it is usually the
// company that invited them, and the mail already said it.
func Mask(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		// Not something Mask can reason about. Reveal nothing rather than
		// guessing at a shape — an unparseable address here means a bug
		// upstream, and leaking it in a refusal is the wrong way to find out.
		return "***"
	}
	local, domain := email[:at], email[at:]
	switch len(local) {
	case 1:
		return "*" + domain
	case 2:
		return local[:1] + "*" + domain
	default:
		return local[:1] + "***" + local[len(local)-1:] + domain
	}
}
