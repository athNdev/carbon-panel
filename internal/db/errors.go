// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import "errors"

// ErrScheduledTaskNotFound is returned when a scheduled task ID does not exist.
// It lets the RPC layer map a missing task to connect.CodeNotFound instead of
// an internal error.
var ErrScheduledTaskNotFound = errors.New("scheduled task not found")
