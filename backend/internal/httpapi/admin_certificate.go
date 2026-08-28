package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/certobserve"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// What the certificate observer saw, read back (change domain-diagnostics,
// tasks 5.2 and 5.3).
//
// Both routes are pure reads of rows internal/certobserve already wrote. Neither
// dials anything: a screen open must not be a reason to connect to a customer's
// machine, and the cadence at which we do that is the worker's business and the
// configuration's, not a page's.
//
// NOTHING HERE COMPUTES A BAND. The vocabulary and the thresholds live in
// internal/certobserve and are put on the wire verbatim, so that a console and
// an adminctl cannot disagree about the same certificate because one of them was
// updated and the other was not.

// certificateState is one domain's current TLS position: the dot, and the rows
// that explain it.
//
// ServerName is at the top because it is the question every address below was
// asked — the wildcard's test hostname where there is one, the domain's own name
// otherwise. Without it a customer looking at "uncovered" cannot tell which name
// the certificate failed to cover.
type certificateState struct {
	ServerName string            `json:"serverName"`
	Band       certobserve.Band  `json:"band"`
	Addresses  []observationView `json:"addresses"`
}

// observationView is one (node, family) pair's most recent observation, or one
// row of the history.
//
// NodeLabel rides along because an address on its own is not what somebody named
// their machine, and a dialog that showed only addresses would make the customer
// do the join themselves. Everything from NotAfter down is null unless Outcome is
// "observed" — a handshake we could not complete has no certificate to describe,
// and zero values would read as a certificate that expired in year zero.
type observationView struct {
	NodeID           model.ID         `json:"nodeId"`
	NodeLabel        string           `json:"nodeLabel"`
	Family           string           `json:"family"`
	Address          string           `json:"address"`
	ObservedAt       time.Time        `json:"observedAt"`
	Outcome          string           `json:"outcome"`
	Band             certobserve.Band `json:"band"`
	NotAfter         *time.Time       `json:"notAfter"`
	Issuer           *string          `json:"issuer"`
	CoversServerName *bool            `json:"coversServerName"`
	ChainTrusted     *bool            `json:"chainTrusted"`
}

// getDomainCertificate answers the dot: the latest observation per address, and
// the worst of them folded into one band.
//
// Available on every account, personal included, because this is what colours
// the dot and a feature whose indicator is missing is a feature nobody knows
// they have (G15). Authorized with inventory.view, which is what already gates
// seeing the domain at all — observing is configured through inventory.write,
// but reading back what was observed is reading the inventory.
func (a *admin) getDomainCertificate(c echo.Context) error {
	d, err := a.observedDomain(c)
	if err != nil {
		return err
	}
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	obs, err := store.LatestObservations(c.Request().Context(), a.db(), sc, d.ID)
	if err != nil {
		return err
	}
	// Both halves from one call at one instant: banding the rows and then asking
	// for the dot separately could publish a dot no row in its own list justifies.
	bands, dot := certobserve.State(obs, time.Now().UTC())
	labels, err := a.nodeLabels(c, d.ProjectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, certificateState{
		ServerName: observedName(*d),
		Band:       dot,
		Addresses:  observationViews(obs, bands, labels),
	})
}

// getDomainCertificateHistory answers the list under the dot: every observation
// this domain has, newest first, capped by the store.
//
// REFUSED ON A PERSONAL ACCOUNT, and refused here rather than by a console
// hiding a section it was sent (G15). History is recorded for everybody — a
// feature that arrives empty for weeks after an upgrade is not a feature — and
// shown to team accounts.
func (a *admin) getDomainCertificateHistory(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccountFeature(c, sc, "the certificate observation history"); err != nil {
		return err
	}
	d, err := a.observedDomain(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	// No limit parameter. The store's own ceiling is the answer to "how much
	// history" — retention downsamples anything older than a couple of weeks to
	// one row per address per day, so the whole of it fits a dialog, and a pager
	// on a list nobody reaches the bottom of is a control with no user.
	obs, err := store.ObservationHistory(ctx, a.db(), sc, d.ID, store.MaxObservationHistory)
	if err != nil {
		return err
	}
	bands, _ := certobserve.State(obs, time.Now().UTC())
	labels, err := a.nodeLabels(c, d.ProjectID)
	if err != nil {
		return err
	}
	// No dot here. The dot is the LATEST observation per address, and a history
	// list folded into one band would answer a different question with the same
	// word — the caller that wants the dot asks for the state.
	return c.JSON(http.StatusOK, observationViews(obs, bands, labels))
}

// observedDomain loads the domain both reads are about and checks the caller may
// see it.
//
// The check is here rather than in internal/inventory because these two routes
// have no service-layer operation to hang it on — they read the store directly,
// as the reporting reads do. store.GetDomain is scoped, so a domain in another
// account is already a not-found before the permission is consulted; this is the
// second half, which is about a member's grants inside their own account.
func (a *admin) observedDomain(c echo.Context) (*model.Domain, error) {
	sc, err := a.scope(c)
	if err != nil {
		return nil, err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return nil, err
	}
	d, err := store.GetDomain(c.Request().Context(), a.db(), sc, id)
	if err != nil {
		return nil, err
	}
	if err := a.authorize(c, authz.ActionViewInventory, authz.ObjectOf(d)); err != nil {
		return nil, err
	}
	return d, nil
}

// observedName is the name we ask a node to present a certificate for: a
// wildcard's test hostname, or the domain's own name.
//
// A wildcard cannot be an SNI value — `*.example.com` is not a name any client
// asks for — so a wildcard with observation on always has a hostname, which the
// store's own constraint enforces. The fallback is for the far commoner case of
// a specific host, and never a derived guess: `www.<domain>` for a wildcard would
// colour a dot red for a name the customer never deployed.
func observedName(d model.Domain) string {
	if d.TLSTestHost != nil && *d.TLSTestHost != "" {
		return *d.TLSTestHost
	}
	return d.Name
}

// nodeLabels maps a project's node identifiers to what the customer called them.
//
// One query for the whole answer rather than a lookup per observation: a
// dual-stack four-node project is eight rows, and eight point reads to render
// eight labels is the shape of query pattern that is fine until it is not.
func (a *admin) nodeLabels(c echo.Context, projectID model.ID) (map[model.ID]string, error) {
	sc, err := a.scope(c)
	if err != nil {
		return nil, err
	}
	nodes, err := store.ListLBNodesForProject(c.Request().Context(), a.db(), sc, projectID)
	if err != nil {
		return nil, err
	}
	out := make(map[model.ID]string, len(nodes))
	for _, n := range nodes {
		out[n.ID] = n.Label
	}
	return out, nil
}

// observationViews renders rows and their already-computed bands together.
//
// bands is passed in rather than computed here so that there is exactly one
// place that decides what an observation means, and it is not this file.
func observationViews(obs []model.CertificateObservation, bands []certobserve.Band, labels map[model.ID]string) []observationView {
	out := make([]observationView, 0, len(obs))
	for i, o := range obs {
		out = append(out, observationView{
			NodeID: o.NodeID, NodeLabel: labels[o.NodeID],
			Family: o.Family, Address: o.Address,
			ObservedAt: o.ObservedAt, Outcome: o.Outcome, Band: bands[i],
			NotAfter: o.NotAfter, Issuer: o.Issuer,
			CoversServerName: o.CoversServerName, ChainTrusted: o.ChainTrusted,
		})
	}
	return out
}

// errTeamAccountFeature is the refusal to serve something a personal account is
// not sold, as opposed to something it cannot structurally have.
//
// It is deliberately NOT store.ErrPersonalAccountHasNoRoster. That one means "an
// account of one cannot have members", which is a statement about shape, and its
// message says so; borrowing it here would tell a customer asking about a
// certificate that their account has no roles. Nothing is meaningless about
// history on a personal account — it is withheld, for a commercial reason, and
// the refusal has to say that rather than dress it up as a structural limit
// (G15).
//
// The message is the load-bearing part, not the status: see handleError's arm
// for 418. So it states the reason and the remedy in one sentence a console can
// render unedited.
var errTeamAccountFeature = errors.New(
	"this is part of a team account; a personal account keeps the current state and not the history")

// requireTeamAccountFeature refuses a COMMERCIALLY gated read in a personal
// account.
//
// Sibling of requireTeamAccount, and separate from it on purpose. That one is
// about people and credentials — a roster that cannot exist — and answers 409;
// this one is about what a plan includes, and answers 418. See errTeamAccountFeature.
func (a *admin) requireTeamAccountFeature(c echo.Context, sc store.Scope, what string) error {
	acc, err := store.GetAccount(c.Request().Context(), a.opts.DB, sc)
	if err != nil {
		return err
	}
	if acc.Kind == model.AccountKindTeam {
		return nil
	}
	return fmt.Errorf("%w: %s", errTeamAccountFeature, what)
}
