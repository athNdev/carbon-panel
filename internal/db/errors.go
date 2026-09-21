package db

import "errors"

// ErrScheduledTaskNotFound is returned when a scheduled task ID does not exist.
// It lets the RPC layer map a missing task to connect.CodeNotFound instead of
// an internal error.
var ErrScheduledTaskNotFound = errors.New("scheduled task not found")
