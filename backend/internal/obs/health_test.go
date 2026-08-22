package obs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestLivenessIgnoresDatabaseDown(t *testing.T) {
	rec := httptest.NewRecorder()
	Liveness().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("liveness with database down = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadinessFailsWhenDatabaseDown(t *testing.T) {
	m := New()
	ping := func(context.Context) error { return errors.New("connection refused") }
	up := func() bool { return true }

	rec := httptest.NewRecorder()
	Readiness(m, up, ping).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code == http.StatusOK {
		t.Fatalf("readiness with database down = %d, want non-200", rec.Code)
	}

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if got := gaugeValue(t, families, "acmeservice_database_up"); got != 0 {
		t.Errorf("database_up after failed ping = %v, want 0 — readiness and the metric must agree", got)
	}
}

func TestReadinessFailsWhenListenersNotUp(t *testing.T) {
	m := New()
	ping := func(context.Context) error { return nil }
	up := func() bool { return false }

	rec := httptest.NewRecorder()
	Readiness(m, up, ping).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code == http.StatusOK {
		t.Fatalf("readiness with listeners not up = %d, want non-200", rec.Code)
	}
}

func TestReadinessOKWhenEverythingUp(t *testing.T) {
	m := New()
	ping := func(context.Context) error { return nil }
	up := func() bool { return true }

	rec := httptest.NewRecorder()
	Readiness(m, up, ping).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("readiness with everything up = %d, want %d", rec.Code, http.StatusOK)
	}

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if got := gaugeValue(t, families, "acmeservice_database_up"); got != 1 {
		t.Errorf("database_up after successful ping = %v, want 1", got)
	}
}

func gaugeValue(t *testing.T, families []*dto.MetricFamily, name string) float64 {
	t.Helper()
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, metric := range fam.GetMetric() {
			return metric.GetGauge().GetValue()
		}
	}
	t.Fatalf("metric family %q not found", name)
	return 0
}
