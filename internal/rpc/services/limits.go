// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package services

// Bounds applied to client-supplied list limits. A negative or zero limit is
// treated as "use the default" because GORM interprets a negative Limit as
// "no limit", which would let a caller dump an entire table in one response.
const (
	defaultListLimit = 50
	maxListLimit     = 500
)

// clampLimit normalises a client-supplied list limit into [1, maxListLimit].
func clampLimit(requested int) int {
	switch {
	case requested <= 0:
		return defaultListLimit
	case requested > maxListLimit:
		return maxListLimit
	default:
		return requested
	}
}
