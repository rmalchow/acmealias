package inventory

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// DefaultTTL is the TTL suggested for the records the customer publishes in
// *their* zone. It has nothing to do with the TTL we answer challenges with.
const DefaultTTL = 300

// Record is one row the outside world must publish. Names and CNAME targets
// are stored relative-looking (no trailing dot); RenderZoneSnippet is what
// qualifies them, so structured consumers get the same spelling the rest of
// the inventory uses.
type Record struct {
	Name  string // "shop.example.com", "_acme-challenge.example.com"
	TTL   int
	Type  string // "A" | "AAAA" | "CNAME" | "CAA"
	Value string // an address, a target name, or CAA rdata (`0 issue "letsencrypt.org"`)
}

// PlanOptions is the configuration the plan depends on: the zone this service
// serves (ACME_ZONE), and the CA identifiers CAA rows should authorize
// (ACME_CAA_ISSUER) — empty means emit no CAA rows at all.
type PlanOptions struct {
	Zone       string
	CAAIssuers []string
	TTL        int
}

// DNSPlan is the complete set of records for one load balancer, plus enough context
// to render a legible header.
type DNSPlan struct {
	ProjectSlug      string
	ProjectName      string
	LoadBalancerSlug string
	LoadBalancerName string
	Nodes            []model.LBNode
	Records          []Record
}

// Plan generates the DNS plan for a load balancer (design D5).
func Plan(ctx context.Context, db *gorm.DB, sc store.Scope, loadBalancerID model.ID, opts PlanOptions) (*DNSPlan, error) {
	lb, err := store.GetLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return nil, err
	}
	project, err := store.GetProject(ctx, db, sc, lb.ProjectID)
	if err != nil {
		return nil, err
	}
	domains, err := store.ListDomainsForLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return nil, err
	}
	aliases, err := store.ListAliasesForLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return nil, err
	}
	plan := buildPlan(project, lb, lb.Nodes, domains, aliases, opts)
	return plan, nil
}

// buildPlan is Plan without the database, which is also what makes the record
// rules testable without one.
func buildPlan(project *model.Project, lb *model.LoadBalancer, nodes []model.LBNode, domains []model.Domain, aliases []model.Alias, opts PlanOptions) *DNSPlan {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	zone := strings.TrimSuffix(strings.ToLower(opts.Zone), ".")

	plan := &DNSPlan{
		ProjectSlug: project.Slug, ProjectName: project.Name,
		LoadBalancerSlug: lb.Slug, LoadBalancerName: lb.Name,
		Nodes: nodes,
	}

	// Address records: every domain at every node address, per family. A node
	// missing a family contributes nothing for it — and a dual-stack load balancer that
	// emitted only A records would leave v6 traffic on the customer's old
	// address, which looks like "the site works for most people".
	for _, d := range domains {
		for _, n := range nodes {
			if n.IPv4 != nil {
				plan.Records = append(plan.Records, Record{Name: d.Name, TTL: ttl, Type: "A", Value: *n.IPv4})
			}
		}
		for _, n := range nodes {
			if n.IPv6 != nil {
				plan.Records = append(plan.Records, Record{Name: d.Name, TTL: ttl, Type: "AAAA", Value: *n.IPv6})
			}
		}
	}

	// One CNAME per *distinct* challenge name. Aliases are already keyed by
	// challenge name, so iterating them — rather than the domains — is what
	// collapses `example.com` and `*.example.com` into one row.
	byID := make(map[model.ID]model.Alias, len(aliases))
	for _, a := range aliases {
		byID[a.ID] = a
	}
	seen := make(map[model.ID]bool, len(aliases))
	for _, d := range domains {
		a, ok := byID[d.AliasID]
		if !ok || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		plan.Records = append(plan.Records, Record{
			Name: a.ChallengeFQDN, TTL: ttl, Type: "CNAME", Value: a.Label + "." + zone,
		})
	}

	plan.Records = append(plan.Records, caaRecords(domains, opts.CAAIssuers, ttl)...)
	return plan
}

// caaRecords emits one CAA set per distinct registrable domain covered by the
// plan — never per host, because CAA is inherited and the nearest ancestor
// wins: a set at shop.example.com would shadow the one at example.com for
// that host alone. Both tags are emitted, since a present issuewild governs
// wildcard issuance *instead of* issue, and wildcards are the case this
// service exists for.
func caaRecords(domains []model.Domain, issuers []string, ttl int) []Record {
	if len(issuers) == 0 {
		// No issuer configured: emit nothing rather than guess. A plan naming a
		// CA we do not use would lock issuance out entirely.
		return nil
	}
	var registrable []string
	seen := map[string]bool{}
	for _, d := range domains {
		r, err := dnsname.RegistrableDomain(d.Name)
		if err != nil || seen[r] {
			// A name with no registrable domain (something under a public
			// suffix we cannot resolve) simply gets no CAA row; the rest of
			// the plan is still correct.
			continue
		}
		seen[r] = true
		registrable = append(registrable, r)
	}
	sort.Strings(registrable)

	var out []Record
	for _, r := range registrable {
		for _, tag := range []string{"issue", "issuewild"} {
			for _, issuer := range issuers {
				out = append(out, Record{
					Name: r, TTL: ttl, Type: "CAA",
					Value: fmt.Sprintf("0 %s %q", tag, issuer),
				})
			}
		}
	}
	return out
}

// RenderZoneSnippet renders the plan as a zone-file fragment with
// fully-qualified names, prefixed by a comment header naming the project, the
// load balancer and its nodes.
func RenderZoneSnippet(p *DNSPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "; DNS records for project %s / load balancer %s\n", p.ProjectSlug, p.LoadBalancerSlug)
	if len(p.Nodes) == 0 {
		b.WriteString("; nodes: none configured — this plan has no address records\n")
	} else {
		var nodes []string
		for _, n := range p.Nodes {
			addrs := []string{}
			if n.IPv4 != nil {
				addrs = append(addrs, *n.IPv4)
			}
			if n.IPv6 != nil {
				addrs = append(addrs, *n.IPv6)
			}
			nodes = append(nodes, n.Label+": "+strings.Join(addrs, " / "))
		}
		fmt.Fprintf(&b, "; nodes: %s\n", strings.Join(nodes, ", "))
	}
	b.WriteString("\n")

	// Column widths so the result reads like a zone file rather than a CSV.
	nameWidth, typeWidth := 0, 0
	for _, r := range p.Records {
		if n := len(fqdn(r.Name)); n > nameWidth {
			nameWidth = n
		}
		if n := len(r.Type); n > typeWidth {
			typeWidth = n
		}
	}
	for _, r := range p.Records {
		value := r.Value
		if r.Type == "CNAME" {
			value = fqdn(value)
		}
		fmt.Fprintf(&b, "%-*s  %d  IN  %-*s  %s\n", nameWidth, fqdn(r.Name), r.TTL, typeWidth, r.Type, value)
	}
	return b.String()
}

func fqdn(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}
