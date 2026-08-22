package reporting

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Bucket sizes the time axis of a usage report.
type BucketSize string

const (
	BucketDay   BucketSize = "day"
	BucketMonth BucketSize = "month"
)

// ParseBucketSize validates a caller-supplied bucket name.
func ParseBucketSize(s string) (BucketSize, error) {
	switch BucketSize(s) {
	case BucketDay:
		return BucketDay, nil
	case BucketMonth:
		return BucketMonth, nil
	default:
		return "", fmt.Errorf("bucket must be %q or %q, got %q", BucketDay, BucketMonth, s)
	}
}

// MaxBuckets caps the length of a series. A range of "from 1970 to now" with a
// daily bucket is a typo, not a question, and answering it with twenty thousand
// entries helps nobody: the series stops here instead. Five years of days.
const MaxBuckets = 1830

// UsageBucket is one period of a project's usage, counted both ways.
//
// Both maps are keyed by outcome and carry an entry for every value in Outcomes,
// including the zeroes — a chart that silently drops a series is worse than one
// that draws it flat.
type UsageBucket struct {
	Start                  time.Time
	ChallengeAttempts      map[string]int64
	IssuanceAttempts       map[string]int64
	ChallengeAttemptsTotal int64
	IssuanceAttemptsTotal  int64
}

// ProjectUsage is a project's attempts over a range.
type ProjectUsage struct {
	Buckets []UsageBucket
	// Recorded is false when this project has NO interaction history at all —
	// not "none in the requested range", none ever. It is what lets a report
	// say "nothing has been recorded yet" instead of "zero attempts", which is
	// a different and much more alarming claim to make about a service that was
	// deployed on Tuesday. History cannot be backfilled, so a freshly deployed
	// service is permanently unable to substantiate a zero for last month.
	Recorded bool
}

// ProjectAttempts aggregates one project's challenge and issuance attempts over
// an explicit range, bucketed and split by outcome.
//
// Attribution follows project_id as it was COPIED ONTO THE ROW at publication
// time, not as the domain is filed today: a domain moved to another project in
// June does not rewrite May's numbers for the project that actually did the
// renewing. That is the whole reason the column is a denormalized bigint with no
// foreign key.
//
// ponytail: the rows of the range are read and grouped in Go rather than by
// GROUP BY, because the issuance grouping (which SQL would need a window
// function for) and the bucketing want the same single pass, and the volume is
// single-digit rows a day. Ceiling: the range's rows are held in memory. Upgrade
// path: aggregate in SQL and group issuances per bucket, when a range first
// holds enough rows to notice.
func ProjectAttempts(ctx context.Context, db *gorm.DB, projectID int64, from, to time.Time, size BucketSize) (ProjectUsage, error) {
	var ins []model.ChallengeInteraction
	err := db.WithContext(ctx).
		Where("project_id = ? AND published_at >= ? AND published_at <= ?", projectID, from, to).
		Order("published_at").
		Find(&ins).Error
	if err != nil {
		return ProjectUsage{}, fmt.Errorf("reporting: read interactions for project %d: %w", projectID, err)
	}

	var everRecorded int64
	err = db.WithContext(ctx).Model(&model.ChallengeInteraction{}).
		Where("project_id = ?", projectID).Count(&everRecorded).Error
	if err != nil {
		return ProjectUsage{}, fmt.Errorf("reporting: count interactions for project %d: %w", projectID, err)
	}

	return ProjectUsage{Buckets: bucketize(ins, from, to, size), Recorded: everRecorded > 0}, nil
}

// bucketize is the pure half of ProjectAttempts: rows in, series out. Split off
// so the counting rules can be tested without a database.
func bucketize(ins []model.ChallengeInteraction, from, to time.Time, size BucketSize) []UsageBucket {
	buckets := map[time.Time]*UsageBucket{}
	at := func(t time.Time) *UsageBucket {
		start := truncate(t, size)
		b := buckets[start]
		if b == nil {
			b = &UsageBucket{Start: start, ChallengeAttempts: zeroCounts(), IssuanceAttempts: zeroCounts()}
			buckets[start] = b
		}
		return b
	}

	for _, in := range ins {
		b := at(in.PublishedAt)
		b.ChallengeAttempts[outcomeOf(in)]++
		b.ChallengeAttemptsTotal++
	}
	// An issuance attempt lands in the bucket it STARTED in, so a validation that
	// straddles midnight is counted once, on the day the client asked for it.
	for _, is := range GroupIssuances(ins) {
		b := at(is.Start)
		b.IssuanceAttempts[is.Outcome]++
		b.IssuanceAttemptsTotal++
	}

	// Every bucket in the range, including the empty ones: a gap in a series
	// should be a zero-height bar, not a missing x value that makes a two-week
	// gap look like two adjacent days.
	out := make([]UsageBucket, 0, len(buckets)+1)
	for t := truncate(from, size); !t.After(to) && len(out) < MaxBuckets; t = next(t, size) {
		if b := buckets[t]; b != nil {
			out = append(out, *b)
			continue
		}
		out = append(out, UsageBucket{Start: t, ChallengeAttempts: zeroCounts(), IssuanceAttempts: zeroCounts()})
	}
	return out
}

func zeroCounts() map[string]int64 {
	m := make(map[string]int64, len(Outcomes))
	for _, o := range Outcomes {
		m[o] = 0
	}
	return m
}

// truncate snaps a timestamp to the start of its bucket, in UTC. UTC and not the
// server's local zone so that the same range asked for twice, from two hosts,
// answers the same thing.
func truncate(t time.Time, size BucketSize) time.Time {
	t = t.UTC()
	if size == BucketMonth {
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// next is the start of the following bucket. Calendar arithmetic via AddDate,
// not a fixed 24h — a day is not always 24 hours in a zone with DST, and a month
// is never a fixed number of them.
func next(t time.Time, size BucketSize) time.Time {
	if size == BucketMonth {
		return t.AddDate(0, 1, 0)
	}
	return t.AddDate(0, 0, 1)
}
