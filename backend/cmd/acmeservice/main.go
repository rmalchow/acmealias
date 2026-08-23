// Command acmeservice is the acmeservice binary: DNS aliasing server, ACME
// challenge API and admin console. This file is the process itself — config,
// migrations, listeners, shutdown; see cli.go for the `serve`/`migrate`/
// `adminctl` subcommand dispatch.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsserver"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/httpapi"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/oidcauth"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/queryobs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/reporting"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/sweeper"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/webui"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

// version is set at build time via -ldflags; "dev" is the fallback for a
// plain `go build`.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

// serve loads and validates the configuration, then hands off to the process
// proper. Configuration failures are reported on stderr rather than through
// the logger, because the log level is one of the settings being read.
func serve() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	log := obs.NewLogger(cfg.LogLevel)
	slog.SetDefault(log)
	// cfg renders through config.LogValue, which redacts the database
	// password and the OIDC client secret.
	log.Info("starting acmeservice", "version", version, "config", cfg)

	if err := runService(cfg, log); err != nil {
		log.Error("startup failed", "error", err)
		return 1
	}
	return 0
}

// runService is the whole running process: migrate, open the pool, load the
// zone view, start the DNS listeners, start the three HTTP listeners, then
// wait for a termination signal and drain.
//
// Any failure before the wait returns an error and the process exits non-zero
// with nothing serving. Cleanup on that path is deliberately partial —
// closing the pool, and letting an already-bound listener die with the
// process — because there is no "keep running degraded" state to protect.
func runService(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations first, before anything binds: a schema the binary cannot
	// use must stop startup, not surface as failing queries once traffic is
	// arriving. Both branches refuse a database that is AHEAD of this binary.
	if cfg.MigrateOnStart {
		if err := store.MigrateUp(cfg.DatabaseURL); err != nil {
			return fmt.Errorf("migrations (ACME_MIGRATE_ON_START=true): %w", err)
		}
	} else if err := store.CheckSchemaVersion(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("schema version (ACME_MIGRATE_ON_START=false): %w", err)
	}

	db, err := store.Open(store.Options{DatabaseURL: cfg.DatabaseURL, Logger: log})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// Tenant isolation is only real if the policies apply to THIS connection.
	// A superuser bypasses every one of them silently, and the default role in
	// most PostgreSQL images is a superuser — so this is checked before the
	// process serves anything, and refuses rather than warns.
	if err := store.CheckTenantIsolation(ctx, db); err != nil {
		return err
	}
	if err := store.PoliciesInstalled(ctx, db); err != nil {
		return err
	}

	// The group mapping now names role ROWS rather than constants, so a
	// configured group can point at a role nobody created. That is reported
	// here rather than refused: configuring the group before creating the role
	// is a reasonable order to do things in, and refusing to start would turn it
	// into a lockout. Silence would be worse than either — the symptom is one
	// person unable to do anything, with nothing saying why.
	if bootstrap, err := store.ScopeForBootstrap(ctx, db); err == nil {
		authz.ReportUnresolvedRoles(ctx, db, bootstrap, authz.MappingFromConfig(cfg), log)
	} else {
		log.Warn("single-account scope not resolved; the group mapping was not checked", "error", err)
	}

	metrics := obs.New()

	view := zoneview.New(zoneview.FromDB(db))
	if err := view.Refresh(ctx); err != nil {
		return fmt.Errorf("initial zone view load: %w", err)
	}

	// oidcauth.New makes no network call: an identity provider down for
	// maintenance must not stop DNS and the challenge API coming up (design
	// D10), so discovery happens lazily on the first sign-in.
	auth := oidcauth.New(cfg, db)
	mapping := authz.MappingFromConfig(cfg)
	// The group-to-role mapping is configuration, and a typo in it is the
	// mistake that actually happens, so it goes in the log where the operator
	// can compare it against Keycloak.
	mapping.Log(log)

	// listenersUp gates readiness: false until every listener is serving, and
	// false again the moment a shutdown starts.
	var up upFlag
	ping := func(ctx context.Context) error { return db.WithContext(ctx).Exec("select 1").Error }

	servers, err := httpapi.NewServers(ctx, httpapi.ServerOptions{
		Challenge: httpapi.ChallengeOptions{
			DB:                db,
			Zone:              cfg.Zone,
			View:              view,
			Lookup:            dbLookup(db),
			LastUsed:          apikey.NewLastUsed(db, log),
			TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
			Lifetime:          cfg.ChallengeLifetime,
			Metrics:           metrics,
			Log:               log,
		},
		Admin: httpapi.AdminOptions{
			DB:      db,
			Metrics: metrics,
			Guard:   &authz.Guard{Auth: auth, Mapping: mapping, Log: log},
			Auth:    auth,
			View:    view,
			Config:  cfg,
			Log:     log,
		},
		Console:  webui.Handler(),
		Liveness: obs.Liveness(),
		// Readiness runs the same probe that sets the database_up gauge, so
		// the endpoint and the metric cannot disagree during an incident.
		Readiness: obs.Readiness(metrics, up.get, ping),
		Metrics:   metrics,
		Log:       log,
	})
	if err != nil {
		return err
	}

	// DNS before HTTP. Start binds both 53/udp and 53/tcp and returns a bind
	// failure synchronously, so a taken port is a startup error rather than a
	// service that reports itself ready while answering no queries.
	// Counts answered queries per alias in memory; the flusher below is the only
	// thing that turns them into rows, so the DNS path keeps its independence
	// from the datastore.
	queries := queryobs.NewCounter()

	dnsSrv := dnsserver.New(&cfg, view, metrics, queries, log)
	if err := dnsSrv.Start(); err != nil {
		return fmt.Errorf("ACME_DNS_LISTEN %q: %w", cfg.DNSListenAddr, err)
	}
	log.Info("listening", "listener", "dns", "addr", cfg.DNSListenAddr)

	httpSrvs := []*http.Server{}
	for _, l := range []struct {
		name, setting, addr string
		h                   http.Handler
	}{
		{"public", "ACME_LISTEN", cfg.ListenAddr, servers.Public},
	} {
		ln, err := net.Listen("tcp", l.addr)
		if err != nil {
			return fmt.Errorf("%s %q: %w", l.setting, l.addr, err)
		}
		// ReadHeaderTimeout because net/http has no default one: a peer that
		// opens a connection and dribbles headers would otherwise hold it
		// forever.
		srv := &http.Server{Handler: l.h, ReadHeaderTimeout: 10 * time.Second}
		httpSrvs = append(httpSrvs, srv)
		go func(name string, srv *http.Server, ln net.Listener) {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("http listener stopped", "listener", name, "error", err)
			}
		}(l.name, srv, ln)
		log.Info("listening", "listener", l.name, "addr", ln.Addr().String())
	}

	go view.Run(ctx, cfg.ZoneViewRefreshInterval, log)
	// Same cadence as the zone view refresh: the counts describe live challenges,
	// which is exactly the window that refresh already defines, and it keeps the
	// number of periodic tasks with their own tuning knob at zero.
	go queries.Run(ctx, cfg.ZoneViewRefreshInterval, func(ctx context.Context, batch map[string]queryobs.Observation, from, to time.Time) error {
		byAlias, dropped, err := store.AliasIDsByLabel(ctx, db, batch)
		if err != nil {
			return err
		}
		if dropped > 0 {
			// Not an error — an alias can be deleted between a query being
			// answered and this flush — but it must not be silent: the symptom
			// of dropping observations wrongly is "nothing was ever queried",
			// which is exactly the failure this history exists to report.
			log.Warn("query observations discarded, alias no longer exists", "labels", dropped)
			metrics.DNSQueryObservations.WithLabelValues(obs.AttributedFalseUnresolvedLabel).Add(float64(dropped))
		}
		unmatched, err := store.ApplyQueryObservations(ctx, db, byAlias, from, to)
		if err != nil {
			return err
		}
		if unmatched > 0 {
			// Alias resolved, but no interaction row overlapped the window: a
			// stray query after the interaction closed, or attribution is
			// broken. Worth its own series — see the DNSQueryObservations doc.
			metrics.DNSQueryObservations.WithLabelValues(obs.AttributedFalseNoInteraction).Add(float64(unmatched))
		}
		if attributed := len(byAlias) - unmatched; attributed > 0 {
			metrics.DNSQueryObservations.WithLabelValues(obs.AttributedTrue).Add(float64(attributed))
		}
		return nil
	}, log)
	go sweeper.Run(ctx, db, cfg.SweepInterval, cfg.InteractionRetention, metrics, log)
	// Inventory size over time, which is the one thing reporting cannot derive
	// from history: domains and aliases are mutable rows with no deletion
	// record, so nobody can say in April how many there were in March unless it
	// was written down in March. Hourly, writing today's row, and idempotent per
	// (project, day) — see reporting.Snapshot.
	go reporting.RunSnapshots(ctx, db, reporting.SnapshotInterval, log)
	go sampleActiveChallenges(ctx, db, metrics, cfg.ZoneViewRefreshInterval, log)

	up.set(true)
	log.Info("acmeservice ready", "zone", cfg.Zone)

	<-ctx.Done()
	// Restore default signal handling: a second SIGTERM from an impatient
	// operator now kills the process instead of being swallowed.
	stop()
	up.set(false)
	log.Info("shutdown signal received, draining", "grace", cfg.ShutdownGracePeriod)

	shutdown(log, cfg.ShutdownGracePeriod, stoppers(dnsSrv, httpSrvs)...)
	log.Info("stopped")
	return nil
}

// stopper is one thing that has to stop on shutdown: a graceful drain bounded
// by a context, and the forced close used when the grace period runs out.
type stopper struct {
	name  string
	drain func(context.Context) error
	force func()
}

func stoppers(dnsSrv *dnsserver.Server, httpSrvs []*http.Server) []stopper {
	ss := []stopper{{name: "dns", drain: dnsSrv.Shutdown, force: func() {}}}
	for i, srv := range httpSrvs {
		ss = append(ss, stopper{
			name:  fmt.Sprintf("http-%d", i),
			drain: srv.Shutdown,
			force: func() { srv.Close() },
		})
	}
	return ss
}

// shutdown stops accepting and lets in-flight work finish within grace, so a
// request that arrived a millisecond before SIGTERM still gets its response.
// Work still running when the grace period expires is cut off and the fact is
// logged at warn — a forced exit that leaves no trace is the version of this
// that wastes an afternoon later.
func shutdown(log *slog.Logger, grace time.Duration, ss ...stopper) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	var wg sync.WaitGroup
	for _, s := range ss {
		wg.Add(1)
		go func(s stopper) {
			defer wg.Done()
			if err := s.drain(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
				log.Warn("shutdown error", "component", s.name, "error", err)
			}
		}(s)
	}
	wg.Wait()

	if ctx.Err() != nil {
		log.Warn("shutdown forced: work was still in flight after the grace period",
			"grace", grace, "setting", "ACME_SHUTDOWN_GRACE")
		for _, s := range ss {
			s.force()
		}
	}
}

// upFlag is the listeners-are-serving signal readiness reads. A mutex rather
// than atomic.Bool for no better reason than that it reads the same as every
// other guarded field in this file.
type upFlag struct {
	mu sync.Mutex
	v  bool
}

func (f *upFlag) set(v bool) { f.mu.Lock(); f.v = v; f.mu.Unlock() }
func (f *upFlag) get() bool  { f.mu.Lock(); defer f.mu.Unlock(); return f.v }

// dbLookup is the production wiring of apikey.Lookup.
func dbLookup(db *gorm.DB) apikey.Lookup {
	return func(ctx context.Context, identifier string) (*model.APIKey, error) {
		return store.FindAPIKeyByIdentifier(ctx, db, identifier)
	}
}

// sampleActiveChallenges keeps the active-challenge gauge current.
//
// ponytail: sampled on the zone view's refresh interval with one count query,
// rather than incremented by every publish, cleanup and sweep. Ceiling: the
// gauge lags a publish by up to one interval, and the query walks the
// unexpired rows — fine at a few hundred, not at a million. Upgrade path:
// have zoneview report its value count and set the gauge in replace().
func sampleActiveChallenges(ctx context.Context, db *gorm.DB, m *obs.Metrics, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		n, err := store.CountActive(ctx, db)
		if err != nil {
			log.Warn("active challenge count not sampled", "error", err)
		} else {
			m.ActiveChallenges.Set(float64(n))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
