package httpapi

import "errors"

// errAuthFailed is the only authentication failure message: it carries no
// mechanism, key or token detail.
var errAuthFailed = errors.New("authentication failed")
