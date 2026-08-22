package oidcauth

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// CSRFCookieName is the cookie Angular's HttpClient looks for.
const CSRFCookieName = "XSRF-TOKEN"

// CSRFHeaderName is the header Angular's HttpClient echoes it back in.
const CSRFHeaderName = "X-XSRF-TOKEN"

// CSRF returns the cross-site request forgery middleware for the management
// API. Cookie authentication needs it: a form on another site can make the
// browser send a request carrying the session cookie, and only a token the
// attacker's page cannot read distinguishes that from the console.
//
// Both names differ from Echo's defaults (`_csrf` and `header:X-CSRF-Token`)
// on purpose. Angular's HttpClient reads `XSRF-TOKEN` and sends
// `X-XSRF-TOKEN` natively, so matching its convention makes the client side
// configuration rather than code — and getting either name wrong produces a
// console where nothing can be saved.
//
// Mount it on the state-changing management routes only. Not on the ACME
// challenge API: that is API-key authenticated, has no cookies and no
// browser, so there is nothing for CSRF to protect and the middleware would
// simply break every client.
//
// Echo validates the token on everything except GET, HEAD, OPTIONS and TRACE,
// so "state-changing" needs no configuration here.
func (s *Service) CSRF() echo.MiddlewareFunc {
	csrf := middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "header:" + CSRFHeaderName,
		CookieName:  CSRFCookieName,
		CookiePath:  "/",

		// This is the one cookie in the service that is deliberately
		// readable by JavaScript. It looks like a mistake next to the
		// session cookie in session.go, which is HttpOnly for exactly the
		// opposite reason — but the whole mechanism depends on the page
		// reading this value back out to put it in the header, and the
		// value is worthless to anyone who cannot also send the session
		// cookie. HttpOnly here would break the console, not harden it.
		CookieHTTPOnly: false,

		CookieSecure:   true,
		CookieSameSite: http.SameSiteLaxMode,
		CookieMaxAge:   int(s.cfg.SessionMaxLifetime.Seconds()),
	})

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		wrapped := csrf(next)
		return func(c echo.Context) error {
			// Echo 4.15 treats a `Sec-Fetch-Site: same-origin` (or `none`)
			// request as proven safe and skips the token entirely. That is
			// sound, but the spec requires the token on every state-changing
			// management request without exception, and Angular always sends
			// it, so the fast path would only create a second, weaker way in.
			// Clearing the header puts every request on the token path.
			//
			// ponytail: a header delete is a blunt way to turn off library
			// behaviour, and it means handlers cannot read Sec-Fetch-Site —
			// none do. Upgrade path: when Echo grows a config knob for this,
			// set it and delete these two lines.
			c.Request().Header.Del(echo.HeaderSecFetchSite)
			return wrapped(c)
		}
	}
}
