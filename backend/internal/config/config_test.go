package config

import (
	"strings"
	"testing"
)

// validEnv returns a minimal set of environment variables satisfying every
// required setting, so individual tests can unset or override just the one
// they're exercising.
func validEnv() map[string]string {
	return map[string]string{
		"ACME_ZONE":               "acmeservice.example.com.",
		"ACME_SOA_PRIMARY_NS":     "ns1.acmeservice.example.com.",
		"ACME_SOA_HOSTMASTER":     "hostmaster.example.com.",
		"ACME_DATABASE_URL":       "postgres://user:pass@localhost:5432/acmeservice",
		"ACME_OIDC_ISSUER":        "https://idp.example.com/realms/acme",
		"ACME_OIDC_CLIENT_ID":     "acmeservice",
		"ACME_OIDC_CLIENT_SECRET": "s3cret",
		"ACME_OIDC_REDIRECT_URL":  "https://acme-console.example.com/api/v1/auth/callback",
	}
}

func withEnv(t *testing.T, overrides map[string]string, unset ...string) {
	t.Helper()
	for k, v := range validEnv() {
		t.Setenv(k, v)
	}
	for k, v := range overrides {
		t.Setenv(k, v)
	}
	for _, k := range unset {
		t.Setenv(k, "")
	}
}

func loadAndValidate(t *testing.T) error {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		return err
	}
	return cfg.Validate()
}

func TestValidConfigPasses(t *testing.T) {
	withEnv(t, nil)
	if err := loadAndValidate(t); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestMissingRequiredSettingNamesIt(t *testing.T) {
	cases := []struct {
		unset string
		want  string
	}{
		{"ACME_ZONE", "ACME_ZONE"},
		{"ACME_SOA_PRIMARY_NS", "ACME_SOA_PRIMARY_NS"},
		{"ACME_SOA_HOSTMASTER", "ACME_SOA_HOSTMASTER"},
		{"ACME_DATABASE_URL", "ACME_DATABASE_URL"},
		{"ACME_OIDC_ISSUER", "ACME_OIDC_ISSUER"},
		{"ACME_OIDC_CLIENT_ID", "ACME_OIDC_CLIENT_ID"},
		{"ACME_OIDC_CLIENT_SECRET", "ACME_OIDC_CLIENT_SECRET"},
		{"ACME_OIDC_REDIRECT_URL", "ACME_OIDC_REDIRECT_URL"},
	}
	for _, tc := range cases {
		t.Run(tc.unset, func(t *testing.T) {
			withEnv(t, nil, tc.unset)
			err := loadAndValidate(t)
			if err == nil {
				t.Fatalf("expected an error for missing %s", tc.unset)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %s", err, tc.want)
			}
		})
	}
}

func TestInvalidZoneNamesTheSetting(t *testing.T) {
	withEnv(t, map[string]string{"ACME_ZONE": "not..a..valid..zone"})
	err := loadAndValidate(t)
	if err == nil || !strings.Contains(err.Error(), "ACME_ZONE") {
		t.Fatalf("expected an ACME_ZONE error, got: %v", err)
	}
}

func TestMalformedDurationNamesTheSetting(t *testing.T) {
	withEnv(t, map[string]string{"ACME_CHALLENGE_TTL": "not-a-duration"})
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ACME_CHALLENGE_TTL") {
		t.Fatalf("expected an ACME_CHALLENGE_TTL error, got: %v", err)
	}
}

func TestNegativeDurationNamesTheSetting(t *testing.T) {
	withEnv(t, map[string]string{"ACME_CHALLENGE_TTL": "-5s"})
	err := loadAndValidate(t)
	if err == nil || !strings.Contains(err.Error(), "ACME_CHALLENGE_TTL") {
		t.Fatalf("expected an ACME_CHALLENGE_TTL error, got: %v", err)
	}
}

func TestInvalidCIDRNamesTheSetting(t *testing.T) {
	withEnv(t, map[string]string{"ACME_TRUSTED_PROXY_CIDRS": "10.0.0.0/8,not-a-cidr"})
	err := loadAndValidate(t)
	if err == nil || !strings.Contains(err.Error(), "ACME_TRUSTED_PROXY_CIDRS") {
		t.Fatalf("expected an ACME_TRUSTED_PROXY_CIDRS error, got: %v", err)
	}
}

func TestInvalidResolverNamesTheSetting(t *testing.T) {
	withEnv(t, map[string]string{"ACME_VERIFY_RESOLVER": "not-host-port"})
	err := loadAndValidate(t)
	if err == nil || !strings.Contains(err.Error(), "ACME_VERIFY_RESOLVER") {
		t.Fatalf("expected an ACME_VERIFY_RESOLVER error, got: %v", err)
	}
}

func TestInvalidOIDCIssuerNamesTheSetting(t *testing.T) {
	withEnv(t, map[string]string{"ACME_OIDC_ISSUER": "not a url"})
	err := loadAndValidate(t)
	if err == nil || !strings.Contains(err.Error(), "ACME_OIDC_ISSUER") {
		t.Fatalf("expected an ACME_OIDC_ISSUER error, got: %v", err)
	}
}

func TestDefaultsApply(t *testing.T) {
	withEnv(t, nil)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DNSListenAddr != ":53" {
		t.Errorf("DNSListenAddr default = %q, want :53", cfg.DNSListenAddr)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr default = %q, want :8080", cfg.ListenAddr)
	}
	if !cfg.MigrateOnStart {
		t.Errorf("MigrateOnStart default = false, want true")
	}
	if cfg.OIDCGroupClaim != "groups" {
		t.Errorf("OIDCGroupClaim default = %q, want groups", cfg.OIDCGroupClaim)
	}
	if cfg.AdminGroup != "" {
		t.Errorf("AdminGroup default = %q, want empty (nobody holds the role)", cfg.AdminGroup)
	}
	if len(cfg.CAAIssuers) != 0 {
		t.Errorf("CAAIssuers default = %v, want empty", cfg.CAAIssuers)
	}
}

func TestRedactionHidesDatabasePasswordAndClientSecret(t *testing.T) {
	withEnv(t, nil)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec := logRecordAttrs(cfg)
	if strings.Contains(rec, "pass") {
		t.Errorf("logged config contains the database password: %s", rec)
	}
	if strings.Contains(rec, "s3cret") {
		t.Errorf("logged config contains the OIDC client secret: %s", rec)
	}
	if !strings.Contains(rec, "REDACTED") {
		t.Errorf("logged config does not show any redaction marker: %s", rec)
	}
}

// logRecordAttrs renders LogValue to a string good enough to substring-search
// in a test, without pulling in a JSON handler and a full slog.Record dance.
func logRecordAttrs(cfg Config) string {
	var sb strings.Builder
	for _, a := range cfg.LogValue().Group() {
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		sb.WriteString(" ")
	}
	return sb.String()
}

// TestAliasUseDomainDefaultsToGenerated pins the default that closes the
// enumeration oracle: unset means generated labels (design A1/A2). A
// deployment that wants readable labels has to say so.
func TestAliasUseDomainDefaultsToGenerated(t *testing.T) {
	withEnv(t, nil, "ACME_ALIAS_USE_DOMAIN")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AliasUseDomain {
		t.Error("ACME_ALIAS_USE_DOMAIN unset should mean generated labels, got derived")
	}

	withEnv(t, map[string]string{"ACME_ALIAS_USE_DOMAIN": "true"})
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.AliasUseDomain {
		t.Error("ACME_ALIAS_USE_DOMAIN=true should mean derived labels, got generated")
	}
}
