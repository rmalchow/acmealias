package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/adminctl"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
	"gorm.io/gorm"
)

// run is the top-level subcommand dispatch: `serve` (the default, no args),
// `migrate up|down|version`, and `adminctl <noun> <verb>`. Every path here
// other than the bare `serve` stub operates directly against the datastore,
// with no server process running (task group 20).
func run(args []string) int {
	if len(args) == 0 {
		return cmdServe()
	}
	switch args[0] {
	case "serve":
		return cmdServe()
	case "migrate":
		return cmdMigrate(args[1:])
	case "adminctl":
		return cmdAdminctl(args[1:])
	case "-h", "-help", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "acmeservice: unknown command %q\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  acmeservice [serve]
  acmeservice migrate up|down|version
  acmeservice adminctl project   create --slug S --name N | list | delete --slug S
  acmeservice adminctl loadbalancer    create --project S --slug S --name N | delete --loadbalancer REF | dns REF
  acmeservice adminctl node      add --loadbalancer REF --label L [--ipv4 A] [--ipv6 A] | delete --id ID
  acmeservice adminctl domain    create --loadbalancer REF --name N [--include-wildcard] | list --loadbalancer REF | delete --id ID
  acmeservice adminctl key       create --loadbalancer REF --name N [--expires DUR] | list --loadbalancer REF | revoke --id ID

REF is a load balancer reference: a numeric id, or "project-slug/loadbalancer-slug".`)
}

// cmdServe runs the server: see serve in main.go, which owns the whole
// bootstrap (configuration, migrations, listeners, shutdown).
func cmdServe() int {
	return serve()
}

func cmdMigrate(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: acmeservice migrate up|down|version")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "ACME_DATABASE_URL is required")
		return 1
	}
	switch args[0] {
	case "up":
		if err := store.MigrateUp(cfg.DatabaseURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("migrated up")
	case "down":
		if err := store.MigrateDown(cfg.DatabaseURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("migrated down")
	case "version":
		v, err := store.EmbeddedVersion()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("embedded schema version:", v)
	default:
		fmt.Fprintln(os.Stderr, "usage: acmeservice migrate up|down|version")
		return 2
	}
	return 0
}

func cmdAdminctl(args []string) int {
	if len(args) < 2 {
		usage()
		return 2
	}
	noun, verb, rest := args[0], args[1], args[2:]

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "ACME_DATABASE_URL is required")
		return 1
	}
	db, err := store.Open(store.Options{DatabaseURL: cfg.DatabaseURL})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}

	ctx := context.Background()
	if err := dispatchAdminctl(ctx, db, cfg, noun, verb, rest); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func dispatchAdminctl(ctx context.Context, db *gorm.DB, cfg config.Config, noun, verb string, args []string) error {
	out := os.Stdout
	switch noun + " " + verb {
	case "project create":
		fs := flag.NewFlagSet("project create", flag.ExitOnError)
		slug := fs.String("slug", "", "project slug")
		name := fs.String("name", "", "project name")
		fs.Parse(args)
		return adminctl.ProjectCreate(ctx, db, out, *slug, *name)
	case "project list":
		return adminctl.ProjectList(ctx, db, out)
	case "project delete":
		fs := flag.NewFlagSet("project delete", flag.ExitOnError)
		slug := fs.String("slug", "", "project slug")
		fs.Parse(args)
		return adminctl.ProjectDelete(ctx, db, out, *slug)

	case "loadbalancer create":
		fs := flag.NewFlagSet("loadbalancer create", flag.ExitOnError)
		project := fs.String("project", "", "project slug")
		slug := fs.String("slug", "", "load balancer slug")
		name := fs.String("name", "", "load balancer name")
		fs.Parse(args)
		return adminctl.LoadBalancerCreate(ctx, db, out, *project, *slug, *name)
	case "loadbalancer delete":
		fs := flag.NewFlagSet("loadbalancer delete", flag.ExitOnError)
		loadbalancer := fs.String("loadbalancer", "", "load balancer reference (project-slug/loadbalancer-slug or id)")
		fs.Parse(args)
		return adminctl.LoadBalancerDelete(ctx, db, out, *loadbalancer)
	case "loadbalancer dns":
		if len(args) != 1 {
			return fmt.Errorf("usage: acmeservice adminctl loadbalancer dns REF")
		}
		opts := inventory.PlanOptions{Zone: cfg.Zone, CAAIssuers: cfg.CAAIssuers}
		return adminctl.LoadBalancerDNS(ctx, db, out, opts, args[0])

	case "node add":
		fs := flag.NewFlagSet("node add", flag.ExitOnError)
		loadbalancer := fs.String("loadbalancer", "", "load balancer reference")
		label := fs.String("label", "", "node label")
		ipv4 := fs.String("ipv4", "", "node IPv4 address")
		ipv6 := fs.String("ipv6", "", "node IPv6 address")
		fs.Parse(args)
		return adminctl.NodeAdd(ctx, db, out, *loadbalancer, *label, *ipv4, *ipv6)
	case "node delete":
		fs := flag.NewFlagSet("node delete", flag.ExitOnError)
		id := fs.Int64("id", 0, "node id")
		fs.Parse(args)
		return adminctl.NodeDelete(ctx, db, out, *id)

	case "domain create":
		fs := flag.NewFlagSet("domain create", flag.ExitOnError)
		loadbalancer := fs.String("loadbalancer", "", "load balancer reference")
		name := fs.String("name", "", "domain name")
		includeWildcard := fs.Bool("include-wildcard", false,
			"also create the name's counterpart: the wildcard of a plain name, the base name of a wildcard")
		fs.Parse(args)
		return adminctl.DomainCreate(ctx, db, out, cfg.Zone, cfg.AliasUseDomain, *loadbalancer, *name, *includeWildcard)
	case "domain list":
		fs := flag.NewFlagSet("domain list", flag.ExitOnError)
		loadbalancer := fs.String("loadbalancer", "", "load balancer reference")
		fs.Parse(args)
		return adminctl.DomainList(ctx, db, out, *loadbalancer)
	case "domain delete":
		fs := flag.NewFlagSet("domain delete", flag.ExitOnError)
		id := fs.Int64("id", 0, "domain id")
		fs.Parse(args)
		return adminctl.DomainDelete(ctx, db, out, *id)

	case "key create":
		fs := flag.NewFlagSet("key create", flag.ExitOnError)
		loadbalancer := fs.String("loadbalancer", "", "load balancer reference")
		name := fs.String("name", "", "key note")
		expires := fs.String("expires", "", "key lifetime, e.g. 8760h (empty: never expires)")
		fs.Parse(args)
		var d time.Duration
		if *expires != "" {
			var err error
			d, err = time.ParseDuration(*expires)
			if err != nil {
				return fmt.Errorf("--expires: %w", err)
			}
		}
		return adminctl.KeyCreate(ctx, db, out, *loadbalancer, *name, d)
	case "key list":
		fs := flag.NewFlagSet("key list", flag.ExitOnError)
		loadbalancer := fs.String("loadbalancer", "", "load balancer reference")
		fs.Parse(args)
		return adminctl.KeyList(ctx, db, out, *loadbalancer)
	case "key revoke":
		fs := flag.NewFlagSet("key revoke", flag.ExitOnError)
		id := fs.Int64("id", 0, "key id")
		fs.Parse(args)
		return adminctl.KeyRevoke(ctx, db, out, *id)

	default:
		usage()
		return fmt.Errorf("unknown adminctl command %q %q", noun, verb)
	}
}
