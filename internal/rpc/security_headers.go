// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package rpc

import (
	"net/http"
	"strings"
)

// contentSecurityPolicy is the default CSP for panel responses.
//
// The SPA needs inline scripts (SvelteKit hydration payload) and inline
// style attributes (progress bars, xterm), so script-src/style-src carry
// 'unsafe-inline'. Everything else is locked to same-origin: no plugins,
// no framing by foreign origins, no cross-origin form posts. connect-src
// covers the console WebSocket on the same origin (ws:/wss:).
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' ws: wss:; " +
	"frame-ancestors 'self'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"object-src 'none'"

// securityHeaders wraps h with the panel's default HTTP response headers
// (MINE-163, research P2-14):
//
//   - Content-Security-Policy (see above for the SPA rationale)
//   - X-Frame-Options: SAMEORIGIN (belt-and-braces next to
//     frame-ancestors for older clients)
//   - X-Content-Type-Options: nosniff
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - Strict-Transport-Security on TLS requests only (plaintext
//     responses must not carry it)
//
// Headers are set (not added) so inner handlers cannot stack duplicates,
// and the middleware never touches upgrade bodies — it only stamps the
// response header map before delegating.
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Frame-Options", "SAMEORIGIN")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		h.ServeHTTP(w, r)
	})
}
