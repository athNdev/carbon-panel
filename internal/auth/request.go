// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"net/http"
	"strings"
)

// SessionCookie is the browser cookie that carries a session or API token
// for WebSocket upgrades and file downloads. Those clients cannot set an
// Authorization header. The token must not be placed in the URL (MINE-185).
const SessionCookie = "cp_session"

// SessionTokenFromRequest returns the session or API token from the
// Authorization header, then from SessionCookie. A ?token= query value is
// ignored so it cannot land in access logs, Referer, or browser history.
func SessionTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(header, "Bearer "), "bearer "))
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}
