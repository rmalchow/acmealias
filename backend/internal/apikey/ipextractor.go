package apikey

import (
	"context"
	"fmt"
	"net"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// IPExtractor builds the echo.IPExtractor used for RealIP resolution. It honours
// X-Forwarded-For only for peers inside trustedCIDRs — every other default trust
// (loopback, link-local, private nets) is switched off, because "private" says
// nothing about who can reach this service.
//
// With no trusted proxies configured the extractor still reads the header from
// nobody, which is to say RealIP is the TCP peer address. That is safe, and
// CheckTrustedProxies below is what stops it from silently defeating a CIDR
// restriction someone believed was protecting them.
func IPExtractor(trustedCIDRs []string) (echo.IPExtractor, error) {
	opts := []echo.TrustOption{
		echo.TrustLoopback(false),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(false),
	}
	for _, c := range trustedCIDRs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("apikey: ACME_TRUSTED_PROXY_CIDRS: %q is not a CIDR block: %w", c, err)
		}
		opts = append(opts, echo.TrustIPRange(n))
	}
	return echo.ExtractIPFromXFFHeader(opts...), nil
}

// CheckTrustedProxies fails closed at startup: if any usable key pins itself to
// source addresses while no trusted proxies are configured, the client address
// would come from a forgeable header and the restriction would be decoration.
// Refuse to start rather than display protection that is not there (design D3).
func CheckTrustedProxies(ctx context.Context, db *gorm.DB, trustedCIDRs []string) error {
	if len(trustedCIDRs) > 0 {
		return nil
	}
	// BOTH kinds of address restriction, and the second one is easy to forget:
	// a project key limited to its project's node addresses (design K3) is
	// judged on exactly the same client address as a hand-typed CIDR block, so
	// it is exactly as decorative when that address comes from a forgeable
	// header. Counting only api_key_networks would have let the newer, more
	// convenient restriction be the one with no protection behind it.
	// CROSS-ACCOUNT, and it has to be. This is a startup guard about the whole
	// deployment: it asks whether ANY account holds a key whose address
	// restriction would be judged on a forgeable header. api_keys forces
	// row-level security, so without the lift the policy is
	// `cross_account_read() or account_id = current_account()` with neither set
	// — false for every row — and the count is zero however many restricted keys
	// exist. The guard would then pass silently and display a protection that is
	// not there, which is the exact failure it was written to prevent. It also
	// names the account in nothing, because it deliberately means all of them.
	var n int64
	err := store.CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Model(&model.APIKey{}).
			Where("disabled_at IS NULL").
			Where("expires_at IS NULL OR expires_at > now()").
			Where("restrict_to_project_nodes OR exists (select 1 from api_key_networks n where n.api_key_id = api_keys.id)").
			Count(&n).Error
	})
	if err != nil {
		return fmt.Errorf("apikey: count source-restricted keys: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("apikey: %d active API key(s) restrict their source address but "+
			"ACME_TRUSTED_PROXY_CIDRS is empty; the client address would be taken from a "+
			"forgeable X-Forwarded-For header. Set ACME_TRUSTED_PROXY_CIDRS to the networks "+
			"of the proxies in front of this service, or remove the restrictions", n)
	}
	return nil
}
