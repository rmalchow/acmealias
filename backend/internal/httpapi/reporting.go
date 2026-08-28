package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/reporting"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Usage reporting, per project. Two routes, both read-only:
//
//	GET /projects/:id/usage               attempts over time, by outcome
//	GET /projects/:id/inventory-history   alias/domain/load balancer counts over time
//
// There is no per-domain route and none is planned (scope decided 2026-08-21):
// reporting is per project and per load balancer. "Which domains never got
// queried" is answered by GET /projects/:id/interactions?unqueried=true,
// which names the alias and the domains it served.
//
// Every response says, in the payload, what it is counting. That is not
// decoration: challenge attempts and issuance attempts are DIFFERENT NUMBERS —
// a wildcard certificate publishes two values for one request — and a client
// that treats one as the other reports double for every wildcard customer.
// Neither is a count of certificates; this service never sees one.

// measureNotes travels in every reporting response so the vocabulary reaches
// whoever is reading the JSON, not only whoever read the docs.
var measureNotes = map[string]string{
	"challenge_attempts": "One published TXT value. A certificate covering example.com and " +
		"*.example.com publishes two values at one challenge name, so this counts two.",
	"issuance_attempts": "Challenge attempts at one challenge name whose lifetimes overlap, " +
		"grouped into one — the wildcard-plus-apex pair above counts one. This is the closest " +
		"thing to 'renewals attempted' that this service can observe.",
	"not_certificates": "Neither figure is a count of certificates issued. The certificate is " +
		"finalized between the load balancer's ACME client and the CA, which this service never sees.",
}

type usageBucketView struct {
	Start                  time.Time        `json:"start"`
	ChallengeAttempts      map[string]int64 `json:"challengeAttempts"`
	IssuanceAttempts       map[string]int64 `json:"issuanceAttempts"`
	ChallengeAttemptsTotal int64            `json:"challengeAttemptsTotal"`
	IssuanceAttemptsTotal  int64            `json:"issuanceAttemptsTotal"`
}

type usageResponse struct {
	ProjectID model.ID          `json:"projectId"`
	From      time.Time         `json:"from"`
	To        time.Time         `json:"to"`
	Bucket    string            `json:"bucket"`
	Outcomes  []string          `json:"outcomes"`
	Measures  map[string]string `json:"measures"`
	// Recorded is false when this project has no interaction history at all.
	// The buckets below are then every-outcome zeroes, and a client MUST render
	// "no data recorded yet" rather than "0 attempts": history cannot be
	// backfilled, so a service deployed last week cannot substantiate a zero for
	// last month, and a fresh deployment that reports zeros looks like a broken
	// one.
	Recorded bool              `json:"recorded"`
	Buckets  []usageBucketView `json:"buckets"`
}

// getProjectUsage serves GET /api/v1/projects/:id/usage.
//
// from and to are REQUIRED. A default range would be a number nobody asked for
// appearing in a report somebody quotes — "attempts: 41" means nothing without
// the period, and the caller is the only one who knows which period they mean.
func (a *admin) getProjectUsage(c echo.Context) error {
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

	from, to, err := readTimeRange(c)
	if err != nil {
		return err
	}
	if from == nil || to == nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			"from and to are required: a usage figure without the period it covers is not a figure")
	}

	// Day unless asked otherwise: it is the grain the data is recorded at, and a
	// month bucket silently hides the weekly renewal pattern operators look for.
	bucket := c.QueryParam("bucket")
	if bucket == "" {
		bucket = string(reporting.BucketDay)
	}
	size, err := reporting.ParseBucketSize(bucket)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	usage, err := reporting.ProjectAttempts(c.Request().Context(), a.db(), sc, id, *from, *to, size)
	if err != nil {
		return err
	}

	buckets := make([]usageBucketView, 0, len(usage.Buckets))
	for _, b := range usage.Buckets {
		buckets = append(buckets, usageBucketView{
			Start: b.Start, ChallengeAttempts: b.ChallengeAttempts, IssuanceAttempts: b.IssuanceAttempts,
			ChallengeAttemptsTotal: b.ChallengeAttemptsTotal, IssuanceAttemptsTotal: b.IssuanceAttemptsTotal,
		})
	}
	return c.JSON(http.StatusOK, usageResponse{
		ProjectID: id, From: *from, To: *to, Bucket: string(size),
		Outcomes: reporting.Outcomes, Measures: measureNotes,
		Recorded: usage.Recorded, Buckets: buckets,
	})
}

type inventoryPointView struct {
	Day         string `json:"day"` // YYYY-MM-DD: a date, not an instant in some zone
	AliasCount  int    `json:"aliasCount"`
	DomainCount int    `json:"domainCount"`
}

type inventoryHistoryResponse struct {
	ProjectID model.ID `json:"projectId"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	// Recorded is false when no daily snapshot has ever been written for this
	// project — the service has been up for less than an hour, or the snapshot
	// job is not running. Same rule as usage: not a zero.
	Recorded bool                 `json:"recorded"`
	Days     []inventoryPointView `json:"days"`
	Current  inventoryPointView   `json:"current"`
	Measures map[string]string    `json:"measures"`
}

// getProjectInventoryHistory serves GET /api/v1/projects/:id/inventory-history.
//
// Days with no snapshot are absent from `days` rather than filled forward.
// Carrying the last reading across a gap would invent a measurement for a day
// nobody measured, and this table exists precisely because inventory size cannot
// be reconstructed after the fact.
func (a *admin) getProjectInventoryHistory(c echo.Context) error {
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

	from, to, err := readTimeRange(c)
	if err != nil {
		return err
	}
	if from == nil || to == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "from and to are required")
	}

	hist, err := reporting.ProjectInventoryHistory(ctx, a.db(), sc, id, *from, *to)
	if err != nil {
		return err
	}
	days := make([]inventoryPointView, 0, len(hist.Points))
	for _, p := range hist.Points {
		days = append(days, inventoryPointView{
			Day: p.Day.Format("2006-01-02"), AliasCount: p.AliasCount, DomainCount: p.DomainCount,
		})
	}

	// Today's counts read live rather than from the snapshot: the console shows
	// "current" beside "historical", and an operator who just added a domain
	// should see it without waiting for the next snapshot tick.
	cur, err := reporting.CurrentInventory(ctx, a.db(), sc, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, inventoryHistoryResponse{
		ProjectID: id,
		From:      from.UTC().Format("2006-01-02"), To: to.UTC().Format("2006-01-02"),
		Recorded: hist.Recorded, Days: days,
		Current: inventoryPointView{
			Day:        time.Now().UTC().Format("2006-01-02"),
			AliasCount: cur.AliasCount, DomainCount: cur.DomainCount,
		},
		Measures: map[string]string{
			"aliasCount":  "Challenge names this project owns — one per group of domains sharing a certificate.",
			"domainCount": "Domains configured, including each wildcard as its own entry.",
			"history":     "Recorded once a day and never backfilled; days before the first snapshot are unknown, not zero.",
		},
	})
}
