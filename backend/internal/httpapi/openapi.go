package httpapi

import (
	_ "embed"
	"net/http"

	"github.com/labstack/echo/v4"
)

// The public API's contract, served publicly (design P15).
//
// One document, for the public surface only. The staff tree is a separate route
// tree with a separate authorization plane precisely so that a staff-only
// operation cannot be one forgotten annotation away from appearing here.
//
// It is hand-written rather than generated. A generator would guarantee the
// document matches the handlers and would guarantee nothing about whether the
// contract is one anyone would want to program against — and the console is the
// first client, so the second property is the one under test. What keeps it
// honest instead is the conformance test: every path the console calls has to
// appear here, or the test fails.
//
//go:embed openapi.yaml
var openAPIDocument []byte

// registerOpenAPI serves the document at a stable, unauthenticated path.
//
// Unauthenticated deliberately: a contract nobody can read without credentials
// is not a published contract, and there is nothing in it that is not already
// discoverable by anyone the API answers.
func registerOpenAPI(e *echo.Echo) {
	e.GET("/api/v1/openapi.yaml", func(c echo.Context) error {
		return c.Blob(http.StatusOK, "application/yaml", openAPIDocument)
	})
}
