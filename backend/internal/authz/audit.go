package authz

import (
	"cmp"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Where an audited action came from.
const (
	SourceConsole = "console"
	SourceCLI     = "cli"
	SourceSystem  = "system"
)

// Actor is who did it.
//
// Roles is recorded alongside Groups deliberately, and it is the answer to "how
// was this person allowed to do that" six months later. It used to come from the
// group-to-role mapping, derived per request; it now comes from the roles they
// held in the account at the time (SubjectForUser), which is a stronger record
// because those rows are the authority rather than a projection of it.
//
// Groups is kept even though nothing grants on it any more: it is what the
// identity provider said about this person at sign-in, and an entry that records
// only what we concluded cannot be checked against what we were told.
type Actor struct {
	Subject     string
	DisplayName string
	Groups      []string
	Roles       []string
	Source      string
	RequestID   string
	ClientIP    string
	// AccountID is the account the action happened inside, so a tenant reading
	// their own trail sees their own entries and nobody else's. Nil for actions
	// that belong to no account — the expiry sweep before it knows whose key it
	// is, and anything the CLI does before an account is resolved.
	AccountID *model.ID
}

// InAccount returns a copy of the actor stamped with the account the action is
// happening inside. It is a copy rather than a setter because an Actor is passed
// by value everywhere and a mutating setter would silently apply to one caller's
// copy only.
func (a Actor) InAccount(id model.ID) Actor {
	a.AccountID = &id
	return a
}

// ConsoleActor builds an Actor from a request that passed Authenticated.
//
// The roles are passed in rather than read off the request context, because
// there is no longer a set of them that is true independently of an account: the
// caller resolves the account first, and the roles it hands over are the ones
// the request was authorized against.
func ConsoleActor(c echo.Context, roles []string) Actor {
	sess := SessionFromContext(c)
	a := Actor{
		Roles:     roles,
		Source:    SourceConsole,
		RequestID: requestID(c),
		ClientIP:  c.RealIP(),
	}
	if sess != nil {
		a.Subject, a.DisplayName, a.Groups = sess.Subject, sess.DisplayName, sess.Groups
	}
	return a
}

// SystemActor is the actor for something the service did on its own — a sweep,
// a scheduled job. It has no subject and no roles.
func SystemActor() Actor { return Actor{Source: SourceSystem} }

func requestID(c echo.Context) string {
	if id := c.Response().Header().Get(echo.HeaderXRequestID); id != "" {
		return id
	}
	return c.Request().Header.Get(echo.HeaderXRequestID)
}

// Event is what happened. ObjectLabel is denormalized so the entry stays
// readable after the object is deleted.
//
// Before and After are arbitrary values, JSON-encoded; they are scrubbed of
// secret-looking fields before they are written (see scrub).
type Event struct {
	Action      string
	ObjectType  string
	ObjectID    *model.ID
	ObjectLabel string
	ProjectID   *model.ID
	Before      any
	After       any
}

// Record appends one audit row. Callers should treat a failure here as a
// failure of the action itself: a privileged change that was not recorded is
// worse than one that did not happen.
//
// m may be nil for a caller that has no metrics registry — the CLI is the one
// such caller. Every long-running path MUST pass it: a counter that is
// incremented on some write paths and not others reports plausible numbers
// while missing most of what it claims to count.
func Record(ctx context.Context, db *gorm.DB, m *obs.Metrics, a Actor, e Event) error {
	before, err := scrub(e.Before)
	if err != nil {
		return err
	}
	after, err := scrub(e.After)
	if err != nil {
		return err
	}

	entry := &model.AuditEntry{
		OccurredAt:   time.Now().UTC(),
		AccountID:    a.AccountID,
		ActorSubject: nonEmpty(a.Subject),
		ActorName:    nonEmpty(a.DisplayName),
		ActorGroups:  a.Groups,
		// Role NAMES as written, not references. A role is a row now and rows
		// are edited and deleted; an entry that pointed at one would become
		// unreadable exactly when it mattered — after somebody removed the role
		// whose authority is being questioned (task 8.3).
		ActorRoles:  a.Roles,
		ActorSource: cmp.Or(a.Source, SourceSystem),
		RequestID:   nonEmpty(a.RequestID),
		ClientIP:    nonEmpty(a.ClientIP),
		Action:      e.Action,
		ObjectType:  e.ObjectType,
		ObjectID:    e.ObjectID,
		ObjectLabel: e.ObjectLabel,
		ProjectID:   e.ProjectID,
		Before:      before,
		After:       after,
	}
	if err := store.AppendAuditEntry(ctx, db, entry); err != nil {
		return err
	}
	// ponytail: counted here, inside the caller's transaction, so every writer
	// gets the counter for free. The ceiling is that a transaction that rolls
	// back after this point leaves the count one too high — audit writes are
	// the last thing in each of the three transactions and a rollback after
	// one is a failing action, so the drift is bounded by the failure rate. If
	// that ever matters, hand callers a post-commit hook instead.
	if m != nil {
		m.AuditEntries.WithLabelValues(cmp.Or(ActionClass(e.Action), obs.ActionClassUnknown)).Inc()
	}
	return nil
}

// secretish names the field names that must never reach an audit row. The
// match is a case-insensitive substring, so "secret", "SecretHash" and
// "api_key_secret" all go.
var secretish = []string{"secret", "token", "password", "passphrase", "credential", "cookie", "session_id", "sessionid", "private_key", "privatekey"}

// scrub JSON-encodes v and replaces any secret-looking field with "[redacted]",
// at any depth.
//
// ponytail: a name-based filter, applied at the one place audit rows are
// written, rather than trusting every caller to hand over a clean struct. It
// catches the fields this schema actually has. Its ceiling is that a secret
// stored under an innocuous name still gets through — if that ever becomes a
// risk, give Event an explicit allowlist of fields per ObjectType instead.
func scrub(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return json.Marshal(redact(decoded))
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, sub := range t {
			if isSecretish(k) {
				t[k] = "[redacted]"
				continue
			}
			t[k] = redact(sub)
		}
		return t
	case []any:
		for i, sub := range t {
			t[i] = redact(sub)
		}
		return t
	default:
		return v
	}
}

func isSecretish(key string) bool {
	k := strings.ToLower(key)
	for _, s := range secretish {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
