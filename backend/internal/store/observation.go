package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The certificate-observation history: one row per TLS handshake attempted,
// written by the internal/certobserve worker and read by the console (change
// domain-diagnostics).
//
// The two halves are scoped differently on purpose, and the split is the same
// one internal/activation has. The worker runs on a ticker with no session, so
// its queue and its write lift the tenant policies; the reads run on a request
// and do not. Nothing here classifies, bands or interprets an observation —
// that is certobserve's, so that the console and adminctl cannot disagree about
// what a row means.

// MaxObservationHistory caps how many observations one read may return.
//
// "Every observation ever" is a typo rather than a question — the same reasoning
// as reporting.MaxBuckets, and the same shape of answer: the history stops here
// instead of streaming a year of rows into a dialog nobody scrolls. Four
// observations a day per address, so this is roughly three months of a
// four-address domain.
const MaxObservationHistory = 1000

// DueObservation is one handshake the observation worker should attempt: which
// name to present, to which address, on behalf of which domain.
//
// It carries the ownership columns because the row the worker writes has to
// carry them too, and reading them back off the domain afterwards would be a
// second query whose answer could have changed. The worker never resolves the
// server name — it dials the ADDRESS and presents ServerName as SNI, which is
// what makes this an observation of OUR inventory rather than of whatever the
// customer's DNS points at today (design G6).
type DueObservation struct {
	DomainID   model.ID `gorm:"column:domain_id"`
	AccountID  model.ID `gorm:"column:account_id"`
	ProjectID  model.ID `gorm:"column:project_id"`
	ServerName string   `gorm:"column:server_name"` // tls_test_host, or the domain's own name
	NodeID     model.ID `gorm:"column:node_id"`
	Family     string   `gorm:"column:family"` // "A" | "AAAA"
	Address    string   `gorm:"column:address"`
}

// DueObservations returns one row per (enabled domain × node address × family)
// that is due, newly-enabled work first, at most limit of them.
//
// A DOMAIN WITH THE SWITCH OFF NEVER APPEARS, and that is the only thing
// standing between this feature and a connection to every load balancer this
// service has been told about. Observation is opt-in per domain (G-constraint 3)
// and this predicate is where the opt-in is enforced — not in the worker, which
// probes whatever it is handed.
//
// Each family is a separate row because each is a separate attempt and a
// separate observation: a node reached over v4 and over v6 goes through
// different transit and different firewalls, and a certificate renewed on one
// listener and not the other is invisible to a dual-stack visitor (G6). A node
// with only one address yields one row, and the absent family is not a failure —
// there was nothing to attempt.
//
// Ordering copies DueDelegations': never observed first, then longest since last
// observed. A batch too small for the backlog therefore delays the settled work
// and never the person who just turned the switch on and is watching the screen
// (G13).
//
// Wrapped in CrossAccount: the worker runs on a ticker with no session, so a
// bare statement here selects from an empty view of every tenant table and finds
// nothing at all, for ever, without erroring — see reporting.Snapshot's doc
// comment for the same trap.
//
// The coalesce needs no fallback beyond the domain's own name: a wildcard with
// observe_tls on and a null tls_test_host cannot exist, because
// domains_wildcard_observation_needs_a_test_host refuses it (G8).
func DueObservations(ctx context.Context, db *gorm.DB, now time.Time,
	interval time.Duration, limit int) ([]DueObservation, error) {

	var out []DueObservation
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Raw(`
		with target as (
		  select d.id as domain_id, d.account_id, d.project_id,
		         coalesce(d.tls_test_host, d.name) as server_name,
		         n.id as node_id, fam.family, fam.address
		    from domains d
		    join lb_nodes n on n.project_id = d.project_id
		    cross join lateral (values ('A', n.ipv4), ('AAAA', n.ipv6)) as fam(family, address)
		   where d.observe_tls and fam.address is not null
		)
		select t.domain_id, t.account_id, t.project_id, t.server_name,
		       t.node_id, t.family, host(t.address) as address
		  from target t
		  left join lateral (
		         select o.observed_at
		           from certificate_observations o
		          where o.domain_id = t.domain_id
		            and o.node_id   = t.node_id
		            and o.family    = t.family
		          order by o.observed_at desc
		          limit 1
		       ) last on true
		 where last.observed_at is null or last.observed_at < ?
		 order by (last.observed_at is null) desc, last.observed_at nulls first
		 limit ?`,
			now.Add(-interval), limit).Scan(&out).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: observations due: %w", err)
	}
	return out, nil
}

// RecordObservation appends one observation — a completed handshake or a failed
// one, which are the same kind of fact and the same kind of row (design G11).
//
// Cross-account for the reason DueObservations is: the worker holds no session,
// and the ownership columns on the row come off the queue entry it was handed,
// which the database itself materialized from the domain. What confines the
// write is therefore the composite foreign keys — an observation naming a domain
// of one account while claiming another references a pair of rows that does not
// exist, and the insert fails rather than landing in the wrong tenant.
func RecordObservation(ctx context.Context, db *gorm.DB, o model.CertificateObservation) error {
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Create(&o).Error
	})
	if err != nil {
		return fmt.Errorf("store: record observation of domain %s: %w", o.DomainID, err)
	}
	return nil
}

// LatestObservations returns the most recent observation for each of a domain's
// (node, family) pairs — the set the current state is derived from.
//
// One row per address rather than one row per domain, because the state IS
// per-address: certobserve takes the worst of them for the dot and the console
// lists them all in the dialog, so collapsing here would throw away the half
// that explains the other half.
//
// Scoped, not CrossAccount: this runs on a request.
func LatestObservations(ctx context.Context, db *gorm.DB, sc Scope, domainID model.ID) ([]model.CertificateObservation, error) {
	var out []model.CertificateObservation
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		// distinct on is Postgres's "first row per group" and needs the group
		// columns to lead the ordering; observed_at desc inside each group is
		// what makes the surviving row the latest one.
		return sc.Where(tx).Model(&model.CertificateObservation{}).
			Select("distinct on (node_id, family) *").
			Where("domain_id = ?", domainID).
			Order("node_id, family, observed_at desc").
			Find(&out).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: latest observations of domain %s: %w", domainID, err)
	}
	return out, nil
}

// ObservationHistory returns a domain's observations newest first, at most limit
// of them and never more than MaxObservationHistory.
//
// The ceiling is applied here rather than trusted from the caller: a limit is a
// query parameter somewhere upstream, and the honest failure for "give me
// everything" is a bounded answer rather than a timeout. A non-positive limit
// takes the ceiling too — it is a caller that did not choose, not a caller
// asking for nothing.
//
// Scoped, not CrossAccount: this runs on a request.
func ObservationHistory(ctx context.Context, db *gorm.DB, sc Scope, domainID model.ID, limit int) ([]model.CertificateObservation, error) {
	if limit <= 0 || limit > MaxObservationHistory {
		limit = MaxObservationHistory
	}
	var out []model.CertificateObservation
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Model(&model.CertificateObservation{}).
			Where("domain_id = ?", domainID).
			Order("observed_at desc").
			Limit(limit).
			Find(&out).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: observation history of domain %s: %w", domainID, err)
	}
	return out, nil
}
