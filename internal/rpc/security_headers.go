package rpc

import (
	"net/http"
)

// contentSecurityPolicy is the default policy applied to every panel HTTP
// response (MINE-163). It is deliberately permissive where SvelteKit needs
// it to be — inline scripts/styles from the compiled bundle — while still
// shutting the risky doors:
//
//   - object-src 'none' blocks plugins (Flash-era vectors, pumice for drive-by
//     downloads served from the panel origin).
//   - frame-ancestors 'self' blocks clickjacking from foreign origins while
//     still allowing the API docs page to iframe the same-origin spec viewer.
//   - connect-src covers the Connect-RPC API ('self') and live console
//     sockets (ws:/wss:).
//   - img-src allows data: avatars plus https: player faces (Minotar/Crafatar).
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self' ws: wss:; " +
	"img-src 'self' data: https:; " +
	"font-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

// securityHeaders wraps the panel mux and stamps a default hardening header
// set on every response: API, health/openapi probes, the WS upgrade path,
// and the embedded SPA. HSTS is intentionally absent: the panel usually sits
// behind a reverse proxy that owns TLS, and emitting HSTS over cleartext
// would be both wrong and a footgun.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		// SAMEORIGIN (not DENY): /docs/api embeds the same-origin spec viewer.
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
