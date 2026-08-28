package resolvercheck

import (
	"fmt"
	"net"

	"github.com/miekg/dns"
)

// ResolverAddress turns the configured resolver setting (ACME_VERIFY_RESOLVER)
// into the "host:port" a Checker queries, falling back to the first system
// resolver when the setting is empty. It returns an error only when nothing at
// all could be determined, which the caller reports in its own words: for the
// console that is "not-checked" on every row (domain-setup-feedback D4), and
// for the activation worker it is a reason not to start at all.
//
// It lives here rather than in the one package that used to need it because two
// unrelated components now resolve the same setting, and a second copy of
// "append :53, else read /etc/resolv.conf" is a copy that eventually disagrees —
// a deployment whose console checked one resolver while the worker checked
// another would report a delegation as correct and refuse the domain anyway.
func ResolverAddress(configured string) (string, error) {
	if configured != "" {
		if _, _, err := net.SplitHostPort(configured); err != nil {
			return net.JoinHostPort(configured, "53"), nil
		}
		return configured, nil
	}
	sys, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return "", fmt.Errorf("resolvercheck: no configured resolver and /etc/resolv.conf unreadable: %w", err)
	}
	if len(sys.Servers) == 0 {
		return "", fmt.Errorf("resolvercheck: no configured resolver and /etc/resolv.conf names no nameserver")
	}
	port := sys.Port
	if port == "" {
		port = "53"
	}
	return net.JoinHostPort(sys.Servers[0], port), nil
}
