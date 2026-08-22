// Package inventory is the service layer over internal/store: projects, LB
// lbs, nodes, domains and their aliases, plus the DNS plan a load balancer implies
// (design D4 and D5).
//
// Everything here is a plain function taking the *gorm.DB the caller already
// holds — the HTTP handlers and the CLI both call these directly, so the
// validation rules cannot diverge between them.
package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// ErrInvalid classifies every validation rejection. It is deliberately the
// same sentinel dnsname uses, so one errors.Is in the HTTP error handler
// covers both layers.
var ErrInvalid = dnsname.ErrInvalid

// ErrConflict is a uniqueness or ownership clash: a domain that already
// exists, an alias label already taken, or a challenge name owned by another
// load balancer. The message always names the existing owner.
var ErrConflict = errors.New("inventory: conflict")

// ErrNotEmpty is the refusal to delete a parent that still has children
// (spec "Non-empty parent is not deleted").
var ErrNotEmpty = errors.New("inventory: not empty")

// CreateProject creates a project. The slug is validated as a DNS label —
// lowercase letters, digits and hyphens — because it is what appears in URLs,
// CLI arguments and log lines.
func CreateProject(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, slug, name string) (*model.Project, error) {
	// Membership, not a grant (design P14): a project sits at the top of the
	// containment tree, so a grant permitting its creation would have to name
	// the account, and an account is deliberately not an object a grant can
	// name.
	if err := requireUnrestricted(subj, "creating a project"); err != nil {
		return nil, err
	}
	if err := dnsname.ValidateLabel(slug); err != nil {
		return nil, fmt.Errorf("project slug: %w", err)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: project name is empty", ErrInvalid)
	}
	p := &model.Project{Slug: slug, Name: name}
	if err := store.CreateProject(ctx, db, sc, p); err != nil {
		return nil, err
	}
	return p, nil
}

// ListProjects returns the whole inventory tree: projects, their load balancers,
// and each load balancer's nodes and domains.
func ListProjects(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject) ([]model.Project, error) {
	ps, _, err := ListProjectsPage(ctx, db, sc, subj, "", 1000, 0)
	return ps, err
}

// ListProjectsPage is ListProjects with the console's filter and paging. The
// count it returns is the number of projects MATCHING the filter, not the
// number on the page and not the number in the table — that is what a pager
// has to show to be honest about how much is behind it.
// The listing is ACL-gated in the QUERY (design P13): the caller's grants are
// compiled into a predicate that is part of the WHERE clause, so a project they
// may not see is never returned by the database rather than filtered out
// afterwards.
//
// No permission check precedes it. A subject holding no grant compiles to a
// predicate matching nothing and gets an empty page — which is the right answer
// and is not the same as a refusal: "you may see none of these" and "you may not
// ask" are different, and only the first is true of a listing.
//
// The count runs the same predicate as the page. A total counting rows the
// caller cannot see would leak the existence of invisible objects through a
// number, undoing what returning none of them was protecting.
func ListProjectsPage(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, query string, limit, offset int) ([]model.Project, int64, error) {
	return store.ListProjectsACL(ctx, db, sc, acl(subj, authz.ActionViewInventory, authz.TypeProject), query, limit, offset)
}

// DeleteProject refuses a project that still owns load balancers.
func DeleteProject(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, id model.ID) error {
	if err := requireUnrestricted(subj, "deleting a project"); err != nil {
		return err
	}
	p, err := store.GetProject(ctx, db, sc, id)
	if err != nil {
		return err
	}
	lbs, err := store.ListLoadBalancersForProject(ctx, db, sc, id)
	if err != nil {
		return err
	}
	if len(lbs) > 0 {
		return fmt.Errorf("%w: project %q still has %d load balancer(s), delete them first", ErrNotEmpty, p.Slug, len(lbs))
	}
	// ponytail: check-then-delete races with a concurrent load balancer creation; the
	// ON DELETE RESTRICT foreign key is the backstop that makes the race a
	// failed delete rather than an orphaned lb.
	return store.DeleteProject(ctx, db, sc, id)
}

// CreateLoadBalancer creates a load balancer under a project.
func CreateLoadBalancer(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, projectID model.ID, slug, name string) (*model.LoadBalancer, error) {
	if err := dnsname.ValidateLabel(slug); err != nil {
		return nil, fmt.Errorf("load balancer slug: %w", err)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: load balancer name is empty", ErrInvalid)
	}
	// Creating is authorized on the PARENT: there is no object yet to hold a
	// grant, and "may write inventory in this project" is what confers the
	// ability to add a load balancer to it.
	parent, err := store.GetProject(ctx, db, sc, projectID)
	if err != nil {
		return nil, err
	}
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(parent)); err != nil {
		return nil, err
	}
	p := &model.LoadBalancer{ProjectID: projectID, Slug: slug, Name: name}
	if err := store.CreateLoadBalancer(ctx, db, sc, p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteLoadBalancer refuses a load balancer that still owns domains or aliases. Nodes
// cascade with the load balancer, so they do not block it.
func DeleteLoadBalancer(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, id model.ID) error {
	lb, err := store.GetLoadBalancer(ctx, db, sc, id)
	if err != nil {
		return err
	}
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(lb)); err != nil {
		return err
	}
	domains, err := store.ListDomainsForLoadBalancer(ctx, db, sc, id)
	if err != nil {
		return err
	}
	if len(domains) > 0 {
		return fmt.Errorf("%w: load balancer %q still has %d domain(s), delete them first", ErrNotEmpty, lb.Slug, len(domains))
	}
	aliases, err := store.ListAliasesForLoadBalancer(ctx, db, sc, id)
	if err != nil {
		return err
	}
	if len(aliases) > 0 {
		return fmt.Errorf("%w: load balancer %q still has %d alias(es)", ErrNotEmpty, lb.Slug, len(aliases))
	}
	return store.DeleteLoadBalancer(ctx, db, sc, id)
}

// AddNode adds a node to a load balancer. ipv4 and ipv6 are optional strings, but
// a node with neither would contribute nothing to the DNS plan and is
// refused. Addresses are stored canonicalized.
func AddNode(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, loadBalancerID model.ID, label, ipv4, ipv6 string) (*model.LBNode, error) {
	if err := dnsname.ValidateLabel(label); err != nil {
		return nil, fmt.Errorf("node label: %w", err)
	}
	v4, v6, err := parseAddrs(ipv4, ipv6)
	if err != nil {
		return nil, err
	}
	parent, err := store.GetLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return nil, err
	}
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(parent)); err != nil {
		return nil, err
	}
	n := &model.LBNode{LoadBalancerID: loadBalancerID, Label: label, IPv4: v4, IPv6: v6}
	if err := store.CreateLBNode(ctx, db, sc, n); err != nil {
		return nil, err
	}
	return n, nil
}

// UpdateNodeAddresses replaces a node's addresses (the renumbering case).
func UpdateNodeAddresses(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, nodeID model.ID, ipv4, ipv6 string) (*model.LBNode, error) {
	v4, v6, err := parseAddrs(ipv4, ipv6)
	if err != nil {
		return nil, err
	}
	// Scoped read, not a bare First: the node has to be one of this account's
	// before anything is decided about it, and the unscoped version reached any
	// node in the deployment by identifier alone.
	n, err := store.GetLBNode(ctx, db, sc, nodeID)
	if err != nil {
		return nil, err
	}
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(n)); err != nil {
		return nil, err
	}
	n.IPv4, n.IPv6 = v4, v6
	if err := store.UpdateLBNode(ctx, db, sc, n); err != nil {
		return nil, err
	}
	return n, nil
}

// DeleteNode removes a node.
func DeleteNode(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, nodeID model.ID) error {
	n, err := store.GetLBNode(ctx, db, sc, nodeID)
	if err != nil {
		return err
	}
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(n)); err != nil {
		return err
	}
	return store.DeleteLBNode(ctx, db, sc, nodeID)
}

// parseAddrs validates the two optional address strings, insisting that each
// is of the family its column stands for — an IPv6 address quietly stored in
// ipv4 would emit an A record with a v6 value.
func parseAddrs(ipv4, ipv6 string) (*string, *string, error) {
	var v4, v6 *string
	if ipv4 != "" {
		a, err := netip.ParseAddr(ipv4)
		if err != nil || !a.Is4() {
			return nil, nil, fmt.Errorf("%w: %q is not an IPv4 address", ErrInvalid, ipv4)
		}
		s := a.String()
		v4 = &s
	}
	if ipv6 != "" {
		a, err := netip.ParseAddr(ipv6)
		if err != nil || !a.Is6() || a.Is4In6() {
			return nil, nil, fmt.Errorf("%w: %q is not an IPv6 address", ErrInvalid, ipv6)
		}
		s := a.String()
		v6 = &s
	}
	if v4 == nil && v6 == nil {
		return nil, nil, fmt.Errorf("%w: a node needs at least one address", ErrInvalid)
	}
	return v4, v6, nil
}
