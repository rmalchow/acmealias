package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// ErrorBody is the JSON every management-API failure produces:
//
//	{"message": "domain \"shop.example.com\" already exists on load balancer \"eu-prod\""}
//
// One field, always present, always a string — the console renders `message`
// and nothing else, so a response that omitted it would surface as an empty
// error box. Handlers that need to say more return a richer struct instead
// (authz.NoRoleBody adds observed_groups); the rule is that whatever goes out
// carries a `message`.
type ErrorBody struct {
	Message string `json:"message"`
}

// handleError is the Echo HTTPErrorHandler for the admin listener. It is the
// one place domain errors become status codes, so a handler can return
// inventory.CreateDomain's error unchanged and get the right code.
//
//	inventory.ErrInvalid (== dnsname.ErrInvalid)  400
//	store.ErrAmountNotOffered                      400
//	store.ErrAccountSuspended                      402
//	inventory.ErrConflict                          409
//	inventory.ErrNotEmpty                          409
//	inventory.ErrNoRoom                            409
//	store.ErrPersonalAccountFixed                  409
//	store.ErrSecondPersonalAccount                 409
//	store.ErrPersonalAccountHasNoRoster            409
//	gorm.ErrRecordNotFound                         404
//	*echo.HTTPError                                its own code
//	anything else                                  500, detail to the log only
func (a *admin) handleError(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	status, body := a.mapError(err, c)

	if c.Request().Method == http.MethodHead {
		_ = c.NoContent(status)
		return
	}
	if err := c.JSON(status, body); err != nil {
		a.log().ErrorContext(c.Request().Context(), "error response could not be written", slog.Any("error", err))
	}
}

func (a *admin) mapError(err error, c echo.Context) (int, any) {
	var he *echo.HTTPError
	if errors.As(err, &he) {
		// A structured message (authz.NoRoleBody) goes out as it is; a string
		// becomes the standard shape. Echo's own errors carry strings.
		if msg, ok := he.Message.(string); ok {
			return he.Code, ErrorBody{Message: msg}
		}
		if he.Message == nil {
			return he.Code, ErrorBody{Message: http.StatusText(he.Code)}
		}
		return he.Code, he.Message
	}

	switch {
	case errors.Is(err, inventory.ErrInvalid):
		return http.StatusBadRequest, ErrorBody{Message: err.Error()}
	case errors.Is(err, authz.ErrInvalidGrant):
		return http.StatusBadRequest, ErrorBody{Message: err.Error()}
	case errors.Is(err, authz.ErrNotPermitted):
		// 403 and not 404: the caller may see the object, they simply may not
		// confer authority they do not hold. Hiding that as "not found" would
		// leave them retrying a request that will never work.
		return http.StatusForbidden, ErrorBody{Message: err.Error()}
	case errors.Is(err, store.ErrUnrestrictedRole), errors.Is(err, store.ErrLastUnrestrictedMember):
		return http.StatusConflict, ErrorBody{Message: err.Error()}
	case errors.Is(err, store.ErrAmountNotOffered):
		// 400: the request named something this service does not sell. It is a
		// malformed request rather than a refused one — there is no state in
		// which 1000000 becomes an amount (design C8).
		return http.StatusBadRequest, ErrorBody{Message: err.Error()}
	case errors.Is(err, store.ErrAccountSuspended):
		// 402 and not 403, everywhere, for the reason design C7 gives about the
		// challenge API and which holds just as well here: nothing is wrong with
		// the caller's authority. A 403 sends somebody to look at their
		// permissions, or at their API key, and both are working perfectly.
		return http.StatusPaymentRequired, ErrorBody{Message: err.Error()}
	case errors.Is(err, inventory.ErrNoRoom):
		// 409 and not 403 (design C13). A quota and a permission are different
		// questions with different remedies — "there is no room, delete one or
		// move to a team account" versus "ask whoever can grant it" — and one
		// status for both produces a message that is wrong for whichever case
		// it was not written for.
		return http.StatusConflict, ErrorBody{Message: err.Error()}
	case errors.Is(err, store.ErrPersonalAccountHasNoRoster):
		// 409: the account is the wrong SHAPE for the request, which is the same
		// sentence as the two below and deliberately not a 403. There is nobody
		// to ask for the permission a 403 would send the caller looking for
		// (design C9, C11). It is also not a 404: the route exists, and a client
		// that got one would reasonably conclude it was talking to an older
		// service.
		return http.StatusConflict, ErrorBody{Message: err.Error()}
	case errors.Is(err, store.ErrPersonalAccountFixed), errors.Is(err, store.ErrSecondPersonalAccount):
		// 409 and not 403: nobody may do this, so there is no grant to go
		// looking for. The account is the wrong shape for what was asked
		// (design C1, C2) — the same distinction C13 draws between a quota and
		// a permission.
		return http.StatusConflict, ErrorBody{Message: err.Error()}
	case errors.Is(err, inventory.ErrConflict), errors.Is(err, inventory.ErrNotEmpty):
		return http.StatusConflict, ErrorBody{Message: err.Error()}
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound, ErrorBody{Message: err.Error()}
	}

	// Everything unrecognized is a bug or an outage. The caller gets a fixed
	// sentence; the driver message, the query and the path go to the log,
	// where they are useful and where they are not a disclosure.
	a.log().ErrorContext(c.Request().Context(), "management request failed",
		slog.String("method", c.Request().Method),
		slog.String("path", c.Request().URL.Path),
		slog.Any("error", err))
	return http.StatusInternalServerError, ErrorBody{Message: "internal error"}
}
