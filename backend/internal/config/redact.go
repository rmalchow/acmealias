package config

import (
	"log/slog"
	"net/url"
)

const redacted = "REDACTED"

// LogValue renders the effective configuration for structured logging with
// every secret redacted: the database password (via url.URL.Redacted) and
// the OIDC client secret. Everything else — including CIDRs, group names
// and listen addresses — is operationally useful and not sensitive, so it
// is logged in full.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("zone", c.Zone),
		slog.String("soa_mname", c.SOAMName),
		slog.String("soa_rname", c.SOARName),
		slog.Uint64("soa_serial", uint64(c.SOASerial)),
		slog.Duration("soa_refresh", c.SOARefresh),
		slog.Duration("soa_retry", c.SOARetry),
		slog.Duration("soa_expire", c.SOAExpire),
		slog.Duration("soa_minimum", c.SOAMinimum),
		slog.Any("ns_names", c.NSNames),
		slog.String("dns_listen_addr", c.DNSListenAddr),
		slog.String("listen_addr", c.ListenAddr),
		slog.String("database_url", redactURL(c.DatabaseURL)),
		slog.Bool("migrate_on_start", c.MigrateOnStart),
		slog.Bool("alias_use_domain", c.AliasUseDomain),
		slog.Duration("challenge_ttl", c.ChallengeTTL),
		slog.Duration("challenge_lifetime", c.ChallengeLifetime),
		slog.Duration("zone_view_refresh_interval", c.ZoneViewRefreshInterval),
		slog.Duration("sweep_interval", c.SweepInterval),
		slog.Duration("interaction_retention", c.InteractionRetention),
		slog.Any("trusted_proxy_cidrs", c.TrustedProxyCIDRs),
		slog.String("verification_resolver", c.VerificationResolver),
		slog.Duration("activation_fresh_interval", c.ActivationFreshInterval),
		slog.Duration("activation_settled_interval", c.ActivationSettledInterval),
		slog.String("oidc_issuer", c.OIDCIssuer),
		slog.String("oidc_client_id", c.OIDCClientID),
		slog.String("oidc_client_secret", redactNonEmpty(c.OIDCClientSecret)),
		slog.String("oidc_redirect_url", c.OIDCRedirectURL),
		slog.String("oidc_group_claim", c.OIDCGroupClaim),
		slog.String("oidc_group_prefix", c.OIDCGroupPrefix),
		slog.String("post_logout_redirect_url", c.PostLogoutRedirectURL),
		slog.Duration("session_idle_timeout", c.SessionIdleTimeout),
		slog.Duration("session_max_lifetime", c.SessionMaxLifetime),
		slog.Any("caa_issuers", c.CAAIssuers),
		slog.String("log_level", c.LogLevel),
		slog.Duration("shutdown_grace_period", c.ShutdownGracePeriod),
	)
}

// redactURL hides the password component of a connection string URL while
// keeping the rest (host, database name, query parameters) visible, since
// those are what you need to diagnose a connectivity problem.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		// Unparsable: don't echo a value that might contain a credential.
		return redacted
	}
	return u.Redacted()
}

func redactNonEmpty(s string) string {
	if s == "" {
		return ""
	}
	return redacted
}
