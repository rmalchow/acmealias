package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Inventory CRUD: projects, load balancers, nodes and domains.
//
// The views below are hand-written rather than the model structs, for one
// reason that matters and one that is convenience. The reason that matters is
// that a model struct grows fields — APIKey already has SecretHash — and a
// response built from `model.X` leaks whatever is added to it next. The
// convenience is naming: the console is TypeScript and reads camelCase.

type projectView struct {
	ID            model.ID           `json:"id"`
	Slug          string             `json:"slug"`
	Name          string             `json:"name"`
	CreatedAt     time.Time          `json:"createdAt"`
	LoadBalancers []loadBalancerView `json:"loadBalancers,omitempty"`
}

type loadBalancerView struct {
	ID        model.ID     `json:"id"`
	ProjectID model.ID     `json:"projectId"`
	Slug      string       `json:"slug"`
	Name      string       `json:"name"`
	CreatedAt time.Time    `json:"createdAt"`
	Nodes     []nodeView   `json:"nodes"`
	Domains   []domainView `json:"domains,omitempty"`
}

type nodeView struct {
	ID             model.ID `json:"id"`
	LoadBalancerID model.ID `json:"loadBalancerId"`
	Label          string   `json:"label"`
	IPv4           *string  `json:"ipv4"`
	IPv6           *string  `json:"ipv6"`
}

type domainView struct {
	ID             model.ID  `json:"id"`
	LoadBalancerID model.ID  `json:"loadBalancerId"`
	Name           string    `json:"name"`
	ChallengeName  string    `json:"challengeName"`
	AliasID        model.ID  `json:"aliasId"`
	AliasLabel     string    `json:"aliasLabel"`
	CreatedAt      time.Time `json:"createdAt"`
}

// aliasView is the audit payload for an alias. Aliases have no route of their
// own — they appear and disappear with domains — so this type exists only so
// the alias entries carry something more useful than an id.
type aliasView struct {
	ID             model.ID `json:"id"`
	LoadBalancerID model.ID `json:"loadBalancerId"`
	Label          string   `json:"label"`
	ChallengeFQDN  string   `json:"challengeFqdn"`
}

func toAlias(a model.Alias) aliasView {
	return aliasView{ID: a.ID, LoadBalancerID: a.LoadBalancerID, Label: a.Label, ChallengeFQDN: a.ChallengeFQDN}
}

func toProject(p model.Project) projectView {
	v := projectView{ID: p.ID, Slug: p.Slug, Name: p.Name, CreatedAt: p.CreatedAt}
	for _, lb := range p.LoadBalancers {
		v.LoadBalancers = append(v.LoadBalancers, toLoadBalancer(lb, nil))
	}
	return v
}

// toLoadBalancer renders a load balancer. aliases may be nil, in which case domains carry no
// alias label — the listing views that do not need it skip the extra query.
func toLoadBalancer(p model.LoadBalancer, aliases map[model.ID]model.Alias) loadBalancerView {
	v := loadBalancerView{ID: p.ID, ProjectID: p.ProjectID, Slug: p.Slug, Name: p.Name, CreatedAt: p.CreatedAt, Nodes: []nodeView{}}
	for _, n := range p.Nodes {
		v.Nodes = append(v.Nodes, toNode(n))
	}
	for _, d := range p.Domains {
		v.Domains = append(v.Domains, toDomain(d, aliases))
	}
	return v
}

func toNode(n model.LBNode) nodeView {
	return nodeView{ID: n.ID, LoadBalancerID: n.LoadBalancerID, Label: n.Label, IPv4: n.IPv4, IPv6: n.IPv6}
}

func toDomain(d model.Domain, aliases map[model.ID]model.Alias) domainView {
	return domainView{
		ID: d.ID, LoadBalancerID: d.LoadBalancerID, Name: d.Name,
		ChallengeName: dnsname.ChallengeName(d.Name),
		AliasID:       d.AliasID, AliasLabel: aliases[d.AliasID].Label,
		CreatedAt: d.CreatedAt,
	}
}

// --- projects --------------------------------------------------------------

// listProjects answers the console's project screen: a page of projects,
// optionally narrowed by a substring of the slug or the name.
//
// `q` matches EITHER field because that is how an operator searches — they
// remember "the shop one" without remembering whether the word is in the slug
// or the display name, and two separate parameters would only make them guess.
func (a *admin) listProjects(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	page, err := readPage(c, 50, 500)
	if err != nil {
		return err
	}
	ps, total, err := inventory.ListProjectsPage(c.Request().Context(), a.db(), sc, subj,
		c.QueryParam("q"), page.Limit, page.Offset)
	if err != nil {
		return err
	}
	setTotal(c, total)
	out := make([]projectView, 0, len(ps))
	for _, p := range ps {
		out = append(out, toProject(p))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *admin) createProject(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	var body struct{ Slug, Name string }
	if err := bind(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var out projectView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		p, err := inventory.CreateProject(ctx, tx, sc, subj, body.Slug, body.Name)
		if err != nil {
			return nil, err
		}
		out = toProject(*p)
		return []authz.Event{{
			Action: authz.ActionProjectCreate, ObjectType: authz.ObjectProject,
			ObjectID: &p.ID, ObjectLabel: p.Slug, ProjectID: &p.ID, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, out)
}

// getProject answers with one project by id.
//
// Added when the console's project list started paging: the detail screen used
// to find its project inside the full list, which quietly became "inside the
// first page of the list" the moment a limit existed. A screen addressed by id
// has to be able to fetch by id.
func (a *admin) getProject(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	p, err := store.GetProject(c.Request().Context(), a.db(), sc, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toProject(*p))
}

// updateProject renames a project. The slug is not editable: it appears in
// URLs, CLI arguments and log lines, so it is identity rather than a label.
func (a *admin) updateProject(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct{ Name string }
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.Name == "" {
		return fmt.Errorf("%w: project name is empty", inventory.ErrInvalid)
	}
	if err := a.authorizeUnrestricted(c, "changing a project"); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var out projectView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		p, err := store.GetProject(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		before := toProject(*p)
		if err := tx.WithContext(ctx).Model(&model.Project{}).Where("id = ?", id).Update("name", body.Name).Error; err != nil {
			return nil, err
		}
		p.Name = body.Name
		out = toProject(*p)
		return []authz.Event{{
			Action: authz.ActionProjectUpdate, ObjectType: authz.ObjectProject,
			ObjectID: &p.ID, ObjectLabel: p.Slug, ProjectID: &p.ID, Before: before, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

func (a *admin) deleteProject(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		p, err := store.GetProject(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if err := inventory.DeleteProject(ctx, tx, sc, subj, id); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionProjectDelete, ObjectType: authz.ObjectProject,
			ObjectID: &id, ObjectLabel: p.Slug, ProjectID: &id, Before: toProject(*p),
		}}, nil
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- load balancers --------------------------------------------------------------

func (a *admin) listLoadBalancers(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := store.GetProject(ctx, a.db(), sc, id); err != nil {
		return err
	}
	lbs, err := store.ListLoadBalancersForProject(ctx, a.db(), sc, id)
	if err != nil {
		return err
	}
	out := make([]loadBalancerView, 0, len(lbs))
	for _, p := range lbs {
		out = append(out, toLoadBalancer(p, nil))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *admin) getLoadBalancer(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	lb, err := store.GetLoadBalancer(ctx, a.db(), sc, id)
	if err != nil {
		return err
	}
	domains, aliases, err := a.domainsOf(c, id)
	if err != nil {
		return err
	}
	lb.Domains = domains
	return c.JSON(http.StatusOK, toLoadBalancer(*lb, aliases))
}

func (a *admin) createLoadBalancer(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	projectID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct{ Slug, Name string }
	if err := bind(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var out loadBalancerView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		lb, err := inventory.CreateLoadBalancer(ctx, tx, sc, subj, projectID, body.Slug, body.Name)
		if err != nil {
			return nil, err
		}
		out = toLoadBalancer(*lb, nil)
		return []authz.Event{{
			Action: authz.ActionLoadBalancerCreate, ObjectType: authz.ObjectLoadBalancer,
			ObjectID: &lb.ID, ObjectLabel: lb.Slug, ProjectID: &projectID, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, out)
}

func (a *admin) updateLoadBalancer(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct{ Name string }
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.Name == "" {
		return fmt.Errorf("%w: load balancer name is empty", inventory.ErrInvalid)
	}
	ctx := c.Request().Context()
	var out loadBalancerView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		lb, err := store.GetLoadBalancer(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if err := a.authorize(c, authz.ActionInventoryWrite, authz.ObjectOf(lb)); err != nil {
			return nil, err
		}
		before := toLoadBalancer(*lb, nil)
		if err := tx.WithContext(ctx).Model(&model.LoadBalancer{}).Where("id = ?", id).Update("name", body.Name).Error; err != nil {
			return nil, err
		}
		lb.Name = body.Name
		out = toLoadBalancer(*lb, nil)
		return []authz.Event{{
			Action: authz.ActionLoadBalancerUpdate, ObjectType: authz.ObjectLoadBalancer,
			ObjectID: &id, ObjectLabel: lb.Slug, ProjectID: &lb.ProjectID, Before: before, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

func (a *admin) deleteLoadBalancer(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	// No zone-view refresh here, and no cascading alias entry: a load balancer
	// that still owns a domain or an alias cannot be deleted at all
	// (inventory.DeleteLoadBalancer refuses it), so nothing can disappear from
	// the served zone by this route.
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		lb, err := store.GetLoadBalancer(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if err := inventory.DeleteLoadBalancer(ctx, tx, sc, subj, id); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionLoadBalancerDelete, ObjectType: authz.ObjectLoadBalancer,
			ObjectID: &id, ObjectLabel: lb.Slug, ProjectID: &lb.ProjectID, Before: toLoadBalancer(*lb, nil),
		}}, nil
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- nodes -----------------------------------------------------------------

func (a *admin) createNode(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	loadBalancerID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		Label string `json:"label"`
		IPv4  string `json:"ipv4"`
		IPv6  string `json:"ipv6"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var v nodeView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		n, err := inventory.AddNode(ctx, tx, sc, subj, loadBalancerID, body.Label, body.IPv4, body.IPv6)
		if err != nil {
			return nil, err
		}
		v = toNode(*n)
		return []authz.Event{{
			Action: authz.ActionNodeCreate, ObjectType: authz.ObjectNode,
			ObjectID: &n.ID, ObjectLabel: n.Label,
			ProjectID: projectOf(ctx, tx, sc, loadBalancerID), After: v,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, v)
}

func (a *admin) updateNode(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		IPv4 string `json:"ipv4"`
		IPv6 string `json:"ipv6"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var v nodeView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		before, err := node(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		n, err := inventory.UpdateNodeAddresses(ctx, tx, sc, subj, id, body.IPv4, body.IPv6)
		if err != nil {
			return nil, err
		}
		v = toNode(*n)
		return []authz.Event{{
			Action: authz.ActionNodeUpdate, ObjectType: authz.ObjectNode,
			ObjectID: &n.ID, ObjectLabel: n.Label,
			ProjectID: projectOf(ctx, tx, sc, n.LoadBalancerID),
			Before:    toNode(*before), After: v,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (a *admin) deleteNode(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		n, err := node(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if err := inventory.DeleteNode(ctx, tx, sc, subj, id); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionNodeDelete, ObjectType: authz.ObjectNode,
			ObjectID: &id, ObjectLabel: n.Label,
			ProjectID: projectOf(ctx, tx, sc, n.LoadBalancerID), Before: toNode(*n),
		}}, nil
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- domains ---------------------------------------------------------------

func (a *admin) listDomains(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	if _, err := store.GetLoadBalancer(c.Request().Context(), a.db(), sc, id); err != nil {
		return err
	}
	domains, aliases, err := a.domainsOf(c, id)
	if err != nil {
		return err
	}
	out := make([]domainView, 0, len(domains))
	for _, d := range domains {
		out = append(out, toDomain(d, aliases))
	}
	return c.JSON(http.StatusOK, out)
}

// domainCreated carries the warnings the spec requires to be surfaced — a
// specific host an existing wildcard already covers is created *and* flagged,
// so the console can say so without the operator having to notice it.
//
// The embedded domainView is the name that was submitted; Domains is every
// domain the request created, which is that one alone unless `includeWildcard`
// asked for its counterpart too. Keeping both means a client that never sets
// the flag sees exactly the body it saw before.
type domainCreated struct {
	domainView
	Domains      []domainView `json:"domains"`
	AliasCreated bool         `json:"aliasCreated"`
	Warnings     []string     `json:"warnings"`
}

func (a *admin) createDomain(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	loadBalancerID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name string `json:"name"`
		// Absent means false: the counterpart is created only when it is asked
		// for, at every entry point (design B4). The field has to be declared
		// here or the strict decoder below would refuse every request carrying
		// it.
		IncludeWildcard bool `json:"includeWildcard"`
	}
	// Strict, unlike every other handler: a request still sending the removed
	// `aliasLabel` is refused as an unknown field rather than having it
	// silently dropped, so a script that used to set the label finds out the
	// first time it runs (design A5).
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return fmt.Errorf("%w: %s", inventory.ErrInvalid, err)
	}
	ctx := c.Request().Context()
	var out domainCreated
	var aliasCreated bool
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		res, err := inventory.CreateDomain(ctx, tx, sc, subj, loadBalancerID, body.Name, a.opts.Config.AliasUseDomain, body.IncludeWildcard)
		if err != nil {
			return nil, err
		}
		aliasCreated = res.AliasCreated
		aliases := map[model.ID]model.Alias{res.Alias.ID: *res.Alias}
		out = domainCreated{
			domainView:   toDomain(*res.Domain, aliases),
			Domains:      make([]domainView, 0, len(res.Domains)),
			AliasCreated: res.AliasCreated,
			Warnings:     []string{},
		}
		for _, d := range res.Domains {
			out.Domains = append(out.Domains, toDomain(*d, aliases))
		}
		for _, w := range res.Warnings {
			out.Warnings = append(out.Warnings, string(w))
		}
		// The trail records objects, and a pair is two of them: one entry per
		// created domain, emitted from the transaction that writes the rows, so
		// the trail can never show one half of a pair (design B1).
		projectID := projectOf(ctx, tx, sc, loadBalancerID)
		events := make([]authz.Event, 0, len(res.Domains)+1)
		for i, d := range res.Domains {
			events = append(events, authz.Event{
				Action: authz.ActionDomainCreate, ObjectType: authz.ObjectDomain,
				ObjectID: &res.Domains[i].ID, ObjectLabel: d.Name,
				ProjectID: projectID, After: out.Domains[i],
			})
		}
		if res.AliasCreated {
			events = append(events, aliasEvent(authz.ActionAliasCreate, *res.Alias, projectID))
		}
		return events, nil
	}); err != nil {
		return err
	}
	// After the commit: a label that does not exist yet must not be served,
	// and a rolled-back creation must not put one in the view at all.
	if aliasCreated {
		a.refreshView(c)
	}
	return c.JSON(http.StatusCreated, out)
}

func (a *admin) deleteDomain(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var aliasRemoved bool
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		d, err := store.GetDomain(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		// Read the alias before the deletion cascades it away — the entry has
		// to name the label the customer published a CNAME to.
		alias, err := store.GetAlias(ctx, tx, sc, d.AliasID)
		if err != nil {
			return nil, err
		}
		aliasRemoved, err = inventory.DeleteDomain(ctx, tx, sc, subj, id)
		if err != nil {
			return nil, err
		}
		projectID := projectOf(ctx, tx, sc, d.LoadBalancerID)
		events := []authz.Event{{
			Action: authz.ActionDomainDelete, ObjectType: authz.ObjectDomain,
			ObjectID: &id, ObjectLabel: d.Name, ProjectID: projectID,
			Before: toDomain(*d, nil),
			After:  map[string]any{"aliasRemoved": aliasRemoved},
		}}
		if aliasRemoved {
			events = append(events, aliasEvent(authz.ActionAliasDelete, *alias, projectID))
		}
		return events, nil
	}); err != nil {
		return err
	}
	if aliasRemoved {
		// The label has to stop answering now, not at the next periodic
		// reload: removing the last domain is what revokes challenge writes
		// for the name.
		a.refreshView(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"aliasRemoved": aliasRemoved})
}

// --- shared lookups --------------------------------------------------------

// domainsOf returns a load balancer's domains together with its aliases keyed by id, so
// a domain view can carry the alias label the console shows next to it.
func (a *admin) domainsOf(c echo.Context, loadBalancerID model.ID) ([]model.Domain, map[model.ID]model.Alias, error) {
	ctx := c.Request().Context()
	sc, err := a.scope(c)
	if err != nil {
		return nil, nil, err
	}
	domains, err := store.ListDomainsForLoadBalancer(ctx, a.db(), sc, loadBalancerID)
	if err != nil {
		return nil, nil, err
	}
	aliases, err := store.ListAliasesForLoadBalancer(ctx, a.db(), sc, loadBalancerID)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[model.ID]model.Alias, len(aliases))
	for _, al := range aliases {
		byID[al.ID] = al
	}
	return domains, byID, nil
}

func node(ctx context.Context, db *gorm.DB, sc store.Scope, id model.ID) (*model.LBNode, error) {
	var n model.LBNode
	if err := sc.Where(db.WithContext(ctx)).First(&n, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("get lb node %s: %w", id, err)
	}
	return &n, nil
}

// projectOf resolves the project a load balancer belongs to, for the audit entry's
// project_id. A failure yields nil rather than an error: an audit row without
// a project filter is still a complete audit row, and losing the mutation
// because its label could not be looked up would be the wrong trade.
//
// db is the mutation's own transaction, so a load balancer created moments ago
// in that transaction still resolves.
func projectOf(ctx context.Context, db *gorm.DB, sc store.Scope, loadBalancerID model.ID) *model.ID {
	lb, err := store.GetLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return nil
	}
	return &lb.ProjectID
}

// aliasEvent builds the entry for an alias appearing or disappearing.
func aliasEvent(action string, al model.Alias, projectID *model.ID) authz.Event {
	e := authz.Event{
		Action: action, ObjectType: authz.ObjectAlias,
		ObjectID: &al.ID, ObjectLabel: al.Label, ProjectID: projectID,
	}
	if action == authz.ActionAliasCreate {
		e.After = toAlias(al)
	} else {
		e.Before = toAlias(al)
	}
	return e
}
