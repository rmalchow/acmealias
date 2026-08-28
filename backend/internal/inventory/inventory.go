// Package inventory is the service layer over internal/store: projects, their
// nodes, domains and aliases, plus the DNS plan a project implies (design D4
// and D5).
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
// project. The message always names the existing owner.
var ErrConflict = errors.New("inventory: conflict")

// ErrNotEmpty is the refusal to delete a parent that still has children
// (spec "Non-empty parent is not deleted").
var ErrNotEmpty = errors.New("inventory: not empty")

// ErrNoRoom is a QUOTA refusal, and it is deliberately not ErrNotPermitted
// (design C13, C11).
//
// The two questions are different and so are their remedies: a permission asks
// "may this caller do it" and is fixed by a grant; a quota asks "is there room"
// and is fixed by deleting something or by paying. Collapsing them produces one
// message that is wrong for both — "ask an administrator for a grant" told to
// somebody who already holds every grant, or "upgrade your account" told to
// somebody whose account is fine and whose role is not.
//
// It maps to 409 rather than 403 for the same reason. See internal/httpapi/errors.go.
var ErrNoRoom = errors.New("inventory: no room")

// Limits is the configured room an account has (design C13).
//
// It is a struct with one field rather than a bare int so that the second limit,
// whenever it arrives, does not change this signature again — and so that the
// call site reads as `lim` rather than as an anonymous number between a subject
// and a slug.
type Limits struct {
	// ProjectsPerPersonalAccount caps a PERSONAL account's projects. Zero or
	// less is no quota. Team accounts are not subject to it at all: credit is
	// their only limit.
	ProjectsPerPersonalAccount int
}

// CreateProject creates a project. The slug is validated as a DNS label —
// lowercase letters, digits and hyphens — because it is what appears in URLs,
// CLI arguments and log lines.
//
// lim is the account's configured room. It is a parameter rather than something
// read from a package-level config because this package takes everything it
// needs from its caller — that is what keeps the API and the CLI from diverging,
// and a quota read from somewhere else would be a rule only one of them obeyed.
func CreateProject(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, lim Limits, slug, name string) (*model.Project, error) {
	// Membership, not a grant (design P14): a project sits at the top of the
	// containment tree, so a grant permitting its creation would have to name
	// the account, and an account is deliberately not an object a grant can
	// name.
	if err := requireUnrestricted(subj, "creating a project"); err != nil {
		return nil, err
	}
	// The quota is checked AFTER authority and BEFORE validation, and the order
	// is the one thing here worth arguing about. After authority, because a
	// caller who may not create projects should not learn how many the account
	// holds. Before validation, because "there is no room" is true whatever the
	// slug says, and telling somebody their slug is malformed when a valid one
	// would have been refused anyway sends them to fix the wrong thing.
	if err := checkProjectQuota(ctx, db, sc, lim); err != nil {
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

// checkProjectQuota refuses a personal account that is full (design C13).
//
// It reads the account to learn its KIND, not to learn who may do what — the
// asymmetry between the two kinds is the whole decision. A team account is
// metered: what it runs draws its balance down, so a project cap on top of that
// would charge twice for the same growth, once in money and once in permission,
// and the second charge buys nothing. The free account is the one that needs a
// number because nothing else limits it.
//
// The refusal names the number and both ways out, because a quota message whose
// only content is "no" leaves the reader to guess whether they should delete
// something or buy something.
func checkProjectQuota(ctx context.Context, db *gorm.DB, sc store.Scope, lim Limits) error {
	if lim.ProjectsPerPersonalAccount <= 0 {
		return nil
	}
	acc, err := store.GetAccount(ctx, db, sc)
	if err != nil {
		return err
	}
	if acc.Kind != model.AccountKindPersonal {
		return nil
	}
	n, err := store.CountProjects(ctx, db, sc)
	if err != nil {
		return err
	}
	if n < int64(lim.ProjectsPerPersonalAccount) {
		return nil
	}
	return fmt.Errorf("%w: a personal account holds %d projects and this one has %d; "+
		"delete one, or use a team account, which has no project limit",
		ErrNoRoom, lim.ProjectsPerPersonalAccount, n)
}

// ListProjects returns the account's projects, each with what is in it.
func ListProjects(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject) ([]store.ProjectListing, error) {
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
func ListProjectsPage(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, query string, limit, offset int) ([]store.ProjectListing, int64, error) {
	return store.ListProjectsACL(ctx, db, sc, acl(subj, authz.ActionViewInventory, authz.TypeProject), query, limit, offset)
}

// DeleteProject refuses a project that still holds domains or aliases. Nodes
// cascade with the project, so they do not block it.
//
// Deleting is governed by MEMBERSHIP rather than by a grant, exactly as creating
// is (design P14), and that survived the collapse deliberately: a project is
// still the top of the containment tree inside an account, so a grant permitting
// its deletion would have to name the account. What changed is that the object
// being deleted now has contents, which is why the emptiness checks below moved
// here from the load balancer.
func DeleteProject(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, id model.ID) error {
	if err := requireUnrestricted(subj, "deleting a project"); err != nil {
		return err
	}
	p, err := store.GetProject(ctx, db, sc, id)
	if err != nil {
		return err
	}
	domains, err := store.ListDomainsForProject(ctx, db, sc, id)
	if err != nil {
		return err
	}
	if len(domains) > 0 {
		return fmt.Errorf("%w: project %q still has %d domain(s), delete them first", ErrNotEmpty, p.Slug, len(domains))
	}
	aliases, err := store.ListAliasesForProject(ctx, db, sc, id)
	if err != nil {
		return err
	}
	if len(aliases) > 0 {
		return fmt.Errorf("%w: project %q still has %d alias(es)", ErrNotEmpty, p.Slug, len(aliases))
	}
	// ponytail: check-then-delete races with a concurrent domain creation; the
	// ON DELETE RESTRICT foreign key is the backstop that makes the race a
	// failed delete rather than an orphaned domain.
	return store.DeleteProject(ctx, db, sc, id)
}

// AddNode adds a node to a project. ipv4 and ipv6 are optional strings, but
// a node with neither would contribute nothing to the DNS plan and is
// refused. Addresses are stored canonicalized.
func AddNode(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, projectID model.ID, label, ipv4, ipv6 string) (*model.LBNode, error) {
	if err := dnsname.ValidateLabel(label); err != nil {
		return nil, fmt.Errorf("node label: %w", err)
	}
	v4, v6, err := parseAddrs(ipv4, ipv6)
	if err != nil {
		return nil, err
	}
	parent, err := store.GetProject(ctx, db, sc, projectID)
	if err != nil {
		return nil, err
	}
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(parent)); err != nil {
		return nil, err
	}
	n := &model.LBNode{ProjectID: projectID, Label: label, IPv4: v4, IPv6: v6}
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
