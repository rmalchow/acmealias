package obs

import (
	"context"
	"log/slog"
	"os"
)

// NewLogger builds the process-wide structured logger: JSON to stdout at the
// given level ("debug", "info", "warn" or "error"; anything else defaults to
// info).
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

// Outcome event names, for LogOutcome's event argument.
const (
	EventPublish      = "challenge_publish"
	EventWithdraw     = "challenge_withdraw"
	EventAuthzRefusal = "authorization_refusal"
	EventAuthFailure  = "authentication_failure"
)

// LogOutcome records a publish, withdraw, authorization refusal or
// authentication failure with the attribute set the service-operations spec
// requires: the credential identifier, the requested FQDN, the resolved
// alias, and the outcome.
//
// keyIdentifier is the API key's public identifier (e.g. "k_7f3a91c4"), never
// the secret itself. Pass whatever identifier is available — including empty,
// when the presented credential could not even be looked up — but never a
// session ID, a bearer token, or an API key secret: this logs the value it is
// given verbatim, so that's on the caller.
func LogOutcome(ctx context.Context, log *slog.Logger, event, keyIdentifier, fqdn, alias, outcome string) {
	log.LogAttrs(ctx, slog.LevelInfo, event,
		slog.String("key_id", keyIdentifier),
		slog.String("fqdn", fqdn),
		slog.String("alias", alias),
		slog.String("outcome", outcome),
	)
}
