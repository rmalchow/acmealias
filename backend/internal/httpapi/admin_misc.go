package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// GET /me, the live challenge view, the audit trail and the redacted
// effective configuration. All four are reads; none of them mutates anything,
// so none of them writes an audit entry.

// meView is what the console shell renders: a name in the corner, the groups
// behind a role, and the roles that decide which controls exist.
//
// permissions is included so the console does not have to reimplement the
// role matrix to decide what to grey out. It is a convenience for the client
// and nothing more: every route checks the permission again server-side,
// because the browser is not the enforcement point (design D11).
type meView struct {
	Subject     string    `json:"subject"`
	DisplayName string    `json:"displayName"`
	Groups      []string  `json:"groups"`
	Roles       []string  `json:"roles"`
	Permissions []string  `json:"permissions"`
	SignedInAt  time.Time `json:"signedInAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (a *admin) me(c echo.Context) error {
	sess := authz.SessionFromContext(c)
	roles := authz.RolesFromContext(c)
	if sess == nil {
		// Unreachable behind Authenticated; present so a rewiring that drops
		// the middleware fails closed instead of answering with an empty user.
		return echo.ErrUnauthorized
	}
	v := meView{
		Subject: sess.Subject, DisplayName: sess.DisplayName,
		Groups: []string(sess.Groups), Roles: roles.List(),
		Permissions: []string{},
		SignedInAt:  sess.CreatedAt, ExpiresAt: sess.ExpiresAt,
	}
	if v.Groups == nil {
		v.Groups = []string{}
	}
	for _, p := range authz.Allowed(roles) {
		v.Permissions = append(v.Permissions, string(p))
	}
	return c.JSON(http.StatusOK, v)
}

// --- live challenges -------------------------------------------------------

type challengeView struct {
	ID            model.ID  `json:"id"`
	AliasLabel    string    `json:"aliasLabel"`
	ChallengeName string    `json:"challengeName"`
	Value         string    `json:"value"`
	PublishedBy   *string   `json:"publishedBy"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	AgeSeconds    int64     `json:"ageSeconds"`
}

// listChallenges answers the live-challenge screen. `q` matches the alias
// label, the challenge FQDN or the key that published it — the three things
// an operator has in hand when they are chasing one renewal through a busy
// window.
func (a *admin) listChallenges(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	page, err := readPage(c, 50, 500)
	if err != nil {
		return err
	}
	cs, total, err := store.ListActivePage(c.Request().Context(), a.db(), sc,
		c.QueryParam("q"), page.Limit, page.Offset)
	if err != nil {
		return err
	}
	setTotal(c, total)
	now := time.Now()
	out := make([]challengeView, 0, len(cs))
	for _, ch := range cs {
		v := challengeView{
			ID: ch.ID, Value: ch.Value, PublishedBy: ch.PublishedBy,
			CreatedAt: ch.CreatedAt, ExpiresAt: ch.ExpiresAt,
			AgeSeconds: int64(now.Sub(ch.CreatedAt).Seconds()),
		}
		if ch.Alias != nil {
			v.AliasLabel, v.ChallengeName = ch.Alias.Label, ch.Alias.ChallengeFQDN
		}
		out = append(out, v)
	}
	return c.JSON(http.StatusOK, out)
}

// --- audit trail -----------------------------------------------------------

type auditView struct {
	ID           model.ID        `json:"id"`
	OccurredAt   time.Time       `json:"occurredAt"`
	ActorSubject *string         `json:"actorSubject"`
	ActorName    *string         `json:"actorName"`
	ActorGroups  []string        `json:"actorGroups"`
	ActorRoles   []string        `json:"actorRoles"`
	ActorSource  string          `json:"actorSource"`
	RequestID    *string         `json:"requestId"`
	ClientIP     *string         `json:"clientIp"`
	Action       string          `json:"action"`
	ObjectType   string          `json:"objectType"`
	ObjectID     *model.ID       `json:"objectId"`
	ObjectLabel  string          `json:"objectLabel"`
	ProjectID    *model.ID       `json:"projectId"`
	Before       json.RawMessage `json:"before,omitempty"`
	After        json.RawMessage `json:"after,omitempty"`
}

// listAudit serves the filtered trail: project, actor and action, which are
// the three questions anyone actually asks of it.
//
// ponytail: the query is built here rather than through store.AuditFilter,
// which has no actor field and belongs to another package in this change.
// Upgrade path: add ActorSubject to store.AuditFilter and call
// store.ListAuditEntries instead — this function then collapses to a filter
// literal.
func (a *admin) listAudit(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	// Confined first, before any caller-supplied filter can narrow or widen it:
	// the trail names people, addresses and object labels, so reading another
	// account's is more disclosive than reading their inventory.
	q := a.db().WithContext(c.Request().Context()).
		Model(&model.AuditEntry{}).
		Where("account_id = ?", sc.AccountID())

	if v := c.QueryParam("projectId"); v != "" {
		id, err := model.ParseID(v)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "projectId must be an identifier")
		}
		q = q.Where("project_id = ?", id)
	}
	if v := c.QueryParam("actor"); v != "" {
		q = q.Where("actor_subject = ? OR actor_name = ?", v, v)
	}
	if v := c.QueryParam("action"); v != "" {
		q = q.Where("action = ?", v)
	}
	if v := c.QueryParam("objectType"); v != "" {
		q = q.Where("object_type = ?", v)
	}
	if v := c.QueryParam("objectId"); v != "" {
		id, err := model.ParseID(v)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "objectId must be an identifier")
		}
		q = q.Where("object_id = ?", id)
	}
	// The class filter answers the two questions the trail is actually opened
	// for — "who changed the setup" and "who touched the credentials" —
	// without the operator having to know the whole action vocabulary. An
	// unknown class is a 400: an empty listing would read as "nothing
	// happened", which is the one answer an audit trail must never give by
	// accident.
	if v := c.QueryParam("actionClass"); v != "" {
		actions := authz.ActionsInClass(v)
		if actions == nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				`actionClass must be "`+authz.ClassConfig+`" or "`+authz.ClassCredential+`"`)
		}
		q = q.Where("action IN ?", actions)
	}

	// The date range the console's range picker submits. Half-open at neither
	// end: an operator who names a day means that whole day, and parseBound
	// has already pushed a bare `to` date to its last instant.
	from, to, err := readTimeRange(c)
	if err != nil {
		return err
	}
	if from != nil {
		q = q.Where("occurred_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("occurred_at <= ?", *to)
	}

	page, err := readPage(c, 50, 1000)
	if err != nil {
		return err
	}

	// Counted before paging, so the pager can say "51-100 of 4,213" — the
	// number that tells an operator whether their filter actually narrowed
	// anything.
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return err
	}
	setTotal(c, total)

	var es []model.AuditEntry
	if err := page.apply(q.Order("occurred_at DESC, id DESC")).Find(&es).Error; err != nil {
		return err
	}
	out := make([]auditView, 0, len(es))
	for _, e := range es {
		out = append(out, auditView{
			ID: e.ID, OccurredAt: e.OccurredAt,
			ActorSubject: e.ActorSubject, ActorName: e.ActorName,
			ActorGroups: []string(e.ActorGroups), ActorRoles: []string(e.ActorRoles),
			ActorSource: e.ActorSource, RequestID: e.RequestID, ClientIP: e.ClientIP,
			Action: e.Action, ObjectType: e.ObjectType, ObjectID: e.ObjectID,
			ObjectLabel: e.ObjectLabel, ProjectID: e.ProjectID,
			Before: e.Before, After: e.After,
		})
	}
	return c.JSON(http.StatusOK, out)
}

// --- effective configuration ----------------------------------------------

// getConfig renders the running configuration with secrets redacted, plus the
// group configured for each role.
//
// The redaction is config.LogValue's, reused rather than repeated: there is
// exactly one list of which settings are secret in this service, and a second
// one here would eventually disagree with it — in the direction that puts a
// client secret on a screen.
//
// The mapping is shown, never edited: it is four environment variables, and
// there is deliberately no route that changes them (design D10).
func (a *admin) getConfig(c echo.Context) error {
	out := map[string]any{}
	for _, attr := range a.opts.Config.LogValue().Group() {
		switch attr.Value.Kind() {
		case slog.KindDuration:
			out[attr.Key] = attr.Value.Duration().String()
		default:
			out[attr.Key] = attr.Value.Any()
		}
	}
	out["roleGroups"] = a.opts.Guard.Mapping.Entries()
	return c.JSON(http.StatusOK, out)
}
