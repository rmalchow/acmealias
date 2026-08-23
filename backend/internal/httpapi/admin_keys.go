package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
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
	Type        string          `json:"type"`
	ObjectID    *model.ID       `json:"objectId,omitempty"`
	Selector    *authz.Selector `json:"selector,omitempty"`
	Action      string          `json:"action"`
	Inheritable bool            `json:"inheritable"`
	// Summary is the grant written the way the design writes them, so an
	// operator reading a listing does not have to assemble four fields in their
	// head.
	Summary string `json:"summary"`
}

type keyView struct {
	ID            model.ID    `json:"id"`
	Grants        []grantView `json:"grants"`
	Identifier    string      `json:"identifier"`
	DisplayPrefix string      `json:"displayPrefix"`
	Note          string      `json:"note"`
	Networks      []string    `json:"networks"`
	CreatedAt     time.Time   `json:"createdAt"`
	ExpiresAt     *time.Time  `json:"expiresAt"`
	LastUsedAt    *time.Time  `json:"lastUsedAt"`
	DisabledAt    *time.Time  `json:"disabledAt"`
	Active        bool        `json:"active"`
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
		ID: k.ID, Identifier: k.Identifier, DisplayPrefix: k.DisplayPrefix,
		Note: k.Note, Networks: []string{}, Grants: []grantView{}, CreatedAt: k.CreatedAt,
		ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, DisabledAt: k.DisabledAt,
	}
	for _, g := range k.Grants {
		parsed := authz.GrantFromRow(g)
		v.Grants = append(v.Grants, grantView{
			Type: g.ObjectType, ObjectID: g.ObjectID, Selector: parsed.Selector,
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
	loadBalancerID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := store.GetLoadBalancer(ctx, a.db(), sc, loadBalancerID); err != nil {
		return err
	}
	ks, err := store.ListAPIKeysForLoadBalancer(ctx, a.db(), sc, loadBalancerID)
	if err != nil {
		return err
	}
	out := make([]keyView, 0, len(ks))
	for _, k := range ks {
		out = append(out, toKey(k))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *admin) createKey(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	loadBalancerID, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		Note      string   `json:"note"`
		Networks  []string `json:"networks"`
		ExpiresIn string   `json:"expiresIn"` // a Go duration, e.g. "720h"; empty means no expiry
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
	if _, err := store.GetLoadBalancer(ctx, a.db(), sc, loadBalancerID); err != nil {
		return err
	}
	// The route is still POST /loadbalancers/{id}/keys, because that is the
	// screen an operator is on. What it creates is a key anchored to the account
	// holding grants over this load balancer — the same authority the dropped
	// column carried, now expressed in a grammar that can also say "this whole
	// project" when someone asks for it.
	grants := []model.APIKeyGrant{
		{ObjectType: string(authz.TypeLoadBalancer), ObjectID: &loadBalancerID, Action: challengePublish, Inheritable: true},
		{ObjectType: string(authz.TypeLoadBalancer), ObjectID: &loadBalancerID, Action: challengeWithdraw, Inheritable: true},
	}
	return a.mint(c, grants, body.Note, nets, expires, authz.ActionKeyCreate)
}

// rotateKey mints a successor for the same load balancer, copying the note and the
// address restrictions, and leaves the existing key working.
//
// That is the whole of design D3's rotation: create, deploy, then revoke the
// old one — the model supports several live keys per load balancer precisely so the
// window between the two never has a gap. Revoking the predecessor here
// automatically would reintroduce the downtime the design removed.
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
	return a.mint(c, grantsOf(old), note, nets, nil, authz.ActionKeyRotate)
}

// mint is the shared body of create and rotate: generate, persist, audit,
// answer once with the secret.
// grantsOf copies a key's grants for a rotation: the new key holds exactly what
// the old one did. Rotation replaces a secret, never an authority — a rotation
// that silently widened or narrowed what a key can do would be the worst kind of
// change, because nobody would look.
func grantsOf(k *model.APIKey) []model.APIKeyGrant {
	out := make([]model.APIKeyGrant, 0, len(k.Grants))
	for _, g := range k.Grants {
		out = append(out, model.APIKeyGrant{
			ObjectType: g.ObjectType, ObjectID: g.ObjectID, Selector: g.Selector,
			Action: g.Action, Inheritable: g.Inheritable,
		})
	}
	return out
}

func (a *admin) mint(c echo.Context, grants []model.APIKeyGrant, note string, networks []string, expires *time.Time, action string) error {
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

	k, secret, err := apikey.New()
	if err != nil {
		return err
	}
	k.Grants = grants
	k.Note = note
	k.ExpiresAt = expires
	for _, n := range networks {
		k.Networks = append(k.Networks, model.APIKeyNetwork{CIDR: n})
	}

	var v keyView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.CreateAPIKey(ctx, tx, sc, k, check); err != nil {
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
	return c.JSON(http.StatusCreated, keyCreated{Key: v, Secret: secret, Notice: secretNotice})
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
		return sc.Where(tx).Preload("Networks").Preload("Grants").First(&k, "id = ?", id).Error
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
