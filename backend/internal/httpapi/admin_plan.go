package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/miekg/dns"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/resolvercheck"
)

// The DNS plan (design D5): structured, rendered, and checked against live DNS.

type recordView struct {
	Name  string `json:"name"`
	TTL   int    `json:"ttl"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type planView struct {
	ProjectSlug      string       `json:"projectSlug"`
	ProjectName      string       `json:"projectName"`
	LoadBalancerSlug string       `json:"loadBalancerSlug"`
	LoadBalancerName string       `json:"loadBalancerName"`
	Records          []recordView `json:"records"`
	Zone             string       `json:"zone"`
}

func (a *admin) plan(c echo.Context) (*inventory.DNSPlan, error) {
	id, err := idParam(c, "id")
	if err != nil {
		return nil, err
	}
	return inventory.Plan(c.Request().Context(), a.db(), id, a.planOptions())
}

func (a *admin) getPlan(c echo.Context) error {
	p, err := a.plan(c)
	if err != nil {
		return err
	}
	v := planView{
		ProjectSlug: p.ProjectSlug, ProjectName: p.ProjectName,
		LoadBalancerSlug: p.LoadBalancerSlug, LoadBalancerName: p.LoadBalancerName,
		Records: make([]recordView, 0, len(p.Records)),
		Zone:    a.opts.Config.Zone,
	}
	for _, r := range p.Records {
		v.Records = append(v.Records, recordView{Name: r.Name, TTL: r.TTL, Type: r.Type, Value: r.Value})
	}
	return c.JSON(http.StatusOK, v)
}

// getPlanZone serves the same plan as the zone-file snippet the customer's DNS
// administrator can paste. text/plain, so a browser shows it and `curl >
// file` produces something usable.
func (a *admin) getPlanZone(c echo.Context) error {
	p, err := a.plan(c)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderContentDisposition,
		`attachment; filename="`+p.ProjectSlug+`-`+p.LoadBalancerSlug+`.zone"`)
	return c.String(http.StatusOK, inventory.RenderZoneSnippet(p))
}

// --- live verification -----------------------------------------------------

// checkResult is one verified row of the plan. It is deliberately not
// one-per-plan-record: a name's four A records are one question to DNS and one
// answer, and reporting them as four independent "incorrect" rows would say
// the same thing four times without saying which address was missing.
//
// Status carries resolvercheck's vocabulary unchanged, including CAA's
// "not-published", which the console must render as information rather than as
// a failure: an absent CAA record is the default state of the internet and
// blocks nothing, while a CAA record naming another authority is the single
// clearest blocker this check can find (design D5).
type checkResult struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Expected []string `json:"expected"`
	Status   string   `json:"status"`
	Found    string   `json:"found,omitempty"`
}

func (a *admin) verifyPlan(c echo.Context) error {
	p, err := a.plan(c)
	if err != nil {
		return err
	}
	if a.resolver == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable,
			"no verification resolver is available: set ACME_VERIFICATION_RESOLVER to a host:port")
	}
	checker := &resolvercheck.Checker{Resolver: a.resolver}

	out := make([]checkResult, 0, len(p.Records))
	for _, group := range groupForChecks(p.Records, a.opts.Config.CAAIssuers) {
		out = append(out, group.run(checker))
	}
	return c.JSON(http.StatusOK, map[string]any{"resolver": a.resolver, "results": out})
}

// planCheck is one question to ask DNS. Exactly one of the three is set.
type planCheck struct {
	name     string
	kind     string
	expected []string

	cname   *resolvercheck.CNAMECheck
	address *resolvercheck.AddressCheck
	caa     *resolvercheck.CAACheck
}

func (p planCheck) run(ch *resolvercheck.Checker) checkResult {
	res := checkResult{Name: p.name, Type: p.kind, Expected: p.expected}
	switch {
	case p.cname != nil:
		r := ch.CheckCNAME(*p.cname)
		res.Status, res.Found = string(r.Status), r.Found
	case p.address != nil:
		r := ch.CheckAddress(*p.address)
		res.Status, res.Found = string(r.Status), r.Found
	case p.caa != nil:
		r := ch.CheckCAA(*p.caa)
		res.Status, res.Found = string(r.Status), r.Found
	}
	return res
}

// groupForChecks turns plan rows into the checks resolvercheck actually takes.
//
// The plan emits one row per address per name (design D5's table), but an
// AddressCheck compares the *whole* answer set for a name and family at once,
// so the rows have to be gathered back up. CAA rows are collapsed the same
// way: the plan emits one row per tag per issuer at a registrable domain, and
// one CAACheck covers the set.
//
// ponytail: this adapter lives here because the management API is its only
// caller — there is no CLI verification subcommand in this change. If one is
// ever added, move it to internal/inventory beside Plan rather than copying it.
func groupForChecks(records []inventory.Record, issuers []string) []planCheck {
	var out []planCheck
	addrIdx := map[string]int{} // "name|family" -> index into out
	caaSeen := map[string]bool{}

	for _, r := range records {
		switch r.Type {
		case "CNAME":
			out = append(out, planCheck{
				name: r.Name, kind: "CNAME", expected: []string{r.Value},
				cname: &resolvercheck.CNAMECheck{Name: r.Name, Expected: dns.Fqdn(r.Value)},
			})
		case "A", "AAAA":
			key := r.Name + "|" + r.Type
			ip := net.ParseIP(r.Value)
			if ip == nil {
				continue // an unparsable address in the plan is a data bug, not a DNS finding
			}
			if i, ok := addrIdx[key]; ok {
				out[i].expected = append(out[i].expected, r.Value)
				out[i].address.Want = append(out[i].address.Want, ip)
				continue
			}
			addrIdx[key] = len(out)
			out = append(out, planCheck{
				name: r.Name, kind: r.Type, expected: []string{r.Value},
				address: &resolvercheck.AddressCheck{Name: r.Name, Family: r.Type, Want: []net.IP{ip}},
			})
		case "CAA":
			if caaSeen[r.Name] {
				continue
			}
			caaSeen[r.Name] = true
			out = append(out, planCheck{
				name: r.Name, kind: "CAA", expected: issuers,
				caa: &resolvercheck.CAACheck{Domain: r.Name, Issuers: issuers},
			})
		}
	}
	return out
}

// verificationResolver decides which resolver live checks query, once, at
// registration.
//
// ACME_VERIFICATION_RESOLVER wins. When it is unset the design says "the
// system resolver", which for a DNS library means reading the same
// /etc/resolv.conf the stdlib would: resolvercheck speaks to a named server,
// it has no "ask the OS" mode. When neither is available the verification
// route says so rather than answering "undetermined" for everything, which
// would read as a customer-side problem.
func verificationResolver(cfg config.Config, log *slog.Logger) string {
	if addr := strings.TrimSpace(cfg.VerificationResolver); addr != "" {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, "53")
		}
		return addr
	}
	sys, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil || len(sys.Servers) == 0 {
		log.Warn("no DNS verification resolver: ACME_VERIFICATION_RESOLVER is unset and no system resolver could be read",
			slog.Any("error", err))
		return ""
	}
	port := sys.Port
	if port == "" {
		port = "53"
	}
	return net.JoinHostPort(sys.Servers[0], port)
}
