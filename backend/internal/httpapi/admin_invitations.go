package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/invitation"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/mail"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// The two halves of this file sit on opposite sides of one line, and the line is
// account SELECTION (design V5).
//
// Issuing, revoking and re-sending are things an account does, so they are
// account-scoped and require membership of the unrestricted role — the owner and
// the administrators, which are the same set of people.
//
// Resolving, accepting and declining are things a person does BEFORE they belong
// to anything. They are addressed by token rather than by account, outside the
// account-scoped tree, and reachable with no selection at all. Putting them under
// /accounts/:accountId would require the caller to name an account the selection
// rule would then refuse, since they are not a member of it yet — a closed loop
// with no way in.

// invitationView is what the roster shows. It carries no token and no hash:
// nothing that leaves this service can be used to accept an invitation.
type invitationView struct {
	ID    model.ID `json:"id"`
	Email string   `json:"email"`
	// InvitedBy is the display name of whoever issued it, so a member reading
	// the roster can ask the right person about a name they do not recognise.
	InvitedBy  string    `json:"invitedBy"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSentAt time.Time `json:"lastSentAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// offerView is what the invited person is shown before answering. It names the
// account and nothing else about it.
//
// There is no role here, and its absence is the design rather than an omission:
// an invitation offers membership and names no role (V6), so the question is
// "Join cloudpowered?" and the answer to "as what" is "we will tell you when
// somebody decides".
type offerView struct {
	Account string `json:"account"`
	// AccountID lets the console select the account after acceptance without a
	// second round trip. It discloses nothing: the holder of the token is about
	// to be a member of it or is not, and either way an identifier is not a key.
	AccountID model.ID  `json:"accountId"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func viewInvitation(inv model.Invitation, inviter string) invitationView {
	return invitationView{
		ID: inv.ID, Email: inv.Email, InvitedBy: inviter,
		CreatedAt: inv.CreatedAt, LastSentAt: inv.LastSentAt, ExpiresAt: inv.ExpiresAt,
	}
}

// listInvitations is readable by every member of the account without a grant,
// exactly like the member list beside it.
//
// Knowing who has been asked into the room is not a privilege. A pending
// invitation nobody can see is how an account acquires a member nobody remembers
// inviting, which is the failure this listing exists to make impossible.
func (a *admin) listInvitations(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "listing invitations"); err != nil {
		return err
	}
	ctx := c.Request().Context()
	invs, err := store.OutstandingInvitations(ctx, a.db(), sc, time.Now().UTC())
	if err != nil {
		return err
	}
	names := a.inviterNames(ctx, invs)
	out := make([]invitationView, 0, len(invs))
	for _, inv := range invs {
		out = append(out, viewInvitation(inv, names[inv.InvitedBy]))
	}
	return c.JSON(http.StatusOK, out)
}

// inviterNames resolves the issuing users to display names in one query.
//
// A failure here is deliberately not fatal: the roster is more useful with a
// blank inviter column than it is with an error page, and the identifier is
// still in the audit trail.
func (a *admin) inviterNames(ctx context.Context, invs []model.Invitation) map[model.ID]string {
	out := map[model.ID]string{}
	if len(invs) == 0 {
		return out
	}
	ids := make([]model.ID, 0, len(invs))
	for _, i := range invs {
		ids = append(ids, i.InvitedBy)
	}
	var users []model.User
	// users carries no row-level security — a person is global and exists before
	// belonging to any account — so this needs no scope and gets none.
	if err := a.db().WithContext(ctx).Find(&users, "id in ?", ids).Error; err != nil {
		a.log().ErrorContext(ctx, "inviter names not resolved", slog.Any("error", err))
		return out
	}
	for _, u := range users {
		out[u.ID] = u.DisplayName
	}
	return out
}

// createInvitation issues one and sends it.
//
// The RESPONSE IS IDENTICAL whether or not the address already has a user, and
// that is a requirement rather than a coincidence: a different status, a
// different body or a materially different latency would make this endpoint an
// oracle telling any account administrator whether a given address has an
// account with us. Nothing below branches on it, and nothing below looks the
// address up in users for any purpose other than the already-a-member check,
// which is about THIS account and is therefore already visible to the caller.
func (a *admin) createInvitation(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "inviting somebody"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "inviting somebody into the account"); err != nil {
		return err
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	inviter, err := a.currentUser(c)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	// The rate limits run BEFORE the insert and outside the audit transaction,
	// because a refusal is not a change: it must leave no invitation behind, and
	// the entry recording it is written on its own below.
	if err := store.CheckInvitationRate(ctx, a.db(), sc, body.Email, a.invitationLimits(), now); err != nil {
		a.recordRateRefusal(c, sc, body.Email, err)
		return err
	}

	var issued *model.Invitation
	var token string
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		inv, tok, err := store.IssueInvitation(ctx, tx, sc, body.Email, inviter.ID, now)
		if err != nil {
			return nil, err
		}
		issued, token = inv, tok
		return []authz.Event{{
			Action: authz.ActionInvitationCreate, ObjectType: authz.ObjectInvitation,
			ObjectID: &inv.ID, ObjectLabel: inv.Email,
			// The invited address and nothing else. There is no role to record —
			// the invitation offers none — and the token, in any form, must
			// never appear here: an audit trail that contained a usable
			// credential would be a credential store with a retention policy.
			After: map[string]any{"email": inv.Email, "expiresAt": inv.ExpiresAt},
		}}, nil
	}); err != nil {
		return err
	}

	// Sent AFTER the transaction commits, deliberately.
	//
	// The other order — send, then write — risks a message pointing at an
	// invitation that does not exist, which is unrecoverable for the recipient
	// and looks like our link being broken. This order risks an invitation
	// nobody was told about, which the account can see in its pending list and
	// fix with the re-send button that exists for exactly this.
	//
	// A send failure is reported, so the person who clicked knows the message
	// did not go. It does NOT roll the invitation back: the offer stands, and
	// removing it would take away the row that makes re-sending possible.
	a.sendInvitation(c, sc, issued, token)
	return c.JSON(http.StatusCreated, viewInvitation(*issued, inviter.DisplayName))
}

func (a *admin) revokeInvitation(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "revoking an invitation"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "revoking an invitation"); err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		inv, err := store.RevokeInvitation(c.Request().Context(), tx, sc, id, now)
		if err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionInvitationRevoke, ObjectType: authz.ObjectInvitation,
			ObjectID: &inv.ID, ObjectLabel: inv.Email,
			Before: map[string]any{"email": inv.Email},
		}}, nil
	}); err != nil {
		return err
	}
	// No message on revocation. Telling somebody an offer they may never have
	// seen has been withdrawn is noise at best, and at worst it is the first
	// they hear of an invitation that was issued in error.
	return c.NoContent(http.StatusNoContent)
}

// resendInvitation issues a new token and kills the previous one.
//
// Killing the previous one is the point. Re-sending is what an administrator
// does when told "I never got it", and the message that was never got is exactly
// the one that might have gone somewhere unintended.
func (a *admin) resendInvitation(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "re-sending an invitation"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "re-sending an invitation"); err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	now := time.Now().UTC()

	existing, err := store.GetInvitation(ctx, a.db(), sc, id)
	if err != nil {
		return err
	}
	if err := store.CheckResendRate(existing, a.invitationLimits(), now); err != nil {
		a.recordRateRefusal(c, sc, existing.Email, err)
		return err
	}

	var issued *model.Invitation
	var token string
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		inv, tok, err := store.ResendInvitation(ctx, tx, sc, id, now)
		if err != nil {
			return nil, err
		}
		issued, token = inv, tok
		return []authz.Event{{
			Action: authz.ActionInvitationResend, ObjectType: authz.ObjectInvitation,
			ObjectID: &inv.ID, ObjectLabel: inv.Email,
			After: map[string]any{"email": inv.Email, "expiresAt": inv.ExpiresAt},
		}}, nil
	}); err != nil {
		return err
	}
	a.sendInvitation(c, sc, issued, token)
	names := a.inviterNames(ctx, []model.Invitation{*issued})
	return c.JSON(http.StatusOK, viewInvitation(*issued, names[issued.InvitedBy]))
}

// --- the selection-free half -----------------------------------------------

// resolveInvitation renders the offer, and CONSUMES NOTHING.
//
// The token arrives in a POST BODY rather than in the path, which is the one
// thing about this route that looks over-careful and is not. A token in a URL
// lands in the reverse proxy's access log, in the browser's history, and in the
// Referer header of every request the page afterwards makes — and this token is
// enough to see which account somebody was invited to. A body is not logged by
// anything by default.
//
// Requires authentication but NOT a selected account. The invitee may belong to
// nothing at all, which is the whole reason this route exists outside the
// account-scoped tree.
func (a *admin) resolveInvitation(c echo.Context) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	inv, acc, err := store.ResolveInvitation(c.Request().Context(), a.db(), body.Token, time.Now().UTC())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, offerView{
		Account: acc.Name, AccountID: acc.ID,
		// The address is returned in full here and MASKED in the refusal below.
		// The difference is who is reading it: this response goes to somebody
		// who is about to be told they may accept, and the mask exists to stop
		// somebody who may NOT accept from learning an address they were only
		// forwarded.
		Email: inv.Email, ExpiresAt: inv.ExpiresAt,
	})
}

// listMyInvitations is the second way in, and it is not a fallback (design V11).
//
// A flow that depends on a link depends on mail delivery being perfect, and it
// is not: the message lands in spam, a corporate gateway rewrites the URL and
// breaks it, somebody reads it on a phone and signs in on a laptop. So a person
// signed in as the invited address sees their invitations whether or not they
// ever opened a message.
//
// This grants nothing the token route does not. Both require authenticating as
// the invited address, which is the whole of the binding; the token only ever
// addressed the row, and being signed in as the address is STRICTLY STRONGER
// evidence than holding a link sent to it.
func (a *admin) listMyInvitations(c echo.Context) error {
	user, err := a.currentUser(c)
	if err != nil {
		return err
	}
	if user.Email == nil || *user.Email == "" {
		// No verified address means nothing can be bound to this person. An
		// empty list rather than an error: it is the truth, and it renders as
		// "no invitations" rather than as a broken dashboard.
		return c.JSON(http.StatusOK, []offerView{})
	}
	invs, accs, err := store.InvitationsForAddress(c.Request().Context(), a.db(), *user.Email, time.Now().UTC())
	if err != nil {
		return err
	}
	names := map[model.ID]string{}
	for _, acc := range accs {
		names[acc.ID] = acc.Name
	}
	out := make([]offerView, 0, len(invs))
	for _, inv := range invs {
		out = append(out, offerView{
			Account: names[inv.AccountID], AccountID: inv.AccountID,
			Email: inv.Email, ExpiresAt: inv.ExpiresAt,
		})
	}
	return c.JSON(http.StatusOK, out)
}

// acceptInvitation creates the membership, consumes the token, and makes the
// joined account the session's selection.
//
// The selection is not a convenience. Leaving somebody in the empty state
// immediately after they joined something is a dead end, and their next intent
// is obviously to act in what they just joined.
func (a *admin) acceptInvitation(c echo.Context) error {
	return a.answerInvitation(c, true)
}

// declineInvitation records a refusal and consumes the invitation exactly as
// accepting does.
//
// Consuming it is the part somebody would leave out. If a decline left the token
// live, "declined" would be a state a stray click could undo, and the person who
// declined would find themselves a member of an account they had refused.
//
// It exists because ignoring an invitation already expires it, and the two look
// identical to the account while meaning opposite things: never saw it — re-send,
// or telephone — versus saw it and said no, which makes re-sending a nuisance
// aimed at somebody who already answered.
func (a *admin) declineInvitation(c echo.Context) error {
	return a.answerInvitation(c, false)
}

func (a *admin) answerInvitation(c echo.Context, accept bool) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	now := time.Now().UTC()

	inv, acc, err := store.ResolveInvitation(ctx, a.db(), body.Token, now)
	if err != nil {
		return err
	}
	user, err := a.currentUser(c)
	if err != nil {
		return err
	}

	// THE BINDING. Only a user whose VERIFIED address is the invited address may
	// answer, and there is deliberately no way to attach the invited address to
	// the user who is signed in.
	//
	// That missing operation is the whole of it. Holding a link sent to
	// alice@corp.com IS proof of control of alice@corp.com — it is exactly the
	// proof a confirmation mail provides — so an "add this address to my
	// account" button would let anybody who was forwarded the message attach the
	// address to themselves and then accept as its rightful owner. Bound would
	// have become bearer with two extra clicks, and every test asserting "the
	// accepting user's address matches the invitation" would still pass.
	if user.Email == nil || *user.Email != inv.Email {
		return echo.NewHTTPError(http.StatusForbidden, fmt.Sprintf(
			"this invitation was sent to %s. Sign in as that address to accept it — an invitation "+
				"belongs to the address it was sent to, not to whoever holds the link.",
			invitation.Mask(inv.Email)))
	}

	sc := store.ScopeForAccount(acc)
	action := authz.ActionInvitationDecline
	if accept {
		action = authz.ActionInvitationAccept
	}
	if err := a.mutateAs(c, sc, nil, func(tx *gorm.DB) ([]authz.Event, error) {
		if accept {
			if err := store.AcceptInvitation(ctx, tx, acc, inv, user, now); err != nil {
				return nil, err
			}
		} else if err := store.DeclineInvitation(ctx, tx, acc, inv, now); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: action, ObjectType: authz.ObjectInvitation,
			ObjectID: &inv.ID, ObjectLabel: inv.Email,
			// The invited ADDRESS and the acting USER, together. On an
			// acceptance that pair is the evidence that the person who joined is
			// the person who was invited — the binding, written down. On a
			// decline it is what lets an administrator tell a refusal from a
			// message that never arrived, which are the two things fourteen days
			// of silence cannot distinguish and which call for opposite
			// responses.
			After: map[string]any{"email": inv.Email, "user": user.DisplayName, "userId": user.ID},
		}}, nil
	}); err != nil {
		return err
	}

	if !accept {
		return c.NoContent(http.StatusNoContent)
	}
	// The selection change re-evaluates the membership like any other, which is
	// trivially satisfied: it was written moments ago in the transaction above.
	if sess := authz.SessionFromContext(c); sess != nil {
		if err := store.SelectAccount(ctx, a.db(), sess.ID, &acc.ID); err != nil {
			// Not fatal. They ARE a member; the console can select the account
			// itself, and failing the acceptance now would leave a membership
			// written and the caller told it was not.
			a.log().ErrorContext(ctx, "account not selected after accepting an invitation",
				slog.Any("error", err))
		}
	}
	return c.JSON(http.StatusOK, offerView{
		Account: acc.Name, AccountID: acc.ID, Email: inv.Email, ExpiresAt: inv.ExpiresAt,
	})
}

// --- plumbing ---------------------------------------------------------------

func (a *admin) invitationLimits() store.InvitationLimits {
	// Read per request rather than captured once, so a reload takes effect
	// without a restart — the same property the project quota has.
	return store.InvitationLimits{
		PerAccountPerHour: a.opts.Config.InvitationsPerAccountPerHour,
		PerAddressPerDay:  a.opts.Config.InvitationsPerAddressPerDay,
		ResendInterval:    a.opts.Config.InvitationResendInterval,
	}
}

// recordRateRefusal writes the entry for an invitation refused by a limit.
//
// Refusals are recorded because a tenant hitting a mail-sending limit REPEATEDLY
// is the signal that they are doing something worth looking at — and that signal
// exists nowhere else, since by definition nothing was created.
//
// A failure to write it is logged and swallowed: the request is being refused
// either way, and turning a rate refusal into a 500 would hide the real answer.
func (a *admin) recordRateRefusal(c echo.Context, sc store.Scope, email string, cause error) {
	ctx := c.Request().Context()
	err := a.mutateAs(c, sc, nil, func(tx *gorm.DB) ([]authz.Event, error) {
		return []authz.Event{{
			Action: authz.ActionInvitationCreate, ObjectType: authz.ObjectInvitation,
			ObjectLabel: email,
			After:       map[string]any{"email": email, "refused": cause.Error()},
		}}, nil
	})
	if err != nil {
		a.log().ErrorContext(ctx, "rate refusal not recorded", slog.Any("error", err))
	}
}

// sendInvitation puts the message on the wire.
//
// The subject follows the grammar in mail.Message's comment — the noun the
// message concerns FIRST, and no constant prefix identifying us, because a
// prefix spends the most valuable characters in an inbox on what the sender
// column already shows. "Invitation to cloudpowered", not "[acmealias] You have
// been invited".
func (a *admin) sendInvitation(c echo.Context, sc store.Scope, inv *model.Invitation, token string) {
	ctx := c.Request().Context()
	acc, err := store.GetAccount(ctx, a.db(), sc)
	if err != nil {
		a.log().ErrorContext(ctx, "invitation not sent: account unreadable", slog.Any("error", err))
		return
	}
	link := a.invitationLink(token)
	m := mail.Message{
		To:      inv.Email,
		Subject: "Invitation to " + acc.Name,
		Body: fmt.Sprintf(`You have been invited to join %s.

Open this link to accept or decline:

  %s

The invitation expires on %s. If you were not expecting it, you can ignore this
message — nothing happens until you accept, and the invitation lapses on its own.

You will be able to accept only while signed in as %s. An invitation belongs to
the address it was sent to, so forwarding this message gives nobody access.`,
			acc.Name, link, inv.ExpiresAt.Format("2 January 2006"), inv.Email),
	}
	if err := a.opts.Mail.Send(ctx, m); err != nil {
		// Logged with the recipient and WITHOUT the token or the link. A log
		// line containing the link would be a usable credential sitting in
		// whatever collects logs, which is the one place a credential is most
		// likely to be read by somebody it was not meant for.
		a.log().ErrorContext(ctx, "invitation message not delivered",
			slog.String("to", inv.Email), slog.Any("error", err))
	}
}

// invitationLink builds the URL that goes in the message.
//
// The console's origin is derived from the OIDC redirect URL rather than
// configured separately, and that is not laziness: the redirect URL is where the
// identity provider sends the browser after a sign-in, so its origin IS the
// console by definition. A second setting for the same fact would be a second
// thing to get wrong, and the failure of getting it wrong is invitation links
// that go somewhere else — discovered by a customer rather than by us.
//
// The token is a QUERY PARAMETER because the console has to read it out of a
// URL somebody clicked in a mail client; there is nowhere else to put it. The
// console drops it from the address bar immediately and sends it to the API in a
// request body, so it does not survive into the browser's history or into any
// Referer header.
func (a *admin) invitationLink(token string) string {
	base := "/invitations/accept"
	if u, err := url.Parse(a.opts.Config.OIDCRedirectURL); err == nil && u.Scheme != "" && u.Host != "" {
		base = u.Scheme + "://" + u.Host + base
	}
	return base + "?token=" + url.QueryEscape(token)
}

// assignMemberRole gives a role to a member who holds none.
//
// This is the second half of what taking the role off the invitation created
// (design V6). Somebody accepted, joined, and can do nothing; this is where an
// administrator says what they may do.
//
// It is a distinct action from adding a member, and recorded as one, because it
// is a distinct event: claiming somebody joined at this moment would contradict
// both the roster and the invitation's own acceptance entry.
func (a *admin) assignMemberRole(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "assigning a role"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "changing membership"); err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		RoleID model.ID `json:"roleId"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.RoleID == model.NilID {
		return fmt.Errorf("%w: roleId is required", inventory.ErrInvalid)
	}
	ctx := c.Request().Context()
	return a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.AssignRole(ctx, tx, sc, id, body.RoleID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Either there is no such membership, or it already holds a
				// role. Same answer for both: assigning is only ever the
				// completion of an invitation, and re-pointing an existing
				// membership at a different role is a remove and an add.
				return nil, fmt.Errorf("%w: no membership waiting for a role", inventory.ErrConflict)
			}
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionMemberAssignRole, ObjectType: authz.ObjectMember,
			ObjectID: &id, ObjectLabel: id.String(),
			After: map[string]any{"roleId": body.RoleID},
		}}, nil
	})
}
