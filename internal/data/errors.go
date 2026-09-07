package data

import "errors"

// ErrRecordNotFound is returned whenever a query matches no rows. Handlers
// translate it into an appropriate HTTP response.
var ErrRecordNotFound = errors.New("no record found")
