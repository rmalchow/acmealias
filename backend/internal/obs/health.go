package obs

import (
	"context"
	"net/http"
	"time"
)

// Liveness and Readiness are served only on the internal listener, alongside
// /metrics — never on the challenge or admin listener, and never behind
// authentication. Group 16's wiring is what enforces that placement (this
// package doesn't know about listeners); this comment just says so where the
// handlers live, per the service-operations spec.

// Liveness reports process health with no dependency checks at all: it
// always returns 200 as long as the process can handle the request. A probe
// that fails on a dependency outage (the database being down, say) gets the
// container killed and restarted for a problem a restart cannot fix — that
// class of check belongs to Readiness, not here.
func Liveness() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

// DatabasePing checks database reachability. Implementations should respect
// ctx's deadline: a readiness probe is typically polled every few seconds
// and must not stack up if the database is slow rather than down.
type DatabasePing func(ctx context.Context) error

// Readiness reports 200 only when listenersUp reports the DNS and HTTP
// listeners are serving and ping succeeds against the database. Group 16
// supplies both: ping wraps whatever it uses to reach the database (it
// doesn't need to import store — a `func(ctx) error { return db.WithContext(ctx).Exec("select 1").Error }`-shaped
// closure is enough), and listenersUp reports whatever "the servers have
// started" signal it already tracks for shutdown.
//
// Every call updates m.DatabaseUp from the same ping result it just used to
// decide the response, rather than keeping the metric and the readiness
// check as two separate ideas of "reachable" that could disagree with each
// other during an incident.
func Readiness(m *Metrics, listenersUp func() bool, ping DatabasePing) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		dbErr := ping(ctx)
		if m != nil {
			if dbErr == nil {
				m.DatabaseUp.Set(1)
			} else {
				m.DatabaseUp.Set(0)
			}
		}

		if listenersUp == nil || !listenersUp() {
			http.Error(w, "listeners not serving", http.StatusServiceUnavailable)
			return
		}
		if dbErr != nil {
			http.Error(w, "database unreachable: "+dbErr.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
