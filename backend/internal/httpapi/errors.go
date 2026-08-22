package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
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
//	inventory.ErrConflict                          409
//	inventory.ErrNotEmpty                          409
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
