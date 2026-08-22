// Package obs owns the service's Prometheus metrics and structured logging.
// Nothing here mounts an HTTP handler or a signal handler — that wiring, and
// the health/readiness endpoints, belong to the caller (task group 16).
package obs

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds every collector the service exposes, registered on their own
// Registry rather than the global default so tests can create independent
// instances and the caller controls what /metrics serves.
//
// Label cardinality is deliberately bounded: every label value here comes
// from a small, fixed vocabulary (a listener name, an HTTP route pattern, a
// status class, a DNS query type, an RCODE, an auth outcome reason). None of
// them is a domain, alias, project or API key identifier — one time series
// per customer name is the growth pattern that makes a Prometheus server
// unusable, and those series would outlive the domain that created them.
// Per-name history belongs in the datastore (store.ListActive, audit log),
// not in a metric label. See metrics_test.go, which asserts this rather than
// leaving it to a comment.
type Metrics struct {
	Registry *prometheus.Registry

	// HTTPRequests counts requests by listener (challenge/admin/internal),
	// route (the matched pattern, e.g. "/challenge/present", never the raw
	// path) and status code.
	HTTPRequests *prometheus.CounterVec

	// DNSQueries counts answered queries by query type (A, TXT, ...) and
	// response code (NOERROR, NXDOMAIN, ...).
	DNSQueries *prometheus.CounterVec

	// ActiveChallenges is the current count of answerable challenge values,
	// as reported by the in-memory zone view.
	ActiveChallenges prometheus.Gauge

	// SweepRemoved counts expired challenge rows deleted by the sweeper.
	SweepRemoved prometheus.Counter

	// DatabaseUp is 1 if the last reachability check succeeded, 0 otherwise.
	DatabaseUp prometheus.Gauge

	// AuthOutcomes counts authentication and authorization outcomes by
	// reason. An invalid key and a disallowed source address both refuse
	// the request with the same HTTP status, but they are counted under
	// different reasons here — that distinction is the entire point of the
	// metric. See the AuthReason* constants for the vocabulary.
	AuthOutcomes *prometheus.CounterVec

	// DNSQueryObservations counts, per flush, what happened to each DNS
	// query observation (one alias label's accumulated query count over a
	// window) as it was folded into challenge_interactions. See the
	// Attributed* constants: an unresolved label and a resolved label that
	// matched no live row are counted separately, because a rising rate on
	// either one means attribution is broken, and confusing them with "the
	// CA never queried anything" is exactly the failure this history exists
	// to catch.
	DNSQueryObservations *prometheus.CounterVec

	// ChallengeInteractions counts challenge_interactions rows by their
	// derived outcome (see httpapi's outcomeOf, which owns the vocabulary).
	// Only the terminal outcomes below have a place today; "live" and
	// "pending" are open-ended states of a row, not one-shot events, so
	// nothing increments them here.
	ChallengeInteractions *prometheus.CounterVec

	// AuditEntries counts audit_entries rows written, by action class.
	// Nothing in this package increments it: the writer that classifies an
	// action is authz.Record, which every path that appends a row goes
	// through. See the ActionClass* constants for the vocabulary.
	AuditEntries *prometheus.CounterVec
}

// Auth outcome reasons. Callers MUST use one of these rather than an
// ad hoc string, so the label's vocabulary stays fixed and small.
const (
	AuthReasonOK                = "ok"
	AuthReasonInvalidKey        = "invalid_key"
	AuthReasonDisallowedSource  = "disallowed_source"
	AuthReasonExpiredSession    = "expired_session"
	AuthReasonInsufficientRole  = "insufficient_role"
	AuthReasonMissingCredential = "missing_credential"
)

// Attribution outcomes for a DNS query observation, i.e. the "attributed"
// label of DNSQueryObservations. Callers MUST use one of these rather than
// an ad hoc string.
//
// AttributedFalseUnresolvedLabel and AttributedFalseNoInteraction are both
// "not attributed", but they mean different things and must stay separate
// series: the first is expected at low volume (a label can be deleted
// between a query being answered and the flush that follows it), while a
// rising AttributedFalseNoInteraction rate means resolved aliases are not
// finding their interaction rows — attribution is broken.
const (
	AttributedTrue                 = "true"
	AttributedFalseUnresolvedLabel = "false_unresolved_label"
	AttributedFalseNoInteraction   = "false_no_interaction_row"
)

// Outcome vocabulary for ChallengeInteractions, matching outcomeOf in
// backend/internal/httpapi/admin_interactions.go. Kept here as the single
// source of truth for the label's fixed vocabulary.
const (
	OutcomeValidated = "validated"
	OutcomeUnqueried = "unqueried"
	OutcomeAbandoned = "abandoned"
	OutcomeLive      = "live"
	OutcomePending   = "pending"
)

// Action-class vocabulary for AuditEntries, per design D7
// (add-audit-and-usage-reporting). The full action vocabulary is task 2.7's
// to add as constants in backend/internal/authz; these two classes are what
// every action rolls up to.
const (
	ActionClassConfig     = "config"
	ActionClassCredential = "credential"

	// ActionClassUnknown catches an action the classifier does not know. It
	// exists so that an action added to the vocabulary without a class shows
	// up as a rising series rather than as entries that are simply never
	// counted — a silently under-reporting counter is worse than a visibly
	// unclassified one.
	ActionClassUnknown = "unknown"
)

// New builds a Metrics with every collector registered on a fresh Registry.
func New() *Metrics {
	m := &Metrics{
		Registry: prometheus.NewRegistry(),
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "acmeservice_http_requests_total",
			Help: "HTTP requests by listener, route and status.",
		}, []string{"listener", "route", "status"}),
		DNSQueries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "acmeservice_dns_queries_total",
			Help: "DNS queries by query type and response code.",
		}, []string{"qtype", "rcode"}),
		ActiveChallenges: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "acmeservice_active_challenges",
			Help: "Current count of answerable challenge values.",
		}),
		SweepRemoved: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "acmeservice_sweep_removed_total",
			Help: "Expired challenge rows removed by the sweep.",
		}),
		DatabaseUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "acmeservice_database_up",
			Help: "1 if the database was reachable at the last check, 0 otherwise.",
		}),
		AuthOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "acmeservice_auth_outcomes_total",
			Help: "Authentication and authorization outcomes by reason.",
		}, []string{"reason"}),
		DNSQueryObservations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "acmeservice_dns_query_observations_total",
			Help: "DNS query observations folded into challenge_interactions, by attribution outcome.",
		}, []string{"attributed"}),
		ChallengeInteractions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "acmeservice_challenge_interactions_total",
			Help: "challenge_interactions rows by derived outcome.",
		}, []string{"outcome"}),
		AuditEntries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "acmeservice_audit_entries_total",
			Help: "audit_entries rows written, by action class.",
		}, []string{"action_class"}),
	}
	m.Registry.MustRegister(
		m.HTTPRequests,
		m.DNSQueries,
		m.ActiveChallenges,
		m.SweepRemoved,
		m.DatabaseUp,
		m.AuthOutcomes,
		m.DNSQueryObservations,
		m.ChallengeInteractions,
		m.AuditEntries,
	)
	return m
}
