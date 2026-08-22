package obs

import (
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

// forbiddenLabels are the identifier kinds no metric may carry, per the
// service-operations spec: a metric describes a shape of traffic, not one
// customer's history. A label named (or containing) any of these would grow
// one time series per domain/alias/project/key and outlive the row it
// described.
var forbiddenLabels = []string{"domain", "alias", "project", "key", "fqdn", "id"}

// TestMetricsCarryNoIdentifierLabels asserts the contract directly against
// the registered descriptors, rather than trusting a comment: no label name
// on any collector may reference a domain, alias, project or API key
// identifier.
func TestMetricsCarryNoIdentifierLabels(t *testing.T) {
	m := New()

	// Touch every collector so it has at least one series to gather.
	m.HTTPRequests.WithLabelValues("challenge", "/present", "200").Inc()
	m.DNSQueries.WithLabelValues("TXT", "NOERROR").Inc()
	m.ActiveChallenges.Set(1)
	m.SweepRemoved.Add(1)
	m.DatabaseUp.Set(1)
	m.AuthOutcomes.WithLabelValues(AuthReasonOK).Inc()
	m.DNSQueryObservations.WithLabelValues(AttributedTrue).Inc()
	m.DNSQueryObservations.WithLabelValues(AttributedFalseUnresolvedLabel).Inc()
	m.DNSQueryObservations.WithLabelValues(AttributedFalseNoInteraction).Inc()
	m.ChallengeInteractions.WithLabelValues(OutcomeValidated).Inc()
	m.AuditEntries.WithLabelValues(ActionClassConfig).Inc()
	m.AuditEntries.WithLabelValues(ActionClassCredential).Inc()
	m.AuditEntries.WithLabelValues(ActionClassUnknown).Inc()

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(families) != 9 {
		t.Fatalf("got %d metric families, want 9", len(families))
	}

	for _, fam := range families {
		for _, metric := range fam.GetMetric() {
			for _, lp := range metric.GetLabel() {
				name := strings.ToLower(lp.GetName())
				for _, forbidden := range forbiddenLabels {
					if strings.Contains(name, forbidden) {
						t.Errorf("metric %s has label %q, which looks like an identifier label (contains %q); "+
							"per-name data belongs in the datastore, not a metric label",
							fam.GetName(), lp.GetName(), forbidden)
					}
				}
			}
		}
	}
}

// TestAuthOutcomesCountReasonsSeparately covers the specific case the spec
// calls out: an invalid key and a disallowed source address both refuse the
// request with the same HTTP status, but must be counted under different
// reasons.
func TestAuthOutcomesCountReasonsSeparately(t *testing.T) {
	m := New()

	m.AuthOutcomes.WithLabelValues(AuthReasonInvalidKey).Inc()
	m.AuthOutcomes.WithLabelValues(AuthReasonInvalidKey).Inc()
	m.AuthOutcomes.WithLabelValues(AuthReasonDisallowedSource).Inc()

	var invalidKey, disallowedSource dto.Metric
	if err := m.AuthOutcomes.WithLabelValues(AuthReasonInvalidKey).Write(&invalidKey); err != nil {
		t.Fatal(err)
	}
	if err := m.AuthOutcomes.WithLabelValues(AuthReasonDisallowedSource).Write(&disallowedSource); err != nil {
		t.Fatal(err)
	}

	if got := invalidKey.GetCounter().GetValue(); got != 2 {
		t.Errorf("invalid_key count = %v, want 2", got)
	}
	if got := disallowedSource.GetCounter().GetValue(); got != 1 {
		t.Errorf("disallowed_source count = %v, want 1", got)
	}
}

// TestDNSQueryObservationsCountAttributionFailuresSeparately covers the case
// obs 6.9/9.1 exist for: an unresolved alias label (deleted between the query
// being answered and the flush) and a resolved alias whose interaction row
// cannot be found both fail attribution, but only the second one means
// attribution is broken, so they must land in different series.
func TestDNSQueryObservationsCountAttributionFailuresSeparately(t *testing.T) {
	m := New()

	m.DNSQueryObservations.WithLabelValues(AttributedTrue).Add(5)
	m.DNSQueryObservations.WithLabelValues(AttributedFalseUnresolvedLabel).Add(2)
	m.DNSQueryObservations.WithLabelValues(AttributedFalseNoInteraction).Add(3)

	var attributed, unresolved, noInteraction dto.Metric
	if err := m.DNSQueryObservations.WithLabelValues(AttributedTrue).Write(&attributed); err != nil {
		t.Fatal(err)
	}
	if err := m.DNSQueryObservations.WithLabelValues(AttributedFalseUnresolvedLabel).Write(&unresolved); err != nil {
		t.Fatal(err)
	}
	if err := m.DNSQueryObservations.WithLabelValues(AttributedFalseNoInteraction).Write(&noInteraction); err != nil {
		t.Fatal(err)
	}

	if got := attributed.GetCounter().GetValue(); got != 5 {
		t.Errorf("attributed=true count = %v, want 5", got)
	}
	if got := unresolved.GetCounter().GetValue(); got != 2 {
		t.Errorf("attributed=false_unresolved_label count = %v, want 2", got)
	}
	if got := noInteraction.GetCounter().GetValue(); got != 3 {
		t.Errorf("attributed=false_no_interaction_row count = %v, want 3", got)
	}
}
