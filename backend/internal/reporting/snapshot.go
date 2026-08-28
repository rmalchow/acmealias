package reporting

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// SnapshotInterval is how often the inventory snapshot runs.
//
// Hourly for a table whose grain is a day, deliberately. The job writes TODAY's
// row, so running it often is how it becomes robust rather than how it becomes
// wasteful: a service restarted at 23:50 still records the day, a service that
// was down at midnight records the day as soon as it is back, and an operator
// who adds thirty domains at lunchtime sees them in today's row within the hour
// instead of tomorrow. Twenty-four upserts a day per project is not a cost worth
// optimising.
const SnapshotInterval = time.Hour

// RunSnapshots writes the inventory snapshot on a ticker until ctx is cancelled.
// It takes one snapshot immediately so a fresh deployment has a first data point
// without waiting an hour for one.
func RunSnapshots(ctx context.Context, db *gorm.DB, interval time.Duration, log *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := Snapshot(ctx, db, time.Now()); err != nil {
			log.Error("inventory snapshot failed", "error", err)
		} else {
			log.Debug("inventory snapshot written", "projects", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Snapshot records every project's inventory size for the day containing `at`,
// and returns how many projects it wrote.
//
// WHAT HAPPENS WHEN IT RUNS TWICE IN A DAY, which it will — hourly ticks, a
// restart, two instances deployed side by side: the second run OVERWRITES the
// first for that (project_id, day). Never a duplicate, never a doubled number.
// That safety is not a lock or a "have I run today" flag; it is the shape of the
// data. Each row is a LEVEL — "this project had 12 aliases" — and re-reading a
// level is idempotent by construction, whereas a table of increments would need
// the run itself to be exactly-once and would silently double the day's figures
// the first time it was not. The last run of the day therefore wins, which is
// also the reading an operator means by "how big was it on the 20th".
//
// The counts are read in the same statement that writes them, so a project
// created mid-pass is either fully counted or not in the pass at all.
// It reads across every account by definition — one pass covers the deployment,
// not a tenant — which is why it is one of the four readers CrossAccount's own
// doc comment enumerates. Saying so is not optional: `projects`, `aliases`,
// `domains` and `load_balancers` all carry the tenant_isolation policy, and
// this runs on a ticker with no session and therefore no
// `app.account_id`, so as a bare statement it selects from an empty view of every
// one of them and writes nothing at all, for ever, without erroring.
func Snapshot(ctx context.Context, db *gorm.DB, at time.Time) (int64, error) {
	day := at.UTC().Format("2006-01-02")
	var rows int64
	err := store.CrossAccount(ctx, db, func(tx *gorm.DB) error {
		res := tx.Exec(`
		insert into project_daily_stats
		       (project_id, project_slug, day, alias_count, domain_count, load_balancer_count)
		select p.id, p.slug, ?::date,
		       (select count(*) from aliases a
		          join load_balancers l on l.id = a.load_balancer_id
		         where l.project_id = p.id),
		       (select count(*) from domains d
		          join load_balancers l on l.id = d.load_balancer_id
		         where l.project_id = p.id),
		       (select count(*) from load_balancers l where l.project_id = p.id)
		  from projects p
		    on conflict (project_id, day) do update
		   set project_slug        = excluded.project_slug,
		       alias_count         = excluded.alias_count,
		       domain_count        = excluded.domain_count,
		       load_balancer_count = excluded.load_balancer_count`, day)
		if res.Error != nil {
			return res.Error
		}
		rows = res.RowsAffected
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("reporting: snapshot inventory for %s: %w", day, err)
	}
	return rows, nil
}

// InventoryPoint is one project's inventory size on one day.
type InventoryPoint struct {
	Day               time.Time
	AliasCount        int
	DomainCount       int
	LoadBalancerCount int
}

// CurrentInventory counts a project's inventory as it is right now, live from
// the inventory tables rather than from the snapshot — so the console's
// "current" figure includes the domain that was added five minutes ago and the
// snapshot has not seen yet.
func CurrentInventory(ctx context.Context, db *gorm.DB, sc store.Scope, projectID model.ID) (InventoryPoint, error) {
	var row struct {
		AliasCount, DomainCount, LoadBalancerCount int
	}
	err := db.WithContext(ctx).Raw(`
		select (select count(*) from aliases a
		          join load_balancers l on l.id = a.load_balancer_id
		         where l.project_id = ?) as alias_count,
		       (select count(*) from domains d
		          join load_balancers l on l.id = d.load_balancer_id
		         where l.project_id = ?) as domain_count,
		       (select count(*) from load_balancers l where l.project_id = ?) as load_balancer_count`,
		projectID, projectID, projectID).Scan(&row).Error
	if err != nil {
		return InventoryPoint{}, fmt.Errorf("reporting: count inventory for project %d: %w", projectID, err)
	}
	return InventoryPoint{
		Day: time.Now().UTC(), AliasCount: row.AliasCount,
		DomainCount: row.DomainCount, LoadBalancerCount: row.LoadBalancerCount,
	}, nil
}

// InventoryHistory is a project's recorded inventory sizes over a range.
type InventoryHistory struct {
	Points []InventoryPoint
	// Recorded is false when no snapshot has EVER been written for this project
	// — the service was deployed less than an hour ago, or the snapshot job is
	// not running. Distinct from a range that happens to hold no rows, and very
	// distinct from a genuine zero: inventory cannot be backfilled, so the days
	// before the first snapshot are permanently unanswerable and must not be
	// drawn as an empty project.
	Recorded bool
}

// ProjectInventoryHistory reads the daily snapshot for one project.
//
// Days with no row are LEFT OUT rather than filled in. Carrying yesterday's
// count forward across a gap would invent a reading for a day nobody measured,
// which is the one thing this table exists to avoid.
func ProjectInventoryHistory(ctx context.Context, db *gorm.DB, sc store.Scope, projectID model.ID, from, to time.Time) (InventoryHistory, error) {
	var rows []model.ProjectDailyStat
	err := db.WithContext(ctx).
		Where("project_id = ? AND day >= ?::date AND day <= ?::date",
			projectID, from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02")).
		Order("day").
		Find(&rows).Error
	if err != nil {
		return InventoryHistory{}, fmt.Errorf("reporting: read inventory history for project %d: %w", projectID, err)
	}

	var ever int64
	err = db.WithContext(ctx).Model(&model.ProjectDailyStat{}).
		Where("project_id = ?", projectID).Count(&ever).Error
	if err != nil {
		return InventoryHistory{}, fmt.Errorf("reporting: count inventory history for project %d: %w", projectID, err)
	}

	out := make([]InventoryPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, InventoryPoint{
			Day: r.Day.UTC(), AliasCount: r.AliasCount,
			DomainCount: r.DomainCount, LoadBalancerCount: r.LoadBalancerCount,
		})
	}
	return InventoryHistory{Points: out, Recorded: ever > 0}, nil
}
