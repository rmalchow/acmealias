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

// Inventory CRUD: projects, nodes and domains.
//
// The views below are hand-written rather than the model structs, for one
// reason that matters and one that is convenience. The reason that matters is
// that a model struct grows fields — APIKey already has SecretHash — and a
// response built from `model.X` leaks whatever is added to it next. The
// convenience is naming: the console is TypeScript and reads camelCase.

// PermittedActions accompanies every object the API reports (task 7.3), so a
// console renders controls from a per-object answer rather than from the
// caller's role name — which said nothing about WHICH objects, and is the thing
// the whole change exists to replace.
//
// It rides along with the listing rather than costing a request per row: a table
// of fifty projects stays one query. And it is not the enforcement point. Every
// request is checked again when it arrives, because a client is not a place to
// keep a permission.
type projectView struct {
	ID               model.ID     `json:"id"`
	Slug             string       `json:"slug"`
	Name             string       `json:"name"`
	CreatedAt        time.Time    `json:"createdAt"`
	PermittedActions []string     `json:"permittedActions"`
	Nodes            []nodeView   `json:"nodes,omitempty"`
	Domains          []domainView `json:"domains,omitempty"`

	// NodeCount and DomainCount are what is IN the project, carried by the
	// LISTING and absent from the detail (design L5). They are pointers so that
	// "not asked for" is a different value from "none": a detail response
	// omitting them must not read as an empty project, since it is carrying the
	// nodes and domains themselves two fields up.
	NodeCount   *int `json:"nodeCount,omitempty"`
	DomainCount *int `json:"domainCount,omitempty"`
}

type nodeView struct {
	ID        model.ID `json:"id"`
	ProjectID model.ID `json:"projectId"`
	Label     string   `json:"label"`
	IPv4      *string  `json:"ipv4"`
	IPv6      *string  `json:"ipv6"`
}

type domainView struct {
	ID            model.ID  `json:"id"`
	ProjectID     model.ID  `json:"projectId"`
	Name          string    `json:"name"`
	ChallengeName string    `json:"challengeName"`
	AliasID       model.ID  `json:"aliasId"`
	AliasLabel    string    `json:"aliasLabel"`
	CreatedAt     time.Time `json:"createdAt"`

	// Activation, as the two timestamps and nothing else (domain-activation
	// A1). There is deliberately no `active` boolean on the wire either: the
	// console derives it from lastSuccessAt exactly as the service does, and a
	// third representation of one fact is a third thing that can disagree with
	// the other two.
	//
	// Both are nullable and mean different absences. lastCheckedAt null is "we
	// have not looked yet" — the state of a domain added a moment ago, and the
	// state every domain that predates this feature was migrated into.
	// lastSuccessAt null is "we have never seen the delegation", which is what
	// makes /present refuse.
	//
	// lastCheckedAt AFTER lastSuccessAt is the third state worth reading: we
	// looked and did not see it. It is information, never a countdown — nothing
	// is ever switched off because of it (A3).
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`

	// The certificate-observation switch and the hostname a wildcard is observed
	// under (domain-diagnostics 2.6). TLSTestHost is nullable and stays set when
	// the switch is turned off, so somebody who turns it back on gets their
	// hostname back rather than being asked again.
	ObserveTLS  bool    `json:"observeTls"`
	TLSTestHost *string `json:"tlsTestHost"`

	// What this domain needs published in somebody else's zone. It used to be
	// served as a project-wide record table with its own endpoint; it is a
	// property of the domain, and the domain read is where a screen showing one
	// row can get it without asking a second question (domain-diagnostics G2).
	//
	// CNAMETarget is what the _acme-challenge CNAME must point at. The empty
	// string is the absence — a domain whose alias has gone missing — and NOT
	// `omitempty`, because an omitted field would make "no alias yet"
	// indistinguishable from an older service, which is the one thing this field
	// exists to be able to say.
	CNAMETarget string `json:"cnameTarget"`
	// ExpectedCAA is the rendered rdata expected at the registrable name, both
	// tags per configured issuer, each pinned to an ACME account:
	// `0 issue "letsencrypt.org; accounturi=…"`. It is null, not
	// empty, when no issuer is configured — there is then nothing to authorize
	// and nothing to check, which is what lets a console hide the CAA dot
	// instead of showing a grey one for a question nobody asked.
	ExpectedCAA []string `json:"expectedCaa"`
	// CAAName is the name that rdata belongs AT — the registrable domain, since
	// CAA is inherited and the nearest ancestor wins. It rides along with the
	// rdata because rdata without an owner name is not a record anybody can
	// publish, and the console cannot derive it: the public suffix list is what
	// separates example.com from example.co.uk, and shipping that to a browser
	// to compute a name the service already knows would be absurd. Empty exactly
	// when ExpectedCAA is null.
	CAAName string `json:"caaName"`
}

// aliasView is the audit payload for an alias. Aliases have no route of their
// own — they appear and disappear with domains — so this type exists only so
// the alias entries carry something more useful than an id.
type aliasView struct {
	ID            model.ID `json:"id"`
	ProjectID     model.ID `json:"projectId"`
	Label         string   `json:"label"`
	ChallengeFQDN string   `json:"challengeFqdn"`
}

func toAlias(a model.Alias) aliasView {
	return aliasView{ID: a.ID, ProjectID: a.ProjectID, Label: a.Label, ChallengeFQDN: a.ChallengeFQDN}
}

// toProject renders a project. aliases may be nil, in which case domains carry
// no alias label — the views that do not need it skip the extra query.
func (a *admin) toProject(p model.Project, aliases map[model.ID]model.Alias, subj authz.Subject) projectView {
	v := projectView{
		ID: p.ID, Slug: p.Slug, Name: p.Name, CreatedAt: p.CreatedAt,
		PermittedActions: authz.PermittedOn(subj, authz.ObjectOf(&p)),
		Nodes:            []nodeView{},
	}
	for _, n := range p.Nodes {
		v.Nodes = append(v.Nodes, toNode(n))
	}
	for _, d := range p.Domains {
		v.Domains = append(v.Domains, a.toDomain(d, aliases))
	}
	return v
}

// toProjectListing is toProject for a row of the list: the counts instead of the
// contents. The two are separate functions rather than one with a flag because
// they answer different questions — "what is in this project" and "how much is
// in it" — and the listing must not start carrying the contents by accident.
func (a *admin) toProjectListing(p store.ProjectListing, subj authz.Subject) projectView {
	v := a.toProject(p.Project, nil, subj)
	// The listing carries the COUNTS and not the contents, and the contents are
	// cleared rather than left as the empty slice toProject builds: a `nodes: []`
	// on a row that has four of them is a claim, and the console would render it.
	v.Nodes = nil
	nodes, domains := p.NodeCount, p.DomainCount
	v.NodeCount, v.DomainCount = &nodes, &domains
	return v
}

func toNode(n model.LBNode) nodeView {
	return nodeView{ID: n.ID, ProjectID: n.ProjectID, Label: n.Label, IPv4: n.IPv4, IPv6: n.IPv6}
}

// toDomain renders a domain. aliases may be nil, in which case the row carries
// no alias label and no CNAME target — the views that do not need them skip the
// extra query, and an audit payload is one of those.
//
// It is a method because the target and the expected CAA depend on the
// deployment's configuration (the served zone, the configured issuers), which is
// read off the effective config per request rather than captured at startup, so
// a reload takes effect without one.
func (a *admin) toDomain(d model.Domain, aliases map[model.ID]model.Alias) domainView {
	v := domainView{
		ID: d.ID, ProjectID: d.ProjectID, Name: d.Name,
		ChallengeName: dnsname.ChallengeName(d.Name),
		AliasID:       d.AliasID, AliasLabel: aliases[d.AliasID].Label,
		CreatedAt:     d.CreatedAt,
		LastCheckedAt: d.LastCheckedAt, LastSuccessAt: d.LastSuccessAt,
		ObserveTLS: d.ObserveTLS, TLSTestHost: d.TLSTestHost,
	}
	// Taken from the ALIAS rather than recomputed from the domain name: the
	// service answers for the alias's label, so rendering the target any other
	// way risks telling a customer to publish a CNAME to a name we do not serve
	// (domain-diagnostics 10.3). ChallengeName above is still computed, and
	// deliberately so — that one is left exactly as it was.
	if al, ok := aliases[d.AliasID]; ok {
		v.CNAMETarget = inventory.DelegationTarget(al, a.opts.Config.Zone)
	}
	// One renderer, in internal/inventory, for a reason the deleted copy here
	// records: two spellings of one record is two records as far as a customer
	// reading them is concerned. CAAName rides along because rdata with no owner
	// name is not something anybody can publish.
	if r, err := dnsname.RegistrableDomain(d.Name); err == nil {
		if lines := inventory.CAALines(r, a.opts.Config.CAAIssuers); lines != nil {
			v.ExpectedCAA, v.CAAName = lines, r
		}
	}
	return v
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
		out = append(out, a.toProjectListing(p, subj))
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
		p, err := inventory.CreateProject(ctx, tx, sc, subj, a.limits(), body.Slug, body.Name)
		if err != nil {
			return nil, err
		}
		out = a.toProject(*p, nil, subj)
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
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
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
	// The detail carries the project's domains, which the load balancer screen
	// used to fetch from a route of its own. One screen, one request.
	domains, aliases, err := a.domainsOf(c, id)
	if err != nil {
		return err
	}
	p.Domains = domains
	return c.JSON(http.StatusOK, a.toProject(*p, aliases, subj))
}

// updateProject renames a project. The slug is not editable: it appears in
// URLs, CLI arguments and log lines, so it is identity rather than a label.
func (a *admin) updateProject(c echo.Context) error {
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
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
		before := a.toProject(*p, nil, subj)
		if err := tx.WithContext(ctx).Model(&model.Project{}).Where("id = ?", id).Update("name", body.Name).Error; err != nil {
			return nil, err
		}
		p.Name = body.Name
		out = a.toProject(*p, nil, subj)
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
			ObjectID: &id, ObjectLabel: p.Slug, ProjectID: &id, Before: a.toProject(*p, nil, subj),
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
	projectID, err := idParam(c, "id")
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
		n, err := inventory.AddNode(ctx, tx, sc, subj, projectID, body.Label, body.IPv4, body.IPv6)
		if err != nil {
			return nil, err
		}
		v = toNode(*n)
		return []authz.Event{{
			Action: authz.ActionNodeCreate, ObjectType: authz.ObjectNode,
			ObjectID: &n.ID, ObjectLabel: n.Label,
			ProjectID: &projectID, After: v,
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
			ProjectID: &n.ProjectID,
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
			ProjectID: &n.ProjectID, Before: toNode(*n),
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
	if _, err := store.GetProject(c.Request().Context(), a.db(), sc, id); err != nil {
		return err
	}
	domains, aliases, err := a.domainsOf(c, id)
	if err != nil {
		return err
	}
	out := make([]domainView, 0, len(domains))
	for _, d := range domains {
		out = append(out, a.toDomain(d, aliases))
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
	projectID, err := idParam(c, "id")
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
		res, err := inventory.CreateDomain(ctx, tx, sc, subj, projectID, body.Name, a.opts.Config.AliasUseDomain, body.IncludeWildcard)
		if err != nil {
			return nil, err
		}
		aliasCreated = res.AliasCreated
		aliases := map[model.ID]model.Alias{res.Alias.ID: *res.Alias}
		out = domainCreated{
			domainView:   a.toDomain(*res.Domain, aliases),
			Domains:      make([]domainView, 0, len(res.Domains)),
			AliasCreated: res.AliasCreated,
			Warnings:     []string{},
		}
		for _, d := range res.Domains {
			out.Domains = append(out.Domains, a.toDomain(*d, aliases))
		}
		for _, w := range res.Warnings {
			out.Warnings = append(out.Warnings, string(w))
		}
		// The trail records objects, and a pair is two of them: one entry per
		// created domain, emitted from the transaction that writes the rows, so
		// the trail can never show one half of a pair (design B1).
		events := make([]authz.Event, 0, len(res.Domains)+1)
		for i, d := range res.Domains {
			events = append(events, authz.Event{
				Action: authz.ActionDomainCreate, ObjectType: authz.ObjectDomain,
				ObjectID: &res.Domains[i].ID, ObjectLabel: d.Name,
				ProjectID: &projectID, After: out.Domains[i],
			})
		}
		if res.AliasCreated {
			events = append(events, aliasEvent(authz.ActionAliasCreate, *res.Alias, &projectID))
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

// updateDomain changes the two properties of a domain that can change: whether
// we observe the certificate it serves, and the hostname a wildcard is observed
// under (domain-diagnostics 2.6, 2.8).
//
// A domain had no update path at all before this. PATCH rather than a
// /observation sub-route, and ActionInventoryWrite rather than a new permission
// — this is a property of a domain, and adding an action would mean a
// role_grants migration and an edit to defaultroles.go for a boolean on a row.
//
// tlsTestHost is a POINTER on the way in, because absent and null are different
// requests. Absent leaves the stored hostname alone, which is what makes turning
// the switch off and on again give somebody their hostname back rather than
// asking for it a second time; null clears it. The validation — a wildcard
// cannot be observed without one, and the hostname has to be one the wildcard
// covers — is internal/inventory's, so the message a customer reads is the same
// one the CLI would print.
//
// The audit entry is written HERE and not in internal/inventory, exactly as
// CreateDomain and DeleteDomain are: the service layer performs the change and
// this layer records who asked for it.
func (a *admin) updateDomain(c echo.Context) error {
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
		ObserveTLS  bool    `json:"observeTls"`
		TLSTestHost *string `json:"tlsTestHost"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var v domainView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		before, err := store.GetDomain(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		d, err := inventory.UpdateDomain(ctx, tx, sc, subj, id, body.ObserveTLS, body.TLSTestHost)
		if err != nil {
			return nil, err
		}
		v = a.toDomain(*d, nil)
		projectID := d.ProjectID
		return []authz.Event{{
			// ponytail: the action string is a literal because authz's vocabulary
			// has no domain.update yet and internal/authz belongs to another group
			// in this change. It needs to become authz.ActionDomainUpdate and to
			// join ClassConfig in vocab.go's actionClasses — until it does, this
			// entry is written and readable but is missing from a listing filtered
			// to `class=config`.
			Action: "domain.update", ObjectType: authz.ObjectDomain,
			ObjectID: &d.ID, ObjectLabel: d.Name, ProjectID: &projectID,
			Before: a.toDomain(*before, nil), After: v,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
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
		projectID := d.ProjectID
		events := []authz.Event{{
			Action: authz.ActionDomainDelete, ObjectType: authz.ObjectDomain,
			ObjectID: &id, ObjectLabel: d.Name, ProjectID: &projectID,
			Before: a.toDomain(*d, nil),
			After:  map[string]any{"aliasRemoved": aliasRemoved},
		}}
		if aliasRemoved {
			events = append(events, aliasEvent(authz.ActionAliasDelete, *alias, &projectID))
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

// domainsOf returns a project's domains together with its aliases keyed by id, so
// a domain view can carry the alias label the console shows next to it.
func (a *admin) domainsOf(c echo.Context, projectID model.ID) ([]model.Domain, map[model.ID]model.Alias, error) {
	ctx := c.Request().Context()
	sc, err := a.scope(c)
	if err != nil {
		return nil, nil, err
	}
	domains, err := store.ListDomainsForProject(ctx, a.db(), sc, projectID)
	if err != nil {
		return nil, nil, err
	}
	aliases, err := store.ListAliasesForProject(ctx, a.db(), sc, projectID)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[model.ID]model.Alias, len(aliases))
	for _, al := range aliases {
		byID[al.ID] = al
	}
	return domains, byID, nil
}

// node reads one node within the acting account.
//
// Scope.Run, not merely sc.Where, and the difference is the whole comment.
// `lb_nodes` carries the tenant_isolation policy, and this is the FIRST statement of
// a.mutate's transaction — which is opened from a plain handle, so nothing has
// set app.account_id yet. Filtering in Go while the policy sees a NULL
// current_account() matches no rows at all: PATCH /nodes/:id and DELETE
// /nodes/:id answered 404 for a perfectly good node under any non-superuser
// role. It survived the test suite because the test connection is a superuser
// and bypasses policies entirely, which is exactly the failure mode Scope.Run's
// own doc comment warns about — a scopeless read of a policied table returns no
// rows and no error. The neighbouring key() has done it this way since the same
// bug was found there.
func node(ctx context.Context, db *gorm.DB, sc store.Scope, id model.ID) (*model.LBNode, error) {
	var n model.LBNode
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).First(&n, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("get lb node %s: %w", id, err)
	}
	return &n, nil
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
