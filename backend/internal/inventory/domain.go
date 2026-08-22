package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// DomainCreation is what adding a domain produced: the domain, the alias it
// ended up pointing at (existing or new), and any warnings the operator
// should see. AliasCreated tells the caller whether a new label now exists in
// the served zone, which is the signal to refresh the zone view.
//
// Domains holds every domain the request created, in creation order. It is the
// submitted name alone unless the counterpart was asked for, in which case it
// is the submitted name followed by its counterpart. Domain is always
// Domains[0], kept because most callers only ever want the name that was
// typed.
type DomainCreation struct {
	Domain       *model.Domain
	Domains      []*model.Domain
	Alias        *model.Alias
	AliasCreated bool
	Warnings     []dnsname.Warning
}

// CreateDomain adds a domain to a load balancer (design D4), optionally
// together with its counterpart (design B1).
//
// It validates the name, derives its challenge name — the leading `*.` label
// stripped, `_acme-challenge.` prefixed, so `example.com` and `*.example.com`
// derive the same one — and finds or creates the alias for that challenge
// name.
//
// includeCounterpart asks for the other form of the submitted name to be
// created with it: the wildcard of a name that is not one, the base name of a
// name that is. The two names differ only by the `*.` prefix, so they derive
// the same challenge name and share the one alias — the pair costs one extra
// `domains` row and nothing else. Both names pass every pre-write check before
// either row is written, and both rows are written in the one transaction, so
// the request creates two domains or none (design B3).
//
// useDomainLabel is deployment configuration (ACME_ALIAS_USE_DOMAIN), not
// operator input: no caller may choose a label, at any entry point (design
// A5). It is consulted only when an alias is actually created. When the
// challenge name already has one — the wildcard-and-apex case, and every case
// after the setting is flipped — that alias is reused with the label it
// already has, because renaming it would break a CNAME the customer already
// published (design A3).
//
// The alias belongs to exactly one load balancer: if the challenge name is already
// claimed by a different load balancer, the creation is refused and the error names
// that load balancer.
func CreateDomain(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, loadBalancerID model.ID, name string, useDomainLabel bool, includeCounterpart bool) (*DomainCreation, error) {
	parent, err := store.GetLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return nil, err
	}
	// Authorized on the parent, like every creation: there is no domain yet to
	// hold a grant, and the authority to add one to this load balancer is what
	// is being asked about.
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(parent)); err != nil {
		return nil, err
	}

	// Task 7.3: a specific host that an existing wildcard on this load balancer already
	// covers is accepted, and warned about. dnsname cannot see the load balancer's other
	// domains, so this check lives here rather than in ValidateDomain. The list is the
	// load balancer's domains as they are *before* this request, which is also why a pair
	// never warns about itself: a member is not in the list, and in any case a counterpart
	// differs only by the `*.` prefix, which coverageWarnings never treats as coverage.
	siblings, err := store.ListDomainsForLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return nil, err
	}

	primary, warnings, err := checkDomainName(ctx, db, sc, name, siblings)
	if err != nil {
		return nil, err
	}
	names := []string{primary}
	if includeCounterpart {
		counterpart := dnsname.Counterpart(primary)
		checked, w, err := checkDomainName(ctx, db, sc, counterpart, siblings)
		if err != nil {
			// The operator typed `primary`; say which derived name was refused
			// and why, rather than leaving them to work out where `*.co.uk`
			// came from. errors.Is still reaches ErrInvalid / ErrConflict.
			return nil, fmt.Errorf("cannot also add %q: %w", counterpart, err)
		}
		names = append(names, checked)
		warnings = append(warnings, w...)
	}

	// Both names derive the same challenge name, so one alias serves the pair.
	challenge := dnsname.ChallengeName(primary)

	alias, err := store.FindAliasByChallengeName(ctx, db, challenge)
	switch {
	case err == nil:
		if alias.LoadBalancerID != loadBalancerID {
			owner := describeLoadBalancer(ctx, db, sc, alias.LoadBalancerID)
			return nil, fmt.Errorf("%w: the challenge name %q for %q is already owned by %s; "+
				"an alias belongs to exactly one load balancer", ErrConflict, challenge, primary, owner)
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		alias = nil
	default:
		return nil, err
	}

	created := alias == nil
	if created {
		label, err := resolveLabel(ctx, db, sc, challenge, useDomainLabel)
		if err != nil {
			return nil, err
		}
		alias = &model.Alias{LoadBalancerID: loadBalancerID, ChallengeFQDN: challenge, Label: label}
	}
	domains := make([]*model.Domain, 0, len(names))
	for _, n := range names {
		domains = append(domains, &model.Domain{LoadBalancerID: loadBalancerID, Name: n})
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if created {
			if err := store.CreateAlias(ctx, tx, sc, alias); err != nil {
				return err
			}
		}
		for _, d := range domains {
			d.AliasID = alias.ID
			if err := store.CreateDomain(ctx, tx, sc, d); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &DomainCreation{Domain: domains[0], Domains: domains, Alias: alias, AliasCreated: created, Warnings: warnings}, nil
}

// checkDomainName runs every pre-write check for one name — the wildcard
// rules, the inventory-wide duplicate check, and the coverage warnings against
// the load balancer's existing domains — and returns the normalized name. It
// writes nothing, so it can be run for both names of a pair before either row
// exists (design B3).
func checkDomainName(ctx context.Context, db *gorm.DB, sc store.Scope, name string, siblings []model.Domain) (string, []dnsname.Warning, error) {
	normalized, warnings, err := dnsname.ValidateDomain(name)
	if err != nil {
		return "", nil, err
	}
	domainName := normalized.String()

	if existing, err := store.FindDomainByName(ctx, db, sc, domainName); err == nil {
		owner := describeLoadBalancer(ctx, db, sc, existing.LoadBalancerID)
		return "", nil, fmt.Errorf("%w: domain %q already exists on %s", ErrConflict, domainName, owner)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, err
	}

	return domainName, append(warnings, coverageWarnings(domainName, siblings)...), nil
}

// coverageWarnings reports each existing wildcard on the load balancer that already
// covers the new name. A wildcard matches exactly one label, so `*.example.com`
// covers `shop.example.com` but neither `example.com` nor `a.b.example.com`.
func coverageWarnings(name string, siblings []model.Domain) []dnsname.Warning {
	if strings.HasPrefix(name, "*.") {
		return nil
	}
	parent := ""
	if i := strings.Index(name, "."); i >= 0 {
		parent = name[i+1:]
	}
	var out []dnsname.Warning
	for _, s := range siblings {
		if strings.TrimPrefix(s.Name, "*.") == parent && strings.HasPrefix(s.Name, "*.") {
			out = append(out, dnsname.Warning(fmt.Sprintf(
				"%q is already covered by the wildcard %q on this load balancer; a separate certificate for it is legal but usually a mistake",
				name, s.Name)))
		}
	}
	return out
}

// resolveLabel picks the label of a new alias: a fresh random one, or — with
// ACME_ALIAS_USE_DOMAIN — the one derived from the challenge name, with
// today's numeric suffix if that is taken (design A4).
func resolveLabel(ctx context.Context, db *gorm.DB, sc store.Scope, challenge string, useDomainLabel bool) (string, error) {
	// ponytail: every alias is loaded to test the label for collisions.
	// Ceiling: O(aliases) per domain creation, fine for the low thousands this
	// service holds; upgrade path is a FindAliasByLabel query in store.
	aliases, err := store.ListAllAliases(ctx, db)
	if err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(aliases))
	for _, a := range aliases {
		taken[a.Label] = true
	}

	if !useDomainLabel {
		// At 80 bits a single collision is already vanishingly unlikely, so a
		// run of five is a broken random source rather than bad luck, and
		// saying so loudly is better than looping (design A4).
		for i := 0; i < 5; i++ {
			label, err := dnsname.GenerateLabel()
			if err != nil {
				return "", err
			}
			if !taken[label] {
				return label, nil
			}
		}
		return "", fmt.Errorf("no free alias label after 5 generated candidates; the random source is suspect")
	}

	base := dnsname.SuggestLabel(challenge)
	label := base
	for n := 2; taken[label]; n++ {
		suffix := fmt.Sprintf("-%d", n)
		trimmed := base
		if len(trimmed)+len(suffix) > 63 {
			trimmed = strings.TrimRight(base[:63-len(suffix)], "-")
		}
		label = trimmed + suffix
	}
	return label, nil
}

// DeleteDomain removes a domain. When it was the last one referencing its
// alias, the alias goes too and its challenge records cascade with it — that
// is what stops further challenge writes for the name. The returned bool is
// true when a label disappeared from the served zone, so the caller knows to
// refresh the zone view.
func DeleteDomain(ctx context.Context, db *gorm.DB, sc store.Scope, subj authz.Subject, domainID model.ID) (aliasRemoved bool, err error) {
	d, err := store.GetDomain(ctx, db, sc, domainID)
	if err != nil {
		return false, err
	}
	if err := authorize(subj, authz.ActionInventoryWrite, authz.ObjectOf(d)); err != nil {
		return false, err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := store.DeleteDomain(ctx, tx, sc, domainID); err != nil {
			return err
		}
		remaining, err := store.ListDomainsForAlias(ctx, tx, sc, d.AliasID)
		if err != nil {
			return err
		}
		if len(remaining) > 0 {
			return nil
		}
		if err := store.DeleteAlias(ctx, tx, sc, d.AliasID); err != nil {
			return err
		}
		aliasRemoved = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return aliasRemoved, nil
}

// describeLoadBalancer renders "project-slug/loadbalancer-slug" for an error message, falling
// back to the bare id if the load balancer cannot be loaded — an error about a
// conflict must never be replaced by an error about rendering the conflict.
func describeLoadBalancer(ctx context.Context, db *gorm.DB, sc store.Scope, loadBalancerID model.ID) string {
	lb, err := store.GetLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		return fmt.Sprintf("load balancer %d", loadBalancerID)
	}
	project, err := store.GetProject(ctx, db, sc, lb.ProjectID)
	if err != nil {
		return fmt.Sprintf("load balancer %q", lb.Slug)
	}
	return fmt.Sprintf("load balancer %q of project %q", lb.Slug, project.Slug)
}
