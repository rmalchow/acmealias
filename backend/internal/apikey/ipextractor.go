package apikey

import (
	"context"
	"fmt"
	"net"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
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
	var n int64
	err := db.WithContext(ctx).
		Model(&model.APIKeyNetwork{}).
		Joins("JOIN api_keys ON api_keys.id = api_key_networks.api_key_id").
		Where("api_keys.disabled_at IS NULL").
		Where("api_keys.expires_at IS NULL OR api_keys.expires_at > now()").
		Count(&n).Error
	if err != nil {
		return fmt.Errorf("apikey: count source-restricted keys: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("apikey: %d active API key CIDR restriction(s) exist but "+
			"ACME_TRUSTED_PROXY_CIDRS is empty; the client address would be taken from a "+
			"forgeable X-Forwarded-For header. Set ACME_TRUSTED_PROXY_CIDRS to the networks "+
			"of the proxies in front of this service, or remove the restrictions", n)
	}
	return nil
}
