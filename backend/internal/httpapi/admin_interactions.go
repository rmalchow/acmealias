package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// The interaction history of one load balancer: what its load balancer actually did
// here, and whether the CA ever came to look.
//
// This is the screen that answers the question the rest of the console cannot:
// a renewal that never happened leaves no trace in the live challenge view
// (nothing is live) and only a thin one in the audit trail (nobody changed any
// inventory). It shows up here as a publication that was never queried.

type interactionView struct {
	ID             int64      `json:"id"`
	AliasLabel     string     `json:"aliasLabel"`
	ChallengeName  string     `json:"challengeName"`
	DomainNames    []string   `json:"domainNames"`
	KeyID          *string    `json:"keyId"`
	ClientIP       *string    `json:"clientIp"`
	FQDNForm       string     `json:"fqdnForm"`
	PublishedAt    time.Time  `json:"publishedAt"`
	WithdrawnAt    *time.Time `json:"withdrawnAt"`
	FirstQueriedAt *time.Time `json:"firstQueriedAt"`
	LastQueriedAt  *time.Time `json:"lastQueriedAt"`
	QueryCount     int64      `json:"queryCount"`
	// Outcome is derived rather than stored, so it stays correct for a row that
	// is still open while being read:
	//
	//   validated   withdrawn, and queried at least once — the normal life
	//   unqueried   withdrawn without ever being queried; the customer's CNAME
	//               is the first thing to check
	//   abandoned   never cleaned up; the sweep closed it. Points at the client
	//               that vanished, not at the customer's DNS
	//   live        still published, queried already
	//   pending     still published, nobody has asked yet
	Outcome string `json:"outcome"`
}

func (a *admin) listInteractions(c echo.Context) error {
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	page, err := readPage(c, 50, 500)
	if err != nil {
		return err
	}

	ins, total, err := store.ListInteractionsForLoadBalancer(
		c.Request().Context(), a.db(), id,
		c.QueryParam("q"),
		c.QueryParam("unqueried") == "true",
		page.Limit, page.Offset,
	)
	if err != nil {
		return err
	}
	setTotal(c, total)

	out := make([]interactionView, 0, len(ins))
	for _, in := range ins {
		names := []string(in.DomainNames)
		if names == nil {
			names = []string{}
		}
		out = append(out, interactionView{
			ID: in.ID, AliasLabel: in.AliasLabel, ChallengeName: in.ChallengeFQDN,
			DomainNames: names, KeyID: in.KeyID, ClientIP: in.ClientIP, FQDNForm: in.FQDNForm,
			PublishedAt: in.PublishedAt, WithdrawnAt: in.WithdrawnAt,
			FirstQueriedAt: in.FirstQueriedAt, LastQueriedAt: in.LastQueriedAt,
			QueryCount: in.QueryCount,
			Outcome:    outcomeOf(in.WithdrawnAt, in.Abandoned, in.QueryCount),
		})
	}
	return c.JSON(http.StatusOK, out)
}

func outcomeOf(withdrawnAt *time.Time, abandoned bool, queries int64) string {
	switch {
	case abandoned:
		return "abandoned"
	case withdrawnAt != nil && queries > 0:
		return "validated"
	case withdrawnAt != nil:
		return "unqueried"
	case queries > 0:
		return "live"
	default:
		return "pending"
	}
}
