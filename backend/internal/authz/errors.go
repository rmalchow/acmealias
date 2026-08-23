package authz

import "errors"

// ErrInvalidGrant classifies every rejection of a grant that cannot mean
// anything. It is deliberately the same shape as inventory.ErrInvalid so one
// errors.Is in the HTTP error handler covers both.
var ErrInvalidGrant = errors.New("authz: invalid grant")

// ErrNotPermitted is the refusal of an operation the subject holds no grant for.
var ErrNotPermitted = errors.New("authz: not permitted")
