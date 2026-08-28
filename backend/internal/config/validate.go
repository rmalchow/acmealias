package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Validate checks Config for internal consistency: required values are
// present, the zone is a syntactically valid absolute DNS name, durations
// are positive, CIDRs and the verification resolver address parse, and the
// OIDC issuer parses as a URL. It makes no network call and checks no
// external service, so an unavailable dependency can never stop startup
// (design: "Configuration comes from the environment and is validated at
// startup").
//
// Every failure names the offending setting; Validate collects all of them
// rather than stopping at the first.
func (c Config) Validate() error {
	var errs loadErrors

	required := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			errs.add(name, fmt.Errorf("required"))
		}
	}

	required("ACME_ZONE", c.Zone)
	if c.Zone != "" {
		if err := validateAbsoluteDNSName(c.Zone); err != nil {
			errs.add("ACME_ZONE", err)
		}
	}

	required("ACME_SOA_PRIMARY_NS", c.SOAMName)
	required("ACME_SOA_HOSTMASTER", c.SOARName)
	required("ACME_DATABASE_URL", c.DatabaseURL)
	if c.DatabaseURL != "" {
		if _, err := url.Parse(c.DatabaseURL); err != nil {
			errs.add("ACME_DATABASE_URL", err)
		}
	}

	required("ACME_OIDC_ISSUER", c.OIDCIssuer)
	if c.OIDCIssuer != "" {
		u, err := url.Parse(c.OIDCIssuer)
		if err != nil {
			errs.add("ACME_OIDC_ISSUER", err)
		} else if u.Scheme == "" || u.Host == "" {
			errs.add("ACME_OIDC_ISSUER", fmt.Errorf("must be an absolute URL"))
		}
	}
	required("ACME_OIDC_CLIENT_ID", c.OIDCClientID)
	required("ACME_OIDC_CLIENT_SECRET", c.OIDCClientSecret)
	required("ACME_OIDC_REDIRECT_URL", c.OIDCRedirectURL)
	if c.OIDCRedirectURL != "" {
		if u, err := url.Parse(c.OIDCRedirectURL); err != nil {
			errs.add("ACME_OIDC_REDIRECT_URL", err)
		} else if u.Scheme == "" || u.Host == "" {
			errs.add("ACME_OIDC_REDIRECT_URL", fmt.Errorf("must be an absolute URL"))
		}
	}

	// Not required — a deployment may legitimately clear it, which turns
	// sign-out back into "destroy our session and go to /" without ending the
	// provider's. Malformed is a different matter: the identity provider would
	// refuse the logout request at the moment somebody used it, which is the
	// worst time to find out.
	if c.PostLogoutRedirectURL != "" {
		if u, err := url.Parse(c.PostLogoutRedirectURL); err != nil {
			errs.add("ACME_POST_LOGOUT_REDIRECT_URL", err)
		} else if u.Scheme == "" || u.Host == "" {
			errs.add("ACME_POST_LOGOUT_REDIRECT_URL", fmt.Errorf("must be an absolute URL"))
		}
	}

	positive := func(name string, d interface{ Seconds() float64 }) {
		if d.Seconds() <= 0 {
			errs.add(name, fmt.Errorf("must be positive"))
		}
	}
	positive("ACME_SOA_REFRESH", c.SOARefresh)
	positive("ACME_SOA_RETRY", c.SOARetry)
	positive("ACME_SOA_EXPIRE", c.SOAExpire)
	positive("ACME_SOA_MINIMUM", c.SOAMinimum)
	positive("ACME_CHALLENGE_TTL", c.ChallengeTTL)
	positive("ACME_CHALLENGE_LIFETIME", c.ChallengeLifetime)
	positive("ACME_VIEW_REFRESH_INTERVAL", c.ZoneViewRefreshInterval)
	positive("ACME_SWEEP_INTERVAL", c.SweepInterval)
	positive("ACME_ACTIVATION_FRESH_INTERVAL", c.ActivationFreshInterval)
	positive("ACME_ACTIVATION_SETTLED_INTERVAL", c.ActivationSettledInterval)
	positive("ACME_SESSION_IDLE_TIMEOUT", c.SessionIdleTimeout)
	positive("ACME_SESSION_MAX_LIFETIME", c.SessionMaxLifetime)
	positive("ACME_SHUTDOWN_GRACE", c.ShutdownGracePeriod)
	if c.InteractionRetention < 0 {
		errs.add("ACME_INTERACTION_RETENTION", fmt.Errorf("must not be negative"))
	}

	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs.add("ACME_TRUSTED_PROXY_CIDRS", fmt.Errorf("%q: %w", cidr, err))
		}
	}

	if c.VerificationResolver != "" {
		host, port, err := net.SplitHostPort(c.VerificationResolver)
		if err != nil {
			errs.add("ACME_VERIFY_RESOLVER", err)
		} else if host == "" || port == "" {
			errs.add("ACME_VERIFY_RESOLVER", fmt.Errorf("must be host:port"))
		}
	}

	for _, addr := range []struct{ name, value string }{
		{"ACME_DNS_LISTEN", c.DNSListenAddr},
		{"ACME_LISTEN", c.ListenAddr},
	} {
		if _, _, err := net.SplitHostPort(addr.value); err != nil {
			errs.add(addr.name, err)
		}
	}

	return errs.errOrNil()
}

// validateAbsoluteDNSName is a minimal, local check that name is a
// syntactically valid, non-empty, absolute (trailing-dot-normalizable) DNS
// name. Full normalization (case-folding, label rules, wildcard handling)
// lives in internal/dnsname (task group 3); this is only the config-time
// sanity check that a malformed zone value is rejected before startup.
func validateAbsoluteDNSName(name string) error {
	trimmed := strings.TrimSuffix(name, ".")
	if trimmed == "" {
		return fmt.Errorf("must not be empty")
	}
	if len(trimmed) > 253 {
		return fmt.Errorf("exceeds 253 octets")
	}
	for _, label := range strings.Split(trimmed, ".") {
		if label == "" {
			return fmt.Errorf("contains an empty label")
		}
		if len(label) > 63 {
			return fmt.Errorf("label %q exceeds 63 octets", label)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("label %q has a leading or trailing hyphen", label)
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return fmt.Errorf("label %q contains an invalid character", label)
			}
		}
	}
	return nil
}
