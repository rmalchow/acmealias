package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// API key management (design D3).
//
// The one invariant worth stating twice: a secret exists in this process for
// the length of one response. keyView has no field it could go in, and every
// read path returns keyView, so "never shown again" is a property of the type
// rather than of each handler remembering. Only createKey and rotateKey
// return keyCreated, and only because apikey.New just produced the value.

// grantView renders one grant a key holds. A key's authority used to be a
// single load balancer id, which the console could show as a name; it is now a
// set of grants, and every screen that lists keys has to show what they can
// actually do (task 9.5).
type grantView struct {
	Type        string    `json:"type"`
	ObjectID    *model.ID `json:"objectId,omitempty"`
	Action      string    `json:"action"`
	Inheritable bool      `json:"inheritable"`
	// Summary is the grant written the way the design writes them, so an
	// operator reading a listing does not have to assemble four fields in their
	// head.
	Summary string `json:"summary"`
}

type keyView struct {
	ID model.ID `json:"id"`
	// Kind is "project" or "system" (design K1). It is on every key view rather
	// than implied by which screen asked, because a program reading this API has
	// no screen.
	Kind string `json:"kind"`
	// ProjectID is set for a project key: the project it dies with.
	ProjectID *model.ID `json:"projectId,omitempty"`
	// RoleID and RoleName describe a system key's authority. The NAME rides
	// along for the same reason the audit trail records role names rather than
	// references: a console that had to resolve the id itself would show a blank
	// where the interesting word belongs.
	RoleID   *model.ID `json:"roleId,omitempty"`
	RoleName string    `json:"roleName,omitempty"`
	// RestrictToNodes is the switch, not a list — see design K3. What it permits
	// is whatever the project's nodes are when a request arrives.
	RestrictToNodes bool        `json:"restrictToNodes"`
	Grants          []grantView `json:"grants"`
	Identifier      string      `json:"identifier"`
	DisplayPrefix   string      `json:"displayPrefix"`
	Note            string      `json:"note"`
	Networks        []string    `json:"networks"`
	CreatedAt       time.Time   `json:"createdAt"`
	ExpiresAt       *time.Time  `json:"expiresAt"`
	LastUsedAt      *time.Time  `json:"lastUsedAt"`
	// The most recent address refusal (design K5). It is here because the
	// refusal on the wire is deliberately opaque — a caller must not be able to
	// tell a wrong secret from a wrong address — so this is the only place
	// somebody behind NAT can see that their node arrives from an address no
	// node carries.
	LastRefusedIP *string    `json:"lastRefusedIp,omitempty"`
	LastRefusedAt *time.Time `json:"lastRefusedAt,omitempty"`
	DisabledAt    *time.Time `json:"disabledAt"`
	Active        bool       `json:"active"`
}

// keyCreated is the only response in the service that carries a secret. The
// field is documented as one-time in the JSON itself, because the console is
// not the only client — an operator with curl gets the warning too.
type keyCreated struct {
	Key    keyView `json:"key"`
	Secret string  `json:"secret"`
	Notice string  `json:"notice"`
}

const secretNotice = "this secret is shown exactly once and is not recoverable; store it now, or revoke this key and create another"

func toKey(k model.APIKey) keyView {
	v := keyView{
		ID: k.ID, Kind: k.Kind, ProjectID: k.ProjectID, RoleID: k.RoleID,
		RestrictToNodes: k.RestrictToProjectNodes,
		Identifier:      k.Identifier, DisplayPrefix: k.DisplayPrefix,
		Note: k.Note, Networks: []string{}, Grants: []grantView{}, CreatedAt: k.CreatedAt,
		ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, DisabledAt: k.DisabledAt,
		LastRefusedIP: k.LastRefusedIP, LastRefusedAt: k.LastRefusedAt,
	}
	if k.Role != nil {
		v.RoleName = k.Role.Name
	}
	for _, g := range k.Grants {
		parsed := authz.GrantFromRow(g)
		v.Grants = append(v.Grants, grantView{
			Type: g.ObjectType, ObjectID: g.ObjectID,
			Action: g.Action, Inheritable: g.Inheritable, Summary: parsed.String(),
		})
	}
	for _, n := range k.Networks {
		v.Networks = append(v.Networks, n.CIDR)
	}
	now := time.Now()
	v.Active = k.DisabledAt == nil && (k.ExpiresAt == nil || k.ExpiresAt.After(now))
	return v
}

func (a *admin) listKeys(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	projectID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := store.GetProject(ctx, a.db(), sc, projectID); err != nil {
		return err
	}
	ks, err := store.ListAPIKeysForProject(ctx, a.db(), sc, projectID)
	if err != nil {
		return err
	}
	out := make([]keyView, 0, len(ks))
	for _, k := range ks {
		out = append(out, toKey(k))
	}
	return c.JSON(http.StatusOK, out)
}

// createKey mints a PROJECT key: bound to this project, dying with it, and
// answering its challenges (design K1, K8).
//
// The two grants it used to write are gone. A project key's authority is
// synthesized from api_keys.project_id in authz.SubjectFromKey, which is what
// stops this change from adding new polymorphic grant rows that nothing deletes
// when their object goes (design K2). They are still SUBSET-CHECKED — passed to
// store.CreateAPIKey as `implied` — because the caller must hold what they
// confer whether or not it is written down.
func (a *admin) createKey(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	projectID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		Note     string   `json:"note"`
		Networks []string `json:"networks"`
		// RestrictToNodes is the switch (design K3). It is a boolean and not a
		// list on purpose: what it permits is whatever the project's nodes are
		// when a request arrives, so there is nothing to keep in step. The
		// console used to offer the same idea by copying the addresses into
		// `networks`, and that copy was right until somebody added a node.
		RestrictToNodes bool   `json:"restrictToNodes"`
		ExpiresIn       string `json:"expiresIn"` // a Go duration, e.g. "720h"; empty means no expiry
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	nets, err := parseCIDRs(body.Networks)
	if err != nil {
		return err
	}
	expires, err := parseExpiry(body.ExpiresIn)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := store.GetProject(ctx, a.db(), sc, projectID); err != nil {
		return err
	}
	k := &model.APIKey{
		Kind: model.APIKeyKindProject, ProjectID: &projectID,
		RestrictToProjectNodes: body.RestrictToNodes,
	}
	return a.mint(c, k, body.Note, nets, expires, authz.ActionKeyCreate)
}

// createSystemKey mints a SYSTEM key: a member of the account that happens not
// to be a person (design K6, K8).
//
// It lives in the account's settings rather than on a project's page, and that
// is the lifetime made visible rather than a placement preference: a project key
// dies with its project and a system key outlives every project in the account.
// One screen for both would leave somebody asking, later and angrily, why
// deleting a project revoked their automation.
//
// TEAM ACCOUNTS ONLY, for the reason roles are (design K7, C9). A personal
// account has no roster and no roles, and a system key is a role-holder — so
// there is nothing for one to hold. The refusal is C9's "there cannot be one
// here" and NOT a permission error: in an account of one there is nobody to ask
// for permission, which is exactly the confusion C11 warns about.
func (a *admin) createSystemKey(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "creating a system key"); err != nil {
		return err
	}
	// The same authority adding a MEMBER needs, because that is what this is.
	if err := a.authorizeUnrestricted(c, "creating a system key"); err != nil {
		return err
	}
	var body struct {
		Note      string   `json:"note"`
		RoleID    model.ID `json:"roleId"`
		Networks  []string `json:"networks"`
		ExpiresIn string   `json:"expiresIn"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.RoleID == model.NilID {
		return fmt.Errorf("%w: a system key holds a role; roleId is required", inventory.ErrInvalid)
	}
	nets, err := parseCIDRs(body.Networks)
	if err != nil {
		return err
	}
	expires, err := parseExpiry(body.ExpiresIn)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	// Read the role through the scope, so naming a role of another account is a
	// not-found rather than a key holding somebody else's authority.
	roles, err := store.RolesFor(ctx, a.db(), sc, body.RoleID)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		return fmt.Errorf("%w: no such role in this account", inventory.ErrInvalid)
	}
	// The role rides along on the object being created, so the response names it
	// without a second read. Only the created row is answered with, and it is
	// answered with once.
	k := &model.APIKey{Kind: model.APIKeyKindSystem, RoleID: &body.RoleID, Role: &roles[0]}
	return a.mint(c, k, body.Note, nets, expires, authz.ActionKeyCreate)
}

// listSystemKeys answers the settings screen. Project keys are deliberately not
// here; they are on their project's page (design K8).
func (a *admin) listSystemKeys(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	// Refused rather than answered empty, for the reason the roster is (design
	// C9, C11). An empty list reads as "there are none YET" and invites a client
	// to render an Add button for something that can never exist here; a refusal
	// says "there cannot be any", which is the true sentence.
	if err := a.requireTeamAccount(c, sc, "system keys"); err != nil {
		return err
	}
	ks, err := store.ListSystemAPIKeys(c.Request().Context(), a.db(), sc)
	if err != nil {
		return err
	}
	out := make([]keyView, 0, len(ks))
	for _, k := range ks {
		out = append(out, toKey(k))
	}
	return c.JSON(http.StatusOK, out)
}

// rotateKey mints a successor of the same KIND, copying everything but the
// secret, and leaves the existing key working.
//
// That is the whole of design D3's rotation: create, deploy, then revoke the old
// one — the model supports several live keys per project precisely so the window
// between the two never has a gap. Revoking the predecessor here automatically
// would reintroduce the downtime the design removed.
//
// "Everything but the secret" now includes the kind, the project or the role,
// the address switch and any hand-typed CIDR blocks. A rotation replaces a
// secret, never an authority: a rotation that silently widened or narrowed what
// a key can do would be the worst kind of change, because nobody would look.
func (a *admin) rotateKey(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	old, err := key(ctx, a.db(), sc, id)
	if err != nil {
		return err
	}
	if err := a.authorize(c, authz.ActionKeyWrite, authz.ObjectOf(old)); err != nil {
		return err
	}
	nets := make([]string, 0, len(old.Networks))
	for _, n := range old.Networks {
		nets = append(nets, n.CIDR)
	}
	note := old.Note
	if note == "" {
		note = fmt.Sprintf("rotated from %s", old.Identifier)
	} else {
		note = fmt.Sprintf("%s (rotated from %s)", note, old.Identifier)
	}
	k := &model.APIKey{
		Kind: old.Kind, ProjectID: old.ProjectID, RoleID: old.RoleID, Role: old.Role,
		RestrictToProjectNodes: old.RestrictToProjectNodes,
		Grants:                 grantsOf(old),
	}
	return a.mint(c, k, note, nets, nil, authz.ActionKeyRotate)
}

// grantsOf copies a key's own grant rows for a rotation. Only the LEGACY shape
// has any: a project key's authority comes from its project and a system key's
// from its role, and both follow the columns copied beside this.
func grantsOf(k *model.APIKey) []model.APIKeyGrant {
	out := make([]model.APIKeyGrant, 0, len(k.Grants))
	for _, g := range k.Grants {
		out = append(out, model.APIKeyGrant{
			ObjectType: g.ObjectType, ObjectID: g.ObjectID,
			Action: g.Action, Inheritable: g.Inheritable,
		})
	}
	return out
}

// impliedGrants are the grants a key will hold with no row of its own, so the
// subset rule can be applied to authority that is synthesized rather than
// stored. A project key's two challenge actions are the only such authority
// today; a system key's come from its role, which was itself subset-checked when
// the role was written.
//
// This has to agree with authz.SubjectFromKey. Two places computing one thing is
// a real risk, and the alternative — importing authz into store, or the
// evaluator into the handler — is a dependency cycle. What holds them together
// is a test asserting a project key's checked authority equals its resolved
// authority.
func impliedGrants(k *model.APIKey) []store.Grant {
	if k.Kind != model.APIKeyKindProject || k.ProjectID == nil {
		return nil
	}
	return []store.Grant{
		{ObjectType: string(authz.TypeProject), ObjectID: k.ProjectID, Action: challengePublish, Inheritable: true},
		{ObjectType: string(authz.TypeProject), ObjectID: k.ProjectID, Action: challengeWithdraw, Inheritable: true},
	}
}

// mint is the shared body of create and rotate: generate, persist, audit,
// answer once with the secret. k arrives carrying its kind and whatever that
// kind hangs off — everything except the identifier and the digest, which only
// apikey.New can produce.
func (a *admin) mint(c echo.Context, k *model.APIKey, note string, networks []string, expires *time.Time, action string) error {
	return a.mintWith(c, k, note, networks, expires, action, func(v keyView, secret string) any {
		return keyCreated{Key: v, Secret: secret, Notice: secretNotice}
	})
}

// mintFor is mint for a caller that answers with something other than
// keyCreated. The action is always key.create: whatever the response looks like,
// what happened is that a key was created, and an audit trail that said
// otherwise would be describing the SCREEN rather than the event.
func (a *admin) mintFor(c echo.Context, k *model.APIKey, note string, networks []string, expires *time.Time, respond func(keyView, string) any) error {
	return a.mintWith(c, k, note, networks, expires, authz.ActionKeyCreate, respond)
}

func (a *admin) mintWith(c echo.Context, k *model.APIKey, note string, networks []string, expires *time.Time, action string, respond func(keyView, string) any) error {
	ctx := c.Request().Context()
	sc, err := a.scope(c)
	if err != nil {
		return err
	}

	// The subset rule (tasks 4.3 and 6.5): a key may be given only what its
	// creator holds, checked one grant at a time against one held grant. The
	// check is handed to the store rather than run here, so a future write path
	// cannot forget it — the parameter is required.
	check, err := a.subsetCheck(c, sc)
	if err != nil {
		return err
	}

	fresh, secret, err := apikey.New()
	if err != nil {
		return err
	}
	k.Identifier = fresh.Identifier
	k.SecretHash = fresh.SecretHash
	k.DisplayPrefix = fresh.DisplayPrefix
	k.Note = note
	k.ExpiresAt = expires
	for _, n := range networks {
		k.Networks = append(k.Networks, model.APIKeyNetwork{CIDR: n})
	}

	var v keyView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.CreateAPIKey(ctx, tx, sc, k, check, impliedGrants(k)...); err != nil {
			return nil, err
		}
		v = toKey(*k)
		// authz.Record scrubs secret-looking fields, but the secret is simply
		// not handed to it: the audit row records that a key was created, by
		// whom, and under which address restrictions — which is what the spec
		// asks for. keyView carries Networks, so the CIDR list rides along.
		return []authz.Event{{
			Action: action, ObjectType: authz.ObjectAPIKey, ObjectID: &k.ID, ObjectLabel: k.Identifier,
			After: v,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, respond(v, secret))
}

// restrictKey replaces a key's source-address restrictions wholesale. An
// empty list means unrestricted, which is a real and intentional state, so
// this is a PUT of the whole set rather than an add/remove pair that would
// make "clear them all" ambiguous.
func (a *admin) restrictKey(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		Networks []string `json:"networks"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	nets, err := parseCIDRs(body.Networks)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var out keyView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		k, err := key(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if err := a.authorize(c, authz.ActionKeyWrite, authz.ObjectOf(k)); err != nil {
			return nil, err
		}
		before := toKey(*k)

		if err := tx.Where("api_key_id = ?", id).Delete(&model.APIKeyNetwork{}).Error; err != nil {
			return nil, err
		}
		rows := make([]model.APIKeyNetwork, 0, len(nets))
		for _, n := range nets {
			rows = append(rows, model.APIKeyNetwork{APIKeyID: id, CIDR: n})
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return nil, err
			}
		}

		k, err = key(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		out = toKey(*k)
		return []authz.Event{{
			Action: authz.ActionKeyRestrict, ObjectType: authz.ObjectAPIKey, ObjectID: &id, ObjectLabel: k.Identifier,
			Before: before, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

// revokeKey disables a key. Revocation, not deletion: the row still names the
// key in past audit entries and challenge attributions.
func (a *admin) revokeKey(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var out keyView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		k, err := key(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if err := a.authorize(c, authz.ActionKeyWrite, authz.ObjectOf(k)); err != nil {
			return nil, err
		}
		before := toKey(*k)
		if err := store.RevokeAPIKey(ctx, tx, sc, id); err != nil {
			return nil, err
		}
		k, err = key(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		out = toKey(*k)
		return []authz.Event{{
			Action: authz.ActionKeyRevoke, ObjectType: authz.ObjectAPIKey, ObjectID: &id, ObjectLabel: k.Identifier,
			Before: before, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

// setKeyExpiry moves or clears a key's expiry.
//
// It exists so that "this key expires next month" is a recorded, attributable
// decision rather than something only expressible by revoking the key and
// minting another. An empty expiresIn clears the expiry — which is a widening
// of access, and is exactly why it is audited as its own action rather than
// folded into a generic key update.
func (a *admin) setKeyExpiry(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		ExpiresIn string `json:"expiresIn"` // a Go duration, e.g. "720h"; empty clears the expiry
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	expires, err := parseExpiry(body.ExpiresIn)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var out keyView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		k, err := key(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if err := a.authorize(c, authz.ActionKeyWrite, authz.ObjectOf(k)); err != nil {
			return nil, err
		}
		before := toKey(*k)
		if err := tx.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", id).
			Update("expires_at", expires).Error; err != nil {
			return nil, err
		}
		k.ExpiresAt = expires
		out = toKey(*k)
		return []authz.Event{{
			Action: authz.ActionKeyExpiryChange, ObjectType: authz.ObjectAPIKey,
			ObjectID: &id, ObjectLabel: k.Identifier,
			Before: before, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

// key reads one key within the acting account, with the networks and grants
// every caller here goes on to render or copy.
//
// Scoped, unlike the version this replaces: `First(&k, id)` reached any key in
// the deployment, which was harmless while a key belonged to a load balancer the
// caller had already been authorized for, and is not harmless now that a key is
// addressed by its own identifier.
func key(ctx context.Context, db *gorm.DB, sc store.Scope, id model.ID) (*model.APIKey, error) {
	var k model.APIKey
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Preload("Networks").Preload("Grants").Preload("Role").First(&k, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("get api key %s: %w", id, err)
	}
	return &k, nil
}

// parseCIDRs validates and canonicalizes restrictions. The prefix is masked
// because PostgreSQL's cidr type refuses a value with bits set below the
// prefix length — "203.0.113.5/24" would be a 500 from the driver rather than
// the 400 it deserves.
func parseCIDRs(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, s := range in {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not a CIDR block (e.g. 203.0.113.0/24)", inventory.ErrInvalid, s)
		}
		out = append(out, p.Masked().String())
	}
	return out, nil
}

func parseExpiry(in string) (*time.Time, error) {
	if in == "" {
		return nil, nil
	}
	d, err := time.ParseDuration(in)
	if err != nil || d <= 0 {
		return nil, fmt.Errorf("%w: expiresIn must be a positive Go duration such as \"720h\", got %q", inventory.ErrInvalid, in)
	}
	t := time.Now().UTC().Add(d)
	return &t, nil
}

// --- acme-dns credentials ---------------------------------------------------

// acmeDNSCredentials is the file every acme-dns client reads, in the field names
// they read it by (design N3, task 4.3).
//
// The names are not ours and must not be tidied. cert-manager's acmeDNS solver
// unmarshals a Kubernetes Secret into exactly these four keys, acme.sh's
// `dns_acmedns` reads the same four, and a Secret is not somewhere a person
// notices a typo — a near-miss here fails at renewal time, weeks later, inside a
// cluster.
//
//	username    the API key's identifier, sent as X-Api-User
//	password    its secret, sent as X-Api-Key
//	fulldomain  the name the customer's CNAME points AT — our alias, in our zone
//	subdomain   just the label, which is what /update names in its body
//
// acme-dns's own file also carries `allowfrom`, which is its per-registration
// CIDR list. It is omitted deliberately: clients ignore it (it is a server-side
// property they cannot change), and our equivalent is the key's own address
// restrictions, which are shown on the key rather than pasted into a Secret
// nobody re-reads.
type acmeDNSCredentials struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	FullDomain string `json:"fulldomain"`
	Subdomain  string `json:"subdomain"`
}

// acmeDNSIssued is what the console renders once and never again.
//
// It carries the credentials file AND the key view, because they answer two
// different questions: the file is what gets pasted into a client, and the key
// is the thing that will appear in the project's key list afterwards and can be
// revoked. Handing back only the file would leave somebody holding a secret with
// no way to find the row it belongs to.
type acmeDNSIssued struct {
	Credentials acmeDNSCredentials `json:"credentials"`
	// Domain is the name the credentials are for, and it is on the wire because
	// HALF THE CLIENTS KEY THE FILE BY IT. There is no single acme-dns
	// credentials format: lego, Traefik, cert-manager and certbot's plugin read
	// a JSON object keyed by domain; win-acme and Certify The Web read a flat
	// object in a file named for the domain; Posh-ACME reads a positional
	// four-element array keyed by `_acme-challenge.<domain>`. The four FIELDS are
	// the same in all of them, so the service answers with the fields and the
	// console wraps them per client — which is why the domain has to come along.
	Domain string  `json:"domain"`
	Key    keyView `json:"key"`
	Notice string  `json:"notice"`
	// CNAME is the record the customer publishes, repeated here because this is
	// the screen somebody is on when they set a client up, and it is the other
	// half of making that client work. It is the same record the DNS plan shows.
	CNAME string `json:"cname"`
}

// issueAcmeDNSCredentials mints a project key for a domain's project and answers
// with the acme-dns credentials file built from it.
//
// WHY THIS IS ONE ROUTE AND NOT TWO. The console could mint a key and assemble
// the file itself — it has the label, and the zone is on the DNS plan. It would
// then be the console's job to concatenate a hostname correctly, and to keep
// doing so, for a file whose whole failure mode is a near-miss discovered weeks
// later. The service knows the zone it is authoritative for; it should be the
// one that writes the name down.
//
// The key is a PROJECT key (api-key-kinds K1): bound to the domain's project by
// a foreign key, holding that project's challenge answering and nothing else,
// and deleted with the project. This is the whole reason that change sits before
// this one — a credentials file handing out a key assembled by hand out of the
// grant grammar is the file somebody pastes into a Kubernetes Secret and never
// looks at again.
//
// It is a POST because it CREATES a credential. Somebody who opens this screen
// twice has two keys, which is correct and is what the key list is for; making
// it a GET would either mean storing the secret or answering a different secret
// each time to a verb that promises neither.
func (a *admin) issueAcmeDNSCredentials(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	domainID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		Note            string `json:"note"`
		RestrictToNodes bool   `json:"restrictToNodes"`
		ExpiresIn       string `json:"expiresIn"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	expires, err := parseExpiry(body.ExpiresIn)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()
	d, err := store.GetDomain(ctx, a.db(), sc, domainID)
	if err != nil {
		return err
	}
	al, err := store.GetAlias(ctx, a.db(), sc, d.AliasID)
	if err != nil {
		return err
	}
	if body.Note == "" {
		body.Note = "acme-dns for " + d.Name
	}

	zone := strings.TrimSuffix(a.opts.Config.Zone, ".")
	k := &model.APIKey{
		Kind: model.APIKeyKindProject, ProjectID: &d.ProjectID,
		RestrictToProjectNodes: body.RestrictToNodes,
	}
	return a.mintFor(c, k, body.Note, nil, expires, func(v keyView, secret string) any {
		return acmeDNSIssued{
			Credentials: acmeDNSCredentials{
				Username:   v.Identifier,
				Password:   secret,
				FullDomain: al.Label + "." + zone,
				Subdomain:  al.Label,
			},
			Domain: d.Name,
			Key:    v,
			Notice: secretNotice,
			CNAME:  al.ChallengeFQDN + ". IN CNAME " + al.Label + "." + zone + ".",
		}
	})
}
