package auth

import "errors"

// Typed errors returned by Clerk token verification. Callers map these to
// HTTP status codes: every ErrToken* except ErrJWKSUnavailable is a 401;
// ErrJWKSUnavailable is a 503 (our dependency is down, not the caller's fault).
var (
	// ErrTokenMalformed covers empty tokens, bad segments, undecodable
	// headers/claims, and a missing subject.
	ErrTokenMalformed = errors.New("auth: token is malformed")
	// ErrTokenExpired covers expired tokens and tokens used before nbf
	// (both enforced with a small leeway).
	ErrTokenExpired = errors.New("auth: token is expired or not yet valid")
	// ErrTokenSignature covers unknown algorithms (including alg:none),
	// unknown kids, and signatures that do not verify.
	ErrTokenSignature = errors.New("auth: token signature is invalid")
	// ErrTokenIssuer covers an iss claim that does not exactly equal the
	// configured issuer.
	ErrTokenIssuer = errors.New("auth: token issuer is not trusted")
	// ErrTokenAudience covers an azp/aud claim rejected by the
	// AuthorizedParties/Audience checks.
	ErrTokenAudience = errors.New("auth: token audience is not allowed")
	// ErrJWKSUnavailable covers JWKS fetch failures (network errors,
	// non-200 responses, undecodable key sets). It is never a silent allow.
	ErrJWKSUnavailable = errors.New("auth: JWKS fetch failed")
)

// Typed errors returned by webhook verification.
var (
	// ErrWebhookHeaders covers missing svix-* headers and unparseable bodies.
	ErrWebhookHeaders = errors.New("auth: webhook headers or body are invalid")
	// ErrWebhookSignature covers a signature that matches none of the
	// provided v1 signatures.
	ErrWebhookSignature = errors.New("auth: webhook signature mismatch")
	// ErrWebhookTimestamp covers timestamps outside the tolerance window
	// (replay protection).
	ErrWebhookTimestamp = errors.New("auth: webhook timestamp outside tolerance")
	// ErrWebhookSecret covers a secret that cannot be base64-decoded.
	ErrWebhookSecret = errors.New("auth: webhook secret is invalid")
)
