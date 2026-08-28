package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/adminctl"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
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
  acmeservice adminctl account create --slug S --name N --owner SUBJECT [--credits N]
  acmeservice adminctl --account SLUG <noun> <verb> ...

  acmeservice adminctl account   credit --credits N | transfer --owner SUBJECT | member-add --subject S --role NAME
  acmeservice adminctl project   create --slug S --name N | list | delete --slug S | dns REF
  acmeservice adminctl node      add --project REF --label L [--ipv4 A] [--ipv6 A] | delete --id ID
  acmeservice adminctl domain    create --project REF --name N [--include-wildcard] | list --project REF | delete --id ID
  acmeservice adminctl key       create [--project REF] [--grant T:O:A]... --name N [--expires DUR] | list --project REF | revoke --id ID

REF is a project reference: its slug, or its identifier.

--account names the account every adminctl verb acts in. ACME_ADMINCTL_ACCOUNT
sets the default; the flag wins. There is no deployment-wide account to fall
back on.`)
}

// takeAccount pulls a leading --account off the adminctl arguments, falling back
// to ACME_ADMINCTL_ACCOUNT.
//
// It is parsed by hand, before the per-verb flag sets, because each verb builds
// its own FlagSet from what follows the noun and the verb — threading a global
// flag through twenty of them would mean twenty places to forget it.
func takeAccount(args []string) (string, []string, error) {
	account := os.Getenv("ACME_ADMINCTL_ACCOUNT")
	for len(args) > 0 {
		switch {
		case args[0] == "--account" || args[0] == "-account":
			if len(args) < 2 {
				return "", nil, fmt.Errorf("--account needs an account slug")
			}
			account, args = args[1], args[2:]
		case strings.HasPrefix(args[0], "--account=") || strings.HasPrefix(args[0], "-account="):
			account, args = args[0][strings.Index(args[0], "=")+1:], args[1:]
		default:
			return account, args, nil
		}
	}
	return account, args, nil
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
	account, args, err := takeAccount(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(args) < 2 {
		usage()
		return 2
	}
	noun, verb, rest := args[0], args[1], args[2:]
	// `account create` is the one verb that cannot name an account: it is what
	// makes the first one. Everything else refuses without one rather than
	// guessing, because there is no deployment-wide account to guess (design
	// C10).
	makesTheAccount := noun == "account" && verb == "create"
	if account == "" && !makesTheAccount {
		fmt.Fprintln(os.Stderr,
			"adminctl needs an account: pass --account SLUG, or set ACME_ADMINCTL_ACCOUNT.\n"+
				"Create the first one with: acmeservice adminctl account create --slug S --name N --owner SUBJECT")
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
	db, err := store.Open(store.Options{DatabaseURL: cfg.DatabaseURL})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}

	ctx := context.Background()
	// The CLI is the break-glass path and its authority is "had the connection
	// string", but that is a reason to record what it did, not a reason to let it
	// write across tenants by accident: it is anchored to one account like every
	// other subject.
	//
	// It used to take the deployment's single account, which stopped being a
	// question with an answer when the internal edition went away (design C10).
	// Naming the account is the replacement, and it is an improvement rather than
	// a tax: the break-glass path should have to say which tenant it is about to
	// write into at three in the morning, rather than inheriting one.
	var sc store.Scope
	if !makesTheAccount {
		sc, err = store.ScopeForSlug(ctx, db, account)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if err := dispatchAdminctl(ctx, db, sc, cfg, noun, verb, rest); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// limitsFrom carries the deployment's quota into the CLI's calls. It is the same
// value the management API passes, from the same config, which is what keeps the
// two from diverging (design P7, C13).
func limitsFrom(cfg config.Config) inventory.Limits {
	return inventory.Limits{ProjectsPerPersonalAccount: cfg.ProjectsPerPersonalAccount}
}

func dispatchAdminctl(ctx context.Context, db *gorm.DB, sc store.Scope, cfg config.Config, noun, verb string, args []string) error {
	out := os.Stdout
	switch noun + " " + verb {
	case "account create":
		fs := flag.NewFlagSet("account create", flag.ExitOnError)
		slug := fs.String("slug", "", "account slug")
		name := fs.String("name", "", "account name")
		owner := fs.String("owner", "", "identity-provider subject of the owner")
		// The opening balance. A team account is created by funding it (design
		// C8), so there is no zero-credit form of this verb — an account made
		// with nothing in it is suspended from its first instant, and its owner
		// would sign in to the pay screen of an account they did not buy.
		//
		// It defaults to the smallest offered amount rather than requiring the
		// flag, because this verb's job is to get a fresh deployment its FIRST
		// account, before anybody can reach the console to buy one, and a
		// bootstrap that fails on a missing flag is a bootstrap somebody has to
		// read the source to perform.
		credits := fs.Int64("credits", store.CheckoutAmounts[0], "opening credit balance, in EUR")
		fs.Parse(args)
		return adminctl.AccountCreate(ctx, db, out, *slug, *name, *owner, *credits)

	// The three repair verbs. They exist for an account nobody can get into: an
	// empty roster and an owner nobody can authenticate as, which is the shape
	// the internal edition's shared account was left in when its membership was
	// derived from a group claim rather than written down (design C10). Nothing
	// in the console can mend that — every route into an account requires being
	// in it — so it lands here, with the rest of the break-glass path. See
	// internal/adminctl/account.go.
	case "account credit":
		fs := flag.NewFlagSet("account credit", flag.ExitOnError)
		credits := fs.Int64("credits", store.CheckoutAmounts[0], "credit to add, in EUR; one of the offered amounts")
		fs.Parse(args)
		return adminctl.AccountCredit(ctx, db, sc, out, *credits)
	case "account transfer":
		fs := flag.NewFlagSet("account transfer", flag.ExitOnError)
		owner := fs.String("owner", "", "identity-provider subject of the new owner")
		fs.Parse(args)
		return adminctl.AccountTransfer(ctx, db, sc, out, *owner)
	case "account member-add":
		fs := flag.NewFlagSet("account member-add", flag.ExitOnError)
		subject := fs.String("subject", "", "identity-provider subject of the person to add")
		role := fs.String("role", "", "name of a role in this account")
		fs.Parse(args)
		return adminctl.AccountMemberAdd(ctx, db, sc, out, *subject, *role)

	case "project create":
		fs := flag.NewFlagSet("project create", flag.ExitOnError)
		slug := fs.String("slug", "", "project slug")
		name := fs.String("name", "", "project name")
		fs.Parse(args)
		return adminctl.ProjectCreate(ctx, db, sc, out, limitsFrom(cfg), *slug, *name)
	case "project list":
		return adminctl.ProjectList(ctx, db, sc, out)
	case "project delete":
		fs := flag.NewFlagSet("project delete", flag.ExitOnError)
		slug := fs.String("slug", "", "project slug")
		fs.Parse(args)
		return adminctl.ProjectDelete(ctx, db, sc, out, *slug)

	case "project dns":
		if len(args) != 1 {
			return fmt.Errorf("usage: acmeservice adminctl project dns REF")
		}
		opts := inventory.PlanOptions{Zone: cfg.Zone, CAAIssuers: cfg.CAAIssuers}
		return adminctl.ProjectDNS(ctx, db, sc, out, opts, args[0])

	case "node add":
		fs := flag.NewFlagSet("node add", flag.ExitOnError)
		project := fs.String("project", "", "project reference")
		label := fs.String("label", "", "node label")
		ipv4 := fs.String("ipv4", "", "node IPv4 address")
		ipv6 := fs.String("ipv6", "", "node IPv6 address")
		fs.Parse(args)
		return adminctl.NodeAdd(ctx, db, sc, out, *project, *label, *ipv4, *ipv6)
	case "node delete":
		fs := flag.NewFlagSet("node delete", flag.ExitOnError)
		id := fs.String("id", "", "node id")
		fs.Parse(args)
		parsed, err := model.ParseID(*id)
		if err != nil {
			return fmt.Errorf("--id must be an identifier: %w", err)
		}
		return adminctl.NodeDelete(ctx, db, sc, out, parsed)

	case "domain create":
		fs := flag.NewFlagSet("domain create", flag.ExitOnError)
		project := fs.String("project", "", "project reference")
		name := fs.String("name", "", "domain name")
		includeWildcard := fs.Bool("include-wildcard", false,
			"also add *.<name>, which shares one delegation with it")
		fs.Parse(args)
		return adminctl.DomainCreate(ctx, db, sc, out, cfg.Zone, cfg.AliasUseDomain, *project, *name, *includeWildcard)
	case "domain list":
		fs := flag.NewFlagSet("domain list", flag.ExitOnError)
		project := fs.String("project", "", "project reference")
		fs.Parse(args)
		return adminctl.DomainList(ctx, db, sc, out, *project)
	case "domain delete":
		fs := flag.NewFlagSet("domain delete", flag.ExitOnError)
		id := fs.String("id", "", "domain id")
		fs.Parse(args)
		parsed, err := model.ParseID(*id)
		if err != nil {
			return fmt.Errorf("--id must be an identifier: %w", err)
		}
		return adminctl.DomainDelete(ctx, db, sc, out, parsed)

	case "key create":
		fs := flag.NewFlagSet("key create", flag.ExitOnError)
		project := fs.String("project", "", "project reference: the two challenge grants over it")
		name := fs.String("name", "", "key note")
		expires := fs.String("expires", "", "key lifetime, e.g. 8760h (empty: never expires)")
		var grants grantFlag
		fs.Var(&grants, "grant", "TYPE:OBJECT:ACTION[:noinherit], repeatable; OBJECT may be * within the account")
		fs.Parse(args)
		var d time.Duration
		if *expires != "" {
			var err error
			d, err = time.ParseDuration(*expires)
			if err != nil {
				return fmt.Errorf("--expires: %w", err)
			}
		}
		return adminctl.KeyCreate(ctx, db, sc, out, *project, *name, d, grants)
	case "key list":
		fs := flag.NewFlagSet("key list", flag.ExitOnError)
		project := fs.String("project", "", "project reference")
		fs.Parse(args)
		return adminctl.KeyList(ctx, db, sc, out, *project)
	case "key revoke":
		fs := flag.NewFlagSet("key revoke", flag.ExitOnError)
		id := fs.String("id", "", "key id")
		fs.Parse(args)
		parsed, err := model.ParseID(*id)
		if err != nil {
			return fmt.Errorf("--id must be an identifier: %w", err)
		}
		return adminctl.KeyRevoke(ctx, db, sc, out, parsed)

	default:
		usage()
		return fmt.Errorf("unknown adminctl command %q %q", noun, verb)
	}
}

// grantFlag collects repeated --grant values.
//
// A key's authority is a set, so the flag is repeatable rather than a single
// comma-joined string: a separator that can occur inside a value is a parser
// waiting to be wrong, and a grant's parts are operator-supplied text.
type grantFlag []adminctl.GrantSpec

func (g *grantFlag) String() string {
	parts := make([]string, 0, len(*g))
	for _, s := range *g {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, " ")
}

func (g *grantFlag) Set(v string) error {
	*g = append(*g, adminctl.GrantSpec(v))
	return nil
}
