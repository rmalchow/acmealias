package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// Paging and free-text filtering for the console's list screens.
//
// The list endpoints answer with a plain JSON array and put the size of the
// FULL result set — the count before limit and offset, after every filter — in
// the `X-Total-Count` response header. The alternative, wrapping the array in
// a `{items, total}` envelope, was rejected: `curl .../projects | jq '.[]'`
// keeps working, adminctl and the acceptance harness keep parsing what they
// always parsed, and a client that does not care about paging (every one of
// them except the console) needs no change at all. The console reads the
// header with `observe: 'response'`.
//
// There is no cursor. These are inventory tables an operator pages through by
// hand, ordered by a stable key, and offset paging is what a page-number
// control needs. A feed that grows at the head while being read — the audit
// trail under load — can shift a row across a page boundary; that is the known
// cost, and it is the right one to pay for a screen whose whole job is "show
// me page 3".

// totalCountHeader is read by the console to size its pager. Not a header the
// browser blocks: same origin, so no CORS exposure list is involved.
const totalCountHeader = "X-Total-Count"

// pageRequest is a validated limit/offset lb.
type pageRequest struct {
	Limit  int
	Offset int
}

// readPage parses `limit` and `offset`. A missing limit means defLimit; a
// limit above maxLimit is refused rather than silently clamped, because a
// client that asked for 10000 rows and got 1000 without being told has no way
// to know its list is short.
func readPage(c echo.Context, defLimit, maxLimit int) (pageRequest, error) {
	p := pageRequest{Limit: defLimit}
	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > maxLimit {
			return p, echo.NewHTTPError(http.StatusBadRequest, "limit must be between 1 and "+strconv.Itoa(maxLimit))
		}
		p.Limit = n
	}
	if v := c.QueryParam("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return p, echo.NewHTTPError(http.StatusBadRequest, "offset must be zero or a positive integer")
		}
		p.Offset = n
	}
	return p, nil
}

// apply adds LIMIT and OFFSET. Call it only after the total has been counted.
func (p pageRequest) apply(q *gorm.DB) *gorm.DB {
	return q.Limit(p.Limit).Offset(p.Offset)
}

// setTotal publishes the pre-paging count.
func setTotal(c echo.Context, n int64) {
	c.Response().Header().Set(totalCountHeader, strconv.FormatInt(n, 10))
}

// readTimeRange parses the `from` and `to` query parameters. Both accept a
// full RFC 3339 timestamp or a bare `YYYY-MM-DD` date, which is what a native
// `<input type="date">` submits. A bare `to` date means the END of that day:
// an operator asking for "to 2026-08-20" means the whole of the 20th, and
// midnight would silently exclude every entry from the day they named.
func readTimeRange(c echo.Context) (from, to *time.Time, err error) {
	if from, err = parseBound(c.QueryParam("from"), false); err != nil {
		return nil, nil, err
	}
	if to, err = parseBound(c.QueryParam("to"), true); err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && to.Before(*from) {
		return nil, nil, echo.NewHTTPError(http.StatusBadRequest, "to must not be earlier than from")
	}
	return from, to, nil
}

func parseBound(v string, endOfDay bool) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t, nil
	}
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "expected an RFC 3339 timestamp or a YYYY-MM-DD date, got "+v)
	}
	if endOfDay {
		d = d.Add(24*time.Hour - time.Nanosecond)
	}
	return &d, nil
}
