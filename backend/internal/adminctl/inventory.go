package adminctl

import (
	"context"
	"fmt"
	"io"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// findProjectBySlug is a lookup, not a validation rule, so it lives here
// rather than in internal/inventory: store has no BySlug query, and the CLI
// is the only caller that ever needs to turn an operator-typed slug into an
// id.
func findProjectBySlug(ctx context.Context, db *gorm.DB, sc store.Scope, slug string) (*model.Project, error) {
	ps, err := store.ListProjects(ctx, db, sc)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		if ps[i].Slug == slug {
			return &ps[i], nil
		}
	}
	return nil, fmt.Errorf("no project with slug %q", slug)
}

// resolveProjectRef resolves the --project value the CLI accepts everywhere: a
// bare identifier, or the slug the operator sees in `project list` output.
//
// It used to take "project-slug/loadbalancer-slug", and the slash went with the
// level. An operator who types one gets told so rather than being told the whole
// string is not a slug, because a two-part reference is what every note, script
// and runbook written before this change still says.
func resolveProjectRef(ctx context.Context, db *gorm.DB, sc store.Scope, ref string) (*model.Project, error) {
	if id, err := model.ParseID(ref); err == nil {
		return store.GetProject(ctx, db, sc, id)
	}
	if projectSlug, lbSlug, ok := strings.Cut(ref, "/"); ok {
		return nil, fmt.Errorf("--project %q: there are no load balancers any more, a project IS one; "+
			"name the project alone (%q was the project, %q the load balancer inside it)",
			ref, projectSlug, lbSlug)
	}
	return findProjectBySlug(ctx, db, sc, ref)
}

// ProjectCreate creates a project and prints the result.
//
// lim is the deployment's configured quota, passed in rather than assumed away.
// The CLI is subject to it for the same reason it is subject to every other rule
// in internal/inventory: a limit the API enforces and the CLI walks past is a
// limit that holds until somebody uses the CLI (design P7, C13).
func ProjectCreate(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, lim inventory.Limits, slug, name string) error {
	var p *model.Project
	err := mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		var err error
		p, err = inventory.CreateProject(ctx, tx, sc, authz.CLISubject(sc.AccountID()), lim, slug, name)
		if err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionProjectCreate, ObjectType: authz.ObjectProject,
			ObjectID: &p.ID, ObjectLabel: p.Slug, ProjectID: &p.ID, After: p,
		}}, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "created project %q (id %s): %s\n", p.Slug, p.ID, p.Name)
	return nil
}

// ProjectList prints every project with its nodes and domains.
func ProjectList(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer) error {
	ps, err := inventory.ListProjects(ctx, db, sc, authz.CLISubject(sc.AccountID()))
	if err != nil {
		return err
	}
	if len(ps) == 0 {
		fmt.Fprintln(out, "no projects")
		return nil
	}
	for _, p := range ps {
		fmt.Fprintf(out, "project %s (id %s) %q — %s\n", p.Slug, p.ID, p.Name, summarize(p.NodeCount, p.DomainCount))
		nodes, err := store.ListLBNodesForProject(ctx, db, sc, p.ID)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			fmt.Fprintf(out, "  node %s (id %s) %s\n", n.Label, n.ID, formatAddrs(n))
		}
		domains, err := store.ListDomainsForProject(ctx, db, sc, p.ID)
		if err != nil {
			return err
		}
		for _, d := range domains {
			fmt.Fprintf(out, "  domain %s (id %s)\n", d.Name, d.ID)
		}
	}
	return nil
}

// summarize says what is in a project, and says EMPTY rather than two zeroes
// (design L5). A freshly created project is the commonest state in the product
// and it is the one with an obvious next action; "0 nodes, 0 domains" reads as a
// measurement where "empty" reads as an instruction.
func summarize(nodes, domains int) string {
	if nodes == 0 && domains == 0 {
		return "empty: no nodes, no domains"
	}
	return fmt.Sprintf("%d node(s), %d domain(s)", nodes, domains)
}

func formatAddrs(n model.LBNode) string {
	var parts []string
	if n.IPv4 != nil {
		parts = append(parts, *n.IPv4)
	}
	if n.IPv6 != nil {
		parts = append(parts, *n.IPv6)
	}
	return strings.Join(parts, " / ")
}

// ProjectDelete deletes a project by slug.
func ProjectDelete(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, slug string) error {
	p, err := findProjectBySlug(ctx, db, sc, slug)
	if err != nil {
		return err
	}
	err = mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := inventory.DeleteProject(ctx, tx, sc, authz.CLISubject(sc.AccountID()), p.ID); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionProjectDelete, ObjectType: authz.ObjectProject,
			ObjectID: &p.ID, ObjectLabel: p.Slug, ProjectID: &p.ID, Before: p,
		}}, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted project %q (id %s)\n", p.Slug, p.ID)
	return nil
}

// NodeAdd adds a node to a project.
func NodeAdd(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, ref, label, ipv4, ipv6 string) error {
	p, err := resolveProjectRef(ctx, db, sc, ref)
	if err != nil {
		return err
	}
	var n *model.LBNode
	err = mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		var err error
		n, err = inventory.AddNode(ctx, tx, sc, authz.CLISubject(sc.AccountID()), p.ID, label, ipv4, ipv6)
		if err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionNodeCreate, ObjectType: authz.ObjectNode,
			ObjectID: &n.ID, ObjectLabel: p.Slug + "/" + n.Label, ProjectID: &p.ID, After: n,
		}}, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "added node %q (id %s) to project %q: %s\n", n.Label, n.ID, p.Slug, formatAddrs(*n))
	return nil
}

// NodeDelete removes a node by id.
func NodeDelete(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, id model.ID) error {
	err := mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		// Read the node first: an entry that names only an id is unreadable
		// the moment the row it points at is gone.
		var n model.LBNode
		if err := tx.WithContext(ctx).First(&n, "id = ?", id).Error; err != nil {
			return nil, fmt.Errorf("get lb node %s: %w", id, err)
		}
		if err := inventory.DeleteNode(ctx, tx, sc, authz.CLISubject(sc.AccountID()), id); err != nil {
			return nil, err
		}
		projectID := n.ProjectID
		return []authz.Event{{
			Action: authz.ActionNodeDelete, ObjectType: authz.ObjectNode,
			ObjectID: &id, ObjectLabel: n.Label, ProjectID: &projectID, Before: n,
		}}, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted node id %s\n", id)
	return nil
}

// DomainCreate adds a domain to a project — with its counterpart, when
// includeWildcard is set — and prints the CNAME record the customer must
// publish. This is the output an operator copies verbatim into an email, so it
// is formatted exactly as it should be pasted. A pair prints two domain lines
// and still one CNAME, because the two names share one alias.
func DomainCreate(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, zone string, useDomainLabel bool, ref, name string, includeWildcard bool) error {
	p, err := resolveProjectRef(ctx, db, sc, ref)
	if err != nil {
		return err
	}
	var result *inventory.DomainCreation
	err = mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		var err error
		result, err = inventory.CreateDomain(ctx, tx, sc, authz.CLISubject(sc.AccountID()), p.ID, name, useDomainLabel, includeWildcard)
		if err != nil {
			return nil, err
		}
		// One entry per created domain: a pair is two objects (design B1).
		events := make([]authz.Event, 0, len(result.Domains)+1)
		for i, d := range result.Domains {
			events = append(events, authz.Event{
				Action: authz.ActionDomainCreate, ObjectType: authz.ObjectDomain,
				ObjectID: &result.Domains[i].ID, ObjectLabel: d.Name,
				ProjectID: &p.ID, After: d,
			})
		}
		if result.AliasCreated {
			events = append(events, authz.Event{
				Action: authz.ActionAliasCreate, ObjectType: authz.ObjectAlias,
				ObjectID: &result.Alias.ID, ObjectLabel: result.Alias.Label,
				ProjectID: &p.ID, After: result.Alias,
			})
		}
		return events, nil
	})
	if err != nil {
		return err
	}

	for _, d := range result.Domains {
		fmt.Fprintf(out, "created domain %q in project %q\n", d.Name, p.Slug)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	fmt.Fprintln(out, "\nPublish this CNAME record for the domain to validate:")
	fmt.Fprintln(out, cnameLine(result.Alias.ChallengeFQDN, result.Alias.Label, zone))
	return nil
}

// cnameLine renders one publishable CNAME record line, fully qualified, in
// zone-file shape. It is the surviving half of the DNS plan the CLI used to
// print: the one record a customer has to publish for a domain they just
// created, said at the moment they need it.
func cnameLine(challengeFQDN, label, zone string) string {
	target := label
	if zone != "" {
		target = label + "." + strings.TrimSuffix(zone, ".")
	}
	return fmt.Sprintf("%s.  %d  IN  CNAME  %s.", challengeFQDN, inventory.DefaultTTL, target)
}

// DomainList prints every domain in a project.
func DomainList(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, ref string) error {
	p, err := resolveProjectRef(ctx, db, sc, ref)
	if err != nil {
		return err
	}
	domains, err := store.ListDomainsForProject(ctx, db, sc, p.ID)
	if err != nil {
		return err
	}
	if len(domains) == 0 {
		fmt.Fprintln(out, "no domains")
		return nil
	}
	for _, d := range domains {
		fmt.Fprintf(out, "%s (id %s)\n", d.Name, d.ID)
	}
	return nil
}

// DomainDelete removes a domain by id.
func DomainDelete(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, id model.ID) error {
	d, err := store.GetDomain(ctx, db, sc, id)
	if err != nil {
		return err
	}
	var aliasRemoved bool
	err = mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		alias, err := store.GetAlias(ctx, tx, sc, d.AliasID)
		if err != nil {
			return nil, err
		}
		aliasRemoved, err = inventory.DeleteDomain(ctx, tx, sc, authz.CLISubject(sc.AccountID()), id)
		if err != nil {
			return nil, err
		}
		projectID := d.ProjectID
		events := []authz.Event{{
			Action: authz.ActionDomainDelete, ObjectType: authz.ObjectDomain,
			ObjectID: &id, ObjectLabel: d.Name, ProjectID: &projectID, Before: d,
		}}
		if aliasRemoved {
			events = append(events, authz.Event{
				Action: authz.ActionAliasDelete, ObjectType: authz.ObjectAlias,
				ObjectID: &alias.ID, ObjectLabel: alias.Label,
				ProjectID: &projectID, Before: alias,
			})
		}
		return events, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted domain %q (id %s)\n", d.Name, id)
	if aliasRemoved {
		fmt.Fprintln(out, "its alias had no other domains and was removed too")
	}
	return nil
}
